package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/joelklabo/agentpay/providers"
	"github.com/spf13/cobra"
)

var cdpCmd = &cobra.Command{
	Use:   "cdp",
	Short: "Manage CDP wallet for x402 payments",
	Long:  `Initialize and manage a Coinbase Developer Platform wallet for making x402 (USDC) payments.`,
}

var cdpInitCmd = &cobra.Command{
	Use:   "init [wallet-name]",
	Short: "Initialize or retrieve a CDP wallet",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		walletName := "agentpay"
		if len(args) > 0 {
			walletName = args[0]
		}

		p, err := newCDPProvider()
		if err != nil {
			return err
		}

		if err := p.Init(context.Background(), walletName); err != nil {
			return fmt.Errorf("init CDP wallet: %w", err)
		}

		result := map[string]string{
			"wallet_name": walletName,
			"address":     p.Address(),
			"status":      "ready",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

var cdpFaucetCmd = &cobra.Command{
	Use:   "faucet",
	Short: "Request testnet tokens from CDP faucet",
	RunE: func(cmd *cobra.Command, args []string) error {
		walletName, _ := cmd.Flags().GetString("wallet")
		network, _ := cmd.Flags().GetString("network")
		token, _ := cmd.Flags().GetString("token")

		p, err := newCDPProvider()
		if err != nil {
			return err
		}

		if err := p.Init(context.Background(), walletName); err != nil {
			return fmt.Errorf("init wallet: %w", err)
		}

		fmt.Printf("Requesting %s on %s for %s...\n", token, network, p.Address())
		if err := p.RequestFaucet(context.Background(), network, token); err != nil {
			return fmt.Errorf("faucet request: %w", err)
		}

		fmt.Println("Faucet request submitted successfully.")
		return nil
	},
}

var cdpInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show CDP wallet information",
	RunE: func(cmd *cobra.Command, args []string) error {
		walletName, _ := cmd.Flags().GetString("wallet")

		p, err := newCDPProvider()
		if err != nil {
			return err
		}

		if err := p.Init(context.Background(), walletName); err != nil {
			return fmt.Errorf("init wallet: %w", err)
		}

		result := map[string]string{
			"wallet_name": walletName,
			"address":     p.Address(),
			"provider":    "Coinbase Developer Platform",
			"networks":    "Base Sepolia (eip155:84532)",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

func newCDPProvider() (*providers.CDPProvider, error) {
	apiKeyID := os.Getenv("CDP_API_KEY_ID")
	apiKeySecret := os.Getenv("CDP_API_KEY_SECRET")
	walletSecret := os.Getenv("CDP_WALLET_SECRET")

	if apiKeyID == "" || apiKeySecret == "" || walletSecret == "" {
		return nil, fmt.Errorf("CDP credentials not set. Required environment variables:\n" +
			"  CDP_API_KEY_ID     - from portal.cdp.coinbase.com\n" +
			"  CDP_API_KEY_SECRET - API key private key (base64)\n" +
			"  CDP_WALLET_SECRET  - wallet-level secret")
	}

	return providers.NewCDPProvider(apiKeyID, apiKeySecret, walletSecret), nil
}

func init() {
	cdpFaucetCmd.Flags().String("wallet", "agentpay", "wallet name")
	cdpFaucetCmd.Flags().String("network", "base-sepolia", "network for faucet")
	cdpFaucetCmd.Flags().String("token", "usdc", "token to request (eth or usdc)")

	cdpInfoCmd.Flags().String("wallet", "agentpay", "wallet name")

	cdpCmd.AddCommand(cdpInitCmd)
	cdpCmd.AddCommand(cdpFaucetCmd)
	cdpCmd.AddCommand(cdpInfoCmd)

	rootCmd.AddCommand(cdpCmd)
}
