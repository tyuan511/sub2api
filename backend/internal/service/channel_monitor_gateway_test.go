package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSharedMonitorBodyCoversProbeProviders(t *testing.T) {
	t.Parallel()
	providers := []struct {
		provider string
		mode     string
		path     string
	}{
		{MonitorProviderOpenAI, MonitorAPIModeResponses, "/v1/responses"},
		{MonitorProviderOpenAI, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorProviderAnthropic, MonitorAPIModeChatCompletions, "/v1/messages"},
		{MonitorProviderGemini, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorProviderGrok, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorProviderKimi, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorProviderZhipu, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorProviderDeepseek, MonitorAPIModeChatCompletions, "/v1/chat/completions"},
	}
	for _, tc := range providers {
		tc := tc
		t.Run(tc.provider+"/"+tc.mode, func(t *testing.T) {
			body, err := buildSharedMonitorBody(tc.provider, tc.mode, "test-model", "2+2=?", nil)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, true, payload["stream"])
			require.Equal(t, tc.path, monitorSharedInboundPath(tc.provider, tc.mode))
		})
	}
}

func TestBuildSharedMonitorBodyRejectsAntigravity(t *testing.T) {
	_, err := buildSharedMonitorBody(MonitorProviderAntigravity, MonitorAPIModeChatCompletions, "model", "prompt", nil)
	require.Error(t, err)
}

func TestExtractSharedMonitorText(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		mode     string
		body     string
		want     string
	}{
		{
			name:     "responses sse",
			provider: MonitorProviderOpenAI,
			mode:     MonitorAPIModeResponses,
			body: "event: response.output_text.delta\ndata: {\"delta\":\"4\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"delta\":\"\"}\n\n" +
				"event: response.completed\ndata: {\"response\":{\"usage\":{\"output_tokens\":1}}}\n\n",
			want: "4",
		},
		{
			name:     "chat sse",
			provider: MonitorProviderGrok,
			mode:     MonitorAPIModeChatCompletions,
			body:     "data: {\"choices\":[{\"delta\":{\"content\":\"4\"}}]}\n\n\ndata: [DONE]\n\n",
			want:     "4",
		},
		{
			name:     "anthropic sse",
			provider: MonitorProviderAnthropic,
			mode:     MonitorAPIModeChatCompletions,
			body:     "event: content_block_delta\ndata: {\"delta\":{\"text\":\"4\"}}\n\n",
			want:     "4",
		},
		{
			name:     "anthropic json",
			provider: MonitorProviderAnthropic,
			mode:     MonitorAPIModeChatCompletions,
			body:     `{"content":[{"type":"text","text":"4"}]}`,
			want:     "4",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, extractSharedMonitorText(tc.provider, tc.mode, []byte(tc.body)))
		})
	}
}

func TestChannelMonitorContextIsolated(t *testing.T) {
	require.False(t, IsChannelMonitorContext(context.Background()))
	require.True(t, IsChannelMonitorContext(withChannelMonitorContext(context.Background())))
}
