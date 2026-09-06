import { describe, expect, it } from 'vitest'
import { imageUrlExpiresAt, isImageUrlFresh } from '../imageUrlCache'

describe('image URL lifetime', () => {
  const now = Date.parse('2026-09-05T08:00:00Z')
  it('reuses S3 signatures until one minute before their exact expiration', () => {
    const url = 'https://storage.test/image.png?X-Amz-Date=20260905T080000Z&X-Amz-Expires=3600&X-Amz-Signature=sample'
    expect(imageUrlExpiresAt(url)).toBe(now + 3_600_000)
    expect(isImageUrlFresh(url, now + 3_539_999)).toBe(true)
    expect(isImageUrlFresh(url, now + 3_540_000)).toBe(false)
    expect(isImageUrlFresh(url, now + 3_600_001)).toBe(false)
  })
  it('handles absolute expirations and refuses malformed or incomplete signatures', () => {
    expect(imageUrlExpiresAt('https://storage.test/image?Expires=1788598800')).toBe(1788598800000)
    for (const query of ['X-Amz-Date=invalid&X-Amz-Expires=3600', 'X-Amz-Date=20260905T080000Z', 'X-Amz-Expires=3600', 'Expires=no']) {
      expect(isImageUrlFresh(`https://storage.test/image?${query}`, now)).toBe(false)
    }
  })
  it('keeps stable public URLs without inventing a signature deadline', () => {
    expect(isImageUrlFresh('https://blob.fastvibe.dev/images/example.png', now)).toBe(true)
    expect(isImageUrlFresh('not a URL', now)).toBe(false)
    expect(isImageUrlFresh('javascript:alert(1)', now)).toBe(false)
  })
})
