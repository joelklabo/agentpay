package cmd

import (
	"context"
	"fmt"

	"github.com/joelklabo/agentpay/providers"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(solanaCmd)
}

var solanaCmd = &cobra.Command{
	Use:   "solana",
	Short: "Solana wallet operations via AgentWallet",
}

var solanaBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Show Solana wallet balance",
	RunE:  runSolanaBalance,
}

var solanaFaucetCmd = &cobra.Command{
	Use:   "faucet",
	Short: "Request devnet SOL from AgentWallet faucet",
	RunE:  runSolanaFaucet,
}

var solanaTransferCmd = &cobra.Command{
	Use:   "transfer <to> <amount>",
	Short: "Transfer USDC on Solana",
	Args:  cobra.ExactArgs(2),
	RunE:  runSolanaTransfer,
}

var solanaSignCmd = &cobra.Command{
	Use:   "sign <message>",
	Short: "Sign a message with your Solana wallet",
	Args:  cobra.ExactArgs(1),
	RunE:  runSolanaSign,
}

var solanaNetwork string

func init() {
	solanaCmd.AddCommand(solanaBalanceCmd)
	solanaCmd.AddCommand(solanaFaucetCmd)
	solanaCmd.AddCommand(solanaTransferCmd)
	solanaCmd.AddCommand(solanaSignCmd)

	solanaCmd.PersistentFlags().StringVar(&solanaNetwork, "network", "devnet", "Solana network: mainnet or devnet")
}

func newSolanaProvider(cfg *AppConfig) *providers.SolanaProvider {
	return providers.NewSolanaProvider(
		cfg.AgentWallet.APIBase,
		cfg.AgentWallet.Username,
		cfg.AgentWallet.Token,
		solanaNetwork,
	)
}

func runSolanaBalance(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sp := newSolanaProvider(cfg)
	ctx := context.Background()
	balances, err := sp.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("get balance: %w", err)
	}

	fmt.Println("Solana Wallet Balances")
	fmt.Println("======================")
	for k, v := range balances {
		fmt.Printf("  %s: %v\n", k, v)
	}
	return nil
}

func runSolanaFaucet(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sp := newSolanaProvider(cfg)
	ctx := context.Background()
	txHash, err := sp.RequestDevnetSOL(ctx)
	if err != nil {
		return fmt.Errorf("faucet: %w", err)
	}

	fmt.Printf("Devnet SOL requested. TX: %s\n", txHash)
	return nil
}

func runSolanaTransfer(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sp := newSolanaProvider(cfg)
	ctx := context.Background()
	txHash, err := sp.TransferUSDC(ctx, args[0], args[1])
	if err != nil {
		return fmt.Errorf("transfer: %w", err)
	}

	fmt.Printf("USDC transferred on Solana (%s). TX: %s\n", solanaNetwork, txHash)
	return nil
}

func runSolanaSign(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sp := newSolanaProvider(cfg)
	ctx := context.Background()
	sig, err := sp.SignMessage(ctx, args[0])
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	fmt.Printf("Signature: %s\n", sig)
	return nil
}
