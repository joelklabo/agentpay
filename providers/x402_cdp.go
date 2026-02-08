package providers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joelklabo/agentpay/router"
)

// CDPProvider handles x402 (USDC) payments via Coinbase Developer Platform wallets.
// Unlike X402Provider which uses AgentWallet, this signs EIP-712 typed data
// directly through the CDP API for EIP-3009 TransferWithAuthorization.
type CDPProvider struct {
	apiKeyID     string // format: "organizations/{org-id}/apiKeys/{key-id}"
	apiKeySecret string // Ed25519 or ECDSA private key (base64)
	walletSecret string // separate wallet-level secret
	apiBaseURL   string
	address      string // CDP-managed wallet address
	client       *http.Client
}

// NewCDPProvider creates a new x402 payment provider backed by CDP wallets.
// Requires CDP API credentials from portal.cdp.coinbase.com.
func NewCDPProvider(apiKeyID, apiKeySecret, walletSecret string) *CDPProvider {
	return &CDPProvider{
		apiKeyID:     apiKeyID,
		apiKeySecret: apiKeySecret,
		walletSecret: walletSecret,
		apiBaseURL:   "https://api.cdp.coinbase.com",
		client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *CDPProvider) Protocol() router.Protocol {
	return router.ProtocolX402
}

// Init creates or retrieves the CDP EVM account. Must be called before Pay.
func (p *CDPProvider) Init(ctx context.Context, walletName string) error {
	// Try to get existing account
	path := fmt.Sprintf("/platform/v2/evm/accounts?name=%s", walletName)
	resp, err := p.cdpRequest(ctx, "GET", path, nil)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}

	var listResp struct {
		Accounts []struct {
			Address string `json:"address"`
			Name    string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(resp, &listResp); err == nil && len(listResp.Accounts) > 0 {
		for _, a := range listResp.Accounts {
			if a.Name == walletName {
				p.address = a.Address
				return nil
			}
		}
	}

	// Create new account
	createBody := map[string]string{"name": walletName}
	bodyBytes, _ := json.Marshal(createBody)
	createResp, err := p.cdpRequest(ctx, "POST", "/platform/v2/evm/accounts", bodyBytes)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	var created struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(createResp, &created); err != nil {
		return fmt.Errorf("parse create response: %w", err)
	}
	p.address = created.Address
	return nil
}

// Address returns the CDP wallet address.
func (p *CDPProvider) Address() string {
	return p.address
}

// RequestFaucet requests testnet tokens from CDP faucet.
func (p *CDPProvider) RequestFaucet(ctx context.Context, network, token string) error {
	body := map[string]string{
		"address": p.address,
		"network": network,
		"token":   token,
	}
	bodyBytes, _ := json.Marshal(body)
	_, err := p.cdpRequest(ctx, "POST", "/platform/v2/evm/faucet", bodyBytes)
	return err
}

func (p *CDPProvider) EstimateCost(req *router.PaymentRequirement) (float64, string, error) {
	if req.X402Requirement == nil || len(req.X402Requirement.Accepts) == 0 {
		return 0, "", fmt.Errorf("no x402 payment options")
	}

	var cheapest *router.X402Accept
	var cheapestUSD float64 = math.MaxFloat64

	for i := range req.X402Requirement.Accepts {
		opt := &req.X402Requirement.Accepts[i]
		amount, err := strconv.ParseFloat(opt.MaxAmountRequired, 64)
		if err != nil {
			continue
		}
		usd := amount / 1e6 // USDC has 6 decimals
		if usd < cheapestUSD {
			cheapestUSD = usd
			cheapest = opt
		}
	}

	if cheapest == nil {
		return 0, "", fmt.Errorf("no parseable payment amounts")
	}

	desc := fmt.Sprintf("$%.4f USDC on %s (CDP)", cheapestUSD, cheapest.Network)
	return cheapestUSD, desc, nil
}

func (p *CDPProvider) Pay(ctx context.Context, req *router.PaymentRequirement) (string, string, error) {
	if p.address == "" {
		return "", "", fmt.Errorf("CDP provider not initialized — call Init first")
	}
	if req.X402Requirement == nil || len(req.X402Requirement.Accepts) == 0 {
		return "", "", fmt.Errorf("no x402 payment options")
	}

	// Pick the cheapest EVM option
	var accept *router.X402Accept
	for i := range req.X402Requirement.Accepts {
		opt := &req.X402Requirement.Accepts[i]
		if strings.HasPrefix(opt.Network, "eip155:") {
			accept = opt
			break
		}
	}
	if accept == nil {
		return "", "", fmt.Errorf("no EVM payment option found")
	}

	// Build EIP-712 TransferWithAuthorization typed data
	nonce := generateNonce()
	validAfter := "0"
	validBefore := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)

	// Extract chain ID from network (e.g., "eip155:84532" -> 84532)
	chainID := int64(84532) // default Base Sepolia
	if parts := strings.SplitN(accept.Network, ":", 2); len(parts) == 2 {
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			chainID = id
		}
	}

	typedData := map[string]interface{}{
		"domain": map[string]interface{}{
			"name":              "USD Coin",
			"version":           "2",
			"chainId":           chainID,
			"verifyingContract": accept.Asset,
		},
		"types": map[string]interface{}{
			"EIP712Domain": []map[string]string{
				{"name": "name", "type": "string"},
				{"name": "version", "type": "string"},
				{"name": "chainId", "type": "uint256"},
				{"name": "verifyingContract", "type": "address"},
			},
			"TransferWithAuthorization": []map[string]string{
				{"name": "from", "type": "address"},
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "validAfter", "type": "uint256"},
				{"name": "validBefore", "type": "uint256"},
				{"name": "nonce", "type": "bytes32"},
			},
		},
		"primaryType": "TransferWithAuthorization",
		"message": map[string]interface{}{
			"from":        p.address,
			"to":          accept.PayTo,
			"value":       accept.MaxAmountRequired,
			"validAfter":  validAfter,
			"validBefore": validBefore,
			"nonce":       nonce,
		},
	}

	bodyBytes, err := json.Marshal(typedData)
	if err != nil {
		return "", "", fmt.Errorf("marshal typed data: %w", err)
	}

	// Sign via CDP API
	path := fmt.Sprintf("/platform/v2/evm/accounts/%s/sign/typed-data", p.address)
	sigResp, err := p.cdpRequest(ctx, "POST", path, bodyBytes)
	if err != nil {
		return "", "", fmt.Errorf("CDP sign: %w", err)
	}

	var sigResult struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(sigResp, &sigResult); err != nil {
		return "", "", fmt.Errorf("parse signature: %w", err)
	}

	// Build the x402 payment payload
	payment := map[string]interface{}{
		"x402Version": 1,
		"scheme":      accept.Scheme,
		"network":     accept.Network,
		"payload": map[string]interface{}{
			"signature":  sigResult.Signature,
			"from":       p.address,
			"to":         accept.PayTo,
			"value":      accept.MaxAmountRequired,
			"validAfter": validAfter,
			"validBefore": validBefore,
			"nonce":      nonce,
		},
	}

	paymentBytes, err := json.Marshal(payment)
	if err != nil {
		return "", "", fmt.Errorf("marshal payment: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(paymentBytes)
	return "Payment", encoded, nil
}

// cdpRequest makes an authenticated request to the CDP API.
func (p *CDPProvider) cdpRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	url := p.apiBaseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Generate Bearer JWT
	bearer, err := p.generateBearerJWT(method, path)
	if err != nil {
		return nil, fmt.Errorf("generate bearer: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	// For signing operations, add wallet auth
	if body != nil && strings.Contains(path, "/sign/") {
		walletAuth, err := p.generateWalletAuthJWT(method, path, body)
		if err != nil {
			return nil, fmt.Errorf("generate wallet auth: %w", err)
		}
		req.Header.Set("X-Wallet-Auth", walletAuth)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("CDP API %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// generateBearerJWT creates a JWT signed with the API key for the Authorization header.
// This is a simplified JWT — production should use a proper JWT library.
func (p *CDPProvider) generateBearerJWT(method, path string) (string, error) {
	now := time.Now()
	header := map[string]string{
		"alg": "ES256",
		"typ": "JWT",
		"kid": p.apiKeyID,
	}
	payload := map[string]interface{}{
		"sub":    p.apiKeyID,
		"iss":    "cdp",
		"aud":    []string{"cdp_service"},
		"nbf":    now.Unix(),
		"exp":    now.Add(2 * time.Minute).Unix(),
		"uris":   []string{fmt.Sprintf("%s %s%s", method, p.apiBaseURL, path)},
	}

	headerBytes, _ := json.Marshal(header)
	payloadBytes, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := headerB64 + "." + payloadB64

	// Sign with the API key secret (ECDSA P-256)
	sig, err := signES256(p.apiKeySecret, []byte(signingInput))
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// generateWalletAuthJWT creates a JWT for the X-Wallet-Auth header.
func (p *CDPProvider) generateWalletAuthJWT(method, path string, body []byte) (string, error) {
	now := time.Now()

	// Hash the canonicalized body
	canonBody := canonicalizeJSON(body)
	bodyHash := sha256.Sum256(canonBody)

	header := map[string]string{
		"alg": "ES256",
		"typ": "JWT",
	}
	payload := map[string]interface{}{
		"sub":       p.apiKeyID,
		"iss":       "cdp",
		"aud":       []string{"cdp_service"},
		"nbf":       now.Unix(),
		"exp":       now.Add(1 * time.Minute).Unix(),
		"uris":      []string{fmt.Sprintf("%s %s%s", method, p.apiBaseURL, path)},
		"body_hash": base64.RawURLEncoding.EncodeToString(bodyHash[:]),
	}

	headerBytes, _ := json.Marshal(header)
	payloadBytes, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := headerB64 + "." + payloadB64

	sig, err := signES256(p.walletSecret, []byte(signingInput))
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// signES256 signs data with an ECDSA P-256 key (base64 or PEM encoded).
func signES256(keyBase64 string, data []byte) ([]byte, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		// Try raw URL-safe base64
		keyBytes, err = base64.RawURLEncoding.DecodeString(keyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode key: %w", err)
		}
	}

	// Parse as ECDSA private key (raw 32-byte scalar)
	key := new(ecdsa.PrivateKey)
	key.Curve = elliptic.P256()
	key.D = new(big.Int).SetBytes(keyBytes)
	key.PublicKey.X, key.PublicKey.Y = key.Curve.ScalarBaseMult(keyBytes)

	hash := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// IEEE P1363 format (r || s, each 32 bytes)
	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	return sig, nil
}

// canonicalizeJSON sorts object keys for deterministic hashing.
func canonicalizeJSON(data []byte) []byte {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return data // fallback to original
	}
	canonical := canonicalize(obj)
	result, err := json.Marshal(canonical)
	if err != nil {
		return data
	}
	return result
}

func canonicalize(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		sorted := make(map[string]interface{})
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sorted[k] = canonicalize(val[k])
		}
		return sorted
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = canonicalize(item)
		}
		return result
	default:
		return v
	}
}

// generateNonce creates a random 32-byte nonce as hex string.
func generateNonce() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based
		return fmt.Sprintf("0x%064x", time.Now().UnixNano())
	}
	return fmt.Sprintf("0x%x", b)
}
