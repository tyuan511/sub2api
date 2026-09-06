import { describe, expect, it } from 'vitest'

import { durationSeverity, firstTokenSeverity, rateSeverity } from '../latencyHealth'

describe('latencyHealth', () => {
  it('classifies first-token latency at 10s/30s/60s boundaries', () => {
    expect(firstTokenSeverity(0)).toBe('good')
    expect(firstTokenSeverity(9_999)).toBe('good')
    expect(firstTokenSeverity(10_000)).toBe('warn')
    expect(firstTokenSeverity(29_999)).toBe('warn')
    expect(firstTokenSeverity(30_000)).toBe('slow')
    expect(firstTokenSeverity(59_999)).toBe('slow')
    expect(firstTokenSeverity(60_000)).toBe('critical')
  })

  it('classifies total duration at 1min/3min/5min boundaries', () => {
    expect(durationSeverity(0)).toBe('good')
    expect(durationSeverity(59_999)).toBe('good')
    expect(durationSeverity(60_000)).toBe('warn')
    expect(durationSeverity(179_999)).toBe('warn')
    expect(durationSeverity(180_000)).toBe('slow')
    expect(durationSeverity(299_999)).toBe('slow')
    expect(durationSeverity(300_000)).toBe('critical')
  })

  it('classifies higher-is-better rates with the shared four-level palette', () => {
    expect(rateSeverity(100)).toBe('good')
    expect(rateSeverity(80)).toBe('good')
    expect(rateSeverity(79.99)).toBe('warn')
    expect(rateSeverity(50)).toBe('warn')
    expect(rateSeverity(49.99)).toBe('slow')
    expect(rateSeverity(20)).toBe('slow')
    expect(rateSeverity(19.99)).toBe('critical')
    expect(rateSeverity(null)).toBeNull()
    expect(rateSeverity(Number.NaN)).toBeNull()
  })
})
