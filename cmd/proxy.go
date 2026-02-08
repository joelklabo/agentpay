package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/joelklabo/agentpay/providers"
	"github.com/joelklabo/agentpay/router"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Run as an HTTP proxy that handles 402 payments transparently",
	Long: `Starts a local HTTP proxy server. Send requests to the proxy, and it
forwards them to the target. If the target returns 402, the proxy handles
payment settlement and retries automatically.

Usage:
  agentpay proxy --port 8402

Then send requests:
  curl -H "X-Target-URL: https://api.example.com/resource" http://localhost:8402`,
	RunE: runProxy,
}

var (
	proxyPort   int
	proxyBudget float64
)

func init() {
	proxyCmd.Flags().IntVarP(&proxyPort, "port", "p", 8402, "Port to listen on")
	proxyCmd.Flags().Float64Var(&proxyBudget, "budget", 10.0, "Maximum USD budget for the session")
}

func runProxy(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	r := router.New(router.Config{
		MaxPerRequestUSD: cfg.Budget.MaxPerRequestUSD,
		MaxSessionUSD:    proxyBudget,
		Verbose:          true,
	})

	if cfg.AgentWallet.Username != "" {
		x402 := providers.NewX402Provider(
			cfg.AgentWallet.APIBase,
			cfg.AgentWallet.Username,
			cfg.AgentWallet.Token,
		)
		r.RegisterProvider(x402)
	}
	if cfg.LNbits.URL != "" {
		l402 := providers.NewL402Provider(cfg.LNbits.URL, cfg.LNbits.AdminKey)
		r.RegisterProvider(l402)
	}

	mux := http.NewServeMux()

	// Proxy handler: reads X-Target-URL header to determine where to forward
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		targetURL := req.Header.Get("X-Target-URL")
		if targetURL == "" {
			// Try using the request path as the target URL
			targetURL = strings.TrimPrefix(req.URL.Path, "/")
			if !strings.HasPrefix(targetURL, "http") {
				http.Error(w, "Set X-Target-URL header or use URL as path", http.StatusBadRequest)
				return
			}
		}

		// Forward headers (except hop-by-hop and our custom ones)
		headers := make(map[string]string)
		for k, v := range req.Header {
			if k == "X-Target-Url" || k == "X-Target-URL" {
				continue
			}
			headers[k] = v[0]
		}

		var body io.Reader
		if req.Body != nil {
			body = req.Body
			defer req.Body.Close()
		}

		ctx := context.Background()
		respBody, receipt, err := r.Fetch(ctx, req.Method, targetURL, body, headers)
		if err != nil {
			log.Printf("ERROR: %v", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		if receipt != nil {
			log.Printf("PAID: %s %s (%s)", receipt.Protocol, receipt.Amount, receipt.URL)
			w.Header().Set("X-AgentPay-Protocol", receipt.Protocol)
			w.Header().Set("X-AgentPay-Cost", receipt.Amount)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBody)
	})

	// Stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		receipts := r.Receipts()
		fmt.Fprintf(w, `{"session_spend_usd":%.4f,"payment_count":%d,"receipts":%d}`,
			r.SessionSpend(), len(receipts), len(receipts))
	})

	addr := fmt.Sprintf(":%d", proxyPort)
	log.Printf("AgentPay proxy listening on %s", addr)
	log.Printf("Session budget: $%.2f", proxyBudget)
	log.Printf("Send requests with X-Target-URL header or URL as path")
	return http.ListenAndServe(addr, mux)
}
