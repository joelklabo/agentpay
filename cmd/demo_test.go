package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/joelklabo/agentpay/router"
)

func TestDemoMockServerL402(t *testing.T) {
	mock, err := newMockServer()
	if err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer mock.close()

	r := router.New(router.Config{
		MaxPerRequestUSD: 1.0,
		MaxSessionUSD:    10.0,
	})
	r.RegisterProvider(&mockL402Provider{})

	body, receipt, err := r.Fetch(context.Background(), "POST", mock.addr()+"/l402/ai",
		strings.NewReader(`{"prompt":"test"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		t.Fatalf("fetch L402: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt for L402 payment")
	}
	if receipt.Protocol != "L402" {
		t.Errorf("protocol = %q, want L402", receipt.Protocol)
	}
	if !strings.Contains(string(body), "result") {
		t.Errorf("body missing result field: %s", body)
	}
}

func TestDemoMockServerX402(t *testing.T) {
	mock, err := newMockServer()
	if err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer mock.close()

	r := router.New(router.Config{
		MaxPerRequestUSD: 1.0,
		MaxSessionUSD:    10.0,
	})
	r.RegisterProvider(&mockX402Provider{})

	body, receipt, err := r.Fetch(context.Background(), "POST", mock.addr()+"/x402/data",
		strings.NewReader(`{"task":"test"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		t.Fatalf("fetch x402: %v", err)
	}
	if receipt == nil {
		t.Fatal("expected receipt for x402 payment")
	}
	if receipt.Protocol != "x402" {
		t.Errorf("protocol = %q, want x402", receipt.Protocol)
	}
	if !strings.Contains(string(body), "analysis") {
		t.Errorf("body missing analysis field: %s", body)
	}
}

func TestDemoMockServerCrossProtocol(t *testing.T) {
	mock, err := newMockServer()
	if err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	defer mock.close()

	r := router.New(router.Config{
		MaxPerRequestUSD: 1.0,
		MaxSessionUSD:    10.0,
	})
	r.RegisterProvider(&mockL402Provider{})
	r.RegisterProvider(&mockX402Provider{})

	ctx := context.Background()

	// L402 call
	_, r1, err := r.Fetch(ctx, "POST", mock.addr()+"/l402/ai",
		strings.NewReader(`{"prompt":"test"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		t.Fatalf("L402: %v", err)
	}

	// x402 call
	_, r2, err := r.Fetch(ctx, "POST", mock.addr()+"/x402/data",
		strings.NewReader(`{"task":"test"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		t.Fatalf("x402: %v", err)
	}

	// Verify both protocols used
	receipts := r.Receipts()
	if len(receipts) != 2 {
		t.Fatalf("receipts = %d, want 2", len(receipts))
	}
	if r1.Protocol != "L402" || r2.Protocol != "x402" {
		t.Errorf("protocols = %s + %s, want L402 + x402", r1.Protocol, r2.Protocol)
	}

	// Verify session spend
	if r.SessionSpend() == 0 {
		t.Error("session spend should be > 0")
	}
}
