import type { RouteLocationNormalizedLoaded } from 'vue-router'

export const SEO_ORIGIN = 'https://fastvibe.dev'
export const DEFAULT_SEO_SITE_NAME = 'FastVibe'
export const DEFAULT_SEO_DESCRIPTION =
  'FastVibe provides fast, reliable AI API access to multiple models with one developer-friendly gateway.'

type SeoOptions = {
  title: string
  description: string
  canonicalPath?: string
  noindex?: boolean
  siteName?: string
}

function upsertMeta(attribute: 'name' | 'property', key: string, content: string): void {
  let element = document.head.querySelector<HTMLMetaElement>(`meta[${attribute}="${key}"]`)
  if (!element) {
    element = document.createElement('meta')
    element.setAttribute(attribute, key)
    document.head.appendChild(element)
  }
  element.content = content
}

function upsertLink(rel: string, href: string): void {
  let element = document.head.querySelector<HTMLLinkElement>(`link[rel="${rel}"]`)
  if (!element) {
    element = document.createElement('link')
    element.rel = rel
    document.head.appendChild(element)
  }
  element.href = href
}

function upsertStructuredData(siteName: string, description: string, url: string): void {
  let element = document.head.querySelector<HTMLScriptElement>('#fastvibe-structured-data')
  if (!element) {
    element = document.createElement('script')
    element.id = 'fastvibe-structured-data'
    element.type = 'application/ld+json'
    document.head.appendChild(element)
  }

  element.textContent = JSON.stringify({
    '@context': 'https://schema.org',
    '@type': 'WebSite',
    name: siteName,
    url,
    description,
    publisher: {
      '@type': 'Organization',
      name: siteName,
      url: SEO_ORIGIN,
    },
  })
}

/** Update the document head for both the initial SPA shell and client-side routes. */
export function updateSeoMetadata(options: SeoOptions): void {
  const siteName = options.siteName?.trim() || DEFAULT_SEO_SITE_NAME
  const description = options.description.trim() || DEFAULT_SEO_DESCRIPTION
  const canonicalPath = options.canonicalPath || window.location.pathname
  const canonicalUrl = new URL(canonicalPath, SEO_ORIGIN).toString()
  const robots = options.noindex ? 'noindex, nofollow' : 'index, follow'

  document.title = options.title.trim() || siteName
  upsertMeta('name', 'description', description)
  upsertMeta('name', 'robots', robots)
  upsertMeta('name', 'googlebot', robots)
  upsertMeta('property', 'og:type', 'website')
  upsertMeta('property', 'og:site_name', siteName)
  upsertMeta('property', 'og:title', document.title)
  upsertMeta('property', 'og:description', description)
  upsertMeta('property', 'og:url', canonicalUrl)
  upsertMeta('property', 'og:image', new URL('/fastvibe-support-logo.png', SEO_ORIGIN).toString())
  upsertMeta('name', 'twitter:card', 'summary')
  upsertMeta('name', 'twitter:title', document.title)
  upsertMeta('name', 'twitter:description', description)
  upsertMeta('name', 'twitter:image', new URL('/fastvibe-support-logo.png', SEO_ORIGIN).toString())
  upsertLink('canonical', canonicalUrl)
  upsertStructuredData(siteName, description, canonicalUrl)
}

export function seoForRoute(
  route: Pick<RouteLocationNormalizedLoaded, 'path' | 'meta'>,
  title: string,
  siteName: string | undefined,
  configuredDescription?: string,
): SeoOptions {
  const isHome = route.path === '/' || route.path === '/home'
  return {
    title,
    description: configuredDescription || DEFAULT_SEO_DESCRIPTION,
    canonicalPath: isHome ? '/home' : route.path,
    noindex: route.meta.requiresAuth !== false,
    siteName,
  }
}
