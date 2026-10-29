const i18nT = (key: string, params?: Record<string, unknown>) => {
  const map: Record<string, string> = {
    'channelMonitorV2.matrix.title': '色块矩阵',
    'channelMonitorV2.matrix.description': '趋势色块视图',
    'channelMonitorV2.bucket.minutes': '{count}分钟',
    'channelMonitorV2.matrix.dimension': '维度',
    'channelMonitorV2.metrics.successRate': '成功率',
    'channelMonitorV2.metrics.availability': '可用率',
    'channelMonitorV2.metrics.availabilityValue': '可用率 {value}',
    'channelMonitorV2.metrics.ttft': '首 Token',
    'channelMonitorV2.metrics.tps': '每秒 Token',
    'channelMonitorV2.metrics.cacheRate': '缓存率',
    'channelMonitorV2.matrix.scoreLine': '评分 {score}',
    'channelMonitorV2.metrics.successRateValue': '成功率 {value}',
    'channelMonitorV2.metrics.errorRateValue': '错误率 {value}',
    'channelMonitorV2.metrics.rpmValue': 'RPM {value}',
    'channelMonitorV2.metrics.tpmValue': 'TPM {value}',
    'channelMonitorV2.metrics.tpsValue': '每秒 Token {value}',
    'channelMonitorV2.metrics.ttftValue': '首 Token {value}',
    'channelMonitorV2.metrics.durationValue': '时长 {value}',
    'channelMonitorV2.metrics.cacheRateValue': '缓存率 {value}',
    'channelMonitorV2.matrix.noTrafficAt': '{time} 无流量',
    'channelMonitorV2.matrix.noTraffic': '无流量',
    'channelMonitorV2.matrix.legendAria': '图例',
    'channelMonitorV2.matrix.bad': '差',
    'channelMonitorV2.matrix.good': '好',
    'channelMonitorV2.matrix.healthyLegend': '健康',
    'channelMonitorV2.matrix.warningLegend': '警告',
    'channelMonitorV2.matrix.criticalLegend': '异常',
    'channelMonitorV2.matrix.unknownLegend': '样本不足',
    'channelMonitorV2.userRate': '倍率 {value}x',
    'monitorCommon.history60pts': '近 {n} 次记录',
    'monitorCommon.nextUpdateIn': '{n}s 后刷新',
    'monitorCommon.past': 'PAST',
    'monitorCommon.now': 'NOW',
    'monitorCommon.providers.openai': 'OpenAI',
    'monitorCommon.providers.anthropic': 'Claude',
    'monitorCommon.providers.grok': 'Grok',
    'monitorCommon.providers.gemini': 'Gemini',
  }
  const template = map[key] || key
  return template.replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
}

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

import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RelayPulseMatrix from '../RelayPulseMatrix.vue'
import type { MonitorHealth, MonitorMetric } from '@/api/channelMonitorV2'

const health: MonitorHealth = {
  overall: 'warning',
  error_rate: 'warning',
  ttft: 'healthy',
  cache: 'warning',
  score: 52,
  error_rate_score: 40,
  ttft_score: 100,
  cache_score: 50,
  minimum_sample: 20,
}

function metrics(requestCount: number): MonitorMetric {
  return {
    success_requests: requestCount ? requestCount - 1 : 0,
    error_requests: requestCount ? 1 : 0,
    request_count: requestCount,
    token_count: 100,
    rpm: 1.5,
    tpm: 10.2,
    error_rate: requestCount ? 1 / requestCount : 0,
    cache_rate: 0.5,
    cache_rate_numerator: 50,
    cache_rate_denominator: 100,
    ttft: { sample_count: requestCount, p50_ms: 100, p95_ms: 300, avg_ms: 150 },
    duration: { sample_count: requestCount, p50_ms: 500, p95_ms: 900, avg_ms: 600 },
    upstream_affected_requests: 2,
    upstream_attempt_count: 3,
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

describe('RelayPulseMatrix', () => {
  it('shows a fixed 18-bar pulse with compact hover tooltips and no zoom chrome', async () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          model: 'gpt-5',
          metrics: metrics(10),
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:00:00Z', metrics: metrics(10), health },
            { bucket_start: '2026-08-01T00:01:00Z', metrics: metrics(0), health },
          ],
        }],
        coverage,
        healthMode: 'overall',
        countdownSeconds: 8,
        ratesByGroupId: { 7: 0.06 },
        probesByGroup: {
          '默认组': {
            claimed_model: 'gpt-5',
            confidence: 0.98,
            identity_status: 'confirmed',
            status: 'completed',
            checked_at: '2026-08-01T00:03:00Z',
            risk_flags: ['should-not-render'],
          },
        },
      },
      global: {
        stubs: {
          MonitorIdentityProbeView: {
            props: ['result'],
            template: `<span data-testid="identity-probe" :data-model="result?.claimed_model" :data-flags="(result?.risk_flags || []).join(',')">probe</span>`,
          },
        },
      },
    })

    const cells = wrapper.findAll('.pulse-cell')
    expect(cells).toHaveLength(18)
    expect(wrapper.text()).toContain('近 18 次记录')
    expect(wrapper.text()).toContain('倍率 0.06x')
    expect(wrapper.find('.user-rate').element.parentElement?.className).toContain('whitespace-nowrap')
    expect(wrapper.find('.user-rate').element.parentElement?.className).not.toContain('flex-wrap')
    expect(wrapper.find('.relay-health-pill').text()).not.toContain('无流量')
    expect(wrapper.text()).toContain('8s 后刷新')
    expect(wrapper.text()).toContain('PAST')
    expect(wrapper.text()).toContain('NOW')
    expect(wrapper.text()).not.toContain('重置缩放')
    expect(wrapper.text()).not.toContain('滚轮')

    const probe = wrapper.get('[data-testid="identity-probe"]')
    expect(probe.attributes('data-model')).toBe('gpt-5')

    const tip = cells.find((cell) => cell.classes().includes('has-data'))?.text() || ''
    expect(tip).toContain('可用率')
    expect(tip).toContain('90.0%')
    expect(tip).toContain('首 Token')
    expect(tip).toContain('缓存率')
    expect(tip).not.toContain('每秒 Token')
    expect(tip).not.toContain('RPM')
    expect(tip).not.toContain('请求数')

    const header = wrapper.find('.matrix-header').text()
    expect(header).toContain('可用率')
    expect(header).toContain('首 Token')
    expect(header).toContain('缓存率')

    expect(cells[0].classes().some((c) => c.startsWith('health-'))).toBe(true)
    await cells[0].trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('shows the latest bucket in the card stats and renders 80% availability as degraded, not dead', () => {
    const bucketHealth: MonitorHealth = {
      ...health,
      overall: 'warning',
      error_rate: 'critical',
      score: 62.5,
      error_rate_score: 50,
    }
    const bucketMetrics = metrics(10)
    bucketMetrics.error_rate = 0.2 // 80% availability
    const aggregateMetrics = metrics(200)
    aggregateMetrics.error_rate = 0.35 // 65% over the whole range
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          metrics: aggregateMetrics,
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:00:00Z', metrics: bucketMetrics, health: bucketHealth },
            { bucket_start: '2026-08-01T00:01:00Z', metrics: bucketMetrics, health: bucketHealth },
          ],
        }],
        coverage,
        healthMode: 'overall',
        countdownSeconds: 8,
      },
      global: {
        stubs: { MonitorIdentityProbeView: true },
      },
    })

    // 卡片统计取最新一个 bucket（最近一次），而不是整个区间的聚合值
    const stats = wrapper.findAll('.relay-stat strong')
    expect(stats[1].text()).toBe('80.0%')
    expect(stats[1].classes()).not.toContain('health-unknown')

    // 健康 pill 同样取自最新 bucket
    expect(wrapper.find('.relay-health-pill').classes()).toContain('health-score6')

    // 可用率 80%（score 62.5）→ 橙色、中高柱，而不是红色矮柱
    const bars = wrapper.findAll('.pulse-cell.has-data')
    expect(bars).toHaveLength(2)
    expect(bars[1].classes()).toContain('health-score6')
    expect(bars[1].classes()).not.toContain('health-critical')
    expect(bars[1].attributes('style')).toContain('height: 63%')
  })
})

describe('RelayPulseMatrix axis range', () => {
  it('pads the selected range to a fixed 18 records instead of zooming', () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          model: 'gpt-5',
          metrics: metrics(10),
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:02:00Z', metrics: metrics(10), health },
          ],
        }],
        coverage: {
          requested_start: '2026-08-01T00:00:00Z',
          requested_end: '2026-08-01T00:05:00Z',
          coverage_start: '2026-08-01T00:02:00Z',
          data_through: '2026-08-01T00:03:00Z',
          computed_at: '2026-08-01T00:03:00Z',
          aggregation_lag_seconds: 0,
          coverage_complete: false,
          bucket_seconds: 60,
        },
        healthMode: 'overall',
      },
    })
    expect(wrapper.findAll('.pulse-cell')).toHaveLength(18)
    expect(wrapper.findAll('.pulse-cell.has-data')).toHaveLength(1)
  })

  it('keeps only the latest 18 buckets when the range is longer', () => {
    const buckets = Array.from({ length: 24 }, (_, i) => ({
      bucket_start: new Date(Date.parse('2026-08-01T00:00:00Z') + i * 3600_000).toISOString(),
      metrics: metrics(10),
      health,
    }))
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          metrics: metrics(10),
          health,
          buckets,
        }],
        coverage: {
          requested_start: '2026-08-01T00:00:00Z',
          requested_end: '2026-08-02T00:00:00Z',
          coverage_start: '2026-08-01T00:00:00Z',
          data_through: '2026-08-02T00:00:00Z',
          computed_at: '2026-08-02T00:00:00Z',
          aggregation_lag_seconds: 0,
          coverage_complete: true,
          bucket_seconds: 3600,
        },
        healthMode: 'overall',
      },
    })
    expect(wrapper.findAll('.pulse-cell')).toHaveLength(18)
    expect(wrapper.findAll('.pulse-cell.has-data')).toHaveLength(18)
  })
})

describe('RelayPulseMatrix privacy-safe bars and platform order', () => {
  it('keeps redacted user buckets tall instead of treating request_count=0 as empty', () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [{
          platform: 'openai',
          group_id: 7,
          group_name: '默认组',
          metrics: metrics(0),
          health,
          buckets: [
            { bucket_start: '2026-08-01T00:02:00Z', metrics: metrics(0), health },
          ],
        }],
        coverage,
        healthMode: 'overall',
        showThroughput: false,
      },
    })
    const dataCell = wrapper.find('.pulse-cell.has-data')
    expect(dataCell.exists()).toBe(true)
    expect(dataCell.attributes('style')).toContain('height: 52%')
    expect(dataCell.classes()).not.toContain('health-idle')
  })

  it('orders platforms as OpenAI, Claude, Grok, then Gemini', () => {
    const row = (platform: string, group: string) => ({
      platform,
      group_id: 1,
      group_name: group,
      metrics: metrics(10),
      health,
      buckets: [] as { bucket_start: string; metrics: ReturnType<typeof metrics>; health: typeof health }[],
    })
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [
          row('gemini', 'g'),
          row('grok', 'x'),
          row('anthropic', 'c'),
          row('openai', 'o'),
        ],
        coverage,
        healthMode: 'overall',
      },
    })
    const headings = wrapper.findAll('h3').map((node) => node.text())
    expect(headings).toEqual(['OpenAI', 'Claude', 'Grok', 'Gemini'])
  })

  it('sorts cards within one platform by rate multiplier, lowest first', () => {
    const row = (platform: string, name: string, groupId: number | null) => ({
      platform,
      group_id: groupId,
      group_name: name,
      metrics: metrics(10),
      health,
      buckets: [] as { bucket_start: string; metrics: ReturnType<typeof metrics>; health: typeof health }[],
    })
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [
          row('openai', '贵的', 3),
          row('openai', '无倍率', null),
          row('grok', 'grok-高', 21),
          row('openai', '中', 2),
          row('grok', 'grok-低', 20),
          row('openai', '便宜的', 1),
        ],
        coverage,
        healthMode: 'overall',
        ratesByGroupId: { 1: 0.02, 2: 0.1, 3: 0.5, 20: 0.3, 21: 0.8 },
      },
    })
    const titles = wrapper.findAll('.relay-card-item h4').map((card) => card.text())
    expect(titles).toEqual(['便宜的', '中', '贵的', '无倍率', 'grok-低', 'grok-高'])
  })

  it('keeps the incoming order when no rate multipliers are loaded', () => {
    const row = (name: string, groupId: number) => ({
      platform: 'openai',
      group_id: groupId,
      group_name: name,
      metrics: metrics(10),
      health,
      buckets: [] as { bucket_start: string; metrics: ReturnType<typeof metrics>; health: typeof health }[],
    })
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [row('A', 1), row('B', 2), row('C', 3)],
        coverage,
        healthMode: 'overall',
      },
    })
    expect(wrapper.findAll('.relay-card-item h4').map((node) => node.text())).toEqual(['A', 'B', 'C'])
  })
})

describe('RelayPulseMatrix idle bars', () => {
  const row = (buckets: { bucket_start: string; metrics: ReturnType<typeof metrics>; health: typeof health }[]) => ({
    platform: 'openai',
    group_id: 7,
    group_name: '默认组',
    metrics: metrics(10),
    health,
    buckets,
  })
  /** 真正零流量：请求数为 0 且 RPM/TPM 也为 0。 */
  const idleMetrics = () => ({ ...metrics(0), rpm: 0, tpm: 0, token_count: 0 })

  it('fills missing intervals with a full-height idle bar instead of blank space', () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [row([{ bucket_start: '2026-08-01T00:02:00Z', metrics: metrics(10), health }])],
        coverage,
        healthMode: 'overall',
      },
    })
    const cells = wrapper.findAll('.pulse-cell')
    expect(cells).toHaveLength(18)
    const idle = wrapper.findAll('.pulse-cell.health-idle')
    expect(idle).toHaveLength(17)
    expect(idle[0].attributes('style')).toContain('height: 100%')
    expect(idle[0].attributes('title')).toContain('无流量')
    expect(wrapper.find('.pulse-cell.has-data').classes()).not.toContain('health-idle')
  })

  it('marks buckets that reported zero traffic as idle too', () => {
    const wrapper = mount(RelayPulseMatrix, {
      props: {
        rows: [row([
          { bucket_start: '2026-08-01T00:00:00Z', metrics: idleMetrics(), health },
          { bucket_start: '2026-08-01T00:01:00Z', metrics: metrics(10), health },
          { bucket_start: '2026-08-01T00:02:00Z', metrics: metrics(10), health },
        ])],
        coverage,
        healthMode: 'overall',
      },
    })
    const cells = wrapper.findAll('.pulse-cell')
    expect(cells[15].classes()).toContain('health-idle')
    expect(cells[15].attributes('style')).toContain('height: 100%')
    expect(cells[15].attributes('title')).toContain('无流量')
    expect(cells[16].classes()).not.toContain('health-idle')
    expect(wrapper.findAll('.pulse-cell.health-idle')).toHaveLength(16)
  })
})
