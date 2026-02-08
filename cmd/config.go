package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig holds all configuration for AgentPay.
type AppConfig struct {
	AgentWallet AgentWalletConfig `json:"agent_wallet"`
	LNbits      LNbitsConfig      `json:"lnbits"`
	WoT         WoTConfig         `json:"wot"`
	Budget      BudgetConfig      `json:"budget"`
}

// AgentWalletConfig holds AgentWallet (x402/Solana) settings.
type AgentWalletConfig struct {
	APIBase        string `json:"api_base"`
	Username       string `json:"username"`
	Token          string `json:"token"`
	PreferredChain string `json:"preferred_chain"` // "evm", "solana", "auto"
}

// LNbitsConfig holds LNbits (Lightning/L402) settings.
type LNbitsConfig struct {
	URL      string `json:"url"`
	AdminKey string `json:"admin_key"`
}

// WoTConfig holds Web of Trust scoring settings.
type WoTConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"` // WoT API endpoint
}

// BudgetConfig holds spending limits.
type BudgetConfig struct {
	MaxPerRequestUSD float64 `json:"max_per_request_usd"`
	MaxSessionUSD    float64 `json:"max_session_usd"`
}

func loadConfig() (*AppConfig, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config at %s: %w", path, err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Set defaults
	if cfg.AgentWallet.APIBase == "" {
		cfg.AgentWallet.APIBase = "https://agentwallet.mcpay.tech"
	}
	if cfg.Budget.MaxPerRequestUSD == 0 {
		cfg.Budget.MaxPerRequestUSD = 1.0
	}
	if cfg.Budget.MaxSessionUSD == 0 {
		cfg.Budget.MaxSessionUSD = 10.0
	}

	return &cfg, nil
}

func saveConfig(cfg *AppConfig) error {
	path := configPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
