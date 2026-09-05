package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrRoutingArtifactPointerConflict = errors.New("routing artifact pointer conflict")
	ErrRoutingArtifactUnavailable     = errors.New("routing artifact unavailable")
)

type RoutingArtifactScope struct {
	ArtifactKind string
	Platform     string
	ModelFamily  string
	EndpointKind string
	Preference   *string
}

type RoutingArtifactPointers struct {
	BaselineVersion          string    `json:"baseline_version"`
	ActiveVersion            string    `json:"active_version"`
	CanaryVersion            string    `json:"canary_version,omitempty"`
	CanaryAllocationBPS      int       `json:"canary_allocation_bps,omitempty"`
	CanaryExperimentID       string    `json:"canary_experiment_id,omitempty"`
	CanaryBucketSaltChecksum string    `json:"canary_bucket_salt_checksum,omitempty"`
	ShadowVersion            string    `json:"shadow_version,omitempty"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type RoutingArtifactCache interface {
	PublishArtifact(ctx context.Context, artifact *RoutingArtifactVersion) error
	SwapPointers(ctx context.Context, scope RoutingArtifactScope, pointers RoutingArtifactPointers, expectedActive *string) error
	LoadPointers(ctx context.Context, scope RoutingArtifactScope) (RoutingArtifactPointers, error)
	LoadArtifact(ctx context.Context, scope RoutingArtifactScope, version string) (*RoutingArtifactVersion, error)
}

func (s RoutingArtifactScope) Validate() error {
	if !oneOf(s.ArtifactKind, RoutingArtifactStrategy, RoutingArtifactScore, RoutingArtifactFeature, RoutingArtifactModel) ||
		!(APIKeyRoutingScoreScope{Platform: s.Platform, ModelFamily: s.ModelFamily, EndpointKind: s.EndpointKind}).Valid() {
		return ErrRoutingArtifactInvalid
	}
	if s.Preference != nil && !oneOf(*s.Preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) {
		return ErrRoutingArtifactInvalid
	}
	return nil
}

func RoutingArtifactScopeFromVersion(artifact *RoutingArtifactVersion) RoutingArtifactScope {
	if artifact == nil {
		return RoutingArtifactScope{}
	}
	return RoutingArtifactScope{
		ArtifactKind: artifact.ArtifactKind, Platform: artifact.Platform, ModelFamily: artifact.ModelFamily,
		EndpointKind: artifact.EndpointKind, Preference: cloneStringPtr(artifact.Preference),
	}
}

func ValidateRoutingArtifactPointers(pointers RoutingArtifactPointers) error {
	if strings.TrimSpace(pointers.BaselineVersion) == "" || strings.TrimSpace(pointers.ActiveVersion) == "" {
		return fmt.Errorf("%w: baseline and active versions are required", ErrRoutingArtifactInvalid)
	}
	if pointers.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: pointer timestamp is required", ErrRoutingArtifactInvalid)
	}
	if pointers.CanaryVersion == "" {
		if pointers.CanaryAllocationBPS != 0 || pointers.CanaryExperimentID != "" || pointers.CanaryBucketSaltChecksum != "" {
			return fmt.Errorf("%w: canary metadata without a canary version", ErrRoutingArtifactInvalid)
		}
	} else {
		checksum, err := hex.DecodeString(pointers.CanaryBucketSaltChecksum)
		if pointers.CanaryAllocationBPS < 1 || pointers.CanaryAllocationBPS > 10000 ||
			strings.TrimSpace(pointers.CanaryExperimentID) == "" || len(pointers.CanaryExperimentID) > 200 ||
			err != nil || len(checksum) != 32 {
			return fmt.Errorf("%w: invalid canary assignment metadata", ErrRoutingArtifactInvalid)
		}
	}
	return nil
}

// ResolveRoutingArtifact always prefers the active object but deterministically
// falls back to baseline if the active pointer is missing, stale, corrupt, or
// incompatible. A canary is selected separately only for eligible new sessions.
func ResolveRoutingArtifact(ctx context.Context, cache RoutingArtifactCache, scope RoutingArtifactScope) (*RoutingArtifactVersion, RoutingArtifactPointers, error) {
	if cache == nil {
		return nil, RoutingArtifactPointers{}, ErrRoutingArtifactUnavailable
	}
	pointers, err := cache.LoadPointers(ctx, scope)
	if err != nil {
		return nil, RoutingArtifactPointers{}, err
	}
	artifact, activeErr := cache.LoadArtifact(ctx, scope, pointers.ActiveVersion)
	if activeErr == nil {
		return artifact, pointers, nil
	}
	if pointers.BaselineVersion == pointers.ActiveVersion {
		return nil, pointers, activeErr
	}
	baseline, baselineErr := cache.LoadArtifact(ctx, scope, pointers.BaselineVersion)
	if baselineErr != nil {
		return nil, pointers, errors.Join(activeErr, baselineErr)
	}
	return baseline, pointers, nil
}
