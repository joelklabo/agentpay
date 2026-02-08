package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var balanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Show wallet balances across all payment rails",
	RunE:  runBalance,
}

func runBalance(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Println("AgentPay Wallet Balances")
	fmt.Println("========================")

	// Check AgentWallet balances
	if cfg.AgentWallet.Username != "" {
		balURL := fmt.Sprintf("%s/api/wallets/%s/balances",
			cfg.AgentWallet.APIBase, cfg.AgentWallet.Username)
		req, _ := http.NewRequest("GET", balURL, nil)
		req.Header.Set("Authorization", "Bearer "+cfg.AgentWallet.Token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("\nx402 (AgentWallet): ERROR - %v\n", err)
		} else {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var balances interface{}
			json.Unmarshal(body, &balances)
			prettyJSON, _ := json.MarshalIndent(balances, "  ", "  ")
			fmt.Printf("\nx402 (AgentWallet - %s):\n  %s\n", cfg.AgentWallet.Username, string(prettyJSON))
		}
	} else {
		fmt.Println("\nx402: not configured")
	}

	// Check LNbits balance
	if cfg.LNbits.URL != "" {
		walURL := fmt.Sprintf("%s/api/v1/wallet", cfg.LNbits.URL)
		req, _ := http.NewRequest("GET", walURL, nil)
		req.Header.Set("X-Api-Key", cfg.LNbits.AdminKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Printf("\nL402 (LNbits): ERROR - %v\n", err)
		} else {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var wallet struct {
				Name    string `json:"name"`
				Balance int64  `json:"balance"`
			}
			json.Unmarshal(body, &wallet)
			sats := wallet.Balance / 1000 // msats to sats
			fmt.Printf("\nL402 (LNbits - %s): %d sats\n", wallet.Name, sats)
		}
	} else {
		fmt.Println("\nL402: not configured")
	}

	return nil
}
