<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Compact Home Page -->
  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="flex min-h-screen flex-col bg-transparent text-gray-900 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img :src="siteLogo || '/logo.png'" :alt="t('home.logoAlt')" class="h-9 w-9 shrink-0 rounded-lg object-contain" />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <div class="home-locale-pill"><Icon name="globe" size="sm" /><LocaleSwitcher /></div>
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="t('home.viewDocs')"
          ><Icon name="book" size="md" /></a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="flex h-10 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('nav.modelPlaza')"
          ><Icon name="grid" size="md" /><span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span></router-link>
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
            @click="toggleTheme"
          ><Icon v-if="isDark" name="sun" size="md" /><Icon v-else name="moon" size="md" /></button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="inline-flex min-h-10 shrink-0 items-center justify-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          >{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="min-w-0 max-w-2xl text-center">
        <img :src="siteLogo || '/logo.png'" :alt="t('home.logoAlt')" class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain" />
        <h1 class="[overflow-wrap:anywhere] text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap [overflow-wrap:anywhere] text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700"
        >{{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}</router-link>
      </div>
    </main>

    <footer class="min-w-0 border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 [overflow-wrap:anywhere] sm:px-6 dark:border-dark-800 dark:text-dark-400">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    data-testid="default-home"
    class="home-page relative flex min-h-screen flex-col overflow-hidden bg-transparent text-gray-950 dark:text-white"
  >
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="home-orb home-orb-primary"></div>
      <div class="home-orb home-orb-secondary"></div>
      <DotFieldBackground
        class="home-dotfield"
        :dot-radius="2.6"
        :dot-spacing="24"
        :bulge-strength="60"
        :gradient-from="dotFieldColors.from"
        :gradient-to="dotFieldColors.to"
        :glow-color="dotFieldColors.glow"
      />
    </div>

    <header class="relative z-20 px-4 py-4 sm:px-6 lg:px-8">
      <nav class="mx-auto flex max-w-7xl items-center justify-between gap-4">
        <router-link to="/" class="flex min-w-0 items-center gap-3" aria-label="Home">
          <span class="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10">
            <img :src="siteLogo || '/logo.png'" :alt="t('home.logoAlt')" class="h-full w-full object-contain" />
          </span>
          <span class="hidden min-w-0 truncate text-sm font-semibold tracking-tight sm:block">{{ siteName }}</span>
        </router-link>

        <div class="flex items-center gap-1.5 sm:gap-2">
          <div class="home-locale-pill">
            <Icon name="globe" size="sm" class="text-primary-600 dark:text-primary-400" />
            <LocaleSwitcher />
          </div>
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="home-icon-button"
            :title="t('home.viewDocs')"
          ><Icon name="book" size="md" /></a>
          <router-link
            v-if="showModelPlazaEntry"
            to="/model-plaza"
            class="hidden items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium text-gray-600 transition hover:bg-white hover:text-gray-950 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white sm:inline-flex"
          ><Icon name="grid" size="sm" />{{ t('nav.modelPlaza') }}</router-link>
          <button class="home-icon-button" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon v-if="isDark" name="sun" size="md" /><Icon v-else name="moon" size="md" />
          </button>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="rounded-xl bg-gray-950 px-3.5 py-2 text-sm font-semibold text-white shadow-sm transition hover:-translate-y-0.5 hover:bg-gray-800 dark:bg-white dark:text-gray-950 dark:hover:bg-gray-200"
          >{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
        </div>
      </nav>
    </header>

    <main class="relative z-10 flex flex-1 flex-col items-center justify-center px-4 py-14 text-center sm:px-6 lg:px-8">
      <div class="hero-stage flex w-full max-w-3xl flex-col items-center">
        <span class="eyebrow rise" style="--delay: 0ms">
          <span class="eyebrow-dot"></span>{{ t('home.badge') }}
        </span>

        <h1 class="hero-title rise" style="--delay: 60ms">
          {{ t('home.titleLead') }}<span class="title-accent">{{ t('home.titleAccent') }}</span>
        </h1>

        <p class="hero-lede rise" style="--delay: 120ms">{{ t('home.heroDesc') }}</p>

        <div class="cta-row rise" style="--delay: 180ms">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="btn-primary-xl">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
            <Icon name="arrowRight" size="sm" />
          </router-link>
          <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="btn-glass-xl">
            <Icon name="grid" size="sm" />{{ t('home.browsePlaza') }}
          </router-link>
        </div>

        <div
          v-if="showModelPlazaEntry"
          class="marquee rise"
          style="--delay: 240ms"
          role="img"
          :aria-label="t('home.marqueeLabel')"
        >
          <div class="marquee-track">
            <span v-for="copy in 2" :key="copy" class="marquee-group" :aria-hidden="copy === 2 ? 'true' : undefined">
              <span v-for="item in showcaseItems" :key="`${copy}-${item.name}`" class="model-pill">
                <PlatformIcon
                  v-if="item.icon"
                  :platform="item.icon.platform"
                  size="sm"
                  class="model-icon"
                  :style="item.icon.color ? { color: item.icon.color } : undefined"
                />
                <span v-else class="model-dot" :style="{ background: modelDotColor(item.name) }"></span>{{ item.name }}
              </span>
            </span>
          </div>
        </div>
        <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="view-all rise" style="--delay: 280ms">
          {{ t('home.viewAllModels') }} <Icon name="arrowRight" size="sm" />
        </router-link>

        <div class="trust-row rise" style="--delay: 320ms">
          <span class="trust-chip"><Icon name="check" size="sm" class="trust-check" />{{ t('home.trust.smartRouting') }}</span>
          <span class="trust-chip"><Icon name="check" size="sm" class="trust-check" />{{ t('home.trust.payAsYouGo') }}</span>
          <span class="trust-chip"><Icon name="check" size="sm" class="trust-check" />{{ t('home.trust.usageInsight') }}</span>
        </div>
      </div>
    </main>

  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import { useTheme } from '@/composables/useTheme'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import DotFieldBackground from '@/components/backgrounds/DotFieldBackground.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupPlatform } from '@/types'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.defaultSubtitle'))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const modelPlazaRequiresAuth = computed(() => appStore.cachedPublicSettings?.model_plaza_require_auth === true)
const isHomeContentUrl = computed(() => /^https?:\/\//.test(homeContent.value.trim()))
const { isDark, toggleTheme } = useTheme()
const isAuthenticated = computed(() => authStore.isAuthenticated)
const showModelPlazaEntry = computed(() => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value))
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())

// 点阵背景配色随亮暗主题切换（暖底上保留蓝紫点缀，但更收敛）
const dotFieldColors = computed(() =>
  isDark.value
    ? {
        from: 'rgba(111, 157, 255, 0.6)',
        to: 'rgba(167, 139, 250, 0.5)',
        glow: 'rgba(111, 157, 255, 0.12)'
      }
    : {
        from: 'rgba(47, 111, 237, 0.65)',
        to: 'rgba(139, 92, 246, 0.5)',
        glow: 'rgba(47, 111, 237, 0.12)'
      }
)

// 首页只展示模型名称（不展示价格），完整列表与价格在模型广场。
const showcaseModels = computed(() =>
  t('home.showcaseModels')
    .split(',')
    .map((name) => name.trim())
    .filter(Boolean)
)

// 模型名 → 平台官方 logo（路径来自 @lobehub/icons / 官方品牌素材，单色渲染）+ 品牌色。
// 未收录的名字回退到平台色圆点。
interface ShowcaseIcon {
  platform: GroupPlatform
  color?: string
}

const MODEL_ICONS: Record<string, ShowcaseIcon> = {
  claude: { platform: 'anthropic', color: '#d97757' },
  anthropic: { platform: 'anthropic', color: '#d97757' },
  gpt: { platform: 'openai', color: '#10a37f' },
  chatgpt: { platform: 'openai', color: '#10a37f' },
  openai: { platform: 'openai', color: '#10a37f' },
  gemini: { platform: 'gemini', color: '#4285f4' },
  deepseek: { platform: 'deepseek', color: '#4d6bfe' },
  grok: { platform: 'grok' },
  qwen: { platform: 'qwen', color: '#615ced' },
  kimi: { platform: 'kimi', color: '#5b6bfa' },
  glm: { platform: 'zhipu', color: '#3859ff' },
  zhipu: { platform: 'zhipu', color: '#3859ff' },
  minimax: { platform: 'minimax', color: '#ef4444' }
}

const showcaseItems = computed(() =>
  showcaseModels.value.map((name) => ({ name, icon: MODEL_ICONS[name.toLowerCase()] }))
)

// 兜底圆点颜色：按平台着色，未识别的名字回退到品牌色。
const MODEL_DOT_COLORS: Record<string, string> = {
  claude: '#d97757',
  gpt: '#10a37f',
  chatgpt: '#10a37f',
  openai: '#10a37f',
  gemini: '#8e75f2',
  deepseek: '#4d6bfe',
  grok: '#94a3b8',
  qwen: '#7c5cff',
  kimi: '#5b6bfa',
  glm: '#4a6cf7'
}

function modelDotColor(name: string) {
  return MODEL_DOT_COLORS[name.trim().toLowerCase()] || 'var(--fv-accent)'
}

onMounted(() => {
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) void appStore.fetchPublicSettings()
})
</script>

<style scoped>
.home-page {
  isolation: isolate;
}

.home-orb {
  position: absolute;
  border-radius: 9999px;
  filter: blur(70px);
  opacity: 0.45;
}

.home-orb-primary {
  right: -8rem;
  top: 5rem;
  width: 28rem;
  height: 28rem;
  background: var(--fv-halo-1);
}

.home-orb-secondary {
  bottom: 15rem;
  left: -12rem;
  width: 30rem;
  height: 30rem;
  background: var(--fv-halo-2);
}

.home-dotfield {
  position: absolute;
  inset: 0;
  opacity: 0.8;
}

.dark .home-dotfield {
  opacity: 0.85;
}

.home-locale-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.15rem;
  border: 1px solid var(--fv-line);
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--fv-surface-solid) 78%, transparent);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  padding-left: 0.55rem;
  color: var(--fv-muted);
}

.home-locale-pill :deep(button) {
  border-radius: 0.7rem;
}

.home-icon-button {
  display: inline-flex;
  height: 2.5rem;
  width: 2.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  color: var(--fv-muted);
  transition: background-color 0.2s, color 0.2s, transform 0.2s;
}

.home-icon-button:hover {
  color: var(--fv-text);
  background: var(--fv-accent-wash);
  transform: translateY(-1px);
}

/* ---------- 极简单屏 Hero ---------- */
.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid rgba(47, 111, 237, 0.25);
  background: var(--fv-accent-wash);
  color: var(--fv-accent);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.dark .eyebrow {
  border-color: rgba(111, 157, 255, 0.3);
}

.eyebrow-dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--fv-accent);
  animation: pulse-dot 2.4s ease-in-out infinite;
}

@keyframes pulse-dot {
  0%, 100% { box-shadow: 0 0 0 0 rgba(47, 111, 237, 0.35); }
  50% { box-shadow: 0 0 0 5px rgba(47, 111, 237, 0); }
}

.hero-title {
  margin-top: 24px;
  font-size: clamp(34px, 5.4vw, 58px);
  line-height: 1.14;
  font-weight: 700;
  letter-spacing: -0.035em;
  color: var(--fv-text);
}

.title-accent {
  display: block;
  padding-bottom: 0.08em;
  background: linear-gradient(94deg, var(--fv-accent) 18%, #8b5cf6 88%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.hero-lede {
  margin-top: 18px;
  max-width: 34rem;
  font-size: 16px;
  line-height: 1.8;
  color: var(--fv-muted);
}

.cta-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 30px;
}

.btn-primary-xl {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 12px 24px;
  border-radius: 12px;
  background: var(--fv-accent);
  color: #ffffff;
  font-size: 14.5px;
  font-weight: 600;
  box-shadow: 0 10px 28px rgba(47, 111, 237, 0.28);
  transition: background-color 0.18s, transform 0.18s, box-shadow 0.18s;
}

.dark .btn-primary-xl {
  color: var(--fv-on-accent);
}

.btn-primary-xl:hover {
  background: var(--fv-accent-hover);
  transform: translateY(-1px);
  box-shadow: 0 14px 34px rgba(47, 111, 237, 0.32);
}

.btn-glass-xl {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 12px 24px;
  border-radius: 12px;
  border: 1px solid var(--fv-line);
  background: color-mix(in srgb, var(--fv-surface-solid) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  color: var(--fv-text-2);
  font-size: 14.5px;
  font-weight: 600;
  transition: background-color 0.18s, color 0.18s, transform 0.18s;
}

.btn-glass-xl:hover {
  background: var(--fv-surface-solid);
  color: var(--fv-text);
  transform: translateY(-1px);
}

/* ---------- 模型跑马灯（仅名称，无价格；完整列表在模型广场） ---------- */
.marquee {
  width: min(560px, 92vw);
  margin-top: 36px;
  overflow: hidden;
  -webkit-mask-image: linear-gradient(90deg, transparent, #000 14%, #000 86%, transparent);
  mask-image: linear-gradient(90deg, transparent, #000 14%, #000 86%, transparent);
}

.marquee-track {
  display: flex;
  width: max-content;
  animation: marquee-move 36s linear infinite;
}

.marquee:hover .marquee-track {
  animation-play-state: paused;
}

.marquee-group {
  display: flex;
  gap: 10px;
  padding-right: 10px;
}

@keyframes marquee-move {
  to {
    transform: translateX(-50%);
  }
}

.model-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 999px;
  border: 1px solid var(--fv-line);
  background: color-mix(in srgb, var(--fv-surface-solid) 62%, transparent);
  color: var(--fv-text-2);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

.model-dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  flex-shrink: 0;
}

.model-icon {
  opacity: 0.95;
}

.view-all {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-top: 16px;
  font-size: 13px;
  font-weight: 600;
  color: var(--fv-accent);
  transition: color 0.15s;
}

.view-all:hover {
  color: var(--fv-accent-hover);
}

.trust-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 10px 26px;
  margin-top: 32px;
}

.trust-chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  color: var(--fv-muted);
}

.trust-check {
  color: var(--fv-accent);
}

/* ---------- 入场动画 ---------- */
.rise {
  opacity: 0;
  animation: rise-in 0.6s cubic-bezier(0.22, 0.61, 0.36, 1) forwards;
  animation-delay: var(--delay, 0ms);
}

@keyframes rise-in {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

/* 用户偏好减少动效：直接呈现静态布局（全局令牌层还会把时长压到 1ms，这里再保证结构不滚动） */
@media (prefers-reduced-motion: reduce) {
  .rise {
    animation: none;
    opacity: 1;
  }

  .eyebrow-dot {
    animation: none;
  }

  .marquee-track {
    animation: none;
    width: auto;
    flex-wrap: wrap;
    justify-content: center;
  }
}

@media (max-width: 640px) {
  .hero-title {
    font-size: 30px;
  }
}
</style>
