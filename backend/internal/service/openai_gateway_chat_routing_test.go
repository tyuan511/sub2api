package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

func TestShouldForwardOpenAIChatCompletionsViaRawChatCompletions(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{
			name: "both endpoints supported uses native chat",
			extra: map[string]any{
				openai_compat.ExtraKeyChatCompletionsSupported: true,
				openai_compat.ExtraKeyResponsesSupported:       true,
			},
			want: true,
		},
		{
			name: "chat unsupported but responses supported converts to responses",
			extra: map[string]any{
				openai_compat.ExtraKeyChatCompletionsSupported: false,
				openai_compat.ExtraKeyResponsesSupported:       true,
			},
			want: false,
		},
		{
			name: "responses unsupported uses native chat",
			extra: map[string]any{
				openai_compat.ExtraKeyChatCompletionsSupported: true,
				openai_compat.ExtraKeyResponsesSupported:       false,
			},
			want: true,
		},
		{
			name: "legacy responses unsupported marker still falls back to chat",
			extra: map[string]any{
				openai_compat.ExtraKeyResponsesSupported: false,
			},
			want: true,
		},
		{
			name:  "unknown capabilities preserve legacy responses conversion",
			extra: map[string]any{},
			want:  false,
		},
		{
			name: "force responses wins",
			extra: map[string]any{
				openai_compat.ExtraKeyResponsesMode:            string(openai_compat.ResponsesSupportModeForceResponses),
				openai_compat.ExtraKeyChatCompletionsSupported: true,
			},
			want: false,
		},
		{
			name: "force chat wins",
			extra: map[string]any{
				openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeForceChatCompletions),
				openai_compat.ExtraKeyResponsesSupported: true,
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra:    tc.extra,
			}
			require.Equal(t, tc.want, shouldForwardOpenAIChatCompletionsViaRawChatCompletions(account))
		})
	}

	require.False(t, shouldForwardOpenAIChatCompletionsViaRawChatCompletions(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}))
	require.True(t, shouldForwardOpenAIChatCompletionsViaRawChatCompletions(&Account{
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
	}))
	require.False(t, shouldForwardOpenAIChatCompletionsViaRawChatCompletions(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}))
}
