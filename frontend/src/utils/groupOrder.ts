/**
 * 平台展示定制顺序（与后端 backend/internal/repository/group_repo.go 里的
 * groupPlatformDisplayOrder 保持一致）；不在列表里的平台排到末尾并按字母序。
 */
export const PLATFORM_DISPLAY_ORDER = ['openai', 'anthropic', 'grok', 'gemini'] as const

export function platformDisplayRank(platform?: string | null): number {
  const index = PLATFORM_DISPLAY_ORDER.indexOf((platform || '').trim().toLowerCase() as (typeof PLATFORM_DISPLAY_ORDER)[number])
  return index === -1 ? PLATFORM_DISPLAY_ORDER.length : index
}

/** 平台名按定制顺序排序（去重后），用于平台 chip / 分组列表表头。 */
export function sortByPlatformDisplayOrder(platforms: Iterable<string>): string[] {
  return [...new Set(platforms)].sort((left, right) => {
    const rank = platformDisplayRank(left) - platformDisplayRank(right)
    return rank !== 0 ? rank : left.localeCompare(right, undefined, { sensitivity: 'base' })
  })
}

/** 参与统一排序所需的最小字段集（Group / AdminGroup / ModelPlazaGroup 等都满足）。 */
export interface SortableGroup {
  id: number
  name: string
  platform: string
  rate_multiplier?: number | null
  sort_order?: number
}

export interface GroupDisplaySortOptions<T> {
  /** 用户专属倍率（group_id -> rate），优先于分组默认倍率。 */
  ratesByGroupId?: Record<number, number> | null
  /** 自定义取倍率的方式（例如模型广场要叠加高峰/生图独立倍率）。 */
  rateOf?: (group: T) => number
}

/** 展示用倍率：用户专属优先，取不到视为 0。 */
export function effectiveGroupRate(
  group: SortableGroup,
  ratesByGroupId?: Record<number, number> | null,
): number {
  const rate = ratesByGroupId?.[group.id] ?? group.rate_multiplier
  const value = Number(rate)
  return Number.isFinite(value) ? value : 0
}

/**
 * 分组展示统一排序：
 * 1) 手动排序 sort_order（有则优先，对应分组管理里的拖拽排序）
 * 2) 平台定制顺序 OpenAI→Claude→Grok→Gemini，其余按平台字母序
 * 3) 倍率从低到高
 * 4) 名称、ID 兜底，保证顺序稳定
 */
export function compareGroupsForDisplay<T extends SortableGroup>(
  left: T,
  right: T,
  options: GroupDisplaySortOptions<T> = {},
): number {
  const manual = (Number(left.sort_order) || 0) - (Number(right.sort_order) || 0)
  if (manual !== 0) return manual

  const rank = platformDisplayRank(left.platform) - platformDisplayRank(right.platform)
  if (rank !== 0) return rank
  const platform = (left.platform || '').localeCompare(right.platform || '', undefined, { sensitivity: 'base' })
  if (platform !== 0) return platform

  const rateOf = options.rateOf
  const leftRate = rateOf ? rateOf(left) : effectiveGroupRate(left, options.ratesByGroupId)
  const rightRate = rateOf ? rateOf(right) : effectiveGroupRate(right, options.ratesByGroupId)
  const rate = leftRate - rightRate
  if (rate !== 0) return rate

  const name = (left.name || '').localeCompare(right.name || '', undefined, { sensitivity: 'base', numeric: true })
  if (name !== 0) return name
  return left.id - right.id
}

/** 返回新数组，不修改入参。 */
export function sortGroupsForDisplay<T extends SortableGroup>(
  groups: readonly T[],
  options: GroupDisplaySortOptions<T> = {},
): T[] {
  return [...groups].sort((left, right) => compareGroupsForDisplay(left, right, options))
}
