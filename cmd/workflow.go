package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joelklabo/agentpay/providers"
	"github.com/joelklabo/agentpay/router"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(workflowCmd)
}

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Run a demo workflow that chains multiple paid API calls",
	Long: `Demonstrates AgentPay's cross-protocol payment routing by executing
a multi-step workflow that calls APIs across different payment rails:

1. Call an L402 API (Lightning) for AI text generation
2. Call an x402 API (USDC on Solana/EVM) for a second service
3. Display a cost breakdown showing payments across protocols

This showcases the core value: one agent, multiple payment rails, transparent settlement.`,
	RunE: runWorkflow,
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w (run 'agentpay init' first)", err)
	}

	r := router.New(router.Config{
		MaxPerRequestUSD: cfg.Budget.MaxPerRequestUSD,
		MaxSessionUSD:    cfg.Budget.MaxSessionUSD,
		DryRun:           fetchDryRun,
		Verbose:          true,
	})

	// Register providers
	if cfg.AgentWallet.Username != "" {
		x402 := providers.NewX402Provider(
			cfg.AgentWallet.APIBase,
			cfg.AgentWallet.Username,
			cfg.AgentWallet.Token,
		)
		if cfg.AgentWallet.PreferredChain != "" {
			x402.PreferredChain = cfg.AgentWallet.PreferredChain
		}
		r.RegisterProvider(x402)
	}
	if cfg.LNbits.URL != "" {
		l402 := providers.NewL402Provider(cfg.LNbits.URL, cfg.LNbits.AdminKey)
		r.RegisterProvider(l402)
	}

	// Enable WoT trust scoring
	wot := router.NewWoTChecker("https://maximumsats.joel-dfd.workers.dev/wot/score")
	r.SetWoTChecker(wot)

	ctx := context.Background()
	start := time.Now()

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║       AgentPay Cross-Protocol Workflow           ║")
	fmt.Println("║       with Web of Trust verification             ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  WoT: Trust scoring enabled (Nostr social graph, 51K+ nodes)")
	fmt.Printf("  WoT: Minimum score %.4f, threshold $%.2f\n", wot.MinScore, wot.ThresholdUSD)
	fmt.Println()

	// Load registry for known APIs
	apis, _ := loadRegistry()

	// Step 1: Call L402 API (Lightning)
	fmt.Println("━━━ Step 1: L402 (Lightning) — AI Text Generation ━━━")
	l402URL := findAPIURL(apis, "l402", "maximumsats-dvm")
	if l402URL != "" {
		fmt.Printf("  Target: %s\n", l402URL)
		body, receipt, err := r.Fetch(ctx, "POST", l402URL,
			strings.NewReader(`{"prompt":"Explain cross-protocol payment routing in one paragraph"}`),
			map[string]string{"Content-Type": "application/json"})
		if err != nil {
			fmt.Printf("  ⚠ L402 call failed: %v\n", err)
		} else {
			printStepResult(body, receipt)
		}
	} else {
		fmt.Println("  ⚠ No L402 API configured in registry")
	}

	// Step 2: Call x402 API (USDC)
	fmt.Println()
	fmt.Println("━━━ Step 2: x402 (USDC) — Secondary Service ━━━")
	x402URL := findAPIURL(apis, "x402", "opspawn-a2a")
	if x402URL != "" {
		fmt.Printf("  Target: %s\n", x402URL)
		body, receipt, err := r.Fetch(ctx, "POST", x402URL,
			strings.NewReader(`{"task":"analyze","input":"cross-protocol payment benefits"}`),
			map[string]string{"Content-Type": "application/json"})
		if err != nil {
			fmt.Printf("  ⚠ x402 call failed: %v\n", err)
		} else {
			printStepResult(body, receipt)
		}
	} else {
		fmt.Println("  ⚠ No x402 API configured in registry")
	}

	// Summary
	fmt.Println()
	fmt.Println("━━━ Cost Breakdown ━━━")
	elapsed := time.Since(start)
	receipts := r.Receipts()
	totalUSD := r.SessionSpend()

	for i, receipt := range receipts {
		fmt.Printf("  %d. %s via %s — %s\n", i+1, receipt.URL, receipt.Protocol, receipt.Amount)
	}
	if len(receipts) == 0 {
		fmt.Println("  No payments made (dry run or no 402 responses)")
	}

	fmt.Printf("\n  Total:    $%.4f across %d payment(s)\n", totalUSD, len(receipts))
	fmt.Printf("  Protocols: %s\n", protocolSummary(receipts))
	fmt.Printf("  Duration:  %s\n", elapsed.Round(time.Millisecond))
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════")

	// Write receipts to file
	if len(receipts) > 0 {
		receiptFile := "agentpay-receipts.json"
		data, _ := json.MarshalIndent(receipts, "", "  ")
		os.WriteFile(receiptFile, data, 0644)
		fmt.Printf("Receipts saved to %s\n", receiptFile)
	}

	return nil
}

func findAPIURL(apis []APIEntry, protocol, name string) string {
	for _, api := range apis {
		if api.Name == name {
			return api.URL
		}
	}
	// Fallback: find any API with matching protocol
	for _, api := range apis {
		if api.Protocol == protocol {
			return api.URL
		}
	}
	return ""
}

func printStepResult(body []byte, receipt *router.Receipt) {
	if receipt != nil {
		fmt.Printf("  ✓ Paid: %s via %s\n", receipt.Amount, receipt.Protocol)
	}
	// Truncate response for display
	s := string(body)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	fmt.Printf("  Response: %s\n", s)
}

func protocolSummary(receipts []router.Receipt) string {
	seen := make(map[string]int)
	for _, r := range receipts {
		seen[r.Protocol]++
	}
	if len(seen) == 0 {
		return "none"
	}
	var parts []string
	for proto, count := range seen {
		parts = append(parts, fmt.Sprintf("%s (%d)", proto, count))
	}
	return strings.Join(parts, ", ")
}
