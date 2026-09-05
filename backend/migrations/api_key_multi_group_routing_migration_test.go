package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration240CreatesAPIKeyMultiGroupRoutingFoundation(t *testing.T) {
	content, err := FS.ReadFile("240_api_key_multi_group_routing.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS schedule_mode VARCHAR(16) NOT NULL DEFAULT 'sequential'")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS smart_preference VARCHAR(16)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS route_version BIGINT NOT NULL DEFAULT 1")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS api_key_group_routes")
	require.Contains(t, sql, "UNIQUE (api_key_id, group_id)")
	require.Contains(t, sql, "UNIQUE (api_key_id, priority)")
	require.Contains(t, sql, "REFERENCES api_keys(id) ON DELETE CASCADE")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS api_key_route_config_outbox")
	require.Contains(t, sql, "ON CONFLICT (api_key_id, group_id) DO NOTHING")
	require.Contains(t, sql, "WHERE group_id IS NOT NULL")
}

func TestMigration248AddsBoundedUserControlsAndIndependentRuntimeVersion(t *testing.T) {
	content, err := FS.ReadFile("248_api_key_routing_controls.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "smart_balance_bps BETWEEN 0 AND 10000")
	require.Contains(t, sql, "routing_min_success_rate BETWEEN 50 AND 95 AND routing_min_success_rate % 5 = 0")
	require.Contains(t, sql, "UPDATE api_keys SET routing_state_version = route_version")
	require.Contains(t, sql, "GREATEST(NEW.route_version, OLD.route_version + 1)")
	require.Contains(t, sql, "ALTER TABLE routing_attempts")
}

func TestMigration249OnlyChangesNewKeyDefault(t *testing.T) {
	content, err := FS.ReadFile("249_api_key_default_success_threshold.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE api_keys ALTER COLUMN routing_min_success_rate SET DEFAULT 80;")
	require.NotContains(t, sql, "UPDATE api_keys")
	require.NotContains(t, sql, "ALTER TABLE routing_attempts")
	require.NotContains(t, sql, "DROP ")

	// Already-applied migration 248 must still initialize legacy keys at 50%.
	previous, err := FS.ReadFile("248_api_key_routing_controls.sql")
	require.NoError(t, err)
	require.Contains(t, string(previous), "ADD COLUMN routing_min_success_rate integer NOT NULL DEFAULT 50")
}

func TestMigration240PreservesLegacyUnscopedKeys(t *testing.T) {
	content, err := FS.ReadFile("240_api_key_multi_group_routing.sql")
	require.NoError(t, err)

	sql := string(content)
	require.NotContains(t, sql, "WHERE group_id IS NULL\nON CONFLICT")
	require.Contains(t, sql, "Historical NULL group_id")
}

func TestMigration241CreatesBoundedReplayAndVersionFoundation(t *testing.T) {
	content, err := FS.ReadFile("241_api_key_routing_optimization_foundation.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS routing_artifact_versions")
	require.Contains(t, sql, "UNIQUE (artifact_kind, version)")
	require.Contains(t, sql, "'draft', 'shadow', 'canary', 'active', 'paused', 'retired'")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS routing_experiments")
	require.Contains(t, sql, "allocation_bps BETWEEN 0 AND 10000")
	require.Contains(t, sql, "offline_replay JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "last_evaluation JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "last_evaluated_at TIMESTAMPTZ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS routing_attempts")
	require.Contains(t, sql, "jsonb_array_length(candidates) <= 8")
	require.Contains(t, sql, "assignment_reason <> 'deterministic' OR action_propensity IS NULL")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS actual_usage JSONB")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS billable_usage JSONB")
	require.NotContains(t, strings.ToLower(sql), "prompt")
	require.NotContains(t, strings.ToLower(sql), "response_body")
	require.NotContains(t, strings.ToLower(sql), "api_key_plaintext")
}

func TestMigration242CreatesActualUsagePriceReplayRollup(t *testing.T) {
	content, err := FS.ReadFile("242_api_key_routing_price_metrics.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS api_key_routing_price_metrics_1m")
	require.Contains(t, sql, "cache_creation_5m_tokens BIGINT NOT NULL")
	require.Contains(t, sql, "cache_creation_1h_tokens BIGINT NOT NULL")
	require.Contains(t, sql, "endpoint_kind TEXT NOT NULL")
	require.Contains(t, sql, "service_tier TEXT NOT NULL")
	require.Contains(t, sql, "context_bucket SMALLINT NOT NULL")
	require.Contains(t, sql, "PRIMARY KEY ( bucket_start, platform, group_id, model, endpoint_kind, service_tier, context_bucket )")
	require.Contains(t, sql, "failover cold-cache compensation rows are excluded")
}

func TestMigration243AddsStickyBreakChainFact(t *testing.T) {
	content, err := FS.ReadFile("243_api_key_routing_stability_facts.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS sticky_broken BOOLEAN NOT NULL DEFAULT FALSE")
}

func TestMigration244AddsConcurrentRoutingFactRetentionIndex(t *testing.T) {
	content, err := FS.ReadFile("244_api_key_routing_fact_retention_index_notx.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_routing_attempts_priority_occurred")
	require.Contains(t, sql, "ON routing_attempts (event_priority, occurred_at, id)")
}

func TestMigration245CreatesEndpointRoutingHealthMetrics(t *testing.T) {
	content, err := FS.ReadFile("245_api_key_routing_health_metrics.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS api_key_routing_health_metrics_1m")
	require.Contains(t, sql, "PRIMARY KEY (bucket_start, platform, group_id, model, endpoint_kind)")
	require.Contains(t, sql, "capacity_overflow_requests BIGINT NOT NULL")
	require.Contains(t, sql, "failure_categories JSONB NOT NULL")
	require.Contains(t, sql, "input_tokens BIGINT NOT NULL")
	require.Contains(t, sql, "ttft_sum_ms BIGINT NOT NULL")
}

func TestMigration246AddsMultiGroupReverseDependencyInvalidation(t *testing.T) {
	content, err := FS.ReadFile("246_api_key_route_dependency_invalidation.sql")
	require.NoError(t, err)
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS routing_dependency_version BIGINT NOT NULL DEFAULT 1",
		"api_keys_routing_dependency_version_check",
		"idx_api_key_group_routes_group_api_key",
		"idx_api_key_route_config_outbox_delivered",
		"enqueue_route_dependent_group_auth_cache_invalidation",
		"JOIN api_keys AS k ON k.id = route.api_key_id",
		"OLD.route_version IS DISTINCT FROM NEW.route_version",
		"OLD.schedule_mode IS DISTINCT FROM NEW.schedule_mode",
		"trg_groups_auth_cache_invalidation",
		"trg_user_allowed_groups_auth_cache_invalidation",
		"OLD.restrict_public_groups IS NOT DISTINCT FROM NEW.restrict_public_groups",
		"SET routing_dependency_version = routing_dependency_version + 1",
		"trg_user_subscriptions_auth_cache_invalidation",
		"OLD.expires_at IS NOT DISTINCT FROM NEW.expires_at",
		"trg_user_group_rates_auth_cache_invalidation",
		"api_key_dependency:%s:v%s",
		"'old_dependency_version', OLD.routing_dependency_version",
		"'dependency_version', NEW.routing_dependency_version",
		"encode(sha256(convert_to(NEW.key, 'UTF8')), 'hex')",
	} {
		require.Contains(t, sql, required)
	}
	require.NotContains(t, sql, "payload, key")
	require.NotContains(t, sql, "OLD.daily_usage_usd IS DISTINCT")
}

func TestMigration247PersistsBatchImageActualGroup(t *testing.T) {
	content, err := FS.ReadFile("247_batch_image_actual_group.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL")
	require.Contains(t, sql, "ON batch_image_jobs (group_id, created_at DESC)")
	require.Contains(t, sql, "Physical group selected before batch submission")
}
