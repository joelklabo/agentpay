package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"

	"github.com/joelklabo/agentpay/router"
)

// X402Provider handles x402 (USDC) payments via AgentWallet.
type X402Provider struct {
	apiBase  string // AgentWallet API base URL
	username string
	token    string
	client   *http.Client
	// PreferredChain: "evm", "solana", or "auto"
	PreferredChain string
}

// NewX402Provider creates a new x402 payment provider backed by AgentWallet.
func NewX402Provider(apiBase, username, token string) *X402Provider {
	return &X402Provider{
		apiBase:        apiBase,
		username:       username,
		token:          token,
		client:         &http.Client{},
		PreferredChain: "auto",
	}
}

func (p *X402Provider) Protocol() router.Protocol {
	return router.ProtocolX402
}

func (p *X402Provider) EstimateCost(req *router.PaymentRequirement) (float64, string, error) {
	if req.X402Requirement == nil || len(req.X402Requirement.Accepts) == 0 {
		return 0, "", fmt.Errorf("no x402 payment options")
	}

	// Find the cheapest option
	var cheapest *router.X402Accept
	var cheapestUSD float64 = math.MaxFloat64

	for i := range req.X402Requirement.Accepts {
		opt := &req.X402Requirement.Accepts[i]
		amount, err := strconv.ParseFloat(opt.MaxAmountRequired, 64)
		if err != nil {
			continue
		}
		// USDC has 6 decimals
		usd := amount / 1e6
		if usd < cheapestUSD {
			cheapestUSD = usd
			cheapest = opt
		}
	}

	if cheapest == nil {
		return 0, "", fmt.Errorf("no parseable payment amounts")
	}

	desc := fmt.Sprintf("$%.4f USDC on %s", cheapestUSD, cheapest.Network)
	return cheapestUSD, desc, nil
}

func (p *X402Provider) Pay(ctx context.Context, req *router.PaymentRequirement) (string, string, error) {
	// Use AgentWallet x402/pay endpoint
	signURL := fmt.Sprintf("%s/api/wallets/%s/actions/x402/pay", p.apiBase, p.username)

	payload := map[string]interface{}{
		"requirement":    req.Raw,
		"preferredChain": p.PreferredChain,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshal sign request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", signURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("build sign request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("sign request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("sign request HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Success          bool   `json:"success"`
		PaymentSignature string `json:"paymentSignature"`
		Usage            struct {
			Header string `json:"header"`
		} `json:"usage"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("parse sign response: %w", err)
	}
	if !result.Success {
		return "", "", fmt.Errorf("sign failed: %s", result.Error)
	}

	// The header name depends on x402 version
	headerName := result.Usage.Header
	if headerName == "" {
		headerName = "Payment-Signature" // v2 default
	}

	return headerName, result.PaymentSignature, nil
}
