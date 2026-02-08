package router

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDetectProtocol_X402Header(t *testing.T) {
	// Create a valid x402 payment-required header
	req := X402Requirement{
		Accepts: []X402Accept{
			{
				Scheme:           "exact",
				Network:          "eip155:84532",
				MaxAmountRequired: "10000", // 0.01 USDC
				PayTo:            "0x1234567890abcdef1234567890abcdef12345678",
				Asset:            "USDC",
			},
		},
	}
	data, _ := json.Marshal(req)
	encoded := base64.StdEncoding.EncodeToString(data)

	resp := &http.Response{
		StatusCode: 402,
		Header:     http.Header{},
	}
	resp.Header.Set("Payment-Required", encoded)

	payReq, err := DetectProtocol(resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payReq.Protocol != ProtocolX402 {
		t.Errorf("expected x402, got %s", payReq.Protocol)
	}
	if payReq.X402Requirement == nil {
		t.Fatal("expected X402Requirement to be set")
	}
	if len(payReq.X402Requirement.Accepts) != 1 {
		t.Errorf("expected 1 accept, got %d", len(payReq.X402Requirement.Accepts))
	}
	if payReq.X402Requirement.Accepts[0].Network != "eip155:84532" {
		t.Errorf("expected network eip155:84532, got %s", payReq.X402Requirement.Accepts[0].Network)
	}
}

func TestDetectProtocol_L402Challenge(t *testing.T) {
	resp := &http.Response{
		StatusCode: 402,
		Header:     http.Header{},
	}
	resp.Header.Set("WWW-Authenticate", `L402 macaroon="abc123", invoice="lnbc100u1..."`)

	payReq, err := DetectProtocol(resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payReq.Protocol != ProtocolL402 {
		t.Errorf("expected L402, got %s", payReq.Protocol)
	}
	if payReq.L402Invoice != "lnbc100u1..." {
		t.Errorf("expected invoice lnbc100u1..., got %s", payReq.L402Invoice)
	}
}

func TestDetectProtocol_L402Body(t *testing.T) {
	body := []byte(`{"invoice":"lnbc50u1pj...","payment_hash":"abc123"}`)
	resp := &http.Response{
		StatusCode: 402,
		Header:     http.Header{},
	}

	payReq, err := DetectProtocol(resp, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payReq.Protocol != ProtocolL402 {
		t.Errorf("expected L402, got %s", payReq.Protocol)
	}
	if payReq.L402Invoice != "lnbc50u1pj..." {
		t.Errorf("expected invoice, got %s", payReq.L402Invoice)
	}
	if payReq.L402Hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", payReq.L402Hash)
	}
}

func TestDetectProtocol_Unknown(t *testing.T) {
	resp := &http.Response{
		StatusCode: 402,
		Header:     http.Header{},
	}

	_, err := DetectProtocol(resp, nil)
	if err != ErrUnknownProtocol {
		t.Errorf("expected ErrUnknownProtocol, got %v", err)
	}
}
