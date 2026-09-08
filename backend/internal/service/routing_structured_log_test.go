package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutingStructuredLogIsCorrelatedAndContainsNoSensitivePayload(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	requestID := "request-1"
	apiKeyID, initialGroup, effectiveGroup := int64(7), int64(11), int64(12)
	outcome := RoutingFactOutcomeSuccess
	preference := APIKeySmartPreferenceBalanced
	logAPIKeyRoutingOutcome(context.Background(), &RoutingAttemptFact{
		RoutingDecisionID: "decision-1", RequestID: &requestID, APIKeyID: &apiKeyID,
		RouteVersion: 4, InitialGroupID: &initialGroup, EffectiveGroupID: &effectiveGroup,
		ScheduleMode: APIKeyScheduleModeSmart, SmartPreference: &preference, AttemptIndex: 1,
		Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses",
		OutcomeCategory: &outcome, StickyBroken: true,
	})

	logged := output.String()
	for _, expected := range []string{"decision-1", "request-1", "route_version", "effective_group_id", "switch_count", "success"} {
		require.Contains(t, logged, expected)
	}
	for _, forbidden := range []string{"prompt", "response_body", "api_key", "credential", "sk-secret"} {
		if forbidden == "api_key" {
			require.NotContains(t, strings.ToLower(logged), "api_key\"")
			continue
		}
		require.NotContains(t, strings.ToLower(logged), forbidden)
	}
}
