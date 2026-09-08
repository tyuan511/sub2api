package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTelegramBotCommands(t *testing.T) {
	cmds := telegramBotCommands()
	require.Len(t, cmds, 1)
	require.Equal(t, "stats", cmds[0]["command"])
	require.NotEmpty(t, cmds[0]["description"])
}

func TestValidateTelegramStatsCron(t *testing.T) {
	require.Error(t, validateTelegramStatsCron(""))
	require.Error(t, validateTelegramStatsCron("not a cron"))
	require.Error(t, validateTelegramStatsCron("0 * * * * *"))
	require.NoError(t, validateTelegramStatsCron("0 * * * *"))
	require.NoError(t, validateTelegramStatsCron("*/15 9-18 * * 1-5"))
}

func TestNextTelegramStatsAt(t *testing.T) {
	now := time.Date(2026, 4, 8, 15, 4, 0, 0, time.UTC)
	next, err := nextTelegramStatsAt(now, "*/5 * * * *")
	require.NoError(t, err)
	require.True(t, next.After(now))
	require.LessOrEqual(t, next.Sub(now), 5*time.Minute)

	_, err = nextTelegramStatsAt(now, "")
	require.Error(t, err)
}

func TestParseTelegramCommand(t *testing.T) {
	cmd, arg := parseTelegramCommand("/stats")
	require.Equal(t, "/stats", cmd)
	require.Equal(t, "", arg)

	cmd, arg = parseTelegramCommand("/stats@MyBot extra")
	require.Equal(t, "/stats", cmd)
	require.Equal(t, "extra", arg)

	cmd, arg = parseTelegramCommand("  /start@FastVibeBot  bind-code  ")
	require.Equal(t, "/start", cmd)
	require.Equal(t, "bind-code", arg)

	cmd, arg = parseTelegramCommand("hello there")
	require.Equal(t, "hello", cmd)
	require.Equal(t, "there", arg)
}

func TestFormatTelegramStationStats(t *testing.T) {
	ttft := int64(320)
	text := formatTelegramStationStats(telegramStationStats{
		CollectedAt: time.Date(2026, 4, 8, 15, 4, 0, 0, time.UTC),
		Platforms: []telegramPlatformConcurrency{
			{Platform: "claude", CurrentInUse: 12},
			{Platform: "openai", CurrentInUse: 8},
		},
		TotalInUse:      20,
		TodayTokens:     1_230_000,
		TodayActualCost: 12.3456,
		MonitorGroups: []telegramMonitorGroup{{
			Platform:  "claude",
			GroupName: "default",
			Points: []telegramMonitorPoint{
				{Time: time.Date(2026, 4, 8, 14, 20, 0, 0, time.UTC), RequestCount: 12, ErrorRate: 0.008, CacheRate: 0.45, TTFTMs: &ttft, Health: "healthy"},
				{Time: time.Date(2026, 4, 8, 14, 25, 0, 0, time.UTC)},
			},
		}},
	})

	require.Contains(t, text, "中转站状态  04-08 15:04")
	require.Contains(t, text, "claude  12")
	require.Contains(t, text, "openai  8")
	require.Contains(t, text, "合计")
	require.Contains(t, text, "20")
	require.Contains(t, text, "Token      1.23M")
	require.Contains(t, text, "实际消耗   $12.3456")
	require.Contains(t, text, "🟩 ⬜")
	require.Contains(t, text, "claude / default · 正常")
	require.NotContains(t, text, "12req")
}

func TestFormatTelegramStationStatsSoftFails(t *testing.T) {
	text := formatTelegramStationStats(telegramStationStats{
		CollectedAt:    time.Date(2026, 4, 8, 15, 4, 0, 0, time.UTC),
		ConcurrencyErr: "并发服务不可用",
		UsageErr:       "用量服务不可用",
		MonitorErr:     "渠道监控未启用",
	})
	require.Contains(t, text, "【并发】")
	require.Contains(t, text, "并发服务不可用")
	require.Contains(t, text, "用量服务不可用")
	require.Contains(t, text, "渠道监控未启用")
}

func TestFormatTelegramMonitorGroupColorBlocks(t *testing.T) {
	text := formatTelegramMonitorGroup(telegramMonitorGroup{
		Platform:  "claude",
		GroupName: "外接稳定分组",
		Points: []telegramMonitorPoint{
			{RequestCount: 10, Health: "warning"},
			{RequestCount: 10, Health: "warning"},
			{RequestCount: 10, Health: "warning"},
			{RequestCount: 10, Health: "critical"},
			{RequestCount: 10, Health: "healthy"},
			{RequestCount: 10, Health: "healthy"},
			{RequestCount: 10, Health: "healthy"},
			{RequestCount: 10, Health: "critical"},
			{RequestCount: 10, Health: "healthy"},
			{RequestCount: 10, Health: "healthy"},
		},
	})
	require.Equal(t, "🟨 🟨 🟨 🟥 🟩 🟩 🟩 🟥 🟩 🟩\nclaude / 外接稳定分组 · 正常", text)

	delayed := formatTelegramMonitorGroup(telegramMonitorGroup{
		GroupName: "下游",
		Points: []telegramMonitorPoint{
			{RequestCount: 3, Health: "warning"},
			{RequestCount: 0},
		},
	})
	require.Equal(t, "🟨 ⬜\n下游 · 延迟", delayed)

	empty := formatTelegramMonitorGroup(telegramMonitorGroup{GroupName: "空"})
	require.Equal(t, "⬜\n空 · 无数据", empty)
}

func TestFormatTelegramStationStatsOmitsEmptyMonitorGroups(t *testing.T) {
	text := formatTelegramStationStats(telegramStationStats{
		CollectedAt: time.Date(2026, 4, 8, 15, 4, 0, 0, time.UTC),
		MonitorGroups: []telegramMonitorGroup{
			{GroupName: "空分组"},
			{GroupName: "全白", Points: []telegramMonitorPoint{{RequestCount: 0}, {RequestCount: 0}}},
			{Platform: "claude", GroupName: "有量", Points: []telegramMonitorPoint{{RequestCount: 4, Health: "healthy"}}},
		},
	})
	require.Contains(t, text, "claude / 有量 · 正常")
	require.NotContains(t, text, "空分组")
	require.NotContains(t, text, "全白")
	require.NotContains(t, text, " · 无数据")
}

func TestTelegramHealthSquare(t *testing.T) {
	require.Equal(t, "🟩", telegramHealthSquare("healthy", 1))
	require.Equal(t, "🟨", telegramHealthSquare("warning", 1))
	require.Equal(t, "🟥", telegramHealthSquare("critical", 1))
	require.Equal(t, "⬜", telegramHealthSquare("healthy", 0))
	require.Equal(t, "⬜", telegramHealthSquare("unknown", 8))
}

func TestLastTelegramMonitorPointsKeepsLastTen(t *testing.T) {
	buckets := make([]ChannelMonitorV2TrendPoint, 18)
	for i := range buckets {
		buckets[i] = ChannelMonitorV2TrendPoint{
			BucketStart: time.Date(2026, 4, 8, 12, i, 0, 0, time.UTC),
			Metrics:     ChannelMonitorV2Metric{RequestCount: int64(i + 1)},
		}
	}
	points := lastTelegramMonitorPoints(buckets, 10)
	require.Len(t, points, 10)
	require.Equal(t, int64(9), points[0].RequestCount)
	require.Equal(t, int64(18), points[9].RequestCount)
}

func TestSplitTelegramMessages(t *testing.T) {
	require.Nil(t, splitTelegramMessages("  ", 10))
	require.Equal(t, []string{"hello"}, splitTelegramMessages("hello", 10))

	long := strings.Repeat("abcdefghij\n", 5)
	parts := splitTelegramMessages(long, 25)
	require.Greater(t, len(parts), 1)
	for _, part := range parts {
		require.LessOrEqual(t, len([]rune(part)), 25)
	}
}

func TestFormatTelegramCount(t *testing.T) {
	require.Equal(t, "999", formatTelegramCount(999))
	require.Equal(t, "1.2K", formatTelegramCount(1234))
	require.Equal(t, "1.23M", formatTelegramCount(1_230_000))
}
