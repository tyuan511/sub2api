/**
 * Structure contracts: channel-monitor-v2 + studio shells must use project
 * design-system utility classes rather than isolated flat RGB skins.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')

function read(rel: string) {
  return readFileSync(resolve(root, rel), 'utf8')
}

describe('channel-monitor-v2 design system structure', () => {
  it('user ChannelStatus V2 shell uses page-header, card, btn, tabs utilities', () => {
    // Route wrapper may switch V1/V2; design chrome lives on the V2 implementation.
    const src = read('views/user/ChannelStatusV2View.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('class="card')
    expect(src).toContain('btn btn-secondary')
    expect(src).toContain('class="tab')
    expect(src).toContain('tab-active')
    expect(src).toContain('badge badge-warning')
    expect(src).toContain('monitor-toolbar')
    expect(src).toContain('overallRates')
    expect(src).not.toContain('passiveUsage')
    expect(src).toContain('rounded-3xl')
    expect(src).toContain('ring-1 ring-gray-900/5')
    expect(src).not.toMatch(/min-width:\s*980px/)
    expect(src).not.toMatch(/min-w-\[980px\]/)
    expect(src).toContain("'platform_group'")
    expect(src).toContain('RelayPulseMatrix')
    expect(src).not.toContain('onChartWheel')
    expect(src).not.toContain('applyWheelZoom')
  })

  it('RelayPulseMatrix uses card chrome and hover tooltips without zoom or click modal', () => {
    const src = read('features/channel-monitor-v2/RelayPulseMatrix.vue')
    expect(src).toContain('relay-card-item')
    expect(src).toContain('!rounded-3xl')
    expect(src).toContain('ring-1 ring-gray-900/5')
    expect(src).toContain('dark:bg-dark-900/70')
    expect(src).toContain('grid-template-columns: 1fr')
    expect(src).toContain('repeat(2, minmax(0, 1fr))')
    expect(src).toContain('repeat(3, minmax(0, 1fr))')
    expect(src).toContain('repeat(4, minmax(0, 1fr))')
    expect(src).toContain('min-width: 1536px')
    expect(src).not.toContain('auto-fit')
    expect(src).not.toContain('minmax(260px')
    expect(src).toContain('pulse-tooltip')
    expect(src).toContain('PULSE_RECORD_COUNT')
    expect(src).toContain('history60pts')
    expect(src).toContain('height: 20px')
    expect(src).toContain('relay-bar-enter')
    expect(src).toContain('prefers-reduced-motion')
    expect(src).toContain('PLATFORM_ORDER')
    expect(src).toContain('PROVIDER_OPENAI')
    expect(src).toContain('PROVIDER_ANTHROPIC')
    expect(src).toContain('PROVIDER_GROK')
    expect(src).toContain('PROVIDER_GEMINI')
    expect(src).not.toContain('applyWheelZoom')
    expect(src).not.toContain('onMatrixWheel')
    expect(src).not.toContain('modal-overlay')
    expect(src).not.toContain('modal-content')
  })

  it('MetricCell uses stat-card utility', () => {
    const src = read('features/channel-monitor-v2/MetricCell.vue')
    expect(src).toContain('stat-card')
    expect(src).toContain('stat-label')
    expect(src).toContain('stat-value')
    expect(src).toContain('rounded-3xl')
  })

  it('MonitorTrendChart uses Ops chart shell tokens', () => {
    const src = read('features/channel-monitor-v2/MonitorTrendChart.vue')
    expect(src).toContain('class="card')
    expect(src).toContain('rounded-3xl')
    expect(src).toContain('ring-1 ring-gray-900/5')
    expect(src).toContain('EmptyState')
    expect(src).toContain('min-h-[360px]')
  })

  it('FilterMultiSelect uses rounded-xl input chrome and dropdown utility', () => {
    const src = read('features/channel-monitor-v2/FilterMultiSelect.vue')
    expect(src).toContain('rounded-xl')
    expect(src).toContain('dropdown')
    expect(src).toContain('dropdown-item')
  })

  it('MonitorSettingsPanel uses page-header, card, btn-primary, tabs', () => {
    const src = read('features/channel-monitor-v2/MonitorSettingsPanel.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('btn btn-primary')
    expect(src).toContain('class="card')
    expect(src).toContain('tab-active')
    expect(src).toMatch(/max-h-\[min\(40vh/)
  })

  it('admin ChannelMonitorView V2 tab chrome uses project tabs', () => {
    const src = read('views/admin/ChannelMonitorView.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('class="tabs')
    expect(src).toContain('tab-active')
    expect(src).toContain('MonitorSettingsPanel')
  })
})
