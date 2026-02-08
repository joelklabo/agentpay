package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// PaymentProvider handles settlement for a specific protocol.
type PaymentProvider interface {
	// Protocol returns which payment protocol this provider handles.
	Protocol() Protocol

	// Pay settles a payment requirement and returns the proof header name, value, and transaction ID.
	Pay(ctx context.Context, req *PaymentRequirement) (headerName, headerValue string, err error)

	// EstimateCost returns the estimated cost in USD for a payment requirement.
	EstimateCost(req *PaymentRequirement) (usdCost float64, description string, err error)
}

// Receipt records a completed payment.
type Receipt struct {
	Timestamp   time.Time `json:"timestamp"`
	URL         string    `json:"url"`
	Protocol    string    `json:"protocol"`
	Amount      string    `json:"amount"`
	USDCost     float64   `json:"usd_cost"`
	Description string    `json:"description"`
	TxID        string    `json:"tx_id,omitempty"`
}

// Config holds router configuration.
type Config struct {
	// MaxPerRequestUSD is the maximum USD amount allowed per single request.
	MaxPerRequestUSD float64
	// MaxSessionUSD is the total USD budget for the session.
	MaxSessionUSD float64
	// DryRun if true, reports what would be paid without settling.
	DryRun bool
	// Verbose enables detailed logging.
	Verbose bool
}

// Router handles cross-protocol payment routing.
type Router struct {
	config    Config
	providers map[Protocol]PaymentProvider
	client    *http.Client
	wot       *WoTChecker

	mu           sync.Mutex
	sessionSpend float64
	receipts     []Receipt
}

// New creates a new payment router.
func New(cfg Config) *Router {
	return &Router{
		config:    cfg,
		providers: make(map[Protocol]PaymentProvider),
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// RegisterProvider adds a payment provider for a protocol.
func (r *Router) RegisterProvider(p PaymentProvider) {
	r.providers[p.Protocol()] = p
}

// SetWoTChecker enables trust scoring before payments.
func (r *Router) SetWoTChecker(w *WoTChecker) {
	r.wot = w
}

// Fetch sends an HTTP request and handles any 402 payment requirements transparently.
// Returns the final response body and receipt (if payment was made).
func (r *Router) Fetch(ctx context.Context, method, url string, body io.Reader, headers map[string]string) ([]byte, *Receipt, error) {
	// Buffer the request body so we can replay it on 402 retry
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, nil, fmt.Errorf("buffer request body: %w", err)
		}
	}

	bodyReader := func() io.Reader {
		if bodyBytes == nil {
			return nil
		}
		return bytes.NewReader(bodyBytes)
	}

	// Build the initial request
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader())
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// First attempt
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}

	// If not 402, return directly
	if resp.StatusCode != http.StatusPaymentRequired {
		if resp.StatusCode >= 400 {
			return respBody, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}
		return respBody, nil, nil
	}

	// Detect the payment protocol
	payReq, err := DetectProtocol(resp, respBody)
	if err != nil {
		return respBody, nil, fmt.Errorf("detect protocol: %w", err)
	}

	// Find a provider for this protocol
	provider, ok := r.providers[payReq.Protocol]
	if !ok {
		return respBody, nil, &PaymentError{
			Protocol: payReq.Protocol,
			Err:      ErrNoProvider,
		}
	}

	// Estimate cost and check budget
	usdCost, description, err := provider.EstimateCost(payReq)
	if err != nil {
		return respBody, nil, fmt.Errorf("estimate cost: %w", err)
	}

	if err := r.checkBudget(usdCost); err != nil {
		return respBody, nil, err
	}

	// WoT trust check: verify the payment recipient before settling
	if r.wot != nil {
		recipientID := extractRecipient(payReq)
		if recipientID != "" {
			if err := r.wot.CheckTrust(recipientID, usdCost); err != nil {
				return respBody, nil, fmt.Errorf("trust check failed: %w", err)
			}
		}
	}

	if r.config.DryRun {
		receipt := &Receipt{
			Timestamp:   time.Now(),
			URL:         url,
			Protocol:    payReq.Protocol.String(),
			Amount:      description,
			USDCost:     usdCost,
			Description: "DRY RUN — would pay",
		}
		return respBody, receipt, nil
	}

	// Settle the payment
	headerName, headerValue, err := provider.Pay(ctx, payReq)
	if err != nil {
		return respBody, nil, &PaymentError{
			Protocol: payReq.Protocol,
			Amount:   description,
			Err:      err,
		}
	}

	// Retry the request with payment proof (body replayed from buffer)
	retryReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader())
	if err != nil {
		return nil, nil, fmt.Errorf("build retry request: %w", err)
	}
	for k, v := range headers {
		retryReq.Header.Set(k, v)
	}
	retryReq.Header.Set(headerName, headerValue)

	retryResp, err := r.client.Do(retryReq)
	if err != nil {
		return nil, nil, fmt.Errorf("retry request failed: %w", err)
	}
	retryBody, err := io.ReadAll(retryResp.Body)
	retryResp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read retry response: %w", err)
	}

	if retryResp.StatusCode >= 400 {
		return retryBody, nil, fmt.Errorf("retry HTTP %d: %s", retryResp.StatusCode, string(retryBody))
	}

	// Record the payment
	receipt := &Receipt{
		Timestamp:   time.Now(),
		URL:         url,
		Protocol:    payReq.Protocol.String(),
		Amount:      description,
		USDCost:     usdCost,
		Description: fmt.Sprintf("Paid %s via %s", description, payReq.Protocol),
	}
	r.recordPayment(usdCost, receipt)

	return retryBody, receipt, nil
}

func (r *Router) checkBudget(usdCost float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.config.MaxPerRequestUSD > 0 && usdCost > r.config.MaxPerRequestUSD {
		return fmt.Errorf("%w: $%.4f exceeds per-request limit of $%.4f",
			ErrBudgetExceeded, usdCost, r.config.MaxPerRequestUSD)
	}
	if r.config.MaxSessionUSD > 0 && r.sessionSpend+usdCost > r.config.MaxSessionUSD {
		return fmt.Errorf("%w: $%.4f would bring session total to $%.4f (limit $%.4f)",
			ErrBudgetExceeded, usdCost, r.sessionSpend+usdCost, r.config.MaxSessionUSD)
	}
	return nil
}

func (r *Router) recordPayment(usdCost float64, receipt *Receipt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionSpend += usdCost
	r.receipts = append(r.receipts, *receipt)
}

// Receipts returns all payment receipts for this session.
func (r *Router) Receipts() []Receipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Receipt, len(r.receipts))
	copy(out, r.receipts)
	return out
}

// SessionSpend returns the total USD spent this session.
func (r *Router) SessionSpend() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionSpend
}

// extractRecipient returns the payment recipient identifier from a payment requirement.
func extractRecipient(req *PaymentRequirement) string {
	if req.X402Requirement != nil && len(req.X402Requirement.Accepts) > 0 {
		return req.X402Requirement.Accepts[0].PayTo
	}
	return req.L402Hash
}
