package domain

import "time"

// BazaarLinkProbeResult is the redacted result of one BazaarLink identity
// probe. Credentials and raw probe responses are deliberately not persisted.
type BazaarLinkProbeResult struct {
	RunID               string    `json:"run_id,omitempty"`
	Status              string    `json:"status,omitempty"`
	Score               *int      `json:"score,omitempty"`
	IdentityStatus      string    `json:"identity_status,omitempty"`
	Confidence          *float64  `json:"confidence,omitempty"`
	ClaimedModel        string    `json:"claimed_model,omitempty"`
	PredictedFamily     string    `json:"predicted_family,omitempty"`
	PredictedModel      string    `json:"predicted_model,omitempty"`
	PredictedModelScore *float64  `json:"predicted_model_score,omitempty"`
	RiskFlags           []string  `json:"risk_flags,omitempty"`
	TotalInputTokens    *int      `json:"total_input_tokens,omitempty"`
	TotalOutputTokens   *int      `json:"total_output_tokens,omitempty"`
	Error               string    `json:"error,omitempty"`
	LatencyMs           *int      `json:"latency_ms,omitempty"`
	CheckedAt           time.Time `json:"checked_at"`
}

// BazaarLinkProbeTask is the durable local state for an asynchronous
// BazaarLink run. The remote API only exposes a run ID, so the monitor service
// keeps the monitor association and expiry locally while it polls.
type BazaarLinkProbeTask struct {
	ID          int64
	MonitorID   int64
	RunID       string
	Status      string
	Result      *BazaarLinkProbeResult
	SubmittedAt time.Time
	ExpiresAt   time.Time
	NextPollAt  time.Time
}
