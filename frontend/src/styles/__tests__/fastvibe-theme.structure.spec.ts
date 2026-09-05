import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const frontendRoot = resolve(__dirname, '../../..')

function read(relativePath: string) {
  return readFileSync(resolve(frontendRoot, relativePath), 'utf8')
}

describe('FastVibe source theme', () => {
  it('loads the compiled theme from the frontend entrypoint', () => {
    const entrypoint = read('src/main.ts')
    const theme = read('src/styles/fastvibe-theme.css')

    expect(entrypoint).toContain("import './styles/fastvibe-theme.css'")
    expect(theme).toContain('--fv-accent: #2f6fed')
    expect(theme).toContain(':root.dark')
    expect(theme).toContain('.sidebar-link-active')
    expect(theme).toContain('.auth-card')
  })

  it('defines the brand palette in Tailwind instead of overriding utility classes at the proxy', () => {
    const tailwindConfig = read('tailwind.config.js')

    expect(tailwindConfig).toContain("500: '#2f6fed'")
    expect(tailwindConfig).toContain("950: '#17274c'")
    expect(tailwindConfig).toContain("'mesh-gradient': 'none'")
  })

  it('keeps decorative auth and app backgrounds out of the component structure', () => {
    const authLayout = read('src/components/layout/AuthLayout.vue')
    const appLayout = read('src/components/layout/AppLayout.vue')

    expect(authLayout).toContain('class="auth-shell')
    expect(authLayout).toContain('class="auth-card')
    expect(authLayout).not.toContain('Gradient Orbs')
    expect(authLayout).not.toContain('blur-3xl')
    expect(appLayout).toContain('class="app-shell')
    expect(appLayout).not.toContain('bg-mesh-gradient')
  })

  it('keeps both support workspaces tied to the global FastVibe theme tokens', () => {
    const userSupport = read('src/components/support/SupportWidget.vue')
    const adminSupport = read('src/components/support/AdminSupportWidget.vue')
    const adminWorkspace = read('src/views/admin/SupportView.vue')

    for (const source of [userSupport, adminSupport, adminWorkspace]) {
      expect(source).toContain('var(--fv-surface')
      expect(source).toContain('var(--fv-accent')
    }
    expect(userSupport).not.toContain('--support-canvas: #')
    expect(adminWorkspace).not.toContain('--chat-canvas: #')
    expect(adminWorkspace).toContain(':z-index="90"')
  })

  it('exposes the admin support workspace only through the header widget', () => {
    const sidebar = read('src/components/layout/AppSidebar.vue')
    const router = read('src/router/index.ts')
    const widget = read('src/components/support/AdminSupportWidget.vue')
    const workspace = read('src/views/admin/SupportView.vue')

    expect(sidebar).not.toContain("path: '/admin/support'")
    expect(sidebar).not.toContain('nav.supportManagement')
    expect(router).not.toContain("path: '/admin/support'")
    expect(router).not.toContain("name: 'AdminSupport'")
    expect(widget).toContain('<SupportView ref="supportViewRef" />')
    expect(workspace).not.toContain('embedded')
    expect(workspace).not.toContain('AppLayout')
    expect(workspace).not.toContain('useRoute')
    expect(workspace).not.toContain('useRouter')
  })

  it('does not submit support messages while an IME composition is active', () => {
    const userSupport = read('src/components/support/SupportWidget.vue')
    const adminSupport = read('src/views/admin/SupportView.vue')

    for (const source of [userSupport, adminSupport]) {
      expect(source).not.toContain('@keydown.enter.exact.prevent')
      expect(source).toContain('isIMECompositionKeyEvent(event)')
    }
    expect(userSupport).toContain('@keydown.enter.exact="handleMessageEnter"')
    expect(adminSupport).toContain('@keydown.enter.exact="handleReplyEnter"')
  })
})
