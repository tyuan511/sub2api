import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import StudioThumbnail from '../StudioThumbnail.vue'

const ensure = vi.hoisted(() => vi.fn())
vi.mock('@/api/imageStudio', () => ({ ensureStudioThumbnail: ensure }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
afterEach(() => { vi.unstubAllGlobals(); ensure.mockReset() })

function visibility() {
  const callbacks: IntersectionObserverCallback[] = []
  vi.stubGlobal('IntersectionObserver', class {
    constructor(callback: IntersectionObserverCallback) { callbacks.push(callback) }
    observe() {}
    disconnect() {}
  })
  return () => callbacks.forEach(callback => callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver))
}
const render = (id: string, thumbnail_url?: string) => mount(StudioThumbnail, {
  props: { image: { id, url: `https://images.test/${id}.png`, thumbnail_url }, alt: 'Image' },
  global: { stubs: { Icon: true } },
})

describe('studio thumbnails', () => {
  it('does not load offscreen images and uses WebP instead of the original', async () => {
    const show = visibility()
    const wrapper = render('visible', 'https://images.test/visible.thumb.webp')
    await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    show(); await flushPromises()
    expect(wrapper.get('img').attributes('src')).toBe('https://images.test/visible.thumb.webp')
    expect(ensure).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('deduplicates missing thumbnail requests and reuses the returned URL', async () => {
    const show = visibility()
    ensure.mockResolvedValue({ thumbnail_url: 'https://images.test/legacy.thumb.webp' })
    const first = render('legacy'), second = render('legacy')
    show(); await flushPromises()
    expect(ensure).toHaveBeenCalledTimes(1)
    expect(first.get('img').attributes('src')).toBe('https://images.test/legacy.thumb.webp')
    first.unmount(); second.unmount()
    const third = render('legacy'); show(); await flushPromises()
    expect(ensure).toHaveBeenCalledTimes(1)
    third.unmount()
  })
  it('renews an expired thumbnail signature without downloading the original', async () => {
    const show = visibility()
    ensure.mockResolvedValue({ thumbnail_url: 'https://images.test/renewed.webp' })
    const wrapper = render('expired', 'https://images.test/expired.webp?X-Amz-Date=20200101T000000Z&X-Amz-Expires=1')
    show(); await flushPromises()
    expect(ensure).toHaveBeenCalledWith('expired')
    expect(wrapper.get('img').attributes('src')).toBe('https://images.test/renewed.webp')
    wrapper.unmount()
  })
  it('keeps a placeholder on failure instead of fetching a large original', async () => {
    const show = visibility()
    ensure.mockRejectedValue(new Error('Unavailable'))
    const wrapper = render('unavailable')
    show(); await flushPromises()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[role="img"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
