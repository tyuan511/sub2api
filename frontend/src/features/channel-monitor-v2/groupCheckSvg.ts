import DOMPurify from 'dompurify'

/** Treat generated SVG as untrusted input. Return an isolated image URL, never markup. */
export function groupCheckSvgUrl(raw: string | null | undefined): string | null {
  if (!raw || raw.length > 1_000_000) return null
  const cleaned = DOMPurify.sanitize(raw, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ['style', 'script', 'foreignObject', 'image', 'use', 'a', 'animate', 'animateTransform', 'animateMotion', 'set'],
    FORBID_ATTR: ['style', 'href', 'xlink:href'],
  })
  const doc = new DOMParser().parseFromString(cleaned, 'image/svg+xml')
  const root = doc.documentElement
  if (root.localName !== 'svg' || root.namespaceURI !== 'http://www.w3.org/2000/svg' || doc.querySelector('parsererror')) return null
  for (const element of [root, ...Array.from(root.querySelectorAll('*'))]) {
    for (const attribute of Array.from(element.attributes)) {
      // Presentation attributes can contain CSS URL references as well as hrefs.
      if (/url\s*\(|(?:href|src)$/i.test(attribute.value) || /(?:href|src)$/i.test(attribute.name)) {
        element.removeAttributeNode(attribute)
      }
    }
  }
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(root))}`
}
