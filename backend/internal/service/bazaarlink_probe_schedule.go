package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	appTimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// bazaarLinkProbeCronParser accepts standard 5-field cron (min hour dom month dow).
// Same dialect as backup / ops cleanup schedules.
var bazaarLinkProbeCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// normalizeBazaarLinkProbeCron trims and falls back to the historical daily 02:00
// expression. Invalid expressions also fall back so runtime readers never panic;
// UpdateSettings validates strictly before persist.
func normalizeBazaarLinkProbeCron(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultBazaarLinkProbeCron
	}
	if err := validateBazaarLinkProbeCron(raw); err != nil {
		return DefaultBazaarLinkProbeCron
	}
	return raw
}

func validateBazaarLinkProbeCron(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("cron expression is required")
	}
	if _, err := bazaarLinkProbeCronParser.Parse(raw); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

// nextBazaarLinkProbeAt returns the next fire time for expr after now, in the
// application timezone. Invalid expr yields an error so the scheduler can back off.
func nextBazaarLinkProbeAt(now time.Time, expr string) (time.Time, error) {
	expr = normalizeBazaarLinkProbeCron(expr)
	sched, err := bazaarLinkProbeCronParser.Parse(expr)
	if err != nil {
		return time.Time{}, err
	}
	loc := appTimezone.Location()
	return sched.Next(now.In(loc)), nil
}
