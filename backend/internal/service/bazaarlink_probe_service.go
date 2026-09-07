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
	GroupName           string
	Provider            string
	Endpoint            string
	APIKey              string
	APIKeyDecryptFailed bool
	Model               string
}

// BazaarLinkProbeGroupInfo is one selectable group for manual batch identity probes.
type BazaarLinkProbeGroupInfo struct {
	GroupName     string `json:"group_name"`
	MonitorCount  int    `json:"monitor_count"`
	EligibleCount int    `json:"eligible_count"`
}

// BazaarLinkProbeBatchResult summarizes a multi-group manual probe submission.
type BazaarLinkProbeBatchResult struct {
	Total     int                         `json:"total"`
	Submitted int                         `json:"submitted"`
	Skipped   int                         `json:"skipped"`
	Failed    int                         `json:"failed"`
	Items     []BazaarLinkProbeBatchItem  `json:"items"`
}

// BazaarLinkProbeBatchItem is one monitor outcome inside a batch submission.
type BazaarLinkProbeBatchItem struct {
	MonitorID int64                       `json:"monitor_id"`
	GroupName string                      `json:"group_name"`
	Status    string                      `json:"status"` // submitted | skipped | failed
	Error     string                      `json:"error,omitempty"`
	Result    *domain.BazaarLinkProbeResult `json:"result,omitempty"`
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
		GroupName:           strings.TrimSpace(monitor.GroupName),
		Provider:            monitor.Provider,
		Endpoint:            monitor.Endpoint,
		APIKey:              monitor.APIKey,
		APIKeyDecryptFailed: monitor.APIKeyDecryptFailed,
		Model:               monitor.PrimaryModel,
	}
}

// bazaarLinkTargetEligible reports whether a target can be submitted to BazaarLink.
func bazaarLinkTargetEligible(target *BazaarLinkProbeTarget) bool {
	if target == nil || target.APIKeyDecryptFailed {
		return false
	}
	if !bazaarLinkProviderSupported(target.Provider) {
		return false
	}
	if strings.TrimSpace(target.Endpoint) == "" || strings.TrimSpace(target.APIKey) == "" {
		return false
	}
	model := strings.TrimSpace(target.Model)
	if model == "" || strings.EqualFold(model, MonitorDefaultQuotaModel) {
		return false
	}
	return true
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
	targets  BazaarLinkProbeTargetReader
	store    BazaarLinkProbeTaskRepository
	settings bazaarLinkProbeRuntimeReader
}

func NewBazaarLinkProbeService(targets BazaarLinkProbeTargetReader, store BazaarLinkProbeTaskRepository, settings bazaarLinkProbeRuntimeReader) *BazaarLinkProbeService {
	return &BazaarLinkProbeService{targets: targets, store: store, settings: settings}
}

// Run submits one asynchronous probe. A target with no concrete model is not
// probeable; this avoids ever sending the quota placeholder as modelId while
// keeping the probe independent from check_mode.
func (s *BazaarLinkProbeService) Run(ctx context.Context, id int64) (*domain.BazaarLinkProbeResult, error) {
	if s == nil || s.targets == nil || s.store == nil {
		return nil, fmt.Errorf("bazaarlink probe is not configured")
	}
	if s.settings != nil && !s.settings.GetBazaarLinkProbeRuntime(ctx).Enabled {
		return nil, ErrChannelMonitorBazaarLinkDisabled
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
	return s.runTarget(ctx, target)
}

// ListProbeGroups returns distinct non-empty group names from enabled monitors,
// with counts used by the admin settings multi-select UI.
func (s *BazaarLinkProbeService) ListProbeGroups(ctx context.Context) ([]BazaarLinkProbeGroupInfo, error) {
	if s == nil || s.targets == nil {
		return nil, fmt.Errorf("bazaarlink probe is not configured")
	}
	targets, err := s.targets.ListEnabledBazaarLinkProbeTargets(ctx)
	if err != nil {
		return nil, err
	}
	type counters struct {
		total    int
		eligible int
	}
	byGroup := make(map[string]*counters)
	order := make([]string, 0)
	for _, target := range targets {
		if target == nil {
			continue
		}
		name := strings.TrimSpace(target.GroupName)
		if name == "" {
			continue
		}
		c, ok := byGroup[name]
		if !ok {
			c = &counters{}
			byGroup[name] = c
			order = append(order, name)
		}
		c.total++
		if bazaarLinkTargetEligible(target) {
			c.eligible++
		}
	}
	out := make([]BazaarLinkProbeGroupInfo, 0, len(order))
	for _, name := range order {
		c := byGroup[name]
		out = append(out, BazaarLinkProbeGroupInfo{
			GroupName:     name,
			MonitorCount:  c.total,
			EligibleCount: c.eligible,
		})
	}
	return out, nil
}

// RunByGroupNames submits identity probes for every eligible enabled monitor
// whose group_name is in the requested set. Submission is concurrent (same
// limits as the scheduled batch) and returns a compact per-monitor summary.
func (s *BazaarLinkProbeService) RunByGroupNames(ctx context.Context, groupNames []string) (*BazaarLinkProbeBatchResult, error) {
	if s == nil || s.targets == nil || s.store == nil {
		return nil, fmt.Errorf("bazaarlink probe is not configured")
	}
	if s.settings != nil && !s.settings.GetBazaarLinkProbeRuntime(ctx).Enabled {
		return nil, ErrChannelMonitorBazaarLinkDisabled
	}
	want := make(map[string]struct{}, len(groupNames))
	for _, raw := range groupNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		want[name] = struct{}{}
	}
	if len(want) == 0 {
		return nil, ErrChannelMonitorBazaarLinkEmptyGroups
	}
	targets, err := s.targets.ListEnabledBazaarLinkProbeTargets(ctx)
	if err != nil {
		return nil, err
	}
	selected := make([]*BazaarLinkProbeTarget, 0)
	for _, target := range targets {
		if target == nil {
			continue
		}
		if _, ok := want[strings.TrimSpace(target.GroupName)]; !ok {
			continue
		}
		selected = append(selected, target)
	}
	result := &BazaarLinkProbeBatchResult{
		Total: len(selected),
		Items: make([]BazaarLinkProbeBatchItem, 0, len(selected)),
	}
	if len(selected) == 0 {
		return result, nil
	}

	type itemOut struct {
		item BazaarLinkProbeBatchItem
	}
	outCh := make(chan itemOut, len(selected))
	sem := make(chan struct{}, monitorBazaarLinkBatchConcurrency)
	var wg sync.WaitGroup
	for _, target := range selected {
		target := target
		if !bazaarLinkTargetEligible(target) {
			result.Skipped++
			result.Items = append(result.Items, BazaarLinkProbeBatchItem{
				MonitorID: target.ID,
				GroupName: target.GroupName,
				Status:    "skipped",
				Error:     "monitor is not eligible for identity probing",
			})
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return result, ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, monitorBazaarLinkRunTimeout)
			defer cancel()
			item := BazaarLinkProbeBatchItem{
				MonitorID: target.ID,
				GroupName: target.GroupName,
			}
			res, runErr := s.runTarget(probeCtx, target)
			if runErr != nil {
				item.Status = "failed"
				item.Error = runErr.Error()
			} else {
				item.Status = "submitted"
				item.Result = res
			}
			outCh <- itemOut{item: item}
		}()
	}
	wg.Wait()
	close(outCh)
	for row := range outCh {
		result.Items = append(result.Items, row.item)
		switch row.item.Status {
		case "submitted":
			result.Submitted++
		case "failed":
			result.Failed++
		default:
			result.Skipped++
		}
	}
	return result, nil
}

func (s *BazaarLinkProbeService) runTarget(ctx context.Context, target *BazaarLinkProbeTarget) (*domain.BazaarLinkProbeResult, error) {
	if target == nil {
		return nil, ErrChannelMonitorNotFound
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
