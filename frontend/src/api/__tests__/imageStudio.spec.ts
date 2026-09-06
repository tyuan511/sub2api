import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildImageRequest, canGenerateImages, generateImages, isImageGroup, getImageGenerationGroups, getImageRatios, getImageResolutions, isValidImageSize, pollImageTask } from '../imageStudio'
import type { ApiKey, Group } from '@/types'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { get: api.get } }))

const group = { id: 1, platform: 'openai', status: 'active', allow_image_generation: true } as Group
const key = { key: 'test-key', status: 'active', quota: 0, quota_used: 0, group } as ApiKey
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
const taskId = 'imgtask_' + 'a'.repeat(32)
const jsonResponse = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })
const accepted = () => jsonResponse({ task_id: taskId, poll_url: 'https://untrusted.example/poll' }, 202)
const completed = () => ({ id: taskId, status: 'completed', images: [{ id: 'file-id', url: 'https://storage.example/image.png' }], references: [] })

describe('image studio gateway contract', () => {
  it('omits size entirely for automatic ratio in JSON and reference-image requests', async () => {
    const fetcher = vi.fn().mockImplementation(async () => accepted())
    vi.stubGlobal('fetch', fetcher)
    api.get.mockResolvedValue({ data: completed() })
    for (const references of [[], [new File(['image'], 'reference.png', { type: 'image/png' })]]) {
      const body = buildImageRequest('gpt-image-2', 'auto scene', 'auto', 2, '4K', '2048x2048')
      expect(body).toEqual({ model: 'gpt-image-2', prompt: 'auto scene', n: 2 })
      await generateImages('test-key', body, new AbortController().signal, references)
    }
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).not.toHaveProperty('size')
    expect(fetcher.mock.calls[1][1].body.has('size')).toBe(false)
  })
  it('preserves exact ratios and satisfies image dimensions and billing tiers for every preset', () => {
    const ratios = getImageRatios('gpt-image-2')
    expect(ratios).toEqual(['1:1', '16:9', '9:16', '4:3', '3:4', '3:2', '2:3', '21:9'])
    for (const ratio of ratios.filter(ratio => ratio !== 'auto')) {
      for (const resolution of getImageResolutions('gpt-image-2', ratio)) {
        const [width, height] = buildImageRequest('gpt-image-2', 'test', ratio, 1, resolution).size!.split('x').map(Number)
        const [x, y] = ratio.split(':').map(Number)
        expect(width * y).toBe(height * x)
        expect(width % 16).toBe(0)
        expect(height % 16).toBe(0)
        expect(width * height).toBeGreaterThanOrEqual(655360)
        expect(width * height).toBeLessThanOrEqual(8294400)
        expect(Math.max(width, height)).toBeLessThanOrEqual(3840)
        expect(Math.max(width, height) / Math.min(width, height)).toBeLessThanOrEqual(3)
        const tier = Math.max(width, height) <= 1024 ? '1K' : Math.max(width, height) <= 2048 ? '2K' : '4K'
        expect(tier).toBe(resolution === '1K' && ['16:9', '9:16', '21:9'].includes(ratio) ? '2K' : resolution)
      }
    }
    expect(buildImageRequest('gpt-image-2', 'test', '16:9', 1, '4K').size).toBe('3840x2160')
    expect(buildImageRequest('gpt-image-2', 'test', '9:16', 1, '4K').size).toBe('2160x3840')
  })
  it('excludes unsupported sizes instead of silently generating a different ratio', () => {
    expect(getImageResolutions('gpt-image-2', '16:9')).toEqual(['1K', '2K', '4K'])
    expect(buildImageRequest('gpt-image-2', 'test', '16:9', 1, '1K').size).toBe('1792x1008')
    expect(getImageRatios('gpt-image-1.5')).toEqual(['1:1', '3:2', '2:3'])
    expect(() => buildImageRequest('gpt-image-1.5', 'test', '4:5', 1)).toThrow(RangeError)
  })
  it('retains valid historical presets and only offers official sizes for older image models', () => {
    for (const ratio of ['4:5', '5:4'] as const) {
      for (const preset of ['1K', '2K', '4K'] as const) {
        expect(isValidImageSize(buildImageRequest('gpt-image-2', 'old record', ratio, 1, preset).size!, ratio)).toBe(true)
      }
    }
    for (const model of ['gpt-image-1', 'gpt-image-1.5']) {
      const sizes = getImageRatios(model).filter(ratio => ratio !== 'auto').map(ratio => buildImageRequest(model, 'legacy', ratio, 1).size)
      expect(sizes).toEqual(['1024x1024', '1536x1024', '1024x1536'])
    }
  })
  it('sends custom dimensions exactly and rejects invalid or legacy overrides before submission', () => {
    expect(buildImageRequest('gpt-image-2', 'wide', '21:9', 2, '4K', '3024x1296').size).toBe('3024x1296')
    for (const size of ['100x100', '1025x1025', '4096x4096', '3840x3840', '2048x1024', 'invalid']) {
      expect(() => buildImageRequest('gpt-image-2', 'test', '1:1', 1, '1K', size)).toThrow(RangeError)
    }
    expect(() => buildImageRequest('gpt-image-1.5', 'test', '1:1', 1, '1K', '1024x1024')).toThrow(RangeError)
  })
  it('only accepts active OpenAI image groups and usable keys', () => {
    expect(canGenerateImages(key)).toBe(true)
    expect(isImageGroup({ ...group, platform: 'grok' })).toBe(false)
    expect(isImageGroup({ ...group, allow_image_generation: false })).toBe(false)
    expect(canGenerateImages({ ...key, status: 'inactive' })).toBe(false)
    expect(canGenerateImages({ ...key, expires_at: '2000-01-01' })).toBe(false)
    expect(canGenerateImages({ ...key, quota: 2, quota_used: 2 })).toBe(false)
  })
  it('loads authorized image groups before any Key is selected', async () => {
    const supported = { ...group, image_models: ['gpt-image-2'] }
    api.get.mockResolvedValue({ data: [supported, { ...group, id: 2, image_models: ['gpt-5'] }, { ...supported, id: 3, allow_image_generation: false }] })
    expect(await getImageGenerationGroups()).toEqual([supported])
    expect(api.get).toHaveBeenCalledWith('/groups/image-generation')
  })
  it('submits exactly once and polls authenticated database history without passing the Key', async () => {
    const submitted = vi.fn()
    const fetcher = vi.fn().mockResolvedValue(accepted())
    vi.stubGlobal('fetch', fetcher)
    api.get.mockResolvedValue({ data: completed() })
    const body = buildImageRequest('gpt-image-2', '  draw a cat  ', '3:2', 2, '2K')
    const result = await generateImages('secret-key', body, new AbortController().signal, [], submitted)
    expect(result.images).toHaveLength(1)
    expect(submitted).toHaveBeenCalledWith({ taskId })
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(fetcher.mock.calls[0][0]).toContain('/v1/images/studio/generations')
    expect(api.get).toHaveBeenLastCalledWith(`/image-studio/history/${taskId}`, { signal: expect.any(AbortSignal) })
    expect(JSON.stringify(api.get.mock.calls)).not.toContain('secret-key')
    expect(JSON.stringify(api.get.mock.calls)).not.toContain('untrusted.example')
  })
  it('sends references to the persistent edits endpoint as multipart without forcing a boundary', async () => {
    const fetcher = vi.fn().mockResolvedValue(accepted())
    vi.stubGlobal('fetch', fetcher)
    api.get.mockResolvedValue({ data: completed() })
    const file = new File(['image'], 'reference.png', { type: 'image/png' })
    await generateImages('test-key', buildImageRequest('gpt-image-1.5', 'edit', '2:3', 1), new AbortController().signal, [file])
    const [url, options] = fetcher.mock.calls[0]
    expect(url).toContain('/v1/images/studio/edits')
    expect(options.body.get('image[]').name).toBe('reference.png')
    expect(options.headers['Content-Type']).toBeUndefined()
  })
  it('retries history reads after a transient failure without resubmitting generation', async () => {
    vi.useFakeTimers()
    const fetcher = vi.fn().mockResolvedValue(accepted())
    vi.stubGlobal('fetch', fetcher)
    api.get.mockRejectedValueOnce(new TypeError('network interrupted')).mockResolvedValueOnce({ data: completed() })
    const pending = generateImages('key', buildImageRequest('gpt-image-2', 'test', '1:1', 1), new AbortController().signal)
    await vi.advanceTimersByTimeAsync(3000)
    expect((await pending).status).toBe('completed')
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
  it('stops polling after account cancellation', async () => {
    vi.useFakeTimers()
    api.get.mockResolvedValue({ data: { id: taskId, status: 'processing' } })
    const controller = new AbortController()
    const pending = pollImageTask(taskId, controller.signal)
    const stopped = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await vi.advanceTimersByTimeAsync(1)
    controller.abort()
    await stopped
  })
  it('never retries a rejected or ambiguous billed submission', async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(jsonResponse({ error: { message: 'insufficient balance' } }, 402)).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    vi.stubGlobal('fetch', fetcher)
    const payload = buildImageRequest('gpt-image-2', 'test', '1:1', 1)
    await expect(generateImages('key', payload, new AbortController().signal)).rejects.toThrow('insufficient balance')
    await expect(generateImages('key', payload, new AbortController().signal)).rejects.toThrow('Failed to fetch')
    expect(fetcher).toHaveBeenCalledTimes(2)
  })
})
