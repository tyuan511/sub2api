package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/ent/admintelegrambinding"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

const (
	telegramStatsRange       = "90m"
	telegramStatsLastBuckets = 10
	telegramMessageSoftLimit = 3900
	telegramStatsTimeout     = 20 * time.Second
	telegramStatsCommand     = "stats"
	telegramStatsCronRecheck = 30 * time.Second
	telegramStatsCronLockTTL = 2 * time.Minute
)

// telegramStatsCronParser accepts standard 5-field cron (min hour dom month dow).
var telegramStatsCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func validateTelegramStatsCron(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("cron expression is required")
	}
	if _, err := telegramStatsCronParser.Parse(raw); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

func nextTelegramStatsAt(now time.Time, expr string) (time.Time, error) {
	if err := validateTelegramStatsCron(expr); err != nil {
		return time.Time{}, err
	}
	sched, err := telegramStatsCronParser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(now.In(timezone.Location())), nil
}

func telegramBotCommands() []map[string]string {
	return []map[string]string{
		{"command": telegramStatsCommand, "description": "查询中转站并发、监控与今日用量"},
	}
}

func (s *SupportTelegramService) syncBotCommands(ctx context.Context, token string, enabled bool) {
	if s == nil || token == "" {
		return
	}
	if !enabled {
		_, _ = s.call(ctx, token, "deleteMyCommands", map[string]any{})
		return
	}
	if _, err := s.call(ctx, token, "setMyCommands", map[string]any{"commands": telegramBotCommands()}); err != nil {
		logger.L().Warn("telegram.set_my_commands_failed", zap.Error(err))
	}
}

type telegramStationStats struct {
	CollectedAt time.Time

	Platforms      []telegramPlatformConcurrency
	TotalInUse     int64
	ConcurrencyErr string

	TodayTokens     int64
	TodayActualCost float64
	UsageErr        string

	MonitorGroups []telegramMonitorGroup
	MonitorErr    string
}

type telegramPlatformConcurrency struct {
	Platform     string
	CurrentInUse int64
}

type telegramMonitorGroup struct {
	Platform  string
	GroupName string
	Points    []telegramMonitorPoint
}

type telegramMonitorPoint struct {
	Time         time.Time
	RequestCount int64
	ErrorRate    float64
	CacheRate    float64
	TTFTMs       *int64
	Health       string
}

func parseTelegramCommand(text string) (cmd, arg string) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return "", ""
	}
	cmd = parts[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	cmd = strings.ToLower(cmd)
	if len(parts) > 1 {
		arg = parts[1]
	}
	return cmd, arg
}

func (s *SupportTelegramService) handleStatsCommand(ctx context.Context, cfg *supportTelegramRuntimeConfig, message *telegramMessage) error {
	if s == nil || cfg == nil || message == nil || message.From == nil {
		return nil
	}
	_, err := s.client.AdminTelegramBinding.Query().Where(
		admintelegrambinding.TelegramUserIDEQ(message.From.ID),
		admintelegrambinding.ChatIDEQ(message.Chat.ID),
		admintelegrambinding.EnabledEQ(true),
	).Only(ctx)
	if err != nil {
		s.sendTelegramText(ctx, cfg.BotToken, message.Chat.ID, "请先在管理后台绑定 Telegram。")
		return nil
	}

	statsCtx, cancel := context.WithTimeout(ctx, telegramStatsTimeout)
	defer cancel()
	stats := s.collectStationStats(statsCtx)
	s.sendTelegramText(ctx, cfg.BotToken, message.Chat.ID, formatTelegramStationStats(stats))
	return nil
}

func (s *SupportTelegramService) notifyStatsCronChanged() {
	if s == nil || s.statsWake == nil {
		return
	}
	select {
	case s.statsWake <- struct{}{}:
	default:
	}
}

func (s *SupportTelegramService) runStatsCronLoop() {
	if s == nil || s.settings == nil {
		return
	}
	for {
		stored, err := s.loadStored(context.Background())
		if err != nil || stored == nil || !stored.Enabled || !stored.StatsCronEnabled || strings.TrimSpace(stored.StatsCron) == "" {
			if ok, _ := s.waitStatsCron(telegramStatsCronRecheck); !ok {
				return
			}
			continue
		}
		next, err := nextTelegramStatsAt(timezone.Now(), stored.StatsCron)
		if err != nil || next.IsZero() {
			logger.L().Warn("telegram.stats_cron_invalid", zap.String("cron", stored.StatsCron), zap.Error(err))
			if ok, _ := s.waitStatsCron(telegramStatsCronRecheck); !ok {
				return
			}
			continue
		}
		ok, woken := s.waitStatsCron(time.Until(next))
		if !ok {
			return
		}
		if woken {
			continue
		}
		stored, err = s.loadStored(context.Background())
		if err != nil || stored == nil || !stored.Enabled || !stored.StatsCronEnabled {
			continue
		}
		if !s.claimStatsCronFire(context.Background(), next) {
			continue
		}
		s.broadcastScheduledStats(context.Background())
	}
}

func (s *SupportTelegramService) waitStatsCron(d time.Duration) (ok bool, woken bool) {
	if s == nil {
		return false, false
	}
	if d < 0 {
		d = 0
	}
	if d == 0 {
		select {
		case <-s.stop:
			return false, false
		default:
			return true, false
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.stop:
		return false, false
	case <-s.statsWake:
		return true, true
	case <-timer.C:
		return true, false
	}
}

func (s *SupportTelegramService) claimStatsCronFire(ctx context.Context, fireAt time.Time) bool {
	if s == nil || s.redis == nil {
		return true
	}
	key := fmt.Sprintf("support:telegram:stats_cron:%d", fireAt.Unix())
	ok, err := s.redis.SetNX(ctx, key, "1", telegramStatsCronLockTTL).Result()
	if err != nil {
		logger.L().Warn("telegram.stats_cron_lock_failed", zap.Error(err))
		return false
	}
	return ok
}

func (s *SupportTelegramService) broadcastScheduledStats(ctx context.Context) {
	if s == nil || s.client == nil {
		return
	}
	cfg, err := s.loadRuntime(ctx)
	if err != nil || cfg == nil || !cfg.Enabled || cfg.BotToken == "" {
		return
	}
	bindings, err := s.client.AdminTelegramBinding.Query().Where(admintelegrambinding.EnabledEQ(true)).All(ctx)
	if err != nil {
		logger.L().Warn("telegram.stats_cron_list_bindings_failed", zap.Error(err))
		return
	}
	if len(bindings) == 0 {
		return
	}
	statsCtx, cancel := context.WithTimeout(ctx, telegramStatsTimeout)
	stats := s.collectStationStats(statsCtx)
	cancel()
	text := formatTelegramStationStats(stats)
	seen := make(map[int64]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		if _, ok := seen[binding.ChatID]; ok {
			continue
		}
		seen[binding.ChatID] = struct{}{}
		s.sendTelegramText(ctx, cfg.BotToken, binding.ChatID, text)
	}
}

func (s *SupportTelegramService) sendTelegramText(ctx context.Context, token string, chatID int64, text string) {
	for _, part := range splitTelegramMessages(text, telegramMessageSoftLimit) {
		if _, err := s.call(ctx, token, "sendMessage", map[string]any{"chat_id": chatID, "text": part}); err != nil {
			logger.L().Error("telegram.send_message_failed", zap.Error(err))
			return
		}
	}
}

func (s *SupportTelegramService) collectStationStats(ctx context.Context) telegramStationStats {
	stats := telegramStationStats{CollectedAt: timezone.Now()}
	if s == nil {
		stats.ConcurrencyErr = "服务不可用"
		stats.UsageErr = "服务不可用"
		stats.MonitorErr = "服务不可用"
		return stats
	}
	s.fillConcurrencyStats(ctx, &stats)
	s.fillUsageStats(ctx, &stats)
	s.fillMonitorStats(ctx, &stats)
	return stats
}

func (s *SupportTelegramService) fillConcurrencyStats(ctx context.Context, stats *telegramStationStats) {
	if s.ops == nil {
		stats.ConcurrencyErr = "并发服务不可用"
		return
	}
	platform, _, _, _, err := s.ops.collectConcurrencyStats(ctx, "", nil)
	if err != nil {
		logger.L().Error("telegram.stats_concurrency_failed", zap.Error(err))
		stats.ConcurrencyErr = err.Error()
		return
	}
	names := make([]string, 0, len(platform))
	for name := range platform {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		info := platform[name]
		if info == nil {
			continue
		}
		stats.Platforms = append(stats.Platforms, telegramPlatformConcurrency{
			Platform:     name,
			CurrentInUse: info.CurrentInUse,
		})
		stats.TotalInUse += info.CurrentInUse
	}
}

func (s *SupportTelegramService) fillUsageStats(ctx context.Context, stats *telegramStationStats) {
	if s.dashboard == nil {
		stats.UsageErr = "用量服务不可用"
		return
	}
	dash, err := s.dashboard.GetDashboardStats(ctx)
	if err != nil {
		logger.L().Error("telegram.stats_usage_failed", zap.Error(err))
		stats.UsageErr = err.Error()
		return
	}
	if dash == nil {
		stats.UsageErr = "暂无用量数据"
		return
	}
	stats.TodayTokens = dash.TodayTokens
	stats.TodayActualCost = dash.TodayActualCost
}

func (s *SupportTelegramService) fillMonitorStats(ctx context.Context, stats *telegramStationStats) {
	if s.monitor == nil {
		stats.MonitorErr = "监控服务不可用"
		return
	}
	filter, err := s.monitor.ParseFilter(telegramStatsRange, nil, nil, nil)
	if err != nil {
		stats.MonitorErr = err.Error()
		return
	}
	matrix, err := s.monitor.Matrix(ctx, filter, ChannelMonitorV2GroupByPlatformGroup, true)
	if err != nil {
		if errors.Is(err, ErrChannelMonitorDisabled) {
			stats.MonitorErr = "渠道监控未启用"
			return
		}
		logger.L().Error("telegram.stats_monitor_failed", zap.Error(err))
		stats.MonitorErr = err.Error()
		return
	}
	if matrix == nil || len(matrix.Items) == 0 {
		stats.MonitorErr = "暂无监控数据"
		return
	}
	groups := make([]telegramMonitorGroup, 0, len(matrix.Items))
	for _, row := range matrix.Items {
		if row.GroupID == nil || *row.GroupID <= 0 {
			continue
		}
		name := strings.TrimSpace(row.GroupName)
		if name == "" {
			name = fmt.Sprintf("#%d", *row.GroupID)
		}
		groups = append(groups, telegramMonitorGroup{
			Platform:  row.Platform,
			GroupName: name,
			Points:    lastTelegramMonitorPoints(row.Buckets, telegramStatsLastBuckets),
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Platform != groups[j].Platform {
			return groups[i].Platform < groups[j].Platform
		}
		return groups[i].GroupName < groups[j].GroupName
	})
	stats.MonitorGroups = groups
	if len(groups) == 0 {
		stats.MonitorErr = "暂无监控数据"
	}
}

func lastTelegramMonitorPoints(buckets []ChannelMonitorV2TrendPoint, n int) []telegramMonitorPoint {
	if n <= 0 || len(buckets) == 0 {
		return nil
	}
	if len(buckets) > n {
		buckets = buckets[len(buckets)-n:]
	}
	out := make([]telegramMonitorPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, telegramMonitorPoint{
			Time:         b.BucketStart.In(timezone.Location()),
			RequestCount: b.Metrics.RequestCount,
			ErrorRate:    b.Metrics.ErrorRate,
			CacheRate:    b.Metrics.CacheRate,
			TTFTMs:       b.Metrics.TTFT.P50Ms,
			Health:       b.Health.Overall,
		})
	}
	return out
}

func formatTelegramStationStats(stats telegramStationStats) string {
	var b strings.Builder
	ts := stats.CollectedAt
	if ts.IsZero() {
		ts = timezone.Now()
	}
	fmt.Fprintf(&b, "中转站状态  %s\n", ts.Format("01-02 15:04"))

	b.WriteString("\n【并发】\n")
	if stats.ConcurrencyErr != "" {
		fmt.Fprintf(&b, "%s\n", stats.ConcurrencyErr)
	} else if len(stats.Platforms) == 0 {
		b.WriteString("暂无数据\n")
	} else {
		width := utf8.RuneCountInString("合计")
		for _, p := range stats.Platforms {
			if w := utf8.RuneCountInString(p.Platform); w > width {
				width = w
			}
		}
		for _, p := range stats.Platforms {
			fmt.Fprintf(&b, "%s  %d\n", padTelegramLabel(p.Platform, width), p.CurrentInUse)
		}
		fmt.Fprintf(&b, "%s  %d\n", padTelegramLabel("合计", width), stats.TotalInUse)
	}

	b.WriteString("\n【今日用量】\n")
	if stats.UsageErr != "" {
		fmt.Fprintf(&b, "%s\n", stats.UsageErr)
	} else {
		fmt.Fprintf(&b, "Token      %s\n", formatTelegramCount(stats.TodayTokens))
		fmt.Fprintf(&b, "实际消耗   $%.4f\n", stats.TodayActualCost)
	}

	b.WriteString("\n【监控 90m】\n")
	if stats.MonitorErr != "" && len(stats.MonitorGroups) == 0 {
		fmt.Fprintf(&b, "%s\n", stats.MonitorErr)
	} else if len(stats.MonitorGroups) == 0 {
		b.WriteString("暂无数据\n")
	} else {
		for i, g := range stats.MonitorGroups {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(formatTelegramMonitorGroup(g))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func padTelegramLabel(s string, width int) string {
	pad := width - utf8.RuneCountInString(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

func formatTelegramMonitorGroup(g telegramMonitorGroup) string {
	if len(g.Points) == 0 {
		return fmt.Sprintf("⬜\n%s · 无数据", telegramMonitorGroupLabel(g))
	}
	squares := make([]string, 0, len(g.Points))
	latest := ""
	for _, p := range g.Points {
		squares = append(squares, telegramHealthSquare(p.Health, p.RequestCount))
		if p.RequestCount > 0 {
			latest = p.Health
		}
	}
	return strings.Join(squares, " ") + "\n" + telegramMonitorGroupLabel(g) + " · " + telegramHealthStatusLabel(latest)
}

func telegramMonitorGroupLabel(g telegramMonitorGroup) string {
	platform := strings.TrimSpace(g.Platform)
	name := strings.TrimSpace(g.GroupName)
	switch {
	case platform != "" && name != "":
		return platform + " / " + name
	case name != "":
		return name
	case platform != "":
		return platform
	default:
		return "未命名分组"
	}
}

func telegramHealthSquare(health string, requestCount int64) string {
	if requestCount <= 0 {
		return "⬜"
	}
	switch health {
	case "healthy":
		return "🟩"
	case "warning":
		return "🟨"
	case "critical":
		return "🟥"
	default:
		return "⬜"
	}
}

func telegramHealthStatusLabel(health string) string {
	switch health {
	case "healthy":
		return "正常"
	case "warning":
		return "延迟"
	case "critical":
		return "异常"
	default:
		return "无数据"
	}
}

func formatTelegramCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func splitTelegramMessages(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if limit <= 0 {
		limit = telegramMessageSoftLimit
	}
	if utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	var parts []string
	var cur strings.Builder
	curCount := 0
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		parts = append(parts, strings.TrimRight(cur.String(), "\n"))
		cur.Reset()
		curCount = 0
	}
	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		need := lineRunes
		if curCount > 0 {
			need++
		}
		if curCount > 0 && curCount+need > limit {
			flush()
		}
		if lineRunes > limit {
			runes := []rune(line)
			for len(runes) > 0 {
				n := limit
				if n > len(runes) {
					n = len(runes)
				}
				parts = append(parts, string(runes[:n]))
				runes = runes[n:]
			}
			continue
		}
		if curCount > 0 {
			cur.WriteByte('\n')
			curCount++
		}
		cur.WriteString(line)
		curCount += lineRunes
	}
	flush()
	return parts
}
