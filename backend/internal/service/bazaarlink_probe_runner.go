package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	appTimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// BazaarLinkProbeRunner owns BazaarLink's independent daily submission and
// five-minute polling schedules. It has no dependency on ChannelMonitorRunner.
type BazaarLinkProbeRunner struct {
	service *BazaarLinkProbeService
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	started bool
	stopped bool
}

func NewBazaarLinkProbeRunner(service *BazaarLinkProbeService) *BazaarLinkProbeRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &BazaarLinkProbeRunner{service: service, ctx: ctx, cancel: cancel}
}

func (r *BazaarLinkProbeRunner) Start() {
	if r == nil || r.service == nil {
		return
	}
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.wg.Add(2)
	r.mu.Unlock()
	go r.runDaily()
	go r.runPoller()
}

func (r *BazaarLinkProbeRunner) runPoller() {
	defer r.wg.Done()
	r.poll()
	ticker := time.NewTicker(monitorBazaarLinkPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.poll()
		}
	}
}

func (r *BazaarLinkProbeRunner) poll() {
	if err := r.service.Poll(r.ctx); err != nil && r.ctx.Err() == nil {
		slog.Warn("bazaarlink: batch poll failed", "error", err)
	}
}

func nextBazaarLinkProbeAt(now time.Time) time.Time {
	loc := appTimezone.Location()
	localNow := now.In(loc)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), monitorBazaarLinkDailyHour, 0, 0, 0, loc)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (r *BazaarLinkProbeRunner) runDaily() {
	defer r.wg.Done()
	for {
		next := nextBazaarLinkProbeAt(appTimezone.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-r.ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			r.submitBatch()
		}
	}
}

func (r *BazaarLinkProbeRunner) submitBatch() {
	loadCtx, loadCancel := context.WithTimeout(r.ctx, monitorStartupLoadTimeout)
	targets, err := r.service.targets.ListEnabledBazaarLinkProbeTargets(loadCtx)
	loadCancel()
	if err != nil {
		slog.Warn("bazaarlink: load probe targets failed", "error", err)
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	sem := make(chan struct{}, monitorBazaarLinkBatchConcurrency)
	var wg sync.WaitGroup
	for _, target := range targets {
		if target == nil || target.APIKeyDecryptFailed ||
			!bazaarLinkProviderSupported(target.Provider) ||
			strings.TrimSpace(target.Endpoint) == "" || strings.TrimSpace(target.APIKey) == "" ||
			strings.TrimSpace(target.Model) == "" || strings.EqualFold(strings.TrimSpace(target.Model), MonitorDefaultQuotaModel) {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			probeCtx, probeCancel := context.WithTimeout(ctx, monitorBazaarLinkRunTimeout)
			defer probeCancel()
			if _, err := r.service.Run(probeCtx, target.ID); err != nil {
				slog.Warn("bazaarlink: daily probe failed", "monitor_id", target.ID, "error", err)
			}
		}()
	}
	wg.Wait()
}

func (r *BazaarLinkProbeRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.cancel()
	r.mu.Unlock()
	r.wg.Wait()
}
