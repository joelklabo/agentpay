package router

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Protocol represents a payment protocol type.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolX402            // USDC via EIP-3009 or Solana SPL
	ProtocolL402            // Lightning Network invoice
)

func (p Protocol) String() string {
	switch p {
	case ProtocolX402:
		return "x402"
	case ProtocolL402:
		return "L402"
	default:
		return "unknown"
	}
}

// PaymentRequirement holds the parsed payment requirement from a 402 response.
type PaymentRequirement struct {
	Protocol Protocol
	Raw      string // original header or body content

	// x402 fields
	X402Requirement *X402Requirement

	// L402 fields
	L402Invoice string
	L402Hash    string
}

// X402Requirement represents a parsed x402 payment-required header.
type X402Requirement struct {
	Accepts []X402Accept `json:"accepts"`
}

// X402Accept is a single payment option within x402.
type X402Accept struct {
	Scheme           string `json:"scheme"`
	Network          string `json:"network"`
	MaxAmountRequired string `json:"maxAmountRequired"`
	Resource         string `json:"resource"`
	Description      string `json:"description"`
	MimeType         string `json:"mimeType"`
	PayTo            string `json:"payTo"`
	MaxTimeoutSeconds int    `json:"maxTimeoutSeconds"`
	Asset            string `json:"asset"`
	Extra            json.RawMessage `json:"extra,omitempty"`
}

// DetectProtocol examines an HTTP 402 response and determines the payment protocol.
func DetectProtocol(resp *http.Response, body []byte) (*PaymentRequirement, error) {
	// Check for x402: payment-required header (v2) or x-payment-required (v1)
	paymentHeader := resp.Header.Get("Payment-Required")
	if paymentHeader == "" {
		paymentHeader = resp.Header.Get("X-Payment-Required")
	}
	if paymentHeader != "" {
		return parseX402Header(paymentHeader)
	}

	// Check for L402: WWW-Authenticate header with LSAT or L402 challenge
	authHeader := resp.Header.Get("WWW-Authenticate")
	if strings.HasPrefix(authHeader, "LSAT ") || strings.HasPrefix(authHeader, "L402 ") {
		return parseL402Challenge(authHeader)
	}

	// Try parsing body as JSON for L402-style payment info
	if len(body) > 0 {
		return parseL402Body(body)
	}

	return nil, ErrUnknownProtocol
}

func parseX402Header(header string) (*PaymentRequirement, error) {
	// x402 headers are base64-encoded JSON
	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		// Try raw JSON
		decoded = []byte(header)
	}

	var req X402Requirement
	if err := json.Unmarshal(decoded, &req); err != nil {
		// Try as array directly
		var accepts []X402Accept
		if err2 := json.Unmarshal(decoded, &accepts); err2 != nil {
			return nil, fmt.Errorf("parse x402 header: %w", err)
		}
		req.Accepts = accepts
	}

	return &PaymentRequirement{
		Protocol:        ProtocolX402,
		Raw:             header,
		X402Requirement: &req,
	}, nil
}

func parseL402Challenge(header string) (*PaymentRequirement, error) {
	// Format: LSAT macaroon="...", invoice="..."
	// or: L402 token="...", invoice="..."
	parts := strings.SplitN(header, " ", 2)
	if len(parts) < 2 {
		return nil, ErrMalformedL402
	}

	params := parseHeaderParams(parts[1])
	invoice := params["invoice"]
	if invoice == "" {
		return nil, ErrMissingInvoice
	}

	return &PaymentRequirement{
		Protocol:    ProtocolL402,
		Raw:         header,
		L402Invoice: invoice,
		L402Hash:    params["payment_hash"],
	}, nil
}

func parseL402Body(body []byte) (*PaymentRequirement, error) {
	var data struct {
		Invoice     string `json:"invoice"`
		PaymentHash string `json:"payment_hash"`
		PR          string `json:"pr"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, ErrUnknownProtocol
	}

	invoice := data.Invoice
	if invoice == "" {
		invoice = data.PR
	}
	if invoice == "" {
		return nil, ErrUnknownProtocol
	}

	return &PaymentRequirement{
		Protocol:    ProtocolL402,
		Raw:         string(body),
		L402Invoice: invoice,
		L402Hash:    data.PaymentHash,
	}, nil
}

// parseHeaderParams parses key="value" pairs from a header.
func parseHeaderParams(s string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.TrimSpace(part[idx+1:])
		val = strings.Trim(val, `"`)
		params[key] = val
	}
	return params
}
