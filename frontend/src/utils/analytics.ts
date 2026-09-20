const GA_MEASUREMENT_ID = 'G-2WK6L032VS'

/**
 * Sends a virtual page view for Vue Router navigations.
 * The initial automatic page view is disabled in index.html so that SPA
 * navigations and the initial route use the same tracking path.
 */
export function trackPageView(path: string, title: string) {
  if (typeof window === 'undefined' || typeof window.gtag !== 'function') return

  window.gtag('event', 'page_view', {
    send_to: GA_MEASUREMENT_ID,
    page_title: title,
    page_location: `${window.location.origin}${path}`,
    page_path: path,
  })
}
