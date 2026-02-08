package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SolanaProvider handles direct Solana SPL token payments via AgentWallet.
// This covers cases where a service accepts direct Solana payments rather
// than using the x402 protocol.
type SolanaProvider struct {
	apiBase  string
	username string
	token    string
	network  string // "mainnet" or "devnet"
	client   *http.Client
}

// NewSolanaProvider creates a Solana payment provider.
func NewSolanaProvider(apiBase, username, token, network string) *SolanaProvider {
	return &SolanaProvider{
		apiBase:  apiBase,
		username: username,
		token:    token,
		network:  network,
		client:   &http.Client{},
	}
}

// TransferUSDC sends USDC on Solana to a recipient address.
func (p *SolanaProvider) TransferUSDC(ctx context.Context, to string, amountMicroUSDC string) (string, error) {
	url := fmt.Sprintf("%s/api/wallets/%s/actions/transfer-solana", p.apiBase, p.username)

	payload := map[string]string{
		"to":      to,
		"amount":  amountMicroUSDC,
		"asset":   "usdc",
		"network": p.network,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal transfer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build transfer request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transfer request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("transfer HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ActionID string `json:"actionId"`
		Status   string `json:"status"`
		TxHash   string `json:"txHash"`
		Explorer string `json:"explorer"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse transfer response: %w", err)
	}

	return result.TxHash, nil
}

// GetBalance returns the Solana wallet balances.
func (p *SolanaProvider) GetBalance(ctx context.Context) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/wallets/%s/balances", p.apiBase, p.username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var balances map[string]interface{}
	json.Unmarshal(body, &balances)
	return balances, nil
}

// RequestDevnetSOL requests free devnet SOL from the AgentWallet faucet.
func (p *SolanaProvider) RequestDevnetSOL(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/api/wallets/%s/actions/faucet-sol", p.apiBase, p.username)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		TxHash  string `json:"txHash"`
		Amount  string `json:"amount"`
		Status  string `json:"status"`
	}
	json.Unmarshal(body, &result)
	return result.TxHash, nil
}

// SignMessage signs a message using the Solana wallet.
func (p *SolanaProvider) SignMessage(ctx context.Context, message string) (string, error) {
	url := fmt.Sprintf("%s/api/wallets/%s/actions/sign-message", p.apiBase, p.username)

	payload := map[string]string{
		"chain":   "solana",
		"message": message,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Signature string `json:"signature"`
	}
	json.Unmarshal(respBody, &result)
	return result.Signature, nil
}
