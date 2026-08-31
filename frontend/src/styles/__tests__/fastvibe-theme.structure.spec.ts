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
})
