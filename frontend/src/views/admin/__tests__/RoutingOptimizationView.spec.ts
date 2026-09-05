import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RoutingOptimizationView from '../RoutingOptimizationView.vue'

const { listExperiments, getPointers, rollbackExperiment, pauseExperiment } = vi.hoisted(() => ({
  listExperiments: vi.fn(),
  getPointers: vi.fn(),
  rollbackExperiment: vi.fn(),
  pauseExperiment: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    routingOptimization: {
      listExperiments,
      getPointers,
      rollbackExperiment,
      pauseExperiment,
      resumeExperiment: vi.fn(),
      runOfflineReplay: vi.fn(),
    },
  },
}))

vi.mock('vue-i18n', async importOriginal => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const experiment = {
  id: 1,
  experiment_key: 'openai-gpt5-price-v2',
  platform: 'openai',
  model_family: 'gpt-5',
  endpoint_kind: 'responses',
  preference: 'price',
  baseline_strategy_version: 'baseline-v1',
  candidate_strategy_version: 'candidate-v2',
  status: 'canary',
  allocation_bps: 500,
  bucket_salt_checksum: 'a'.repeat(64),
  guardrails: {},
  offline_replay: {
    replay_version: 'routing-offline-replay-v1',
    passed: true,
    decision_count: 2000,
    replayed_decisions: 2000,
    changed_top_choice: 200,
    invalid_decisions: 0,
    hard_constraint_violations: 0,
    selection_bias_notice: 'observational only',
    dataset_manifest: {
      purpose: 'offline routing safety replay',
      query_version: 'routing-replay-decisions-v2',
      since: '2026-09-04T00:00:00Z',
      until: '2026-09-05T00:00:00Z',
      feature_schema_versions: ['routing-features-v1'],
      sampling_rule: 'recorded sample_probability',
      exclusion_rules: ['outside window'],
      point_in_time_join: 'decision-time features only',
      row_count: 2000,
      minimum_sample_probability: 0.01,
      maximum_sample_probability: 1,
      checksum: 'b'.repeat(64),
      created_at: '2026-09-05T00:00:00Z',
    },
  },
  last_evaluation: {
    metric_policy_version: 'routing-promotion-metrics-v1',
    score_loss_mapping_version: 'routing-score-loss-map-v1',
    primary_metric: 'cost_per_success',
    promotion_eligible: true,
    baseline_samples: 10000,
    candidate_samples: 1000,
    success_rate_drift: -0.001,
    cost_drift_ratio: -0.05,
    latency_drift_ratio: -0.1,
    baseline_metrics: {
      final_success_rate: 0.991,
      failure_risk: 0.009,
      expected_successful_cost: 1.2,
      expected_time_to_success_ms: 900,
      p95_latency_ms: 1100,
      p99_latency_ms: 2000,
      p95_ttft_ms: 220,
      p99_ttft_ms: 420,
      stability_loss: 0.02,
    },
    candidate_metrics: {
      decisions: 1000,
      expected_decisions: 1100,
      observation_duration: 7_200_000_000_000,
      event_coverage: 1,
      final_success_rate: 0.99,
      failure_risk: 0.01,
      success_rate_lower_bound: 0.981,
      success_rate_upper_bound: 0.995,
      p95_ttft_ms: 200,
      p99_ttft_ms: 390,
      p95_latency_ms: 1000,
      p99_latency_ms: 1800,
      expected_successful_cost: 1.14,
      expected_time_to_success_ms: 810,
      stability_loss: 0.015,
      prediction_calibration_error: 0.04,
      missing_feature_rate: 0.01,
      feature_drift_detected: false,
      critical_slices: [
        {
          api_key_id: 42,
          decisions: 200,
          final_success_rate: 0.99,
          success_rate_lower_bound: 0.96,
          success_rate_upper_bound: 1,
        },
      ],
    },
  },
  stop_reason: 'canary_guardrail:event_coverage_incomplete',
  created_at: '2026-09-05T00:00:00Z',
  updated_at: '2026-09-05T01:00:00Z',
}

describe('RoutingOptimizationView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listExperiments.mockResolvedValue([experiment])
    getPointers.mockResolvedValue({
      baseline_version: 'baseline-v1',
      active_version: 'baseline-v1',
      canary_version: 'candidate-v2',
      shadow_version: 'candidate-v3',
      updated_at: '2026-09-05T01:00:00Z',
    })
    rollbackExperiment.mockResolvedValue({ active_version: 'baseline-v1' })
    pauseExperiment.mockResolvedValue({ ...experiment, status: 'paused' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  it('展示版本指针、实验证据、关键切片和回滚原因', async () => {
    const wrapper = mount(RoutingOptimizationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('candidate-v2')
    expect(wrapper.text()).toContain('candidate-v3')
    expect(wrapper.text()).toContain('cost_per_success')
    expect(wrapper.text()).toContain('routing-score-loss-map-v1')
    expect(wrapper.text()).toContain('routing-replay-decisions-v2')
    expect(wrapper.text()).toContain('routing-features-v1')
    expect(wrapper.text()).toContain('P99 Duration')
    expect(wrapper.text()).toContain('1800 ms')
    expect(wrapper.text()).toContain('90.91%')
    expect(wrapper.text()).toContain('canary_guardrail:event_coverage_incomplete')
    expect(wrapper.text()).toContain('42')

    const rollback = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.routingOptimization.rollback'))
    expect(rollback).toBeDefined()
    await rollback!.trigger('click')
    await flushPromises()
    expect(rollbackExperiment).toHaveBeenCalledWith(expect.objectContaining({ id: 1 }))
  })
})
