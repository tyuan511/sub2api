/**
 * 模型广场「按模型聚合」逻辑：
 * 一个模型可能被多个分组提供（同名不同倍率），聚合后每模型一条记录，
 * 卡片展示最低实付价与最低倍率，弹窗再按分组展开明细。
 */
import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { sortByPlatformDisplayOrder } from '@/utils/groupOrder'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, type BillingMode } from '@/constants/channel'

/** 单个分组对该模型的报价（倍率已折算到 effectiveRate）。 */
export interface ModelOffer {
  group: ModelPlazaGroup
  model: PlazaModel
  /** 生效倍率 = 用户专属 ?? 分组默认；图片/视频模型在分组开启独立倍率时取对应倍率。 */
  effectiveRate: number
}

export interface AggregatedModel {
  /** platform:name */
  key: string
  name: string
  platform: string
  billingMode: BillingMode
  /** 按生效倍率升序（最低倍率的分组排最前）。 */
  offers: ModelOffer[]
  /** 全部分组中的最低生效倍率。 */
  lowestRate: number
  /** 最低实付输入价（$/1M，已乘倍率）；全部分组均未配置时为 null。 */
  minInput: number | null
  /** 最低实付输出价（$/1M）。 */
  minOutput: number | null
  /** 最低实付缓存读取价（$/1M）。 */
  minCacheRead: number | null
  /** 最低按次/按图实付价（未做每百万换算）。 */
  minRequestPrice: number | null
}

export function modelBillingMode(model: PlazaModel): BillingMode {
  return (model.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

/**
 * 该报价的生效倍率：
 * - 图片模型在分组开启独立倍率时取 image_rate_multiplier；
 * - 视频模型同理取 video_rate_multiplier；
 * - 其余取用户专属倍率 ?? 分组默认倍率。
 */
export function modelEffectiveRate(group: ModelPlazaGroup, model: PlazaModel): number {
  const mode = modelBillingMode(model)
  if (mode === BILLING_MODE_IMAGE && group.image_rate_independent) {
    return group.image_rate_multiplier
  }
  if (mode === BILLING_MODE_VIDEO && group.video_rate_independent) {
    return group.video_rate_multiplier
  }
  return group.user_rate_multiplier ?? group.rate_multiplier
}

function minNumber(current: number | null, candidate: number | null): number | null {
  if (candidate == null) return current
  if (current == null) return candidate
  return Math.min(current, candidate)
}

/** 聚合分组列表为按模型去重的记录；返回结果按平台展示顺序 + 输出价降序 + 名称降序排列。 */
export function aggregateModels(groups: ModelPlazaGroup[]): AggregatedModel[] {
  const merged = new Map<string, AggregatedModel>()

  for (const group of groups) {
    for (const model of group.models) {
      const key = `${model.platform}:${model.name}`
      const rate = modelEffectiveRate(group, model)
      const mode = modelBillingMode(model)
      let entry = merged.get(key)

      if (!entry) {
        entry = {
          key,
          name: model.name,
          platform: model.platform,
          billingMode: mode,
          offers: [],
          lowestRate: rate,
          minInput: null,
          minOutput: null,
          minCacheRead: null,
          minRequestPrice: null
        }
        merged.set(key, entry)
      }

      entry.offers.push({ group, model, effectiveRate: rate })
      const pricing = model.pricing
      entry.minInput = minNumber(entry.minInput, pricing?.input_price == null ? null : pricing.input_price * rate)
      entry.minOutput = minNumber(entry.minOutput, pricing?.output_price == null ? null : pricing.output_price * rate)
      entry.minCacheRead = minNumber(entry.minCacheRead, pricing?.cache_read_price == null ? null : pricing.cache_read_price * rate)
      if (mode !== BILLING_MODE_TOKEN && pricing?.per_request_price != null) {
        entry.minRequestPrice = minNumber(entry.minRequestPrice, pricing.per_request_price * rate)
      }
    }
  }

  const list = [...merged.values()]
  for (const entry of list) {
    entry.offers.sort((a, b) => a.effectiveRate - b.effectiveRate || a.group.rate_multiplier - b.group.rate_multiplier)
    entry.lowestRate = entry.offers[0]?.effectiveRate ?? 1
  }
  return sortAggregatedModels(list)
}

/** 展示排序：token 计费在前 → 实付输出价从高到低 → 名称降序（新版本号在前）。 */
export function sortAggregatedModels(list: AggregatedModel[]): AggregatedModel[] {
  const platformOrder = new Map(
    sortByPlatformDisplayOrder([...new Set(list.map((item) => item.platform))]).map((p, idx) => [p, idx])
  )
  return [...list].sort((a, b) => {
    const pa = platformOrder.get(a.platform) ?? Number.MAX_SAFE_INTEGER
    const pb = platformOrder.get(b.platform) ?? Number.MAX_SAFE_INTEGER
    if (pa !== pb) return pa - pb
    const ta = a.billingMode === BILLING_MODE_TOKEN
    const tb = b.billingMode === BILLING_MODE_TOKEN
    if (ta !== tb) return ta ? -1 : 1
    const oa = a.minOutput
    const ob = b.minOutput
    if (oa != null && ob != null && oa !== ob) return ob - oa
    if (oa != null && ob == null) return -1
    if (oa == null && ob != null) return 1
    return b.name.localeCompare(a.name)
  })
}
