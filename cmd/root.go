package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agentpay",
	Short: "Cross-protocol payment router for AI agents",
	Long: `AgentPay routes HTTP requests through paid APIs automatically.
It detects x402 (USDC), L402 (Lightning), and Solana SPL payment requirements
and handles settlement transparently. The calling agent never needs to know
which payment rail the target service uses.`,
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(balanceCmd)
	rootCmd.AddCommand(registryCmd)
}

// configPath returns the config file path.
func configPath() string {
	home, _ := os.UserHomeDir()
	p := os.Getenv("AGENTPAY_CONFIG")
	if p != "" {
		return p
	}
	return fmt.Sprintf("%s/.agentpay/config.json", home)
}
