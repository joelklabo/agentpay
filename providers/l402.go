package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/joelklabo/agentpay/router"
)

// L402Provider handles L402 (Lightning) payments via LNbits.
type L402Provider struct {
	lnbitsURL string
	adminKey  string
	client    *http.Client
	// SatPriceUSD is the current price of 1 sat in USD (for cost estimation).
	SatPriceUSD float64
}

// NewL402Provider creates a new L402 payment provider backed by LNbits.
func NewL402Provider(lnbitsURL, adminKey string) *L402Provider {
	return &L402Provider{
		lnbitsURL:   strings.TrimRight(lnbitsURL, "/"),
		adminKey:    adminKey,
		client:      &http.Client{},
		SatPriceUSD: 0.00001, // ~$100K/BTC default
	}
}

func (p *L402Provider) Protocol() router.Protocol {
	return router.ProtocolL402
}

func (p *L402Provider) EstimateCost(req *router.PaymentRequirement) (float64, string, error) {
	if req.L402Invoice == "" {
		return 0, "", fmt.Errorf("no Lightning invoice")
	}

	// Decode invoice amount from BOLT11
	sats, err := decodeBolt11Amount(req.L402Invoice)
	if err != nil {
		return 0, "", fmt.Errorf("decode invoice: %w", err)
	}

	usd := float64(sats) * p.SatPriceUSD
	desc := fmt.Sprintf("%d sats ($%.4f)", sats, usd)
	return usd, desc, nil
}

func (p *L402Provider) Pay(ctx context.Context, req *router.PaymentRequirement) (string, string, error) {
	if req.L402Invoice == "" {
		return "", "", fmt.Errorf("no Lightning invoice to pay")
	}

	// Pay the invoice via LNbits
	payURL := fmt.Sprintf("%s/api/v1/payments", p.lnbitsURL)
	payload := fmt.Sprintf(`{"out":true,"bolt11":"%s"}`, req.L402Invoice)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", payURL, strings.NewReader(payload))
	if err != nil {
		return "", "", fmt.Errorf("build pay request: %w", err)
	}
	httpReq.Header.Set("X-Api-Key", p.adminKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("pay request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", "", fmt.Errorf("LNbits pay HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		PaymentHash string `json:"payment_hash"`
		Preimage    string `json:"checking_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("parse pay response: %w", err)
	}

	// Return the preimage/payment_hash as the proof
	// L402 uses the preimage in an Authorization header
	proofValue := fmt.Sprintf("L402 %s:%s", "", result.PaymentHash)
	if req.L402Hash != "" {
		proofValue = fmt.Sprintf("L402 %s:%s", req.L402Hash, result.PaymentHash)
	}

	return "Authorization", proofValue, nil
}

// decodeBolt11Amount extracts the amount in sats from a BOLT11 invoice string.
// BOLT11 format: lnbc<amount><multiplier>1...
func decodeBolt11Amount(invoice string) (int64, error) {
	invoice = strings.ToLower(invoice)
	var prefix string
	for _, p := range []string{"lnbcrt", "lntbs", "lntb", "lnbc"} {
		if strings.HasPrefix(invoice, p) {
			prefix = p
			break
		}
	}
	if prefix == "" {
		return 0, fmt.Errorf("not a valid BOLT11 invoice")
	}

	rest := invoice[len(prefix):]

	// Find the separator '1' that precedes the data part
	sepIdx := strings.LastIndex(rest, "1")
	if sepIdx < 1 {
		return 0, fmt.Errorf("no amount in invoice")
	}
	amountStr := rest[:sepIdx]

	if len(amountStr) == 0 {
		return 0, fmt.Errorf("no amount in invoice")
	}

	// The last character is the multiplier
	multiplier := amountStr[len(amountStr)-1]
	numStr := amountStr[:len(amountStr)-1]

	var num int64
	for _, c := range numStr {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid amount character: %c", c)
		}
		num = num*10 + int64(c-'0')
	}

	// Multipliers: m=milli(0.001), u=micro(0.000001), n=nano(0.000000001), p=pico(0.000000000001)
	// 1 BTC = 100,000,000 sats
	switch multiplier {
	case 'm':
		return num * 100000, nil // milli-BTC to sats
	case 'u':
		return num * 100, nil // micro-BTC to sats
	case 'n':
		// nano-BTC: 1 nano = 0.1 sat, need to handle sub-sat
		return num / 10, nil
	case 'p':
		// pico-BTC: 1 pico = 0.0001 sat
		return num / 10000, nil
	default:
		// No multiplier — amount is in BTC
		if multiplier >= '0' && multiplier <= '9' {
			num = num*10 + int64(multiplier-'0')
			return num * 100000000, nil // BTC to sats
		}
		return 0, fmt.Errorf("unknown multiplier: %c", multiplier)
	}
}
