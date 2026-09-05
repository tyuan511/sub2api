import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchImage } from '../fetchImage'

afterEach(() => vi.unstubAllGlobals())
describe('image file reads', () => {
  const url = 'https://storage.test/image.png?X-Amz-Signature=keep-this-signature'
  it('uses the normal browser cache when it is readable', async () => {
    const response = new Response('image')
    const request = vi.fn().mockResolvedValue(response)
    vi.stubGlobal('fetch', request)
    expect(await fetchImage(url, new AbortController().signal)).toBe(response)
    expect(request).toHaveBeenCalledTimes(1)
    expect(request.mock.calls[0][1]).not.toHaveProperty('cache')
  })
  it('recovers from an opaque cached response once, without changing signed URLs or sending credentials', async () => {
    const response = new Response('image')
    const request = vi.fn().mockRejectedValueOnce(new TypeError('Failed to fetch')).mockResolvedValueOnce(response)
    vi.stubGlobal('fetch', request)
    const controller = new AbortController()
    expect(await fetchImage(url, controller.signal)).toBe(response)
    expect(request).toHaveBeenCalledTimes(2)
    expect(request.mock.calls[1]).toEqual([url, { credentials: 'omit', referrerPolicy: 'no-referrer', signal: controller.signal, cache: 'reload' }])
    expect(request.mock.calls[1][1]).not.toHaveProperty('headers')
  })
  it('does not retry cancelled requests or loop on persistent failures', async () => {
    const controller = new AbortController()
    const request = vi.fn().mockImplementation(() => { controller.abort(); return Promise.reject(new TypeError('Failed to fetch')) })
    vi.stubGlobal('fetch', request)
    await expect(fetchImage(url, controller.signal)).rejects.toThrow('Failed to fetch')
    expect(request).toHaveBeenCalledTimes(1)
    request.mockReset().mockRejectedValue(new TypeError('Failed to fetch'))
    await expect(fetchImage(url, new AbortController().signal)).rejects.toThrow('Failed to fetch')
    expect(request).toHaveBeenCalledTimes(2)
  })
})
