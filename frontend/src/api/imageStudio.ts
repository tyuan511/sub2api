import { buildGatewayUrl } from './url'
import { sanitizeUrl } from '@/utils/url'
import type { ApiKey, Group } from '@/types'
import { apiClient } from './client'

export interface ImageGenerationGroup extends Group {
  image_models: string[]
}

export async function getImageGenerationGroups(): Promise<ImageGenerationGroup[]> {
  const { data } = await apiClient.get<ImageGenerationGroup[]>('/groups/image-generation')
  return data.filter(group => isImageGroup(group) && group.image_models.some(isImageModel))
}

const imageRatios = ['1:1', '16:9', '9:16', '4:3', '3:4', '3:2', '2:3', '21:9'] as const
// Retain historical ratios in persisted records without offering new presets.
export type ImageRatio = typeof imageRatios[number] | '4:5' | '5:4' | 'auto'
// Familiar preset labels, with exact WxH shown in the size fields and tooltips.
// Preset dimensions depend on the ratio; e.g. the '4K' square preset is 2880x2880.
export type ImageResolution = '1K' | '2K' | '4K'
const imageResolutions: ImageResolution[] = ['1K', '2K', '4K']
const legacyImageSizes = { '1:1': '1024x1024', '3:2': '1536x1024', '2:3': '1024x1536' }
const legacyImageRatios: ImageRatio[] = ['1:1', '3:2', '2:3']

// Exact ratios and 16px alignment, within GPT Image 2's 655,360–8,294,400
// pixel range and 3840px edge limit. UI resolution presets are independent
// from the gateway's longest-edge billing tiers.
// https://developers.openai.com/api/docs/guides/image-generation#size-and-quality-options
const imageSizes: Record<ImageResolution, Record<Exclude<ImageRatio, 'auto'>, string | null>> = {
  '1K': { '1:1': '1024x1024', '16:9': '1792x1008', '9:16': '1008x1792', '4:3': '1024x768', '3:4': '768x1024', '3:2': '1008x672', '2:3': '672x1008', '4:5': '768x960', '5:4': '960x768', '21:9': '1792x768' },
  '2K': { '1:1': '2048x2048', '16:9': '2048x1152', '9:16': '1152x2048', '4:3': '2048x1536', '3:4': '1536x2048', '3:2': '2016x1344', '2:3': '1344x2016', '4:5': '1600x2000', '5:4': '2000x1600', '21:9': '2016x864' },
  '4K': { '1:1': '2880x2880', '16:9': '3840x2160', '9:16': '2160x3840', '4:3': '3264x2448', '3:4': '2448x3264', '3:2': '3456x2304', '2:3': '2304x3456', '4:5': '2560x3200', '5:4': '3200x2560', '21:9': '3696x1584' },
}

export function getImageRatios(model: string): readonly ImageRatio[] {
  return model.startsWith('gpt-image-2') ? imageRatios : legacyImageRatios
}

export function getImageResolutions(model: string, ratio: ImageRatio): ImageResolution[] {
  if (ratio === 'auto') return model.startsWith('gpt-image-2') ? imageResolutions : ['1K']
  if (!model.startsWith('gpt-image-2')) return legacyImageRatios.includes(ratio) ? ['1K'] : []
  return imageResolutions.filter(resolution => imageSizes[resolution][ratio])
}
export interface ImageGenerationRequest {
  model: string
  prompt: string
  n: number
  size?: string
  aspect_ratio?: ImageRatio
}
export interface StudioImage {
  url: string
  revisedPrompt?: string
}

export function isImageGroup(group: Group): boolean {
  return group.platform === 'openai' && group.status === 'active' && group.allow_image_generation === true
}

export function canGenerateImages(key: ApiKey): boolean {
  return key.status === 'active' && !!key.key &&
    (!key.expires_at || Date.parse(key.expires_at) > Date.now()) &&
    (key.quota <= 0 || key.quota_used < key.quota) &&
    !!key.group && isImageGroup(key.group)
}

// Keep this aligned with validateOpenAIImagesModel in the existing gateway.
export function isImageModel(model: string): boolean {
  return /^gpt-image-/i.test(model)
}

export function isValidImageSize(size: string, ratio: ImageRatio): boolean {
  if (!/^\d+x\d+$/.test(size)) return false
  const [width, height] = size.split('x').map(Number)
  const [rw, rh] = ratio.split(':').map(Number)
  const pixels = width * height
  return width > 0 && height > 0 && width % 16 === 0 && height % 16 === 0 &&
    Math.max(width, height) <= 3840 && Math.max(width, height) / Math.min(width, height) <= 3 &&
    pixels >= 655360 && pixels <= 8294400 && width * rh === height * rw
}

export function imageSizeResolution(size: string): ImageResolution {
  const preset = imageResolutions.find(resolution => Object.values(imageSizes[resolution]).includes(size))
  if (preset) return preset
  const edge = Math.max(...size.split('x').map(Number))
  return edge <= 1024 ? '1K' : edge <= 2048 ? '2K' : '4K'
}

export function buildImageRequest(model: string, prompt: string, ratio: ImageRatio, count: number, resolution: ImageResolution = '1K', customSize?: string): ImageGenerationRequest {
  if (ratio === 'auto') return { model, prompt: prompt.trim(), n: count }
  if (customSize !== undefined && (!model.startsWith('gpt-image-2') || !isValidImageSize(customSize, ratio))) throw new RangeError('Unsupported image size')
  const size = customSize ?? (model.startsWith('gpt-image-2') ? imageSizes[resolution][ratio]
    : legacyImageSizes[ratio as keyof typeof legacyImageSizes])
  if (!size) throw new RangeError('Unsupported image ratio or resolution')
  if (model.startsWith('gpt-image-2') && !isValidImageSize(size, ratio)) throw new RangeError('Unsupported image size')
  return { model, prompt: prompt.trim(), n: count, size }
}

export interface StudioTask { taskId: string }
export interface StudioAsset extends StudioImage {
  id: string
  filename: string
  content_type: string
  size: number
  revised_prompt?: string
}
export interface StudioCreationResponse {
  id: string
  prompt: string
  model: string
  ratio: ImageRatio
  resolution: ImageResolution
  size?: string
  count: number
  key_id: number
  key_name: string
  created_at: number
  status: 'processing' | 'completed' | 'failed'
  references: StudioAsset[]
  images: StudioAsset[]
  error?: string
}
export async function getImageStudioStatus(): Promise<{ available: boolean }> {
  const { data } = await apiClient.get<{ available: boolean }>('/image-studio/status')
  return data
}
export async function getStudioHistory(page = 1, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: StudioCreationResponse[]; has_more: boolean }>('/image-studio/history', { params: { page }, signal })
  return data
}
export async function getLegacyStudioTask(id: string, signal?: AbortSignal) {
  return (await apiClient.get<{ status: string; images: StudioImage[]; error?: string }>(`/image-studio/legacy/${encodeURIComponent(id)}`, { signal })).data
}
export async function getStudioCreation(id: string, signal?: AbortSignal) {
  const { data } = await apiClient.get<StudioCreationResponse>(`/image-studio/history/${encodeURIComponent(id)}`, { signal })
  return data
}
export async function getStudioFile(id: string): Promise<StudioAsset> {
  const { data } = await apiClient.get<StudioAsset>(`/image-studio/files/${encodeURIComponent(id)}`)
  if (!sanitizeUrl(data.url)) throw new Error('image_result_not_stored')
  return data
}
export async function deleteStudioCreation(id: string) {
  await apiClient.delete(`/image-studio/history/${encodeURIComponent(id)}`, { timeout: 120000 })
}
export async function importStudioHistory(metadata: Record<string, unknown>, references: File[], outputs: File[], signal?: AbortSignal) {
  const form = new FormData()
  form.append('metadata', JSON.stringify(metadata))
  references.forEach(file => form.append('reference', file, file.name))
  outputs.forEach(file => form.append('output', file, file.name))
  const { data } = await apiClient.post<StudioCreationResponse>('/image-studio/history/import', form, { signal, timeout: 120000 })
  return data
}
class ImageRequestError extends Error {
  constructor(message: string, public status: number) { super(message) }
}
async function gatewayRequest(url: string, options: RequestInit, signal: AbortSignal) {
  const controller = new AbortController()
  const abort = () => controller.abort(signal.reason)
  if (signal.aborted) abort()
  else signal.addEventListener('abort', abort, { once: true })
  const timeout = window.setTimeout(() => controller.abort(new DOMException('Image request timed out', 'TimeoutError')), 120000)
  try {
    const response = await fetch(url, { ...options, signal: controller.signal })
    const body = await response.json().catch(() => null)
    if (!response.ok) {
      const message = body?.error?.message || body?.message
      throw new ImageRequestError(typeof message === 'string' ? message.slice(0, 600) : `HTTP ${response.status}`, response.status)
    }
    return { response, body }
  } finally {
    window.clearTimeout(timeout)
    signal.removeEventListener('abort', abort)
  }
}
function waitForPoll(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(signal.reason); return }
    const abort = () => { window.clearTimeout(timer); reject(signal.reason) }
    const timer = window.setTimeout(() => { signal.removeEventListener('abort', abort); resolve() }, ms)
    signal.addEventListener('abort', abort, { once: true })
  })
}
const validTaskId = (value: unknown): value is string => typeof value === 'string' && /^imgtask_[a-f0-9]{32}$/.test(value)
// Poll with the signed-in account. A revoked/expired generation Key does not
// remove access to that account's already persisted history.
export async function pollImageTask(taskId: string, signal: AbortSignal): Promise<StudioCreationResponse> {
  if (!validTaskId(taskId)) throw new Error('image_task_invalid')
  let failures = 0
  while (!signal.aborted) {
    let creation: StudioCreationResponse
    try { creation = await getStudioCreation(taskId, signal) }
    catch (error) {
      if (signal.aborted) throw signal.reason
      const failure = error as { status?: number; response?: { status?: number } }
      const status = failure.response?.status ?? failure.status
      if (status === 404) throw new Error('image_task_expired')
      if (status && status < 500 && status !== 429) throw error
      if (++failures >= 3) throw new TypeError('image_task_poll_failed')
      await waitForPoll(failures * 3000, signal)
      continue
    }
    failures = 0
    if (creation.id !== taskId) throw new TypeError('image_task_poll_failed')
    if (creation.status !== 'processing') return creation
    await waitForPoll(3000, signal)
  }
  throw signal.reason
}
export async function generateImages(apiKey: string, payload: ImageGenerationRequest, signal: AbortSignal, references: File[] = [], onSubmitted?: (task: StudioTask) => void): Promise<StudioCreationResponse> {
  const form = new FormData()
  if (references.length) {
    Object.entries(payload).forEach(([key, value]) => form.append(key, String(value)))
    references.forEach(file => form.append('image[]', file, file.name))
  }
  // Submit once; only the safe history GET is retried after interruption.
  const { response, body } = await gatewayRequest(buildGatewayUrl(references.length ? '/v1/images/studio/edits' : '/v1/images/studio/generations'), {
    method: 'POST',
    headers: { Authorization: `Bearer ${apiKey}`, ...(!references.length ? { 'Content-Type': 'application/json' } : {}) },
    body: references.length ? form : JSON.stringify(payload),
  }, signal)
  if (response.status !== 202 || !validTaskId(body?.task_id)) throw new TypeError('image_task_submission_unknown')
  onSubmitted?.({ taskId: body.task_id })
  return pollImageTask(body.task_id, signal)
}
