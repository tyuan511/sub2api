import { apiClient } from '../client'

export type RoutingLifecycle = 'draft' | 'shadow' | 'canary' | 'active' | 'paused' | 'retired'

export interface RoutingArtifactPointers {
  baseline_version: string
  active_version: string
  canary_version?: string
  canary_allocation_bps?: number
  canary_experiment_id?: string
  canary_bucket_salt_checksum?: string
  shadow_version?: string
  updated_at: string
}

export interface RoutingOfflineReplayReport {
  replay_version: string
  candidate_strategy_version: string
  source_strategy_version: string
  since: string
  until: string
  minimum_decisions: number
  decision_count: number
  replayed_decisions: number
  changed_top_choice: number
  missing_feature_decisions: number
  invalid_decisions: number
  hard_constraint_violations: number
  passed: boolean
  causal_claim_allowed: boolean
  selection_bias_notice: string
  dataset_manifest: RoutingDatasetManifest
  evaluated_at: string
}

export interface RoutingDatasetManifest {
  purpose: string
  query_version: string
  since: string
  until: string
  feature_schema_versions: string[]
  sampling_rule: string
  exclusion_rules: string[]
  point_in_time_join: string
  row_count: number
  minimum_sample_probability: number
  maximum_sample_probability: number
  checksum: string
  created_at: string
}

export interface RoutingCanarySliceMetric {
  api_key_id: number
  decisions: number
  final_events: number
  final_success_rate: number
  success_rate_lower_bound: number
  success_rate_upper_bound: number
}

export interface RoutingCanaryMetrics {
  strategy_version: string
  decisions: number
  expected_decisions: number
  final_events: number
  error_events: number
  billing_events: number
  latency_events: number
  orphan_final_events: number
  observation_duration: number
  event_coverage: number
  billing_coverage: number
  latency_coverage: number
  final_success_rate: number
  failure_risk: number
  success_rate_lower_bound: number
  success_rate_upper_bound: number
  p95_latency_ms: number
  p95_ttft_ms: number
  p99_latency_ms: number
  p99_ttft_ms: number
  cost_per_success: number
  expected_successful_cost: number
  expected_time_to_success_ms: number
  expected_ttft_to_success_ms: number
  supplier_cost_per_decision: number
  switch_rate: number
  average_group_switches: number
  sticky_break_rate: number
  cache_cold_rate: number
  stability_loss: number
  score_loss_mapping_version: string
  prediction_calibration_error: number
  missing_feature_rate: number
  feature_schema_version_count: number
  feature_schema_version: string
  feature_drift_detected: boolean
  critical_slices_healthy: boolean
  critical_slices?: RoutingCanarySliceMetric[]
}

export interface RoutingCanaryEvaluation {
  ready: boolean
  rollback: boolean
  promotion_eligible: boolean
  preference: string
  metric_policy_version: string
  score_loss_mapping_version: string
  baseline_strategy_version: string
  candidate_strategy_version: string
  primary_metric: string
  baseline_primary_value: number
  candidate_primary_value: number
  balanced_loss_difference: number
  success_rate_drift: number
  cost_drift_ratio: number
  latency_drift_ratio: number
  violations?: string[]
  baseline_samples: number
  candidate_samples: number
  baseline_metrics: RoutingCanaryMetrics
  candidate_metrics: RoutingCanaryMetrics
}

export interface RoutingExperiment {
  id: number
  experiment_key: string
  platform: string
  model_family: string
  endpoint_kind: string
  preference: string
  baseline_strategy_version: string
  candidate_strategy_version: string
  status: RoutingLifecycle
  allocation_bps: number
  bucket_salt_checksum: string
  guardrails: Record<string, unknown>
  offline_replay?: Partial<RoutingOfflineReplayReport>
  last_evaluation?: Partial<RoutingCanaryEvaluation>
  last_evaluated_at?: string
  started_at?: string
  stopped_at?: string
  stop_reason?: string
  approved_by?: number
  created_at: string
  updated_at: string
}

export async function listExperiments(status = ''): Promise<RoutingExperiment[]> {
  const { data } = await apiClient.get('/admin/routing-optimization/experiments', {
    params: status ? { status } : undefined,
  })
  return data
}

export async function getPointers(experiment: RoutingExperiment): Promise<RoutingArtifactPointers> {
  const { data } = await apiClient.get('/admin/routing-optimization/pointers', {
    params: {
      artifact_kind: 'strategy',
      platform: experiment.platform,
      model_family: experiment.model_family,
      endpoint_kind: experiment.endpoint_kind,
      preference: experiment.preference,
    },
  })
  return data
}

export async function runOfflineReplay(experimentKey: string): Promise<RoutingOfflineReplayReport> {
  const { data } = await apiClient.post(`/admin/routing-optimization/experiments/${encodeURIComponent(experimentKey)}/offline-replay`, {})
  return data
}

export async function pauseExperiment(experimentKey: string, reason: string): Promise<RoutingExperiment> {
  const { data } = await apiClient.post(`/admin/routing-optimization/experiments/${encodeURIComponent(experimentKey)}/pause`, { reason })
  return data
}

export async function resumeExperiment(experimentKey: string): Promise<RoutingExperiment> {
  const { data } = await apiClient.post(`/admin/routing-optimization/experiments/${encodeURIComponent(experimentKey)}/resume`, {})
  return data
}

export async function rollbackExperiment(experiment: RoutingExperiment): Promise<RoutingArtifactPointers> {
  const { data } = await apiClient.post('/admin/routing-optimization/rollback', {
    artifact_kind: 'strategy',
    platform: experiment.platform,
    model_family: experiment.model_family,
    endpoint_kind: experiment.endpoint_kind,
    preference: experiment.preference,
  })
  return data
}

export const routingOptimizationAPI = {
  listExperiments,
  getPointers,
  runOfflineReplay,
  pauseExperiment,
  resumeExperiment,
  rollbackExperiment,
}

export default routingOptimizationAPI
