import { describe, expect, it } from 'vitest'
import { aggregateModels, modelEffectiveRate } from '../aggregate'
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'

function tokenModel(name: string, input: number, output: number, overrides: Partial<PlazaModel> = {}): PlazaModel {
  return {
    name,
    platform: 'anthropic',
    pricing: {
      billing_mode: 'token',
      input_price: input,
      output_price: output,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
      per_request_price: null,
      intervals: []
    },
    ...overrides
  }
}

function group(overrides: Partial<ModelPlazaGroup> & { id: number; name: string; models: PlazaModel[] }): ModelPlazaGroup {
  return {
    description: '',
    platform: 'anthropic',
    subscription_type: 'standard',
    rate_multiplier: 1,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 0,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    long_context_pricing_enabled: true,
    ...overrides
  }
}

describe('aggregateModels', () => {
  it('跨分组合并同名模型,取最低价与最低倍率,offers 按倍率升序', () => {
    const expensive = group({
      id: 1,
      name: 'expensive',
      models: [tokenModel('claude-sonnet', 3e-6, 1.5e-5)],
      rate_multiplier: 1.2
    })
    const cheap = group({
      id: 2,
      name: 'cheap',
      models: [tokenModel('claude-sonnet', 3e-6, 1.5e-5)],
      rate_multiplier: 0.5
    })

    const result = aggregateModels([expensive, cheap])
    expect(result).toHaveLength(1)

    const entry = result[0]
    expect(entry.name).toBe('claude-sonnet')
    expect(entry.offers).toHaveLength(2)
    // offers 按生效倍率升序,最低倍率分组排最前
    expect(entry.offers[0].group.id).toBe(2)
    expect(entry.lowestRate).toBe(0.5)
    // 最低实付价取跨分组的最小值:输入 3e-6 * 0.5
    expect(entry.minInput).toBeCloseTo(1.5e-6, 12)
    expect(entry.minOutput).toBeCloseTo(7.5e-6, 12)
  })

  it('不同模型各自聚合,按输出价降序排列', () => {
    const g = group({
      id: 1,
      name: 'g',
      models: [tokenModel('small-model', 1e-6, 5e-6), tokenModel('big-model', 1e-5, 5e-5)]
    })

    const result = aggregateModels([g])
    expect(result.map((entry) => entry.name)).toEqual(['big-model', 'small-model'])
  })

  it('图片模型在分组开启独立倍率时按图片倍率计生效倍率', () => {
    const g = group({
      id: 1,
      name: 'image-group',
      models: [
        {
          name: 'gpt-image-1',
          platform: 'openai',
          pricing: {
            billing_mode: 'image',
            input_price: null,
            output_price: null,
            cache_write_price: null,
            cache_read_price: null,
            image_input_price: null,
            image_output_price: null,
            per_request_price: 0.05,
            intervals: []
          }
        }
      ],
      rate_multiplier: 0.5,
      image_rate_independent: true,
      image_rate_multiplier: 2
    })

    const result = aggregateModels([g])
    expect(result[0].lowestRate).toBe(2)
    expect(result[0].minRequestPrice).toBeCloseTo(0.1, 10)
  })

  it('modelEffectiveRate 对普通模型取用户专属倍率 ?? 分组默认倍率', () => {
    const g = group({ id: 1, name: 'g', models: [], rate_multiplier: 0.7, user_rate_multiplier: 0.4 })
    expect(modelEffectiveRate(g, tokenModel('m', 1e-6, 2e-6))).toBe(0.4)
  })
})
