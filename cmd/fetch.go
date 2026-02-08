package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joelklabo/agentpay/providers"
	"github.com/joelklabo/agentpay/router"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Fetch a URL, automatically handling 402 payments",
	Long: `Sends an HTTP request to the given URL. If the server responds with
HTTP 402, AgentPay detects the payment protocol (x402 or L402), settles the
payment, and retries the request with proof. Returns the final response.`,
	Args: cobra.ExactArgs(1),
	RunE: runFetch,
}

var (
	fetchMethod  string
	fetchBody    string
	fetchDryRun  bool
	fetchBudget  float64
	fetchVerbose bool
	fetchHeaders []string
	fetchWoT     bool
)

func init() {
	fetchCmd.Flags().StringVarP(&fetchMethod, "method", "X", "GET", "HTTP method")
	fetchCmd.Flags().StringVarP(&fetchBody, "data", "d", "", "Request body")
	fetchCmd.Flags().BoolVar(&fetchDryRun, "dry-run", false, "Preview payment cost without paying")
	fetchCmd.Flags().Float64Var(&fetchBudget, "budget", 1.0, "Maximum USD to spend per request")
	fetchCmd.Flags().BoolVarP(&fetchVerbose, "verbose", "v", false, "Verbose output")
	fetchCmd.Flags().StringArrayVarP(&fetchHeaders, "header", "H", nil, "HTTP headers (key: value)")
	fetchCmd.Flags().BoolVar(&fetchWoT, "wot", false, "Enable Web of Trust trust scoring before payments")
}

func runFetch(cmd *cobra.Command, args []string) error {
	url := args[0]
	ctx := context.Background()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w (run 'agentpay init' to set up)", err)
	}

	r := router.New(router.Config{
		MaxPerRequestUSD: fetchBudget,
		MaxSessionUSD:    fetchBudget * 10,
		DryRun:           fetchDryRun,
		Verbose:          fetchVerbose,
	})

	// Register providers based on config
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

	if fetchWoT {
		wot := router.NewWoTChecker("https://maximumsats.joel-dfd.workers.dev/wot/score")
		r.SetWoTChecker(wot)
		if fetchVerbose {
			fmt.Fprintln(os.Stderr, "WoT trust scoring enabled")
		}
	}

	// Parse headers
	hdrs := make(map[string]string)
	for _, h := range fetchHeaders {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			hdrs[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Build body reader
	var bodyReader *strings.Reader
	if fetchBody != "" {
		bodyReader = strings.NewReader(fetchBody)
	}

	var bodyIO interface{ Read([]byte) (int, error) }
	if bodyReader != nil {
		bodyIO = bodyReader
	}

	respBody, receipt, err := r.Fetch(ctx, fetchMethod, url, bodyIO, hdrs)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	// Print receipt if payment was made
	if receipt != nil {
		receiptJSON, _ := json.MarshalIndent(receipt, "", "  ")
		fmt.Fprintf(os.Stderr, "\n--- Payment Receipt ---\n%s\n-----------------------\n\n", receiptJSON)
	}

	// Print response body
	fmt.Print(string(respBody))
	return nil
}
