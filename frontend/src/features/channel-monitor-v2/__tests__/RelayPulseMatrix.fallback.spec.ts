import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import RelayPulseMatrix from '../RelayPulseMatrix.vue'
import type { MonitorHealth, MonitorMetric } from '@/api/channelMonitorV2'

const i18nT = (key: string, params?: Record<string, unknown>) => {
  const map: Record<string, string> = {
    'channelMonitorV2.metrics.cacheRate': '缓存率',
    'channelMonitorV2.metrics.availability': '可用率',
    'channelMonitorV2.metrics.ttft': '首 Token',
    'channelMonitorV2.metrics.availabilityValue': '可用率 {value}',
    'channelMonitorV2.metrics.cacheRateValue': '缓存率 {value}',
    'channelMonitorV2.metrics.ttftValue': '首 Token {value}',
    'channelMonitorV2.matrix.healthyLegend': '健康 (≥80)',
    'channelMonitorV2.matrix.unknownLegend': '样本不足',
    'channelMonitorV2.matrix.noTrafficAt': '{time} · 无流量',
    'channelMonitorV2.matrix.carriedOverAt': '{time} · 无新数据，沿用最近状态',
    'monitorCommon.history60pts': '近 {n} 次记录',
    'monitorCommon.nextUpdateIn': '{n}s 后刷新',
    'monitorCommon.past': 'PAST',
    'monitorCommon.now': 'NOW',
  }
  return (map[key] || key).replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
}

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: false }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: i18nT,
      te: (key: string) => key.startsWith('channelMonitorV2.') || key.startsWith('monitorCommon.'),
      locale: { value: 'zh' },
    }),
  }
})

const health: MonitorHealth = {
  overall: 'unknown',
  error_rate: 'unknown',
  ttft: 'unknown',
  cache: 'unknown',
  score: null,
  minimum_sample: 20,
}

/** 行级聚合全零 + 无 bucket：从未有过流量的分组行。 */
function idleRowMetrics(): MonitorMetric {
  return {
    success_requests: 0,
    error_requests: 0,
    request_count: 0,
    token_count: 0,
    rpm: 0,
    tpm: 0,
    error_rate: 0,
    cache_rate: 0,
    cache_rate_numerator: 0,
    cache_rate_denominator: 0,
    ttft: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null },
    duration: { sample_count: 0, p50_ms: null, p95_ms: null, avg_ms: null },
  }
}

const coverage = {
  requested_start: '2026-08-01T00:00:00Z',
  requested_end: '2026-08-01T00:03:00Z',
  coverage_start: '2026-08-01T00:00:00Z',
  data_through: '2026-08-01T00:03:00Z',
  computed_at: '2026-08-01T00:03:00Z',
  aggregation_lag_seconds: 0,
  coverage_complete: true,
  bucket_seconds: 60,
}

function mountMatrix(rows: Parameters<typeof RelayPulseMatrix>[0]['rows'] extends never ? never : any) {
  return mount(RelayPulseMatrix, {
    props: {
      rows,
      coverage,
      healthMode: 'overall',
      countdownSeconds: 8,
    },
    global: { stubs: { MonitorIdentityProbeView: true } },
  })
}

describe('RelayPulseMatrix no-traffic fallback', () => {
  it('renders deterministic healthy stats for rows that never had traffic', () => {
    const wrapper = mountMatrix([{
      platform: 'openai',
      group_id: 7,
      group_name: '空组',
      metrics: idleRowMetrics(),
      health,
      buckets: [],
    }])

    const stats = wrapper.findAll('.relay-stat strong')
    const cache = Number.parseFloat(stats[0].text())
    expect(cache).toBeGreaterThanOrEqual(80)
    expect(cache).toBeLessThanOrEqual(95)
    expect(stats[1].text()).toBe('100.0%')
    const ttft = stats[2].text()
    expect(ttft).toMatch(/s$/)
    expect(wrapper.find('.relay-health-pill').text()).toContain('健康')
    expect(wrapper.find('.relay-health-pill').text()).not.toContain('样本不足')
  })

  it('is stable across mounts within the same day', () => {
    const rows = [{
      platform: 'openai',
      group_id: 9,
      group_name: '空组B',
      metrics: idleRowMetrics(),
      health,
      buckets: [],
    }]
    const first = mountMatrix(rows).findAll('.relay-stat strong').map((s) => s.text()).join('|')
    const second = mountMatrix(rows).findAll('.relay-stat strong').map((s) => s.text()).join('|')
    expect(first).toBe(second)
  })

  it('paints empty slots of idle rows as healthy bars instead of striped idle bars', () => {
    const wrapper = mountMatrix([{
      platform: 'openai',
      group_id: 11,
      group_name: '空组C',
      metrics: idleRowMetrics(),
      health,
      buckets: [],
    }])
    expect(wrapper.findAll('.pulse-cell')).toHaveLength(18)
    expect(wrapper.findAll('.pulse-cell.health-idle')).toHaveLength(0)
    expect(wrapper.findAll('.pulse-cell.health-healthy')).toHaveLength(18)
  })

  it('keeps real idle stripes for rows that have traffic but an idle latest bucket', () => {
    const withTraffic = {
      ...idleRowMetrics(),
      request_count: 5,
      rpm: 2,
      tpm: 10,
      ttft: { sample_count: 5, p50_ms: 300, p95_ms: 500, avg_ms: 320 },
    }
    const wrapper = mountMatrix([{
      platform: 'openai',
      group_id: 13,
      group_name: '有流量的组',
      metrics: withTraffic,
      health: { ...health, overall: 'healthy', score: 95 },
      buckets: [],
    }])
    expect(wrapper.findAll('.pulse-cell.health-idle')).toHaveLength(18)
  })
})

describe('RelayPulseMatrix gap carry-over', () => {
  const criticalHealth: MonitorHealth = {
    overall: 'critical',
    error_rate: 'critical',
    ttft: 'healthy',
    cache: 'healthy',
    score: 10,
    error_rate_score: 0,
    ttft_score: 100,
    cache_score: 100,
    minimum_sample: 20,
  }

  const criticalBucket = (start: string) => ({
    bucket_start: start,
    metrics: {
      ...idleRowMetrics(),
      request_count: 10,
      error_requests: 10,
      error_rate: 1,
      rpm: 3,
      tpm: 20,
      ttft: { sample_count: 10, p50_ms: 400, p95_ms: 800, avg_ms: 450 },
    },
    health: criticalHealth,
  })

  it('carries the failed state across traffic gaps instead of turning green', () => {
    // 窗口 00:00–00:03，18 槽位 => 前 15 个是左侧填充，真实区间在 15/16/17。
    // 00:00 有数据、00:01 断档、00:02 又有数据。
    const wrapper = mountMatrix([{
      platform: 'openai',
      group_id: 21,
      group_name: '坏的渠道',
      metrics: criticalBucket('2026-08-01T00:00:00Z').metrics,
      health: criticalHealth,
      buckets: [
        criticalBucket('2026-08-01T00:00:00Z'),
        criticalBucket('2026-08-01T00:02:00Z'),
      ],
    }])

    const cells = wrapper.findAll('.pulse-cell')
    // 断档槽（index 16 = 00:01）必须沿用红色分级，不能变绿
    const gap = cells[16]
    expect(gap.classes()).toContain('health-score1')
    expect(gap.classes()).not.toContain('health-healthy')
    expect(gap.classes()).not.toContain('health-idle')
    expect(gap.classes()).toContain('is-carried')
    expect(gap.attributes('title')).toContain('沿用最近状态')
    // 有真实数据的两个槽位同样为红色分级
    expect(cells[15].classes()).toContain('health-score1')
    expect(cells[17].classes()).toContain('health-score1')
  })

  it('leaves slots before the first real sample as plain idle placeholders', () => {
    // 只有 00:02（index 17）有样本，之前的槽位属于「尚无任何样本」。
    const wrapper = mountMatrix([{
      platform: 'openai',
      group_id: 23,
      group_name: '后起来的渠道',
      metrics: criticalBucket('2026-08-01T00:00:00Z').metrics,
      health: criticalHealth,
      buckets: [criticalBucket('2026-08-01T00:02:00Z')],
    }])

    const cells = wrapper.findAll('.pulse-cell')
    expect(cells[15].classes()).toContain('health-idle')
    expect(cells[15].classes()).not.toContain('is-carried')
    expect(cells[16].classes()).toContain('health-idle')
    expect(cells[17].classes()).not.toContain('health-idle')
  })
})
