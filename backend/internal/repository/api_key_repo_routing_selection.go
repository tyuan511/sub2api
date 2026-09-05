package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// LoadRecentAPIKeyRoutingSelectionObservations is a bounded control-plane
// read. It reconstructs recent successful landing shares from sampled facts
// with inverse-probability weights; it is deliberately not used by gateway
// authentication or request routing.
func (r *apiKeyRepository) LoadRecentAPIKeyRoutingSelectionObservations(
	ctx context.Context,
	apiKeyIDs []int64,
	since time.Time,
) ([]service.APIKeyRoutingSelectionObservation, error) {
	if r == nil || r.sql == nil || len(apiKeyIDs) == 0 {
		return []service.APIKeyRoutingSelectionObservation{}, nil
	}
	if len(apiKeyIDs) > 500 {
		apiKeyIDs = apiKeyIDs[:500]
	}
	rows, err := r.sql.QueryContext(ctx, `
WITH successful_decisions AS (
    SELECT DISTINCT ON (routing_decision_id)
        api_key_id,
        route_version,
        platform,
        model_family,
        endpoint_kind,
        strategy_version,
        smart_preference,
        selected_group_id,
        sample_probability,
        occurred_at
    FROM routing_attempts
    WHERE api_key_id = ANY($1)
      AND schedule_mode = 'smart'
      AND outcome_category = 'success'
      AND selected_group_id IS NOT NULL
      AND sample_probability > 0
      AND created_at >= $2
      AND occurred_at >= $2
    ORDER BY routing_decision_id, occurred_at DESC, id DESC
)
SELECT
    api_key_id,
    route_version,
    platform,
    model_family,
    endpoint_kind,
    strategy_version,
    smart_preference,
    selected_group_id,
    COUNT(*)::bigint AS sampled_selections,
    SUM(1.0 / sample_probability)::double precision AS weighted_selections,
    SUM(1.0 / (sample_probability * sample_probability))::double precision AS weight_squares,
    MAX(occurred_at) AS data_through
FROM successful_decisions
GROUP BY api_key_id, route_version, platform, model_family, endpoint_kind,
         strategy_version, smart_preference, selected_group_id
ORDER BY api_key_id, route_version, platform, model_family, endpoint_kind,
         strategy_version, smart_preference, selected_group_id`, pq.Array(apiKeyIDs), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("load api key routing selection observations: %w", err)
	}
	defer rows.Close()

	result := make([]service.APIKeyRoutingSelectionObservation, 0)
	for rows.Next() {
		var observation service.APIKeyRoutingSelectionObservation
		if err := rows.Scan(
			&observation.APIKeyID,
			&observation.RouteVersion,
			&observation.Platform,
			&observation.ModelFamily,
			&observation.EndpointKind,
			&observation.StrategyVersion,
			&observation.SmartPreference,
			&observation.GroupID,
			&observation.SampledSelections,
			&observation.WeightedSelections,
			&observation.WeightSquares,
			&observation.DataThrough,
		); err != nil {
			return nil, fmt.Errorf("scan api key routing selection observation: %w", err)
		}
		result = append(result, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api key routing selection observations: %w", err)
	}
	return result, nil
}
