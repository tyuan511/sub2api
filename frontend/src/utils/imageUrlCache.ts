// S3-compatible URLs encode their own lifetime. Keep the exact URL while it
// is usable so the browser can reuse the response instead of a new signature.
export function imageUrlExpiresAt(value: string): number {
  try {
    const url = new URL(value)
    const date = url.searchParams.get('X-Amz-Date')
    const seconds = url.searchParams.get('X-Amz-Expires')
    if (date !== null || seconds !== null) {
      const parts = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/.exec(date || '')
      if (!parts || !seconds || !/^\d+$/.test(seconds)) return 0
      const start = Date.parse(`${parts[1]}-${parts[2]}-${parts[3]}T${parts[4]}:${parts[5]}:${parts[6]}Z`)
      const expiry = start + Number(seconds) * 1000
      return Number.isFinite(expiry) ? expiry : 0
    }
    // Older S3 signatures use an absolute Unix timestamp.
    const expires = url.searchParams.get('Expires')
    if (expires !== null) return /^\d+$/.test(expires) ? Number(expires) * 1000 : 0
    return ['https:', 'http:', 'blob:'].includes(url.protocol) ? Infinity : 0
  } catch { return 0 }
}

export function isImageUrlFresh(url: string, now = Date.now()): boolean {
  return imageUrlExpiresAt(url) > now + 60_000
}
