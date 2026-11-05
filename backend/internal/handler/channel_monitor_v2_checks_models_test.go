package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupCheckModelPlatformAllowlist(t *testing.T) {
	for _, tc := range []struct {
		platform, model string
		allowed         bool
	}{
		{service.PlatformOpenAI, "gpt-6-astra", true},
		{service.PlatformOpenAI, "gpt-6.1-sol", true},
		{service.PlatformAnthropic, "claude-opus-5.5", true},
		{service.PlatformAnthropic, "claude-sonnet-5.5", true},
		{service.PlatformOpenAI, "claude-opus-5.5", false},
		{service.PlatformAnthropic, "gpt-6-astra", false},
		{service.PlatformOpenAI, "gpt-6", false},
		{service.PlatformComposite, "gpt-6-astra", false},
	} {
		t.Run(tc.platform+"/"+tc.model, func(t *testing.T) { require.Equal(t, tc.allowed, groupCheckModelAllowed(tc.platform, tc.model)) })
	}
}
