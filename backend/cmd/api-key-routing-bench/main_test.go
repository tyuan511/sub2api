package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDurationQuantilesUsesNearestRank(t *testing.T) {
	values := make([]time.Duration, 100)
	for index := range values {
		values[index] = time.Duration(index+1) * time.Millisecond
	}
	got := durationQuantiles(values)
	require.Equal(t, 100, got.Samples)
	require.Equal(t, float64(50), got.P50MS)
	require.Equal(t, float64(95), got.P95MS)
	require.Equal(t, float64(99), got.P99MS)
	require.Equal(t, float64(100), got.MaxMS)
}

func TestChunkHasSemanticOutputIgnoresRoleAndDetectsReasoning(t *testing.T) {
	var roleOnly streamChunk
	require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"delta":{"role":"assistant","content":""}}]}`), &roleOnly))
	require.False(t, chunkHasSemanticOutput(roleOnly))

	var reasoning streamChunk
	require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`), &reasoning))
	require.True(t, chunkHasSemanticOutput(reasoning))
}

func TestBuildReportSeparatesUsageCoverageAndFailures(t *testing.T) {
	report := buildReport("eight-smart", []requestSample{
		{success: true, latency: 100 * time.Millisecond, ttft: 20 * time.Millisecond, hasTTFT: true, hasUsage: true, usage: tokenUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}},
		{latency: 200 * time.Millisecond, errorKind: "http_503"},
	}, time.Second)
	require.Equal(t, 2, report.Requests)
	require.Equal(t, 1, report.Successes)
	require.Equal(t, 1, report.Failures)
	require.Equal(t, 0.5, report.SuccessRate)
	require.Equal(t, 1, report.Tokens.UsageSamples)
	require.Equal(t, int64(12), report.Tokens.TotalTokens)
	require.Equal(t, float64(12), report.Tokens.TotalTokensPerSecond)
	require.Equal(t, 1, report.Errors["http_503"])
}
