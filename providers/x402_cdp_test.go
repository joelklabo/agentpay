package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joelklabo/agentpay/router"
)

func TestCDPProvider_Protocol(t *testing.T) {
	p := NewCDPProvider("key-id", "key-secret", "wallet-secret")
	if p.Protocol() != router.ProtocolX402 {
		t.Errorf("expected ProtocolX402, got %v", p.Protocol())
	}
}

func TestCDPProvider_EstimateCost(t *testing.T) {
	p := NewCDPProvider("key-id", "key-secret", "wallet-secret")

	tests := []struct {
		name    string
		req     *router.PaymentRequirement
		wantUSD float64
		wantErr bool
	}{
		{
			name: "single USDC option $0.001",
			req: &router.PaymentRequirement{
				Protocol: router.ProtocolX402,
				X402Requirement: &router.X402Requirement{
					Accepts: []router.X402Accept{{
						Scheme:            "exact",
						Network:           "eip155:84532",
						MaxAmountRequired: "1000",
						PayTo:             "0xpayee",
						Asset:             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
					}},
				},
			},
			wantUSD: 0.001,
		},
		{
			name: "picks cheapest EVM option",
			req: &router.PaymentRequirement{
				Protocol: router.ProtocolX402,
				X402Requirement: &router.X402Requirement{
					Accepts: []router.X402Accept{
						{Network: "eip155:84532", MaxAmountRequired: "50000", PayTo: "0xa"},
						{Network: "eip155:84532", MaxAmountRequired: "1000", PayTo: "0xb"},
					},
				},
			},
			wantUSD: 0.001,
		},
		{
			name:    "nil requirement",
			req:     &router.PaymentRequirement{Protocol: router.ProtocolX402},
			wantErr: true,
		},
		{
			name: "empty accepts",
			req: &router.PaymentRequirement{
				Protocol:        router.ProtocolX402,
				X402Requirement: &router.X402Requirement{Accepts: []router.X402Accept{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usd, desc, err := p.EstimateCost(tt.req)
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
			if !strings.Contains(desc, "CDP") {
				t.Errorf("description should mention CDP: %s", desc)
			}
		})
	}
}

func TestCDPProvider_Init(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "GET" && strings.Contains(r.URL.Path, "/evm/accounts") {
			// Return existing account
			json.NewEncoder(w).Encode(map[string]interface{}{
				"accounts": []map[string]string{
					{"address": "0xCDP_WALLET_ADDRESS", "name": "agentpay-wallet"},
				},
			})
			return
		}

		http.Error(w, "unexpected request", 400)
	}))
	defer srv.Close()

	p := NewCDPProvider("key-id", "a2V5LXNlY3JldA==", "d2FsbGV0LXNlY3JldA==")
	p.apiBaseURL = srv.URL

	err := p.Init(context.Background(), "agentpay-wallet")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if p.Address() != "0xCDP_WALLET_ADDRESS" {
		t.Errorf("expected address 0xCDP_WALLET_ADDRESS, got %s", p.Address())
	}
}

func TestCDPProvider_InitCreatesNew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "GET" {
			// No existing accounts
			json.NewEncoder(w).Encode(map[string]interface{}{
				"accounts": []interface{}{},
			})
			return
		}

		if r.Method == "POST" && strings.Contains(r.URL.Path, "/evm/accounts") {
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			json.Unmarshal(body, &req)
			if req["name"] != "my-wallet" {
				t.Errorf("expected name 'my-wallet', got %s", req["name"])
			}
			json.NewEncoder(w).Encode(map[string]string{
				"address": "0xNEW_WALLET",
			})
			return
		}

		http.Error(w, "unexpected", 400)
	}))
	defer srv.Close()

	p := NewCDPProvider("key-id", "a2V5LXNlY3JldA==", "d2FsbGV0LXNlY3JldA==")
	p.apiBaseURL = srv.URL

	err := p.Init(context.Background(), "my-wallet")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if p.Address() != "0xNEW_WALLET" {
		t.Errorf("expected 0xNEW_WALLET, got %s", p.Address())
	}
}

func TestCDPProvider_Pay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/sign/typed-data") {
			// Verify it's a POST with typed data
			if r.Method != "POST" {
				t.Errorf("expected POST for signing, got %s", r.Method)
			}

			body, _ := io.ReadAll(r.Body)
			var typedData map[string]interface{}
			if err := json.Unmarshal(body, &typedData); err != nil {
				t.Fatalf("invalid typed data JSON: %v", err)
			}

			// Verify structure
			if typedData["primaryType"] != "TransferWithAuthorization" {
				t.Errorf("expected TransferWithAuthorization, got %v", typedData["primaryType"])
			}

			msg := typedData["message"].(map[string]interface{})
			if msg["from"] != "0xMY_WALLET" {
				t.Errorf("expected from=0xMY_WALLET, got %v", msg["from"])
			}
			if msg["to"] != "0xPAYEE" {
				t.Errorf("expected to=0xPAYEE, got %v", msg["to"])
			}

			json.NewEncoder(w).Encode(map[string]string{
				"signature": "0xSIGNATURE_FROM_CDP",
			})
			return
		}

		http.Error(w, "unexpected request: "+r.URL.Path, 400)
	}))
	defer srv.Close()

	p := NewCDPProvider("key-id", "a2V5LXNlY3JldA==", "d2FsbGV0LXNlY3JldA==")
	p.apiBaseURL = srv.URL
	p.address = "0xMY_WALLET"

	req := &router.PaymentRequirement{
		Protocol: router.ProtocolX402,
		X402Requirement: &router.X402Requirement{
			Accepts: []router.X402Accept{{
				Scheme:            "exact",
				Network:           "eip155:84532",
				MaxAmountRequired: "1000",
				PayTo:             "0xPAYEE",
				Asset:             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
			}},
		},
	}

	headerName, headerValue, err := p.Pay(context.Background(), req)
	if err != nil {
		t.Fatalf("Pay failed: %v", err)
	}
	if headerName != "Payment" {
		t.Errorf("expected header 'Payment', got %q", headerName)
	}
	if headerValue == "" {
		t.Error("expected non-empty payment header value")
	}
}

func TestCDPProvider_PayNotInitialized(t *testing.T) {
	p := NewCDPProvider("key-id", "secret", "wallet")
	// address not set — Init not called

	req := &router.PaymentRequirement{
		Protocol: router.ProtocolX402,
		X402Requirement: &router.X402Requirement{
			Accepts: []router.X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "1000",
				PayTo:             "0xpayee",
			}},
		},
	}

	_, _, err := p.Pay(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for uninitialized provider")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestCDPProvider_PayNoEVMOption(t *testing.T) {
	p := NewCDPProvider("key-id", "secret", "wallet")
	p.address = "0xWALLET"

	req := &router.PaymentRequirement{
		Protocol: router.ProtocolX402,
		X402Requirement: &router.X402Requirement{
			Accepts: []router.X402Accept{{
				Network:           "solana:devnet",
				MaxAmountRequired: "1000",
				PayTo:             "SOL_ADDR",
			}},
		},
	}

	_, _, err := p.Pay(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-EVM payment option")
	}
	if !strings.Contains(err.Error(), "no EVM payment option") {
		t.Errorf("expected 'no EVM payment option' error, got: %v", err)
	}
}

func TestGenerateNonce(t *testing.T) {
	nonce := generateNonce()
	if !strings.HasPrefix(nonce, "0x") {
		t.Errorf("nonce should start with 0x, got %s", nonce)
	}
	if len(nonce) < 10 {
		t.Errorf("nonce too short: %s", nonce)
	}

	// Two nonces should be different
	nonce2 := generateNonce()
	if nonce == nonce2 {
		t.Error("two nonces should be different")
	}
}

func TestCanonicalizeJSON(t *testing.T) {
	input := `{"z":"last","a":"first","m":"middle"}`
	result := canonicalizeJSON([]byte(input))

	var obj map[string]interface{}
	json.Unmarshal(result, &obj)

	// Verify all keys are present
	if obj["a"] != "first" || obj["m"] != "middle" || obj["z"] != "last" {
		t.Errorf("canonicalized JSON lost data: %s", string(result))
	}
}
