package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// BazaarLinkProbeTarget is the small configuration projection that the
// independent BazaarLink probe service needs. It deliberately contains no
// monitor scheduling or check_mode semantics.
type BazaarLinkProbeTarget struct {
	ID                  int64
	Provider            string
	Endpoint            string
	APIKey              string
	APIKeyDecryptFailed bool
	Model               string
}

// BazaarLinkProbeTargetReader is implemented by a narrow configuration
// adapter. BazaarLink never depends on ChannelMonitorService.
type BazaarLinkProbeTargetReader interface {
	GetBazaarLinkProbeTarget(ctx context.Context, id int64) (*BazaarLinkProbeTarget, error)
	ListEnabledBazaarLinkProbeTargets(ctx context.Context) ([]*BazaarLinkProbeTarget, error)
}

// BazaarLinkProbeTargetReaderAdapter reads only the monitor configuration
// needed by BazaarLink. The probe service does not depend on
// ChannelMonitorService or its scheduling/check-mode behavior.
type BazaarLinkProbeTargetReaderAdapter struct {
	repo      ChannelMonitorRepository
	encryptor SecretEncryptor
}

func NewBazaarLinkProbeTargetReaderAdapter(repo ChannelMonitorRepository, encryptor SecretEncryptor) BazaarLinkProbeTargetReader {
	return &BazaarLinkProbeTargetReaderAdapter{repo: repo, encryptor: encryptor}
}

func (r *BazaarLinkProbeTargetReaderAdapter) GetBazaarLinkProbeTarget(ctx context.Context, id int64) (*BazaarLinkProbeTarget, error) {
	if r == nil || r.repo == nil {
		return nil, ErrChannelMonitorNotFound
	}
	monitor, err := r.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	r.decryptAPIKey(monitor)
	return bazaarLinkProbeTargetFromMonitor(monitor), nil
}

func (r *BazaarLinkProbeTargetReaderAdapter) ListEnabledBazaarLinkProbeTargets(ctx context.Context) ([]*BazaarLinkProbeTarget, error) {
	if r == nil || r.repo == nil {
		return nil, fmt.Errorf("bazaarlink probe target repository is not configured")
	}
	monitors, err := r.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]*BazaarLinkProbeTarget, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor == nil {
			continue
		}
		r.decryptAPIKey(monitor)
		if target := bazaarLinkProbeTargetFromMonitor(monitor); target != nil {
			targets = append(targets, target)
		}
	}
	return targets, nil
}

func (r *BazaarLinkProbeTargetReaderAdapter) decryptAPIKey(monitor *ChannelMonitor) {
	if monitor == nil || monitor.APIKey == "" || monitor.APIKeyDecryptFailed {
		return
	}
	if r.encryptor == nil {
		monitor.APIKey = ""
		monitor.APIKeyDecryptFailed = true
		return
	}
	plain, err := r.encryptor.Decrypt(monitor.APIKey)
	if err != nil {
		slog.Warn("bazaarlink: decrypt monitor api key failed", "monitor_id", monitor.ID, "error", err)
		monitor.APIKey = ""
		monitor.APIKeyDecryptFailed = true
		return
	}
	monitor.APIKey = plain
}

func bazaarLinkProbeTargetFromMonitor(monitor *ChannelMonitor) *BazaarLinkProbeTarget {
	if monitor == nil {
		return nil
	}
	return &BazaarLinkProbeTarget{
		ID:                  monitor.ID,
		Provider:            monitor.Provider,
		Endpoint:            monitor.Endpoint,
		APIKey:              monitor.APIKey,
		APIKeyDecryptFailed: monitor.APIKeyDecryptFailed,
		Model:               monitor.PrimaryModel,
	}
}

// BazaarLinkProbeReader is the read-side projection consumed by monitor cards.
type BazaarLinkProbeReader interface {
	ListLatestBazaarLinkProbes(ctx context.Context, ids []int64) (map[int64]*domain.BazaarLinkProbeResult, error)
}

// BazaarLinkProbeTaskRepository owns the durable asynchronous task stream.
// The monitor repository implements this interface for now; the service does
// not depend on the monitor repository type.
type BazaarLinkProbeTaskRepository interface {
	BazaarLinkProbeReader
	InsertBazaarLinkProbe(ctx context.Context, monitorID int64, result *domain.BazaarLinkProbeResult) error
	FindActiveBazaarLinkProbeTask(ctx context.Context, monitorID int64, now time.Time) (*domain.BazaarLinkProbeTask, error)
	InsertBazaarLinkProbeTask(ctx context.Context, task *domain.BazaarLinkProbeTask) error
	ListActiveBazaarLinkProbeTasks(ctx context.Context) ([]*domain.BazaarLinkProbeTask, error)
	UpdateBazaarLinkProbeTask(ctx context.Context, taskID int64, status string, result *domain.BazaarLinkProbeResult, nextPollAt *time.Time) error
}

// BazaarLinkProbeService submits and polls BazaarLink tasks. It is an
// optional integration: the monitor domain only supplies target configuration
// and reads the latest result projection.
type BazaarLinkProbeService struct {
	targets BazaarLinkProbeTargetReader
	store   BazaarLinkProbeTaskRepository
}

func NewBazaarLinkProbeService(targets BazaarLinkProbeTargetReader, store BazaarLinkProbeTaskRepository) *BazaarLinkProbeService {
	return &BazaarLinkProbeService{targets: targets, store: store}
}

// Run submits one asynchronous probe. A target with no concrete model is not
// probeable; this avoids ever sending the quota placeholder as modelId while
// keeping the probe independent from check_mode.
func (s *BazaarLinkProbeService) Run(ctx context.Context, id int64) (*domain.BazaarLinkProbeResult, error) {
	if s == nil || s.targets == nil || s.store == nil {
		return nil, fmt.Errorf("bazaarlink probe is not configured")
	}
	target, err := s.targets.GetBazaarLinkProbeTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrChannelMonitorNotFound
	}
	if target.APIKeyDecryptFailed {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	if !bazaarLinkProviderSupported(target.Provider) {
		return nil, ErrChannelMonitorBazaarLinkUnsupported
	}
	if strings.TrimSpace(target.Model) == "" || strings.EqualFold(strings.TrimSpace(target.Model), MonitorDefaultQuotaModel) {
		return nil, ErrChannelMonitorBazaarLinkUnsupported
	}
	now := time.Now()
	if existing, err := s.store.FindActiveBazaarLinkProbeTask(ctx, target.ID, now); err != nil {
		return nil, fmt.Errorf("find active bazaarlink probe: %w", err)
	} else if existing != nil && existing.Result != nil {
		return existing.Result, nil
	}
	result := runBazaarLinkProbeTarget(ctx, target)
	if result == nil {
		return nil, fmt.Errorf("bazaarlink probe returned no verdict")
	}
	if strings.TrimSpace(result.RunID) == "" {
		if err := s.store.InsertBazaarLinkProbe(ctx, target.ID, result); err != nil {
			return nil, fmt.Errorf("persist bazaarlink probe submission failure: %w", err)
		}
		return result, nil
	}
	taskStatus := bazaarLinkTaskStatus(result.Status)
	task := &domain.BazaarLinkProbeTask{
		MonitorID: target.ID, RunID: result.RunID, Status: taskStatus, Result: result,
		SubmittedAt: now, ExpiresAt: now.Add(monitorBazaarLinkTaskTimeout),
		NextPollAt: now.Add(monitorBazaarLinkPollInterval),
	}
	if taskStatus == bazaarLinkTaskStatusCompleted || taskStatus == bazaarLinkTaskStatusFailed {
		task.NextPollAt = time.Time{}
	}
	if err := s.store.InsertBazaarLinkProbeTask(ctx, task); err != nil {
		return nil, fmt.Errorf("persist bazaarlink probe task: %w", err)
	}
	return result, nil
}

// Poll performs one five-minute batch poll and closes local tasks after two
// hours. The remote API has no documented cancellation endpoint.
func (s *BazaarLinkProbeService) Poll(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("bazaarlink probe is not configured")
	}
	tasks, err := s.store.ListActiveBazaarLinkProbeTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active bazaarlink probe tasks: %w", err)
	}
	now := time.Now()
	sem := make(chan struct{}, monitorBazaarLinkPollConcurrency)
	var wg sync.WaitGroup
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.RunID) == "" {
			continue
		}
		if !task.ExpiresAt.IsZero() && !now.Before(task.ExpiresAt) {
			result := cloneBazaarLinkProbeResult(task.Result)
			if result == nil {
				result = &domain.BazaarLinkProbeResult{RunID: task.RunID}
			}
			result.RunID = task.RunID
			result.Status = bazaarLinkTaskStatusTimedOut
			result.Error = "identity probe timed out after 2 hours"
			result.CheckedAt = now
			if err := s.store.UpdateBazaarLinkProbeTask(ctx, task.ID, bazaarLinkTaskStatusTimedOut, result, nil); err != nil {
				slog.Warn("bazaarlink: close expired probe task failed", "task_id", task.ID, "error", err)
			}
			continue
		}
		if !task.NextPollAt.IsZero() && now.Before(task.NextPollAt) {
			continue
		}
		task := task
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			pollCtx, cancel := context.WithTimeout(ctx, monitorBazaarLinkPollRequestTimeout)
			defer cancel()
			result, pollErr := pollBazaarLinkProbe(pollCtx, task.RunID)
			if pollErr != nil {
				result = cloneBazaarLinkProbeResult(task.Result)
				if result == nil {
					result = &domain.BazaarLinkProbeResult{RunID: task.RunID}
				}
				result.RunID = task.RunID
				result.Status = bazaarLinkTaskStatusRunning
				result.Error = "identity probe result polling failed; will retry"
				result.CheckedAt = time.Now()
				next := time.Now().Add(monitorBazaarLinkPollInterval)
				if updateErr := s.store.UpdateBazaarLinkProbeTask(ctx, task.ID, bazaarLinkTaskStatusRunning, result, &next); updateErr != nil {
					slog.Warn("bazaarlink: update probe retry failed", "task_id", task.ID, "error", updateErr)
				}
				return
			}
			status := bazaarLinkTaskStatus(result.Status)
			var next *time.Time
			if status == bazaarLinkTaskStatusQueued || status == bazaarLinkTaskStatusRunning {
				pollAt := time.Now().Add(monitorBazaarLinkPollInterval)
				next = &pollAt
			}
			if !task.ExpiresAt.IsZero() && !time.Now().Before(task.ExpiresAt) && status != bazaarLinkTaskStatusCompleted && status != bazaarLinkTaskStatusFailed {
				status = bazaarLinkTaskStatusTimedOut
				result.Status = status
				result.Error = "identity probe timed out after 2 hours"
				next = nil
			}
			if updateErr := s.store.UpdateBazaarLinkProbeTask(ctx, task.ID, status, result, next); updateErr != nil {
				slog.Warn("bazaarlink: update probe task failed", "task_id", task.ID, "error", updateErr)
			}
		}()
	}
	wg.Wait()
	return nil
}
