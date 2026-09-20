import type { PublicSettings } from '@/types'

declare global {
  interface Window {
    __APP_CONFIG__?: PublicSettings
    dataLayer?: unknown[]
    gtag?: (...args: unknown[]) => void
  }
}

export {}
