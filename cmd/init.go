package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize AgentPay configuration",
	Long: `Creates the AgentPay config file with your payment provider credentials.

Supports:
  - AgentWallet (x402/USDC on EVM and Solana)
  - LNbits (L402/Lightning Network)
  - Web of Trust scoring for payment safety`,
	RunE: runInit,
}

var (
	initAWUser  string
	initAWToken string
	initAWChain string
	initLNURL   string
	initLNKey   string
	initWoT     bool
	initWoTURL  string
)

func init() {
	initCmd.Flags().StringVar(&initAWUser, "aw-user", "", "AgentWallet username")
	initCmd.Flags().StringVar(&initAWToken, "aw-token", "", "AgentWallet API token")
	initCmd.Flags().StringVar(&initAWChain, "aw-chain", "auto", "Preferred chain: evm, solana, auto")
	initCmd.Flags().StringVar(&initLNURL, "lnbits-url", "", "LNbits URL")
	initCmd.Flags().StringVar(&initLNKey, "lnbits-key", "", "LNbits admin key")
	initCmd.Flags().BoolVar(&initWoT, "wot", false, "Enable WoT trust scoring")
	initCmd.Flags().StringVar(&initWoTURL, "wot-url", "https://maximumsats.joel-dfd.workers.dev/wot/score", "WoT API endpoint")
}

func runInit(cmd *cobra.Command, args []string) error {
	cfg := &AppConfig{
		AgentWallet: AgentWalletConfig{
			APIBase:        "https://agentwallet.mcpay.tech",
			Username:       initAWUser,
			Token:          initAWToken,
			PreferredChain: initAWChain,
		},
		LNbits: LNbitsConfig{
			URL:      initLNURL,
			AdminKey: initLNKey,
		},
		WoT: WoTConfig{
			Enabled:  initWoT,
			Endpoint: initWoTURL,
		},
		Budget: BudgetConfig{
			MaxPerRequestUSD: 1.0,
			MaxSessionUSD:    10.0,
		},
	}

	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Config saved to %s\n\n", configPath())
	fmt.Println("Configured providers:")
	if cfg.AgentWallet.Username != "" {
		fmt.Printf("  x402: AgentWallet (%s) on %s\n", cfg.AgentWallet.Username, cfg.AgentWallet.PreferredChain)
	}
	if cfg.LNbits.URL != "" {
		fmt.Printf("  L402: LNbits (%s)\n", cfg.LNbits.URL)
	}
	if cfg.WoT.Enabled {
		fmt.Printf("  WoT:  %s\n", cfg.WoT.Endpoint)
	}

	return nil
}
