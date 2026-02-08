package router

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockProvider is a test payment provider.
type mockProvider struct {
	protocol    Protocol
	cost        float64
	description string
	headerName  string
	headerValue string
	payErr      error
}

func (m *mockProvider) Protocol() Protocol { return m.protocol }

func (m *mockProvider) EstimateCost(req *PaymentRequirement) (float64, string, error) {
	return m.cost, m.description, nil
}

func (m *mockProvider) Pay(ctx context.Context, req *PaymentRequirement) (string, string, error) {
	if m.payErr != nil {
		return "", "", m.payErr
	}
	return m.headerName, m.headerValue, nil
}

func TestRouter_FetchNon402(t *testing.T) {
	// Server that returns 200 directly
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0})
	body, receipt, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt != nil {
		t.Error("expected no receipt for non-402 response")
	}
	if string(body) != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRouter_FetchX402(t *testing.T) {
	callCount := 0

	// Server that returns 402 on first call, 200 on retry with payment proof
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(200)
			w.Write([]byte(`{"result":"paid content"}`))
			return
		}

		// Return x402 payment requirement
		req := X402Requirement{
			Accepts: []X402Accept{{
				Scheme:            "exact",
				Network:           "eip155:84532",
				MaxAmountRequired: "10000", // $0.01
				PayTo:             "0xabc123",
				Asset:             "USDC",
			}},
		}
		data, _ := json.Marshal(req)
		encoded := base64.StdEncoding.EncodeToString(data)
		w.Header().Set("Payment-Required", encoded)
		w.WriteHeader(402)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        0.01,
		description: "$0.01 USDC",
		headerName:  "Payment-Signature",
		headerValue: "sig_test_123",
	})

	body, receipt, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt for 402 response")
	}
	if receipt.Protocol != "x402" {
		t.Errorf("expected protocol x402, got %s", receipt.Protocol)
	}
	if string(body) != `{"result":"paid content"}` {
		t.Errorf("unexpected body: %s", body)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (original + retry), got %d", callCount)
	}
}

func TestRouter_FetchL402(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Authorization") != "" && strings.HasPrefix(r.Header.Get("Authorization"), "L402") {
			w.WriteHeader(200)
			w.Write([]byte(`{"result":"lightning paid"}`))
			return
		}

		w.WriteHeader(402)
		w.Write([]byte(`{"invoice":"lnbc100u1pjtest","payment_hash":"hash123"}`))
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolL402,
		cost:        0.001,
		description: "10000 sats",
		headerName:  "Authorization",
		headerValue: "L402 hash123:preimage123",
	})

	body, receipt, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt")
	}
	if receipt.Protocol != "L402" {
		t.Errorf("expected L402, got %s", receipt.Protocol)
	}
	if string(body) != `{"result":"lightning paid"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRouter_BudgetExceeded(t *testing.T) {
	// Server that always returns 402
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := X402Requirement{
			Accepts: []X402Accept{{
				Scheme:            "exact",
				Network:           "eip155:84532",
				MaxAmountRequired: "10000000", // $10
				PayTo:             "0xabc123",
				Asset:             "USDC",
			}},
		}
		data, _ := json.Marshal(req)
		encoded := base64.StdEncoding.EncodeToString(data)
		w.Header().Set("Payment-Required", encoded)
		w.WriteHeader(402)
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 5.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        10.0,
		description: "$10.00 USDC",
	})

	_, _, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("expected budget error, got: %v", err)
	}
}

func TestRouter_DryRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := X402Requirement{
			Accepts: []X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "10000",
				PayTo:             "0xabc123",
			}},
		}
		data, _ := json.Marshal(req)
		w.Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(data))
		w.WriteHeader(402)
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0, DryRun: true})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        0.01,
		description: "$0.01 USDC",
	})

	_, receipt, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected dry-run receipt")
	}
	if !strings.Contains(receipt.Description, "DRY RUN") {
		t.Errorf("expected dry run receipt, got: %s", receipt.Description)
	}
	if r.SessionSpend() != 0 {
		t.Error("dry run should not affect session spend")
	}
}

func TestRouter_NoProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		w.Write([]byte(`{"invoice":"lnbc100u1pjtest"}`))
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0})
	// No providers registered

	_, _, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}

	fmt.Println("error:", err)
}

func TestRouter_SessionSpendTracking(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(200)
			w.Write([]byte(`ok`))
			return
		}
		req := X402Requirement{
			Accepts: []X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "10000",
			}},
		}
		data, _ := json.Marshal(req)
		w.Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(data))
		w.WriteHeader(402)
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 0.05})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        0.01,
		description: "$0.01",
		headerName:  "Payment-Signature",
		headerValue: "sig_123",
	})

	// First 5 requests should succeed (5 * $0.01 = $0.05 == $0.05 limit)
	for i := 0; i < 5; i++ {
		_, _, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}
	}

	if r.SessionSpend() != 0.05 {
		t.Errorf("expected session spend $0.05, got $%.4f", r.SessionSpend())
	}

	// 6th request should fail (would exceed $0.05 budget)
	_, _, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected budget error on 6th request")
	}
}

func TestRouter_FetchWithBody(t *testing.T) {
	// Verify that POST bodies are correctly replayed after 402 payment
	var firstBody, retryBody string
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		b, _ := io.ReadAll(r.Body)
		if callCount == 1 {
			firstBody = string(b)
			req := X402Requirement{
				Accepts: []X402Accept{{
					Network:           "eip155:84532",
					MaxAmountRequired: "10000",
					PayTo:             "0xabc123",
				}},
			}
			data, _ := json.Marshal(req)
			w.Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(data))
			w.WriteHeader(402)
			return
		}
		retryBody = string(b)
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        0.01,
		description: "$0.01",
		headerName:  "Payment-Signature",
		headerValue: "sig_test",
	})

	payload := `{"query":"important data"}`
	body, receipt, err := r.Fetch(context.Background(), "POST", srv.URL, strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if firstBody != payload {
		t.Errorf("first request body = %q, want %q", firstBody, payload)
	}
	if retryBody != payload {
		t.Errorf("retry request body = %q, want %q (body not replayed)", retryBody, payload)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("unexpected response: %s", body)
	}
}

func TestRouter_WoTTrustBlock(t *testing.T) {
	// WoT service that returns a low trust score
	wotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WoTScore{Pubkey: "0xuntrusted", Score: 0.0001, Rank: 50000})
	}))
	defer wotSrv.Close()

	// Target that returns 402 with x402 requirement
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := X402Requirement{
			Accepts: []X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "10000000",
				PayTo:             "0xuntrusted",
			}},
		}
		data, _ := json.Marshal(req)
		w.Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(data))
		w.WriteHeader(402)
	}))
	defer srv.Close()

	wot := NewWoTChecker(wotSrv.URL)
	wot.MinScore = 0.001
	wot.ThresholdUSD = 0.01

	r := New(Config{MaxPerRequestUSD: 100.0, MaxSessionUSD: 1000.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        1.0,
		description: "$1.00 USDC",
		headerName:  "Payment-Signature",
		headerValue: "sig_test",
	})
	r.SetWoTChecker(wot)

	_, _, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected trust check error")
	}
	if !strings.Contains(err.Error(), "trust check failed") {
		t.Errorf("expected trust check error, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected only 1 call (no retry after trust failure), got %d", callCount)
	}
}

func TestRouter_WoTTrustAllow(t *testing.T) {
	// WoT service that returns a high trust score
	wotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WoTScore{Pubkey: "0xtrusted", Score: 0.05, Rank: 10})
	}))
	defer wotSrv.Close()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(200)
			w.Write([]byte(`{"result":"trusted payment"}`))
			return
		}
		req := X402Requirement{
			Accepts: []X402Accept{{
				Network:           "eip155:84532",
				MaxAmountRequired: "10000",
				PayTo:             "0xtrusted",
			}},
		}
		data, _ := json.Marshal(req)
		w.Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(data))
		w.WriteHeader(402)
	}))
	defer srv.Close()

	wot := NewWoTChecker(wotSrv.URL)
	wot.MinScore = 0.001
	wot.ThresholdUSD = 0.001

	r := New(Config{MaxPerRequestUSD: 1.0, MaxSessionUSD: 10.0})
	r.RegisterProvider(&mockProvider{
		protocol:    ProtocolX402,
		cost:        0.01,
		description: "$0.01 USDC",
		headerName:  "Payment-Signature",
		headerValue: "sig_trusted",
	})
	r.SetWoTChecker(wot)

	body, receipt, err := r.Fetch(context.Background(), "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt for trusted payment")
	}
	if string(body) != `{"result":"trusted payment"}` {
		t.Errorf("unexpected body: %s", body)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", callCount)
	}
}
