package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/joelklabo/agentpay/router"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(demoCmd)
}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Run a self-contained demo with mock L402 and x402 servers",
	Long: `Spins up local mock servers for both L402 (Lightning) and x402 (USDC)
payment protocols, then executes a multi-step workflow against them.

No external services, wallets, or API keys needed — everything runs locally.

This demonstrates AgentPay's core capability: transparent cross-protocol
payment routing where the agent never handles payment logic directly.`,
	RunE: runDemo,
}

// mockPaymentServer runs local L402 and x402 endpoints that simulate the 402 flow.
type mockPaymentServer struct {
	mu       sync.Mutex
	paid     map[string]bool // payment_hash → paid
	listener net.Listener
	server   *http.Server
}

func newMockServer() (*mockPaymentServer, error) {
	m := &mockPaymentServer{
		paid: make(map[string]bool),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/l402/ai", m.handleL402)
	mux.HandleFunc("/x402/data", m.handleX402)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	m.listener = listener
	m.server = &http.Server{Handler: mux}

	go m.server.Serve(listener)
	return m, nil
}

func (m *mockPaymentServer) addr() string {
	return "http://" + m.listener.Addr().String()
}

func (m *mockPaymentServer) close() {
	m.server.Close()
}

func (m *mockPaymentServer) randomHash() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// handleL402 simulates an L402 (Lightning) paywall.
// First request → 402 with invoice. Second request with payment proof → 200.
func (m *mockPaymentServer) handleL402(w http.ResponseWriter, r *http.Request) {
	// Check for payment proof in Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "L402 ") || strings.HasPrefix(auth, "LSAT ") {
		token := strings.TrimPrefix(auth, "L402 ")
		token = strings.TrimPrefix(token, "LSAT ")
		parts := strings.SplitN(token, ":", 2)
		hash := parts[0]

		m.mu.Lock()
		isPaid := m.paid[hash]
		m.mu.Unlock()

		if isPaid {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"result": "Cross-protocol payment routing enables AI agents to interact with " +
					"any paid API regardless of the underlying payment rail. AgentPay detects " +
					"the protocol (x402, L402, or SPL), settles via the correct provider, and " +
					"retries with proof — all transparently.",
				"model":   "mock-llm-70b",
				"service": "Maximum Sats AI (demo)",
				"paid":    true,
			})
			return
		}
	}

	// No valid payment — return 402 with L402 challenge
	hash := m.randomHash()
	invoice := "lnbc100n1demo" + hash[:16] // mock BOLT11

	m.mu.Lock()
	m.paid[hash] = true // auto-settle for demo purposes
	m.mu.Unlock()

	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`L402 invoice="%s", payment_hash="%s"`, invoice, hash))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"status":          "payment_required",
		"protocol":        "L402",
		"price_sats":      10,
		"payment_request": invoice,
		"payment_hash":    hash,
	})
}

// handleX402 simulates an x402 (USDC) paywall.
func (m *mockPaymentServer) handleX402(w http.ResponseWriter, r *http.Request) {
	// Check for x402 payment proof
	paymentHeader := r.Header.Get("X-Payment")
	if paymentHeader != "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"analysis": "Agent economy analysis: 900+ autonomous agents are currently " +
				"participating in the Colosseum hackathon. Payment infrastructure is the " +
				"critical bottleneck — agents need to pay for services across multiple rails " +
				"without manual intervention. Cross-protocol routers like AgentPay solve this.",
			"confidence": 0.94,
			"service":    "Agent Analytics (demo)",
			"paid":       true,
		})
		return
	}

	// Return 402 with x402 payment requirement
	payReq := map[string]any{
		"accepts": []map[string]any{
			{
				"scheme":            "exact",
				"network":           "eip155:84532",
				"maxAmountRequired": "1000",
				"resource":          m.addr() + "/x402/data",
				"description":       "Agent analytics API access",
				"payTo":             "0x5049CaCF18346ee22EBA390B9B6309cb3f03abFB",
				"maxTimeoutSeconds":  60,
				"asset":             "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
			},
		},
	}
	payReqJSON, _ := json.Marshal(payReq)
	encoded := base64.StdEncoding.EncodeToString(payReqJSON)

	w.Header().Set("Payment-Required", encoded)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "payment_required",
		"protocol": "x402",
		"amount":   "$0.001 USDC",
	})
}

// mockL402Provider auto-pays L402 invoices in demo mode.
type mockL402Provider struct{}

func (p *mockL402Provider) Protocol() router.Protocol { return router.ProtocolL402 }

func (p *mockL402Provider) Pay(ctx context.Context, req *router.PaymentRequirement) (string, string, error) {
	// In demo mode, the mock server auto-settles, so just return the proof header
	return "Authorization", fmt.Sprintf("L402 %s:demo_preimage", req.L402Hash), nil
}

func (p *mockL402Provider) EstimateCost(req *router.PaymentRequirement) (float64, string, error) {
	return 0.000007, "10 sats (~$0.000007)", nil
}

// mockX402Provider auto-pays x402 invoices in demo mode.
type mockX402Provider struct{}

func (p *mockX402Provider) Protocol() router.Protocol { return router.ProtocolX402 }

func (p *mockX402Provider) Pay(ctx context.Context, req *router.PaymentRequirement) (string, string, error) {
	return "X-Payment", "demo_payment_proof_" + time.Now().Format("150405"), nil
}

func (p *mockX402Provider) EstimateCost(req *router.PaymentRequirement) (float64, string, error) {
	return 0.001, "$0.001 USDC", nil
}

func runDemo(cmd *cobra.Command, args []string) error {
	// Start mock payment servers
	mock, err := newMockServer()
	if err != nil {
		return fmt.Errorf("start mock server: %w", err)
	}
	defer mock.close()

	// Create router with mock providers
	r := router.New(router.Config{
		MaxPerRequestUSD: 1.0,
		MaxSessionUSD:    10.0,
		Verbose:          true,
	})
	r.RegisterProvider(&mockL402Provider{})
	r.RegisterProvider(&mockX402Provider{})

	ctx := context.Background()
	start := time.Now()

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║         AgentPay — Cross-Protocol Payment Demo          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Mock servers running at %s\n", mock.addr())
	fmt.Println("  L402 endpoint: /l402/ai   (Lightning — 10 sats)")
	fmt.Println("  x402 endpoint: /x402/data (USDC — $0.001)")
	fmt.Println()
	fmt.Println("  Budget: $1.00/request, $10.00/session")
	fmt.Println()

	// Step 1: L402 call
	fmt.Println("━━━ Step 1: L402 (Lightning) — AI Text Generation ━━━")
	fmt.Printf("  Target: %s/l402/ai\n", mock.addr())
	fmt.Println("  → Sending POST request...")

	body1, receipt1, err := r.Fetch(ctx, "POST", mock.addr()+"/l402/ai",
		strings.NewReader(`{"prompt":"Explain cross-protocol payment routing"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		fmt.Printf("  ✗ Error: %v\n", err)
	} else {
		fmt.Println("  → Server returned HTTP 402 (Payment Required)")
		fmt.Println("  → Detected protocol: L402 (Lightning invoice)")
		if receipt1 != nil {
			fmt.Printf("  → Paid: %s via %s\n", receipt1.Amount, receipt1.Protocol)
		}
		fmt.Println("  → Retried request with payment proof")
		fmt.Printf("  ✓ Response received (%d bytes)\n", len(body1))
		printDemoJSON(body1)
	}

	fmt.Println()

	// Step 2: x402 call
	fmt.Println("━━━ Step 2: x402 (USDC) — Agent Analytics ━━━")
	fmt.Printf("  Target: %s/x402/data\n", mock.addr())
	fmt.Println("  → Sending POST request...")

	body2, receipt2, err := r.Fetch(ctx, "POST", mock.addr()+"/x402/data",
		strings.NewReader(`{"task":"analyze","input":"agent economy trends"}`),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		fmt.Printf("  ✗ Error: %v\n", err)
	} else {
		fmt.Println("  → Server returned HTTP 402 (Payment Required)")
		fmt.Println("  → Detected protocol: x402 (USDC payment)")
		if receipt2 != nil {
			fmt.Printf("  → Paid: %s via %s\n", receipt2.Amount, receipt2.Protocol)
		}
		fmt.Println("  → Retried request with payment proof")
		fmt.Printf("  ✓ Response received (%d bytes)\n", len(body2))
		printDemoJSON(body2)
	}

	// Summary
	fmt.Println()
	fmt.Println("━━━ Payment Summary ━━━")
	elapsed := time.Since(start)
	receipts := r.Receipts()

	for i, rcpt := range receipts {
		fmt.Printf("  %d. %s → %s (%s)\n", i+1, rcpt.Protocol, rcpt.Amount, rcpt.URL)
	}

	fmt.Printf("\n  Total spent:  $%.6f across %d payment(s)\n", r.SessionSpend(), len(receipts))
	fmt.Printf("  Protocols:    %s\n", protocolSummary(receipts))
	fmt.Printf("  Duration:     %s\n", elapsed.Round(time.Millisecond))
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  This demo ran entirely locally with mock payment servers.")
	fmt.Println("  In production, AgentPay connects to real LNbits (Lightning)")
	fmt.Println("  and AgentWallet (Solana/EVM) for actual payments.")
	fmt.Println()
	fmt.Println("  Repo:  https://github.com/joelklabo/agentpay")
	fmt.Println("  Agent: max-sats (#900) — max@klabo.world")
	fmt.Println()

	return nil
}

func printDemoJSON(body []byte) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Printf("  %s\n", string(body))
		return
	}
	// Print select fields nicely
	for _, key := range []string{"result", "analysis"} {
		if v, ok := data[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 120 {
				s = s[:120] + "..."
			}
			fmt.Printf("  \"%s\"\n", s)
		}
	}
}
