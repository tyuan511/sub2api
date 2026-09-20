import { sanitizeUrl } from '@/utils/url'

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  const logoPath = sanitizedLogoUrl.split('?')[0].toLowerCase()
  link.type = logoPath.endsWith('.svg')
    ? 'image/svg+xml'
    : logoPath.endsWith('.png')
      ? 'image/png'
      : 'image/x-icon'
  link.href = sanitizedLogoUrl
}
