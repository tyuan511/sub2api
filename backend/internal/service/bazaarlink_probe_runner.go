package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	appTimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// bazaarLinkProbeRuntimeReader is the optional settings view used to gate and
// schedule identity probes without coupling to SettingService concrete type.
type bazaarLinkProbeRuntimeReader interface {
	GetBazaarLinkProbeRuntime(ctx context.Context) BazaarLinkProbeRuntime
}

// BazaarLinkProbeRunner owns BazaarLink's independent scheduled submission and
// five-minute polling schedules. It has no dependency on ChannelMonitorRunner.
type BazaarLinkProbeRunner struct {
	service  *BazaarLinkProbeService
	settings bazaarLinkProbeRuntimeReader
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	started  bool
	stopped  bool
}

func NewBazaarLinkProbeRunner(service *BazaarLinkProbeService, settings bazaarLinkProbeRuntimeReader) *BazaarLinkProbeRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &BazaarLinkProbeRunner{service: service, settings: settings, ctx: ctx, cancel: cancel}
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
	go r.runScheduler()
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

func (r *BazaarLinkProbeRunner) probeRuntime() BazaarLinkProbeRuntime {
	if r == nil || r.settings == nil {
		return BazaarLinkProbeRuntime{Enabled: true, Cron: DefaultBazaarLinkProbeCron}
	}
	return r.settings.GetBazaarLinkProbeRuntime(r.ctx)
}

// runScheduler waits for the next cron fire (or rechecks settings when disabled).
// Settings are re-read every cycle so admin toggles/cron edits take effect without restart.
func (r *BazaarLinkProbeRunner) runScheduler() {
	defer r.wg.Done()
	for {
		rt := r.probeRuntime()
		if !rt.Enabled {
			if !r.sleep(monitorBazaarLinkScheduleRecheck) {
				return
			}
			continue
		}
		next, err := nextBazaarLinkProbeAt(appTimezone.Now(), rt.Cron)
		if err != nil || next.IsZero() {
			slog.Warn("bazaarlink: invalid probe cron; backing off", "cron", rt.Cron, "error", err)
			if !r.sleep(monitorBazaarLinkScheduleRecheck) {
				return
			}
			continue
		}
		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		if !r.sleep(delay) {
			return
		}
		// Re-check after wake: cron/enabled may have changed while waiting.
		rt = r.probeRuntime()
		if !rt.Enabled {
			continue
		}
		r.submitBatch()
	}
}

func (r *BazaarLinkProbeRunner) sleep(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-r.ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return false
	case <-timer.C:
		return true
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
		if !bazaarLinkTargetEligible(target) {
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
				slog.Warn("bazaarlink: scheduled probe failed", "monitor_id", target.ID, "error", err)
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
