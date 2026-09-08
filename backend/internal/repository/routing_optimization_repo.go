package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/routingartifactversion"
	"github.com/Wei-Shaw/sub2api/ent/routingexperiment"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type routingOptimizationRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewRoutingOptimizationRepository(pool *RoutingBackgroundDB) service.RoutingOptimizationRepository {
	if pool == nil {
		return &routingOptimizationRepository{}
	}
	return &routingOptimizationRepository{client: pool.Client, db: pool.DB}
}

func (r *routingOptimizationRepository) CreateArtifact(ctx context.Context, artifact *service.RoutingArtifactVersion) error {
	if err := service.ValidateRoutingArtifact(artifact); err != nil {
		return err
	}
	created, err := r.client.RoutingArtifactVersion.Create().
		SetArtifactKind(artifact.ArtifactKind).
		SetVersion(artifact.Version).
		SetNillableParentVersion(artifact.ParentVersion).
		SetPlatform(artifact.Platform).
		SetModelFamily(artifact.ModelFamily).
		SetEndpointKind(artifact.EndpointKind).
		SetNillablePreference(artifact.Preference).
		SetStatus(artifact.Status).
		SetSchemaVersion(artifact.SchemaVersion).
		SetChecksum(artifact.Checksum).
		SetPayload(artifact.Payload).
		SetDependencies(artifact.Dependencies).
		SetLineage(artifact.Lineage).
		SetNillableCreatedBy(artifact.CreatedBy).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create routing artifact: %w", err)
	}
	artifact.ID = created.ID
	artifact.CreatedAt = created.CreatedAt
	return nil
}

func (r *routingOptimizationRepository) GetArtifact(ctx context.Context, kind, version string) (*service.RoutingArtifactVersion, error) {
	entity, err := r.client.RoutingArtifactVersion.Query().
		Where(routingartifactversion.ArtifactKindEQ(kind), routingartifactversion.VersionEQ(version)).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, service.ErrRoutingArtifactNotFound
	}
	if err != nil {
		return nil, err
	}
	return routingArtifactEntityToService(entity), nil
}

func (r *routingOptimizationRepository) ListArtifacts(ctx context.Context, kind, status string, limit int) ([]*service.RoutingArtifactVersion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.client.RoutingArtifactVersion.Query()
	if kind != "" {
		query = query.Where(routingartifactversion.ArtifactKindEQ(kind))
	}
	if status != "" {
		query = query.Where(routingartifactversion.StatusEQ(status))
	}
	entities, err := query.Order(dbent.Desc(routingartifactversion.FieldCreatedAt), dbent.Desc(routingartifactversion.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*service.RoutingArtifactVersion, 0, len(entities))
	for _, entity := range entities {
		result = append(result, routingArtifactEntityToService(entity))
	}
	return result, nil
}

func (r *routingOptimizationRepository) TransitionArtifact(ctx context.Context, id int64, fromStatus, toStatus string, at time.Time) error {
	if err := service.ValidateRoutingLifecycleTransition(fromStatus, toStatus); err != nil {
		return err
	}
	update := r.client.RoutingArtifactVersion.Update().
		Where(routingartifactversion.IDEQ(id), routingartifactversion.StatusEQ(fromStatus)).
		SetStatus(toStatus)
	if toStatus == service.RoutingLifecycleActive {
		update.SetActivatedAt(at)
	}
	if toStatus == service.RoutingLifecycleRetired {
		update.SetRetiredAt(at)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("transition routing artifact: %w", err)
	}
	if count != 1 {
		return service.ErrRoutingLifecycleConflict
	}
	return nil
}

func (r *routingOptimizationRepository) PromoteArtifact(ctx context.Context, id int64, fromStatus, toStatus string, expectedActiveVersion *string, at time.Time) error {
	if err := service.ValidateRoutingLifecycleTransition(fromStatus, toStatus); err != nil {
		return err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	target, err := tx.RoutingArtifactVersion.Query().Where(routingartifactversion.IDEQ(id)).ForUpdate().Only(ctx)
	if dbent.IsNotFound(err) {
		return service.ErrRoutingArtifactNotFound
	}
	if err != nil {
		return err
	}
	if target.Status != fromStatus {
		return service.ErrRoutingLifecycleConflict
	}
	scopePredicates := []predicate.RoutingArtifactVersion{
		routingartifactversion.ArtifactKindEQ(target.ArtifactKind),
		routingartifactversion.PlatformEQ(target.Platform),
		routingartifactversion.ModelFamilyEQ(target.ModelFamily),
		routingartifactversion.EndpointKindEQ(target.EndpointKind),
	}
	if target.Preference == nil {
		scopePredicates = append(scopePredicates, routingartifactversion.PreferenceIsNil())
	} else {
		scopePredicates = append(scopePredicates, routingartifactversion.PreferenceEQ(*target.Preference))
	}
	scopeRows, err := tx.RoutingArtifactVersion.Query().Where(scopePredicates...).Order(dbent.Asc(routingartifactversion.FieldID)).ForUpdate().All(ctx)
	if err != nil {
		return err
	}
	var currentActive *dbent.RoutingArtifactVersion
	for _, row := range scopeRows {
		if row.Status == service.RoutingLifecycleActive {
			currentActive = row
			break
		}
	}
	if expectedActiveVersion != nil {
		current := ""
		if currentActive != nil {
			current = currentActive.Version
		}
		if current != *expectedActiveVersion {
			return service.ErrRoutingLifecycleConflict
		}
	}
	if toStatus == service.RoutingLifecycleActive && currentActive != nil && currentActive.ID != target.ID {
		if _, err := tx.RoutingArtifactVersion.UpdateOneID(currentActive.ID).SetStatus(service.RoutingLifecyclePaused).Save(ctx); err != nil {
			return err
		}
	}
	if toStatus == service.RoutingLifecycleCanary {
		for _, row := range scopeRows {
			if row.Status == service.RoutingLifecycleCanary && row.ID != target.ID {
				if _, err := tx.RoutingArtifactVersion.UpdateOneID(row.ID).SetStatus(service.RoutingLifecyclePaused).Save(ctx); err != nil {
					return err
				}
			}
		}
	}
	update := tx.RoutingArtifactVersion.UpdateOneID(target.ID).SetStatus(toStatus)
	if toStatus == service.RoutingLifecycleActive {
		update.SetActivatedAt(at)
	}
	if toStatus == service.RoutingLifecycleRetired {
		update.SetRetiredAt(at)
	}
	if _, err := update.Save(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *routingOptimizationRepository) CreateExperiment(ctx context.Context, experiment *service.RoutingExperiment) error {
	if err := service.ValidateRoutingExperiment(experiment); err != nil {
		return err
	}
	created, err := r.client.RoutingExperiment.Create().
		SetExperimentKey(experiment.ExperimentKey).
		SetPlatform(experiment.Platform).
		SetModelFamily(experiment.ModelFamily).
		SetEndpointKind(experiment.EndpointKind).
		SetPreference(experiment.Preference).
		SetBaselineStrategyVersion(experiment.BaselineStrategyVersion).
		SetCandidateStrategyVersion(experiment.CandidateStrategyVersion).
		SetStatus(experiment.Status).
		SetAllocationBps(experiment.AllocationBPS).
		SetBucketSaltChecksum(experiment.BucketSaltChecksum).
		SetGuardrails(experiment.Guardrails).
		SetOfflineReplay(experiment.OfflineReplay).
		SetLastEvaluation(experiment.LastEvaluation).
		SetNillableLastEvaluatedAt(experiment.LastEvaluatedAt).
		SetNillableApprovedBy(experiment.ApprovedBy).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create routing experiment: %w", err)
	}
	experiment.ID = created.ID
	experiment.CreatedAt = created.CreatedAt
	experiment.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *routingOptimizationRepository) UpdateExperimentEvidence(ctx context.Context, id int64, expectedStatus string, offlineReplay, evaluation json.RawMessage, at time.Time) error {
	update := r.client.RoutingExperiment.Update().
		Where(routingexperiment.IDEQ(id), routingexperiment.StatusEQ(expectedStatus)).
		SetLastEvaluatedAt(at)
	if len(offlineReplay) > 0 {
		update.SetOfflineReplay(offlineReplay)
	}
	if len(evaluation) > 0 {
		update.SetLastEvaluation(evaluation)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("update routing experiment evidence: %w", err)
	}
	if count != 1 {
		return service.ErrRoutingLifecycleConflict
	}
	return nil
}

func (r *routingOptimizationRepository) GetExperiment(ctx context.Context, experimentKey string) (*service.RoutingExperiment, error) {
	entity, err := r.client.RoutingExperiment.Query().Where(routingexperiment.ExperimentKeyEQ(experimentKey)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, service.ErrRoutingArtifactNotFound
	}
	if err != nil {
		return nil, err
	}
	return routingExperimentEntityToService(entity), nil
}

func (r *routingOptimizationRepository) ListExperiments(ctx context.Context, status string, limit int) ([]*service.RoutingExperiment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := r.client.RoutingExperiment.Query()
	if status != "" {
		query = query.Where(routingexperiment.StatusEQ(status))
	}
	entities, err := query.Order(dbent.Desc(routingexperiment.FieldCreatedAt), dbent.Desc(routingexperiment.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*service.RoutingExperiment, 0, len(entities))
	for _, entity := range entities {
		result = append(result, routingExperimentEntityToService(entity))
	}
	return result, nil
}

func (r *routingOptimizationRepository) TransitionExperiment(ctx context.Context, id int64, fromStatus, toStatus string, approvedBy *int64, stopReason *string, at time.Time) error {
	if err := service.ValidateRoutingExperimentTransition(fromStatus, toStatus); err != nil {
		return err
	}
	update := r.client.RoutingExperiment.Update().
		Where(routingexperiment.IDEQ(id), routingexperiment.StatusEQ(fromStatus)).
		SetStatus(toStatus)
	if approvedBy != nil {
		update.SetApprovedBy(*approvedBy)
	}
	if toStatus == service.RoutingLifecycleCanary {
		update.SetStartedAt(at).ClearStoppedAt().ClearStopReason()
	}
	if toStatus == service.RoutingLifecyclePaused || toStatus == service.RoutingLifecycleRetired {
		update.SetStoppedAt(at).SetNillableStopReason(stopReason)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("transition routing experiment: %w", err)
	}
	if count != 1 {
		return service.ErrRoutingLifecycleConflict
	}
	return nil
}

func (r *routingOptimizationRepository) LoadCanaryMetrics(ctx context.Context, experimentKey, strategyVersion string, since time.Time) (service.RoutingCanaryMetrics, error) {
	if r == nil || r.db == nil {
		return service.RoutingCanaryMetrics{}, service.ErrRoutingArtifactUnavailable
	}
	const query = `
WITH attempt_features AS (
    SELECT
        attempts.*,
        CASE WHEN outcome_category = 'routing_decision' THEN (
            SELECT COALESCE(
                NULLIF(candidate ->> 'smoothed_success_rate', '')::double precision,
                NULLIF(candidate ->> 'success_rate', '')::double precision
            )
            FROM jsonb_array_elements(attempts.candidates) candidate
            WHERE (candidate ->> 'group_id')::bigint = attempts.selected_group_id
            LIMIT 1
        ) END AS predicted_success
    FROM routing_attempts attempts
    JOIN routing_experiments experiment ON experiment.experiment_key = $1
    WHERE attempts.experiment_id = $1 AND attempts.strategy_version = $2 AND attempts.occurred_at >= $3
      AND attempts.platform = experiment.platform
      AND attempts.model_family = experiment.model_family
      AND attempts.endpoint_kind = experiment.endpoint_kind
), decision_rollup AS (
    SELECT
        routing_decision_id,
        MAX(CASE WHEN outcome_category = 'routing_decision' THEN 1 ELSE 0 END)::double precision AS has_decision,
        MAX(CASE WHEN outcome_category IN ('success', 'all_candidates_failed') THEN 1 ELSE 0 END)::double precision AS has_final,
        MAX(CASE WHEN outcome_category = 'success' THEN 1 ELSE 0 END)::double precision AS final_success,
        MAX(CASE WHEN outcome_category IN ('route_attempt_failed', 'all_candidates_failed') THEN 1 ELSE 0 END)::double precision AS has_error,
        MAX(CASE WHEN outcome_category = 'success' AND billed_cost IS NOT NULL THEN 1 ELSE 0 END)::double precision AS has_billing,
        MAX(CASE WHEN outcome_category = 'success' AND duration_ms IS NOT NULL THEN 1 ELSE 0 END)::double precision AS has_latency,
		MAX(CASE WHEN outcome_category = 'routing_decision' THEN NULLIF(feature_schema_version, '') END) AS feature_schema_version,
        MAX(predicted_success) AS predicted_success,
        SUM(CASE WHEN outcome_category IN ('route_attempt_failed', 'capacity_overflow', 'success')
                 THEN COALESCE(queue_ms, 0) + COALESCE(duration_ms, 0) ELSE 0 END)::double precision AS chain_duration_ms,
        SUM(CASE
                WHEN outcome_category IN ('route_attempt_failed', 'capacity_overflow')
                    THEN COALESCE(queue_ms, 0) + COALESCE(duration_ms, 0)
                WHEN outcome_category = 'success'
                    THEN COALESCE(queue_ms, 0) + COALESCE(ttft_ms, duration_ms, 0)
                ELSE 0
            END)::double precision AS chain_ttft_ms,
        COALESCE(SUM(COALESCE(billed_cost, 0))
            FILTER (WHERE outcome_category IN ('route_attempt_failed', 'capacity_overflow', 'success')), 0)::double precision AS chain_billed_cost,
        COALESCE(SUM(COALESCE(actual_cost, 0))
            FILTER (WHERE outcome_category IN ('route_attempt_failed', 'capacity_overflow', 'success')), 0)::double precision AS chain_supplier_cost,
        MAX(CASE WHEN switched_group THEN GREATEST(attempt_index, 1) ELSE 0 END)::double precision AS group_switches,
        MAX(CASE WHEN switched_group THEN 1 ELSE 0 END)::double precision AS switched,
        MAX(CASE WHEN sticky_broken THEN 1 ELSE 0 END)::double precision AS sticky_broken,
        MAX(CASE WHEN cache_cold_due_to_failover THEN 1 ELSE 0 END)::double precision AS cache_cold
    FROM attempt_features
    GROUP BY routing_decision_id
)
SELECT
    COUNT(*) FILTER (WHERE has_decision = 1)::bigint,
    COUNT(*) FILTER (WHERE has_final = 1)::bigint,
    COUNT(*) FILTER (WHERE has_error = 1)::bigint,
    COUNT(*) FILTER (WHERE has_billing = 1)::bigint,
    COUNT(*) FILTER (WHERE has_latency = 1)::bigint,
    COUNT(*) FILTER (WHERE has_final = 1 AND has_decision = 0)::bigint,
    COALESCE(SUM(has_final) FILTER (WHERE has_decision = 1) / NULLIF(SUM(has_decision), 0), 0),
    COALESCE(SUM(has_billing) / NULLIF(SUM(final_success), 0), 1),
    COALESCE(SUM(has_latency) / NULLIF(SUM(final_success), 0), 1),
    COALESCE(SUM(final_success) / NULLIF(SUM(has_final), 0), 0),
    1 - COALESCE(SUM(final_success) FILTER (WHERE has_decision = 1) / NULLIF(SUM(has_decision), 0), 0),
    percentile_cont(0.95) WITHIN GROUP (ORDER BY chain_duration_ms)
        FILTER (WHERE final_success = 1),
    percentile_cont(0.95) WITHIN GROUP (ORDER BY chain_ttft_ms)
        FILTER (WHERE final_success = 1),
    percentile_cont(0.99) WITHIN GROUP (ORDER BY chain_duration_ms)
        FILTER (WHERE final_success = 1),
    percentile_cont(0.99) WITHIN GROUP (ORDER BY chain_ttft_ms)
        FILTER (WHERE final_success = 1),
    SUM(chain_billed_cost) / NULLIF(SUM(final_success), 0),
    AVG(chain_duration_ms) FILTER (WHERE final_success = 1),
    AVG(chain_ttft_ms) FILTER (WHERE final_success = 1),
    SUM(chain_supplier_cost) / NULLIF(SUM(has_decision), 0),
    COALESCE(AVG(switched) FILTER (WHERE has_final = 1), 0),
    COALESCE(AVG(group_switches) FILTER (WHERE has_final = 1), 0),
    COALESCE(AVG(sticky_broken) FILTER (WHERE has_final = 1), 0),
    COALESCE(AVG(cache_cold) FILTER (WHERE has_final = 1), 0),
    COALESCE(AVG(LEAST(1.0, (group_switches + sticky_broken + cache_cold) / 3.0)) FILTER (WHERE has_final = 1), 0),
    COALESCE(AVG(POWER(predicted_success - final_success, 2)) FILTER (WHERE has_final = 1 AND predicted_success IS NOT NULL), 1),
	COALESCE(AVG(CASE WHEN predicted_success IS NULL OR feature_schema_version IS NULL THEN 1.0 ELSE 0.0 END) FILTER (WHERE has_decision = 1), 1),
	COUNT(DISTINCT feature_schema_version) FILTER (WHERE has_decision = 1)::bigint,
	MAX(feature_schema_version) FILTER (WHERE has_decision = 1)
FROM decision_rollup`
	var metrics service.RoutingCanaryMetrics
	var p95, p95TTFT, p99, p99TTFT, cost, expectedTime, expectedTTFT, supplierCost sql.NullFloat64
	var featureSchemaVersion sql.NullString
	if err := r.db.QueryRowContext(ctx, query, experimentKey, strategyVersion, since).Scan(
		&metrics.Decisions, &metrics.FinalEvents, &metrics.ErrorEvents, &metrics.BillingEvents, &metrics.LatencyEvents,
		&metrics.OrphanFinalEvents, &metrics.EventCoverage, &metrics.BillingCoverage, &metrics.LatencyCoverage,
		&metrics.FinalSuccessRate, &metrics.FailureRisk, &p95, &p95TTFT, &p99, &p99TTFT, &cost, &expectedTime, &expectedTTFT, &supplierCost,
		&metrics.SwitchRate, &metrics.AverageGroupSwitches, &metrics.StickyBreakRate, &metrics.CacheColdRate, &metrics.StabilityLoss,
		&metrics.PredictionCalibrationError, &metrics.MissingFeatureRate, &metrics.FeatureSchemaVersionCount, &featureSchemaVersion,
	); err != nil {
		return service.RoutingCanaryMetrics{}, fmt.Errorf("load routing canary metrics: %w", err)
	}
	metrics.P95LatencyMS = p95.Float64
	metrics.P95TTFTMS = p95TTFT.Float64
	metrics.P99LatencyMS = p99.Float64
	metrics.P99TTFTMS = p99TTFT.Float64
	metrics.CostPerSuccess = cost.Float64
	metrics.ExpectedSuccessfulCost = cost.Float64
	metrics.ExpectedTimeToSuccessMS = expectedTime.Float64
	metrics.ExpectedTTFTToSuccessMS = expectedTTFT.Float64
	metrics.SupplierCostPerDecision = supplierCost.Float64
	metrics.StrategyVersion = strategyVersion
	metrics.ScoreLossMappingVersion = service.RoutingScoreLossMappingVersion
	metrics.FeatureSchemaVersion = featureSchemaVersion.String
	metrics.FeatureDriftDetected = metrics.FeatureSchemaVersionCount != 1
	metrics.ObservationDuration = time.Since(since)
	metrics.CriticalSlicesHealthy = metrics.Decisions > 0 && metrics.OrphanFinalEvents == 0
	successes := int64(math.Round(metrics.FinalSuccessRate * float64(metrics.FinalEvents)))
	metrics.SuccessRateLowerBound, metrics.SuccessRateUpperBound = service.RoutingWilsonInterval(successes, metrics.FinalEvents)
	slices, err := r.loadCanaryKeySlices(ctx, experimentKey, strategyVersion, since)
	if err != nil {
		return service.RoutingCanaryMetrics{}, err
	}
	metrics.CriticalSlices = slices
	return metrics, nil
}

func (r *routingOptimizationRepository) loadCanaryKeySlices(ctx context.Context, experimentKey, strategyVersion string, since time.Time) ([]service.RoutingCanarySliceMetric, error) {
	const query = `
WITH per_decision AS (
    SELECT
        routing_decision_id,
        MAX(api_key_id) AS api_key_id,
        MAX(CASE WHEN outcome_category = 'routing_decision' THEN 1 ELSE 0 END)::bigint AS has_decision,
        MAX(CASE WHEN outcome_category IN ('success', 'all_candidates_failed') THEN 1 ELSE 0 END)::bigint AS has_final,
        MAX(CASE WHEN outcome_category = 'success' THEN 1 ELSE 0 END)::bigint AS final_success
    FROM routing_attempts attempts
    JOIN routing_experiments experiment ON experiment.experiment_key = $1
    WHERE attempts.experiment_id = $1 AND attempts.strategy_version = $2 AND attempts.occurred_at >= $3
      AND attempts.api_key_id IS NOT NULL
      AND attempts.platform = experiment.platform
      AND attempts.model_family = experiment.model_family
      AND attempts.endpoint_kind = experiment.endpoint_kind
    GROUP BY routing_decision_id
), per_key AS (
    SELECT api_key_id, SUM(has_decision)::bigint AS decisions, SUM(has_final)::bigint AS finals,
           SUM(final_success)::bigint AS successes
    FROM per_decision
    GROUP BY api_key_id
)
SELECT api_key_id, decisions, finals, successes
FROM per_key
WHERE decisions >= 10
ORDER BY decisions DESC, api_key_id
LIMIT 200`
	rows, err := r.db.QueryContext(ctx, query, experimentKey, strategyVersion, since)
	if err != nil {
		return nil, fmt.Errorf("load routing canary key slices: %w", err)
	}
	defer rows.Close()
	result := make([]service.RoutingCanarySliceMetric, 0)
	for rows.Next() {
		var item service.RoutingCanarySliceMetric
		var successes int64
		if err := rows.Scan(&item.APIKeyID, &item.Decisions, &item.FinalEvents, &successes); err != nil {
			return nil, fmt.Errorf("scan routing canary key slice: %w", err)
		}
		if item.FinalEvents > 0 {
			item.FinalSuccessRate = float64(successes) / float64(item.FinalEvents)
		}
		item.SuccessRateLowerBound, item.SuccessRateUpperBound = service.RoutingWilsonInterval(successes, item.FinalEvents)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing canary key slices: %w", err)
	}
	return result, nil
}

func (r *routingOptimizationRepository) CreateRoutingAttempts(ctx context.Context, facts []*service.RoutingAttemptFact) error {
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
			SetNillableSmartPreference(fact.SmartPreference).
			SetNillableSmartBalanceBps(fact.SmartBalanceBPS).
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

func (r *routingOptimizationRepository) PruneRoutingAttempts(ctx context.Context, sampleBefore, diagnosticBefore, criticalBefore time.Time, limit int) (int64, error) {
	if r == nil || r.db == nil {
		return 0, service.ErrRoutingArtifactUnavailable
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

func (r *routingOptimizationRepository) LoadRoutingReplayDecisions(ctx context.Context, scope service.RoutingArtifactScope, strategyVersion string, since, until time.Time, limit int) ([]service.RoutingReplayDecision, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrRoutingArtifactUnavailable
	}
	if limit <= 0 || limit > 50_000 {
		limit = 50_000
	}
	preference := ""
	if scope.Preference != nil {
		preference = *scope.Preference
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT routing_decision_id, route_version, selected_group_id, candidates,
       feature_schema_version, sample_probability, occurred_at, smart_balance_bps, routing_min_success_rate
FROM routing_attempts
WHERE outcome_category = 'routing_decision'
  AND platform = $1 AND model_family = $2 AND endpoint_kind = $3
  AND COALESCE(smart_preference, '') = $4
  AND strategy_version = $5
  AND occurred_at >= $6 AND occurred_at < $7
ORDER BY occurred_at, routing_decision_id
LIMIT $8`, scope.Platform, scope.ModelFamily, scope.EndpointKind, preference, strategyVersion, since, until, limit)
	if err != nil {
		return nil, fmt.Errorf("load routing replay decisions: %w", err)
	}
	defer rows.Close()
	result := make([]service.RoutingReplayDecision, 0)
	for rows.Next() {
		var item service.RoutingReplayDecision
		var candidates []byte
		if err := rows.Scan(&item.RoutingDecisionID, &item.RouteVersion, &item.SelectedGroupID, &candidates,
			&item.FeatureSchemaVersion, &item.SampleProbability, &item.OccurredAt, &item.SmartBalanceBPS, &item.RoutingMinSuccessRate); err != nil {
			return nil, fmt.Errorf("scan routing replay decision: %w", err)
		}
		if err := json.Unmarshal(candidates, &item.Candidates); err != nil {
			return nil, fmt.Errorf("decode routing replay candidates: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing replay decisions: %w", err)
	}
	return result, nil
}

func routingArtifactEntityToService(entity *dbent.RoutingArtifactVersion) *service.RoutingArtifactVersion {
	if entity == nil {
		return nil
	}
	return &service.RoutingArtifactVersion{
		ID: entity.ID, ArtifactKind: entity.ArtifactKind, Version: entity.Version,
		ParentVersion: entity.ParentVersion, Platform: entity.Platform, ModelFamily: entity.ModelFamily,
		EndpointKind: entity.EndpointKind, Preference: entity.Preference, Status: entity.Status,
		SchemaVersion: entity.SchemaVersion, Checksum: entity.Checksum, Payload: entity.Payload,
		Dependencies: entity.Dependencies, Lineage: entity.Lineage, CreatedBy: entity.CreatedBy,
		ActivatedAt: entity.ActivatedAt, RetiredAt: entity.RetiredAt, CreatedAt: entity.CreatedAt,
	}
}

func routingExperimentEntityToService(entity *dbent.RoutingExperiment) *service.RoutingExperiment {
	if entity == nil {
		return nil
	}
	return &service.RoutingExperiment{
		ID: entity.ID, ExperimentKey: entity.ExperimentKey, Platform: entity.Platform, ModelFamily: entity.ModelFamily,
		EndpointKind: entity.EndpointKind, Preference: entity.Preference, BaselineStrategyVersion: entity.BaselineStrategyVersion,
		CandidateStrategyVersion: entity.CandidateStrategyVersion, Status: entity.Status, AllocationBPS: entity.AllocationBps,
		BucketSaltChecksum: entity.BucketSaltChecksum, Guardrails: entity.Guardrails, StartedAt: entity.StartedAt,
		OfflineReplay: entity.OfflineReplay, LastEvaluation: entity.LastEvaluation, LastEvaluatedAt: entity.LastEvaluatedAt,
		StoppedAt: entity.StoppedAt, StopReason: entity.StopReason, ApprovedBy: entity.ApprovedBy,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

var _ service.RoutingOptimizationRepository = (*routingOptimizationRepository)(nil)
