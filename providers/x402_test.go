package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joelklabo/agentpay/router"
)

func TestX402Provider_EstimateCost(t *testing.T) {
	p := NewX402Provider("http://localhost", "user", "token")

	tests := []struct {
		name    string
		req     *router.PaymentRequirement
		wantUSD float64
		wantErr bool
	}{
		{
			name: "single option $0.01",
			req: &router.PaymentRequirement{
				Protocol: router.ProtocolX402,
				X402Requirement: &router.X402Requirement{
					Accepts: []router.X402Accept{{
						Network:           "eip155:84532",
						MaxAmountRequired: "10000",
						PayTo:             "0xabc",
					}},
				},
			},
			wantUSD: 0.01,
		},
		{
			name: "picks cheapest of multiple options",
			req: &router.PaymentRequirement{
				Protocol: router.ProtocolX402,
				X402Requirement: &router.X402Requirement{
					Accepts: []router.X402Accept{
						{Network: "eip155:84532", MaxAmountRequired: "50000", PayTo: "0xabc"},
						{Network: "solana:devnet", MaxAmountRequired: "10000", PayTo: "0xdef"},
					},
				},
			},
			wantUSD: 0.01,
		},
		{
			name: "no accepts",
			req: &router.PaymentRequirement{
				Protocol:        router.ProtocolX402,
				X402Requirement: &router.X402Requirement{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usd, _, err := p.EstimateCost(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if usd != tt.wantUSD {
				t.Errorf("got $%.4f, want $%.4f", usd, tt.wantUSD)
			}
		})
	}
}

func TestX402Provider_Pay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("invalid JSON payload: %v", err)
		}
		if payload["preferredChain"] != "auto" {
			t.Errorf("expected preferredChain=auto, got %v", payload["preferredChain"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":          true,
			"paymentSignature": "0xsig_test_abc",
			"usage":            map[string]string{"header": "Payment"},
		})
	}))
	defer srv.Close()

	p := NewX402Provider(srv.URL, "testuser", "test-token")

	req := &router.PaymentRequirement{
		Protocol: router.ProtocolX402,
		Raw:      "base64-encoded-requirement",
		X402Requirement: &router.X402Requirement{
			Accepts: []router.X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "10000",
				PayTo:             "0xabc",
			}},
		},
	}

	headerName, headerValue, err := p.Pay(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if headerName != "Payment" {
		t.Errorf("expected header name 'Payment', got %q", headerName)
	}
	if headerValue != "0xsig_test_abc" {
		t.Errorf("expected sig, got %q", headerValue)
	}
}

func TestX402Provider_PayFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "insufficient funds",
		})
	}))
	defer srv.Close()

	p := NewX402Provider(srv.URL, "testuser", "test-token")
	req := &router.PaymentRequirement{
		Protocol: router.ProtocolX402,
		Raw:      "req",
	}

	_, _, err := p.Pay(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for failed payment")
	}
	if !contains(err.Error(), "insufficient funds") {
		t.Errorf("expected 'insufficient funds' in error, got: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
