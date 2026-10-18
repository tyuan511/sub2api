package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 平台定制顺序必须与前端 frontend/src/utils/groupOrder.ts 的 PLATFORM_DISPLAY_ORDER 一致，
// 否则分组页 / 渠道页 / 各下拉的分组顺序会互相打架。改动时两边一起改。
func TestGroupPlatformDisplayOrderMatchesFrontendContract(t *testing.T) {
	require.Equal(t, []string{"openai", "anthropic", "grok", "gemini"}, GroupPlatformDisplayOrder)
	require.Equal(t, 0, GroupPlatformDisplayRank(PlatformOpenAI))
	require.Equal(t, 1, GroupPlatformDisplayRank(" Anthropic "))
	require.Equal(t, 2, GroupPlatformDisplayRank(PlatformGrok))
	require.Equal(t, 3, GroupPlatformDisplayRank(PlatformGemini))
	// 不在列表里的平台一律排到末尾。
	require.Equal(t, 4, GroupPlatformDisplayRank(PlatformComposite))
	require.Equal(t, 4, GroupPlatformDisplayRank(PlatformKimi))
	require.Equal(t, 4, GroupPlatformDisplayRank(""))
}
