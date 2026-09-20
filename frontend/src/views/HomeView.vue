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
    class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 px-4 py-4 sm:px-6 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img :src="siteLogo || '/logo.svg'" :alt="t('home.logoAlt')" class="h-9 w-9 shrink-0 rounded-lg object-contain" />
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
        <img :src="siteLogo || '/logo.svg'" :alt="t('home.logoAlt')" class="mx-auto mb-6 h-20 w-20 rounded-2xl object-contain" />
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
    class="home-page relative flex min-h-screen flex-col overflow-hidden bg-[#f7faf9] text-gray-950 dark:bg-dark-950 dark:text-white"
  >
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="home-orb home-orb-primary"></div>
      <div class="home-orb home-orb-secondary"></div>
      <div class="absolute inset-0 opacity-50 [background-image:linear-gradient(rgba(15,118,110,0.035)_1px,transparent_1px),linear-gradient(90deg,rgba(15,118,110,0.035)_1px,transparent_1px)] [background-size:72px_72px] dark:opacity-20"></div>
    </div>

    <header class="relative z-20 border-b border-gray-900/5 bg-white/55 px-4 py-4 backdrop-blur-xl dark:border-white/5 dark:bg-dark-950/50 sm:px-6 lg:px-8">
      <nav class="mx-auto flex max-w-7xl items-center justify-between gap-4">
        <router-link to="/" class="flex min-w-0 items-center gap-3" aria-label="Home">
          <span class="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10">
            <img :src="siteLogo || '/logo.svg'" :alt="t('home.logoAlt')" class="h-full w-full object-contain" />
          </span>
          <span class="hidden min-w-0 truncate text-sm font-semibold tracking-tight sm:block">{{ siteName }}</span>
        </router-link>

        <div class="flex items-center gap-1.5 sm:gap-2">
          <div class="home-locale-pill border-gray-900/10 bg-white/80 dark:border-white/10 dark:bg-dark-800/80">
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

    <main class="relative z-10 flex-1 px-4 py-12 sm:px-6 sm:py-16 lg:px-8 lg:py-20">
      <div class="mx-auto max-w-7xl">
        <section class="grid items-center gap-12 lg:grid-cols-[minmax(0,1.05fr)_minmax(420px,0.95fr)] lg:gap-20">
          <div class="max-w-2xl">
            <div class="mb-6 inline-flex items-center gap-2 rounded-full border border-primary-500/20 bg-primary-500/10 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.16em] text-primary-700 dark:text-primary-300">
              <span class="h-1.5 w-1.5 rounded-full bg-primary-500 shadow-[0_0_0_4px_rgba(20,184,166,0.12)]"></span>
              {{ t('home.eyebrow') }}
            </div>
            <h1 class="max-w-3xl text-4xl font-semibold leading-[1.08] tracking-[-0.04em] text-gray-950 dark:text-white sm:text-6xl lg:text-7xl">{{ siteName }}</h1>
            <p class="mt-6 max-w-xl text-lg leading-8 text-gray-600 dark:text-dark-300 sm:text-xl">{{ siteSubtitle }}</p>
            <p class="mt-4 max-w-xl text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('home.heroDescription') }}</p>
            <div class="mt-8 flex flex-wrap items-center gap-3">
              <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="inline-flex items-center gap-2 rounded-xl bg-primary-600 px-5 py-3 text-sm font-semibold text-white shadow-lg shadow-primary-600/20 transition hover:-translate-y-0.5 hover:bg-primary-700">
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="sm" />
              </router-link>
              <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="inline-flex items-center gap-2 rounded-xl border border-gray-900/10 bg-white/75 px-5 py-3 text-sm font-semibold text-gray-700 transition hover:-translate-y-0.5 hover:bg-white dark:border-white/10 dark:bg-dark-800/70 dark:text-dark-100 dark:hover:bg-dark-800">
                <Icon name="grid" size="sm" />
                {{ t('home.viewModels') }}
              </router-link>
            </div>
            <div class="mt-9 flex flex-wrap gap-x-6 gap-y-3 text-sm text-gray-500 dark:text-dark-400">
              <span class="inline-flex items-center gap-2"><Icon name="check" size="sm" class="text-primary-500" />{{ t('home.trust.singleKey') }}</span>
              <span class="inline-flex items-center gap-2"><Icon name="check" size="sm" class="text-primary-500" />{{ t('home.trust.smartRouting') }}</span>
              <span class="inline-flex items-center gap-2"><Icon name="check" size="sm" class="text-primary-500" />{{ t('home.trust.livePricing') }}</span>
            </div>
          </div>

          <div class="terminal-container mx-auto w-full max-w-xl lg:ml-auto">
            <div class="home-console overflow-hidden rounded-[1.75rem] border border-white/10 bg-[#101a1a] shadow-2xl shadow-primary-950/20">
              <div class="flex items-center justify-between border-b border-white/10 px-5 py-4">
                <div class="flex items-center gap-2">
                  <span class="h-2.5 w-2.5 rounded-full bg-rose-400"></span><span class="h-2.5 w-2.5 rounded-full bg-amber-300"></span><span class="h-2.5 w-2.5 rounded-full bg-emerald-400"></span>
                </div>
                <span class="font-mono text-xs text-white/45">{{ t('home.console.title') }}</span>
                <span class="text-[10px] uppercase tracking-[0.18em] text-primary-300/70">live</span>
              </div>
              <div class="p-5 sm:p-7">
                <div class="font-mono text-xs leading-7 text-white/55 sm:text-sm">
                  <div><span class="text-emerald-400">$</span> curl -X POST <span class="text-sky-300">/v1/messages</span></div>
                  <div class="text-white/35">{{ t('home.console.routing') }}</div>
                  <div class="mt-3 rounded-xl border border-white/10 bg-white/[0.04] p-4">
                    <div class="flex items-center justify-between text-[11px] uppercase tracking-[0.16em] text-white/40"><span>{{ t('home.console.response') }}</span><span class="text-emerald-300">200 OK</span></div>
                    <div class="mt-3 text-sm leading-6 text-white/85">{ "content": "{{ t('home.console.responseText') }}" }</div>
                  </div>
                </div>
                <div class="mt-6 grid grid-cols-3 gap-2 border-t border-white/10 pt-5">
                  <div><div class="text-xl font-semibold text-white">{{ modelCount || '—' }}</div><div class="mt-1 text-[10px] uppercase tracking-wider text-white/40">{{ t('home.stats.models') }}</div></div>
                  <div><div class="text-xl font-semibold text-white">{{ platformCount || '—' }}</div><div class="mt-1 text-[10px] uppercase tracking-wider text-white/40">{{ t('home.stats.platforms') }}</div></div>
                  <div><div class="text-xl font-semibold text-white">{{ groupCount || '—' }}</div><div class="mt-1 text-[10px] uppercase tracking-wider text-white/40">{{ t('home.stats.groups') }}</div></div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section data-testid="home-model-plaza" class="mt-20 sm:mt-28">
          <div class="flex flex-col justify-between gap-5 sm:flex-row sm:items-end">
            <div>
              <div class="text-xs font-semibold uppercase tracking-[0.18em] text-primary-600 dark:text-primary-400">{{ t('home.catalog.kicker') }}</div>
              <h2 class="mt-3 text-3xl font-semibold tracking-[-0.03em] text-gray-950 dark:text-white sm:text-4xl">{{ t('home.catalog.title') }}</h2>
              <p class="mt-3 max-w-2xl text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('home.catalog.description') }}</p>
            </div>
            <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="inline-flex shrink-0 items-center gap-2 text-sm font-semibold text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200">
              {{ t('home.catalog.viewAll') }} <Icon name="arrowRight" size="sm" />
            </router-link>
          </div>

          <div v-if="modelPlazaLoading" class="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <div v-for="index in 6" :key="index" class="rounded-2xl border border-gray-900/5 bg-white/70 p-5 shadow-sm dark:border-white/5 dark:bg-dark-800/50" :class="index ? 'h-40' : ''"><div class="h-3 w-20 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div><div class="mt-5 h-5 w-36 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div><div class="mt-4 h-3 w-full animate-pulse rounded bg-gray-100 dark:bg-dark-800"></div></div>
          </div>
          <div v-else-if="featuredModels.length" class="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <article v-for="entry in featuredModels" :key="`${entry.platform}-${entry.name}`" class="group relative overflow-hidden rounded-2xl border border-gray-900/5 bg-white/80 p-5 shadow-sm transition duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-gray-900/5 dark:border-white/5 dark:bg-dark-800/55 dark:hover:shadow-black/20">
              <div :class="['absolute inset-x-0 top-0 h-1', platformAccentBarClass(entry.platform)]"></div>
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <span :class="['inline-flex rounded-md px-2 py-1 text-[10px] font-semibold uppercase tracking-wider', platformBadgeLightClass(entry.platform)]">{{ platformLabel(entry.platform) }}</span>
                  <h3 class="mt-4 truncate text-base font-semibold text-gray-950 dark:text-white" :title="entry.name">{{ entry.name }}</h3>
                  <p class="mt-1 truncate text-xs text-gray-400 dark:text-dark-500">{{ t('home.catalog.lowestPrice') }}</p>
                </div>
                <Icon name="externalLink" size="sm" class="shrink-0 text-gray-300 transition group-hover:-translate-y-0.5 group-hover:translate-x-0.5 group-hover:text-primary-500 dark:text-dark-600" />
              </div>
              <div class="mt-5 grid grid-cols-2 gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
                <div v-if="entry.billingMode === BILLING_MODE_TOKEN"><div class="text-[10px] uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('home.catalog.input') }}</div><div class="mt-1 font-mono text-sm font-semibold text-gray-800 dark:text-dark-100">{{ formatPrice(entry.inputPrice, 1_000_000) }}</div></div>
                <div v-if="entry.billingMode === BILLING_MODE_TOKEN"><div class="text-[10px] uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('home.catalog.output') }}</div><div class="mt-1 font-mono text-sm font-semibold text-gray-800 dark:text-dark-100">{{ formatPrice(entry.outputPrice, 1_000_000) }}</div></div>
                <div v-else class="col-span-2"><div class="text-[10px] uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ billingLabel(entry.model) }}</div><div class="mt-1 font-mono text-lg font-semibold text-gray-800 dark:text-dark-100">{{ formatPrice(entry.requestPrice, 1) }}</div></div>
              </div>
            </article>
          </div>
          <div v-else class="mt-8 rounded-2xl border border-dashed border-gray-300 bg-white/50 px-6 py-12 text-center text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900/30 dark:text-dark-400">
            {{ modelPlazaFailed ? t('home.catalog.unavailable') : t('home.catalog.empty') }}
          </div>
        </section>

        <section class="mt-20 grid gap-4 border-t border-gray-900/5 pt-8 sm:grid-cols-3 dark:border-white/5">
          <div v-for="item in valueProps" :key="item.title" class="rounded-2xl px-1 py-3 sm:px-4">
            <Icon :name="item.icon" size="md" class="text-primary-600 dark:text-primary-400" />
            <h3 class="mt-4 text-sm font-semibold text-gray-950 dark:text-white">{{ t(item.title) }}</h3>
            <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t(item.description) }}</p>
          </div>
        </section>
      </div>
    </main>

    <footer class="relative z-10 border-t border-gray-900/5 px-4 py-8 dark:border-white/5 sm:px-6 lg:px-8">
      <div class="mx-auto flex max-w-7xl flex-col items-center justify-between gap-4 text-center text-sm text-gray-500 dark:text-dark-400 sm:flex-row sm:text-left">
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <div class="flex items-center gap-4"><a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="hover:text-gray-900 dark:hover:text-white">{{ t('home.docs') }}</a><a :href="githubUrl" target="_blank" rel="noopener noreferrer" class="hover:text-gray-900 dark:hover:text-white">GitHub</a></div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { getModelPlaza, type ModelPlazaGroup, type PlazaModel, type ModelPlazaResponse } from '@/api/modelPlaza'
import { BILLING_MODE_TOKEN, BILLING_MODE_IMAGE, BILLING_MODE_VIDEO, type BillingMode } from '@/constants/channel'
import { resolveIntervalPrices } from '@/utils/pricing'
import { platformAccentBarClass, platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
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
const isDark = ref(document.documentElement.classList.contains('dark'))
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'
const isAuthenticated = computed(() => authStore.isAuthenticated)
const showModelPlazaEntry = computed(() => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value))
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const currentYear = computed(() => new Date().getFullYear())

const modelPlaza = ref<ModelPlazaResponse | null>(null)
const modelPlazaLoading = ref(false)
const modelPlazaFailed = ref(false)

interface FeaturedModel {
  model: PlazaModel
  platform: string
  name: string
  billingMode: BillingMode
  inputPrice: number | null
  outputPrice: number | null
  requestPrice: number | null
}

const plazaGroups = computed<ModelPlazaGroup[]>(() => modelPlaza.value?.groups ?? [])
const modelCount = computed(() => new Set(plazaGroups.value.flatMap((group) => group.models.map((model) => model.name))).size)
const platformCount = computed(() => new Set(plazaGroups.value.map((group) => group.platform).filter(Boolean)).size)
const groupCount = computed(() => plazaGroups.value.length)

function lowerPrice(current: number | null, candidate: number | null): number | null {
  if (current == null) return candidate
  if (candidate == null) return current
  return Math.min(current, candidate)
}

const featuredModels = computed<FeaturedModel[]>(() => {
  const merged = new Map<string, FeaturedModel>()
  for (const group of plazaGroups.value) {
    for (const model of group.models) {
      const platform = model.platform || group.platform
      const key = `${platform}-${model.name}`
      const pricing = effectivePricing(model)
      const mode = billingMode(model)
      const current = merged.get(key)
      if (!current) {
        merged.set(key, {
          model,
          platform,
          name: model.name,
          billingMode: mode,
          inputPrice: pricing?.input_price ?? null,
          outputPrice: pricing?.output_price ?? null,
          requestPrice: pricing?.per_request_price ?? null
        })
        continue
      }
      current.inputPrice = lowerPrice(current.inputPrice, pricing?.input_price ?? null)
      current.outputPrice = lowerPrice(current.outputPrice, pricing?.output_price ?? null)
      current.requestPrice = lowerPrice(current.requestPrice, pricing?.per_request_price ?? null)
    }
  }
  return [...merged.values()].slice(0, 9)
})

const valueProps = [
  { icon: 'key', title: 'home.valueProps.singleKey', description: 'home.valueProps.singleKeyDesc' },
  { icon: 'server', title: 'home.valueProps.smartRouting', description: 'home.valueProps.smartRoutingDesc' },
  { icon: 'chart', title: 'home.valueProps.clearUsage', description: 'home.valueProps.clearUsageDesc' },
] as const

function effectivePricing(model: PlazaModel) {
  const pricing = model.pricing
  const interval = pricing?.intervals?.[0]
  return interval && pricing ? resolveIntervalPrices(interval, pricing) : pricing
}

function billingMode(model: PlazaModel): BillingMode {
  return (model.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

function billingLabel(model: PlazaModel) {
  const mode = billingMode(model)
  if (mode === BILLING_MODE_IMAGE) return t('home.catalog.perImage')
  if (mode === BILLING_MODE_VIDEO) return t('home.catalog.perVideo')
  return t('home.catalog.perRequest')
}

function formatPrice(value: number | null | undefined, scale: number) {
  if (value == null) return t('home.catalog.notAvailable')
  const amount = value * scale
  return `$${amount < 0.01 ? amount.toPrecision(3) : amount.toFixed(amount >= 100 ? 0 : 2).replace(/\.00$/, '')}`
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

async function loadModelPlaza() {
  if (!modelPlazaEnabled.value || hasHomeContent.value || compactHomeEnabled.value) return
  modelPlazaLoading.value = true
  try {
    modelPlaza.value = await getModelPlaza()
  } catch {
    modelPlazaFailed.value = true
  } finally {
    modelPlazaLoading.value = false
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) void appStore.fetchPublicSettings()
  void loadModelPlaza()
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
  background: rgb(45 212 191 / 18%);
}

.home-orb-secondary {
  bottom: 15rem;
  left: -12rem;
  width: 30rem;
  height: 30rem;
  background: rgb(125 211 252 / 13%);
}

.home-locale-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.15rem;
  border: 1px solid rgb(17 24 39 / 8%);
  border-radius: 0.75rem;
  background: rgb(255 255 255 / 70%);
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
  background: rgb(255 255 255 / 75%);
  transform: translateY(-1px);
}

.home-console {
  transform: perspective(1200px) rotateY(-2deg) rotateX(1deg);
  transition: transform 0.3s ease, box-shadow 0.3s ease;
}

.home-console:hover {
  transform: perspective(1200px) rotateY(0deg) rotateX(0deg) translateY(-4px);
  box-shadow: 0 30px 70px rgb(15 118 110 / 18%);
}
</style>
