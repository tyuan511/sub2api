package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RoutingArtifactStrategy = "strategy"
	RoutingArtifactScore    = "score"
	RoutingArtifactFeature  = "feature"
	RoutingArtifactModel    = "model"

	RoutingLifecycleDraft   = "draft"
	RoutingLifecycleShadow  = "shadow"
	RoutingLifecycleCanary  = "canary"
	RoutingLifecycleActive  = "active"
	RoutingLifecyclePaused  = "paused"
	RoutingLifecycleRetired = "retired"
)

var (
	ErrRoutingArtifactInvalid      = errors.New("routing artifact is invalid")
	ErrRoutingArtifactNotFound     = errors.New("routing artifact not found")
	ErrRoutingLifecycleConflict    = errors.New("routing artifact lifecycle conflict")
	ErrRoutingArtifactPointerStale = errors.New("routing artifact pointer is stale")
	ErrRoutingPromotionEvidence    = errors.New("routing promotion evidence is insufficient")
)

type RoutingArtifactVersion struct {
	ID            int64           `json:"id"`
	ArtifactKind  string          `json:"artifact_kind"`
	Version       string          `json:"version"`
	ParentVersion *string         `json:"parent_version,omitempty"`
	Platform      string          `json:"platform"`
	ModelFamily   string          `json:"model_family"`
	EndpointKind  string          `json:"endpoint_kind"`
	Preference    *string         `json:"preference,omitempty"`
	Status        string          `json:"status"`
	SchemaVersion string          `json:"schema_version"`
	Checksum      string          `json:"checksum"`
	Payload       json.RawMessage `json:"payload"`
	Dependencies  json.RawMessage `json:"dependencies"`
	Lineage       json.RawMessage `json:"lineage"`
	CreatedBy     *int64          `json:"created_by,omitempty"`
	ActivatedAt   *time.Time      `json:"activated_at,omitempty"`
	RetiredAt     *time.Time      `json:"retired_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type RoutingExperiment struct {
	ID                       int64           `json:"id"`
	ExperimentKey            string          `json:"experiment_key"`
	Platform                 string          `json:"platform"`
	ModelFamily              string          `json:"model_family"`
	EndpointKind             string          `json:"endpoint_kind"`
	Preference               string          `json:"preference"`
	BaselineStrategyVersion  string          `json:"baseline_strategy_version"`
	CandidateStrategyVersion string          `json:"candidate_strategy_version"`
	Status                   string          `json:"status"`
	AllocationBPS            int             `json:"allocation_bps"`
	BucketSaltChecksum       string          `json:"bucket_salt_checksum"`
	Guardrails               json.RawMessage `json:"guardrails"`
	OfflineReplay            json.RawMessage `json:"offline_replay"`
	LastEvaluation           json.RawMessage `json:"last_evaluation"`
	LastEvaluatedAt          *time.Time      `json:"last_evaluated_at,omitempty"`
	StartedAt                *time.Time      `json:"started_at,omitempty"`
	StoppedAt                *time.Time      `json:"stopped_at,omitempty"`
	StopReason               *string         `json:"stop_reason,omitempty"`
	ApprovedBy               *int64          `json:"approved_by,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

type RoutingOptimizationRepository interface {
	RoutingFactRepository
	CreateArtifact(ctx context.Context, artifact *RoutingArtifactVersion) error
	GetArtifact(ctx context.Context, kind, version string) (*RoutingArtifactVersion, error)
	ListArtifacts(ctx context.Context, kind, status string, limit int) ([]*RoutingArtifactVersion, error)
	TransitionArtifact(ctx context.Context, id int64, fromStatus, toStatus string, at time.Time) error
	PromoteArtifact(ctx context.Context, id int64, fromStatus, toStatus string, expectedActiveVersion *string, at time.Time) error
	CreateExperiment(ctx context.Context, experiment *RoutingExperiment) error
	GetExperiment(ctx context.Context, experimentKey string) (*RoutingExperiment, error)
	ListExperiments(ctx context.Context, status string, limit int) ([]*RoutingExperiment, error)
	TransitionExperiment(ctx context.Context, id int64, fromStatus, toStatus string, approvedBy *int64, stopReason *string, at time.Time) error
	UpdateExperimentEvidence(ctx context.Context, id int64, expectedStatus string, offlineReplay, evaluation json.RawMessage, at time.Time) error
	LoadCanaryMetrics(ctx context.Context, experimentKey, strategyVersion string, since time.Time) (RoutingCanaryMetrics, error)
}

func ValidateRoutingArtifact(artifact *RoutingArtifactVersion) error {
	if artifact == nil {
		return fmt.Errorf("%w: nil artifact", ErrRoutingArtifactInvalid)
	}
	if !oneOf(artifact.ArtifactKind, RoutingArtifactStrategy, RoutingArtifactScore, RoutingArtifactFeature, RoutingArtifactModel) {
		return fmt.Errorf("%w: unsupported artifact kind", ErrRoutingArtifactInvalid)
	}
	if strings.TrimSpace(artifact.Version) == "" || strings.TrimSpace(artifact.SchemaVersion) == "" {
		return fmt.Errorf("%w: version and schema version are required", ErrRoutingArtifactInvalid)
	}
	if !(APIKeyRoutingScoreScope{Platform: artifact.Platform, ModelFamily: artifact.ModelFamily, EndpointKind: artifact.EndpointKind}).Valid() {
		return fmt.Errorf("%w: invalid scope", ErrRoutingArtifactInvalid)
	}
	if artifact.Preference != nil && !oneOf(*artifact.Preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) {
		return fmt.Errorf("%w: invalid preference", ErrRoutingArtifactInvalid)
	}
	if !oneOf(artifact.Status, RoutingLifecycleDraft, RoutingLifecycleShadow, RoutingLifecycleCanary, RoutingLifecycleActive, RoutingLifecyclePaused, RoutingLifecycleRetired) {
		return fmt.Errorf("%w: invalid lifecycle status", ErrRoutingArtifactInvalid)
	}
	if !jsonObject(artifact.Payload) || !jsonArray(artifact.Dependencies) || !jsonObject(artifact.Lineage) {
		return fmt.Errorf("%w: payload, dependencies, or lineage schema mismatch", ErrRoutingArtifactInvalid)
	}
	sum := sha256.Sum256(artifact.Payload)
	if !strings.EqualFold(strings.TrimSpace(artifact.Checksum), hex.EncodeToString(sum[:])) {
		return fmt.Errorf("%w: payload checksum mismatch", ErrRoutingArtifactInvalid)
	}
	if artifact.ArtifactKind == RoutingArtifactStrategy {
		if _, err := ParseAPIKeyRoutingStrategyArtifact(artifact); err != nil {
			return err
		}
	}
	if artifact.ArtifactKind == RoutingArtifactFeature {
		if _, err := ParseAPIKeyRoutingPersonalizationArtifact(artifact); err != nil {
			return err
		}
	}
	if artifact.ArtifactKind == RoutingArtifactModel {
		if _, err := ParseAPIKeyRoutingPredictionModel(artifact); err != nil {
			return err
		}
	}
	return nil
}

func ValidateRoutingLifecycleTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		RoutingLifecycleDraft:   {RoutingLifecycleShadow: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleShadow:  {RoutingLifecycleCanary: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleCanary:  {RoutingLifecycleActive: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleActive:  {RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecyclePaused:  {RoutingLifecycleShadow: true, RoutingLifecycleCanary: true, RoutingLifecycleActive: true, RoutingLifecycleRetired: true},
		RoutingLifecycleRetired: {},
	}
	if !allowed[from][to] {
		return fmt.Errorf("%w: %s -> %s", ErrRoutingLifecycleConflict, from, to)
	}
	return nil
}

func ValidateRoutingExperiment(experiment *RoutingExperiment) error {
	if experiment == nil || strings.TrimSpace(experiment.ExperimentKey) == "" {
		return fmt.Errorf("%w: experiment key is required", ErrRoutingArtifactInvalid)
	}
	if !(APIKeyRoutingScoreScope{Platform: experiment.Platform, ModelFamily: experiment.ModelFamily, EndpointKind: experiment.EndpointKind}).Valid() {
		return fmt.Errorf("%w: invalid experiment scope", ErrRoutingArtifactInvalid)
	}
	if !oneOf(experiment.Preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) ||
		!oneOf(experiment.Status, RoutingLifecycleDraft, RoutingLifecycleShadow, RoutingLifecycleCanary, RoutingLifecycleActive, RoutingLifecyclePaused, RoutingLifecycleRetired) ||
		experiment.BaselineStrategyVersion == experiment.CandidateStrategyVersion ||
		strings.TrimSpace(experiment.BaselineStrategyVersion) == "" || strings.TrimSpace(experiment.CandidateStrategyVersion) == "" ||
		experiment.AllocationBPS < 0 || experiment.AllocationBPS > 10000 || !jsonObject(experiment.Guardrails) {
		return fmt.Errorf("%w: invalid experiment contract", ErrRoutingArtifactInvalid)
	}
	if (len(experiment.OfflineReplay) > 0 && !jsonObject(experiment.OfflineReplay)) ||
		(len(experiment.LastEvaluation) > 0 && !jsonObject(experiment.LastEvaluation)) {
		return fmt.Errorf("%w: experiment evidence must be objects", ErrRoutingArtifactInvalid)
	}
	checksum, err := hex.DecodeString(experiment.BucketSaltChecksum)
	if err != nil || len(checksum) != 32 {
		return fmt.Errorf("%w: bucket salt checksum must be sha256", ErrRoutingArtifactInvalid)
	}
	if _, err := ParseRoutingCanaryGuardrails(experiment.Guardrails); err != nil {
		return err
	}
	return nil
}

func ValidateRoutingExperimentTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		RoutingLifecycleDraft:   {RoutingLifecycleShadow: true, RoutingLifecycleCanary: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleShadow:  {RoutingLifecycleCanary: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleCanary:  {RoutingLifecycleActive: true, RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecycleActive:  {RoutingLifecyclePaused: true, RoutingLifecycleRetired: true},
		RoutingLifecyclePaused:  {RoutingLifecycleShadow: true, RoutingLifecycleCanary: true, RoutingLifecycleRetired: true},
		RoutingLifecycleRetired: {},
	}
	if !allowed[from][to] {
		return fmt.Errorf("%w: experiment %s -> %s", ErrRoutingLifecycleConflict, from, to)
	}
	return nil
}

// StableRoutingExperimentBucket never uses API key plaintext. Existing sticky
// sessions are filtered by the caller before a candidate strategy is selected.
func StableRoutingExperimentBucket(userID, apiKeyID int64, salt []byte) int {
	hash := sha256.New()
	_, _ = hash.Write(salt)
	var ids [16]byte
	binary.BigEndian.PutUint64(ids[:8], uint64(userID))
	binary.BigEndian.PutUint64(ids[8:], uint64(apiKeyID))
	_, _ = hash.Write(ids[:])
	sum := hash.Sum(nil)
	return int(binary.BigEndian.Uint64(sum[:8]) % 10000)
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func jsonObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var decoded map[string]any
	return json.Unmarshal(value, &decoded) == nil && decoded != nil
}

func jsonArray(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var decoded []any
	return json.Unmarshal(value, &decoded) == nil && decoded != nil
}
