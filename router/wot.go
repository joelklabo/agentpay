package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WoTChecker checks trust scores before allowing payments.
type WoTChecker struct {
	endpoint string
	client   *http.Client
	// MinScore is the minimum trust score (0-1) required for payments.
	MinScore float64
	// ThresholdUSD is the USD amount above which WoT check is required.
	ThresholdUSD float64
}

// WoTScore represents a trust score result.
type WoTScore struct {
	Pubkey string  `json:"pubkey"`
	Score  float64 `json:"score"`
	Rank   int     `json:"rank,omitempty"`
}

// NewWoTChecker creates a WoT trust checker.
func NewWoTChecker(endpoint string) *WoTChecker {
	return &WoTChecker{
		endpoint:     endpoint,
		client:       &http.Client{Timeout: 5 * time.Second},
		MinScore:     0.001,    // minimum trust score
		ThresholdUSD: 0.10,     // require WoT check above $0.10
	}
}

// CheckTrust verifies the trust score for a payment recipient.
// Returns nil if trusted or below threshold, error if untrusted.
func (w *WoTChecker) CheckTrust(recipientID string, usdAmount float64) error {
	if usdAmount < w.ThresholdUSD {
		return nil // small payments skip trust check
	}

	score, err := w.GetScore(recipientID)
	if err != nil {
		// If WoT service is unavailable, allow payment with warning
		return nil
	}

	if score.Score < w.MinScore {
		return fmt.Errorf("recipient %s has low trust score (%.6f < %.6f minimum)",
			recipientID, score.Score, w.MinScore)
	}

	return nil
}

// GetScore fetches the trust score for an identifier.
func (w *WoTChecker) GetScore(id string) (*WoTScore, error) {
	url := fmt.Sprintf("%s?pubkey=%s", w.endpoint, id)
	resp, err := w.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("wot request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("wot HTTP %d: %s", resp.StatusCode, string(body))
	}

	var score WoTScore
	if err := json.Unmarshal(body, &score); err != nil {
		return nil, fmt.Errorf("parse wot score: %w", err)
	}

	return &score, nil
}
