package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// routingFactRepository persists the bounded routing fact stream. Smart
// routing artifacts/experiments were removed, so this repository now only owns
// the append-only attempt facts and their retention window.
type routingFactRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewRoutingOptimizationRepository(pool *RoutingBackgroundDB) service.RoutingFactRepository {
	if pool == nil {
		return &routingFactRepository{}
	}
	return &routingFactRepository{client: pool.Client, db: pool.DB}
}

func (r *routingFactRepository) CreateRoutingAttempts(ctx context.Context, facts []*service.RoutingAttemptFact) error {
	if len(facts) == 0 {
		return nil
	}
	builders := make([]*dbent.RoutingAttemptCreate, 0, len(facts))
	for _, fact := range facts {
		if err := service.ValidateRoutingAttemptFact(fact); err != nil {
			return err
		}
		candidates, err := json.Marshal(fact.Candidates)
		if err != nil {
			return fmt.Errorf("marshal routing fact candidates: %w", err)
		}
		builder := r.client.RoutingAttempt.Create().
			SetEventID(fact.EventID).
			SetRoutingDecisionID(fact.RoutingDecisionID).
			SetNillableRequestID(fact.RequestID).
			SetNillableAPIKeyID(fact.APIKeyID).
			SetRouteVersion(fact.RouteVersion).
			SetNillableInitialGroupID(fact.InitialGroupID).
			SetNillableAttemptedGroupID(fact.AttemptedGroupID).
			SetNillableEffectiveGroupID(fact.EffectiveGroupID).
			SetNillableSelectedGroupID(fact.SelectedGroupID).
			SetScheduleMode(fact.ScheduleMode).
			SetRoutingMinSuccessRate((&service.APIKey{RoutingMinSuccessRate: fact.RoutingMinSuccessRate}).EffectiveRoutingMinSuccessRate()).
			SetAttemptIndex(fact.AttemptIndex).
			SetPlatform(fact.Platform).
			SetModelFamily(fact.ModelFamily).
			SetEndpointKind(fact.EndpointKind).
			SetStrategyVersion(fact.StrategyVersion).
			SetScoreVersion(fact.ScoreVersion).
			SetFeatureSchemaVersion(fact.FeatureSchemaVersion).
			SetNillableModelVersion(fact.ModelVersion).
			SetNillableExperimentID(fact.ExperimentID).
			SetNillableExperimentBucket(fact.ExperimentBucket).
			SetSampleProbability(fact.SampleProbability).
			SetNillableActionPropensity(fact.ActionPropensity).
			SetAssignmentReason(fact.AssignmentReason).
			SetCandidates(jsontext.Value(candidates)).
			SetNillableSelectedReason(fact.SelectedReason).
			SetOutcomeVisibility(fact.OutcomeVisibility).
			SetNillableOutcomeCategory(fact.OutcomeCategory).
			SetRetryable(fact.Retryable).
			SetSemanticOutput(fact.SemanticOutput).
			SetSwitchedGroup(fact.SwitchedGroup).
			SetStickyBroken(fact.StickyBroken).
			SetNillableBreakerTransition(fact.BreakerTransition).
			SetNillableQueueMs(fact.QueueMS).
			SetNillableTtftMs(fact.TTFTMS).
			SetNillableDurationMs(fact.DurationMS).
			SetNillableActualCost(fact.ActualCost).
			SetNillableBilledCost(fact.BilledCost).
			SetCacheColdDueToFailover(fact.CacheColdDueToFailover).
			SetEventPriority(fact.EventPriority).
			SetOccurredAt(fact.OccurredAt)
		if fact.RoutingStateVersion > 0 {
			builder.SetRoutingStateVersion(fact.RoutingStateVersion)
		}
		if len(fact.ActualUsage) > 0 {
			builder.SetActualUsage(jsontext.Value(fact.ActualUsage))
		}
		if len(fact.BillableUsage) > 0 {
			builder.SetBillableUsage(jsontext.Value(fact.BillableUsage))
		}
		builders = append(builders, builder)
	}
	if err := r.client.RoutingAttempt.CreateBulk(builders...).OnConflictColumns("event_id").DoNothing().Exec(ctx); err != nil {
		return fmt.Errorf("create routing attempts: %w", err)
	}
	return nil
}

func (r *routingFactRepository) PruneRoutingAttempts(ctx context.Context, sampleBefore, diagnosticBefore, criticalBefore time.Time, limit int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("routing fact repository unavailable")
	}
	if limit <= 0 || limit > 50_000 {
		limit = 5000
	}
	result, err := r.db.ExecContext(ctx, `
WITH expired AS (
    SELECT id
    FROM routing_attempts
    WHERE (event_priority = 'sample' AND occurred_at < $1)
       OR (event_priority = 'diagnostic' AND occurred_at < $2)
       OR (event_priority = 'critical' AND occurred_at < $3)
    ORDER BY occurred_at, id
    LIMIT $4
)
DELETE FROM routing_attempts target
USING expired
WHERE target.id = expired.id`, sampleBefore, diagnosticBefore, criticalBefore, limit)
	if err != nil {
		return 0, fmt.Errorf("prune routing attempts: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("routing attempt prune result: %w", err)
	}
	return deleted, nil
}
