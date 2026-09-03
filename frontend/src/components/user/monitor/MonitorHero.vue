<template>
  <section class="relative isolate py-4 md:py-6">
    <div
      aria-hidden="true"
      class="pointer-events-none absolute -inset-x-8 top-0 h-64 bg-[radial-gradient(circle_at_18%_10%,rgba(47,111,237,0.16),transparent_38%),radial-gradient(circle_at_82%_0%,rgba(14,165,233,0.12),transparent_32%)] dark:bg-[radial-gradient(circle_at_18%_10%,rgba(126,166,255,0.12),transparent_38%),radial-gradient(circle_at_82%_0%,rgba(14,165,233,0.08),transparent_32%)]"
    ></div>

    <div
      class="relative overflow-hidden rounded-[30px] border border-primary-200/70 bg-gradient-to-br from-white via-primary-50/80 to-slate-100/80 shadow-[0_24px_70px_rgba(30,64,175,0.12)] dark:border-primary-900/60 dark:from-dark-800 dark:via-dark-900 dark:to-[#101a2b] dark:shadow-[0_24px_70px_rgba(0,0,0,0.24)]"
    >
      <span
        aria-hidden="true"
        class="pointer-events-none absolute -right-24 -top-28 h-80 w-80 rounded-full border-[36px] border-primary-200/30 dark:border-primary-500/10"
      ></span>
      <span
        aria-hidden="true"
        class="pointer-events-none absolute -right-8 -top-12 h-56 w-56 rounded-full border border-primary-300/30 dark:border-primary-400/10"
      ></span>
      <span
        aria-hidden="true"
        class="pointer-events-none absolute inset-0 opacity-40 [background-image:linear-gradient(rgba(47,111,237,0.06)_1px,transparent_1px),linear-gradient(90deg,rgba(47,111,237,0.06)_1px,transparent_1px)] [background-size:28px_28px] dark:opacity-20"
      ></span>

      <div class="relative grid gap-8 p-6 md:p-8 lg:grid-cols-[minmax(0,1fr)_minmax(310px,0.58fr)] lg:gap-12 lg:p-10">
        <div class="min-w-0">
          <div class="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.28em] text-primary-600 dark:text-primary-300">
            <span class="relative flex h-2.5 w-2.5">
              <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400/70"></span>
              <span class="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500 ring-4 ring-emerald-500/10"></span>
            </span>
            {{ t('channelStatus.kicker') }}
          </div>

          <div class="mt-4 flex flex-wrap items-center gap-3">
            <h1 class="font-mono text-3xl font-bold tracking-tight text-gray-950 dark:text-white md:text-5xl">
              {{ t('channelStatus.title') }}
            </h1>
            <span
              class="inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-[11px] font-bold tracking-[0.16em] ring-1 ring-inset ring-black/5 dark:ring-white/10"
              :class="overallChipClass"
            >
              <span class="h-2 w-2 rounded-full" :class="overallDotClass"></span>
              {{ overallLabel }}
            </span>
          </div>

          <p class="mt-4 max-w-2xl text-sm leading-7 text-gray-600 dark:text-gray-300 md:text-base">
            {{ t('channelStatus.description') }}
          </p>

          <div class="mt-8 grid max-w-2xl grid-cols-3 divide-x divide-gray-300/70 dark:divide-dark-600/70">
            <div class="pr-4 md:pr-8">
              <div class="font-mono text-3xl font-bold tabular-nums text-gray-950 dark:text-white md:text-4xl">
                {{ channelCount }}
              </div>
              <div class="mt-1 text-[11px] font-semibold uppercase tracking-[0.13em] text-gray-500 dark:text-gray-400">
                {{ t('channelStatus.monitored') }}
              </div>
            </div>
            <div class="px-4 md:px-8">
              <div class="font-mono text-3xl font-bold tabular-nums text-emerald-600 dark:text-emerald-300 md:text-4xl">
                {{ healthyCount }}
              </div>
              <div class="mt-1 text-[11px] font-semibold uppercase tracking-[0.13em] text-gray-500 dark:text-gray-400">
                {{ t('channelStatus.healthy') }}
              </div>
            </div>
            <div class="pl-4 md:pl-8">
              <div class="font-mono text-3xl font-bold tabular-nums text-amber-600 dark:text-amber-300 md:text-4xl">
                {{ degradedCount }}
              </div>
              <div class="mt-1 text-[11px] font-semibold uppercase tracking-[0.13em] text-gray-500 dark:text-gray-400">
                {{ t('channelStatus.attention') }}
              </div>
            </div>
          </div>
        </div>

        <div class="flex min-w-0 flex-col justify-end">
          <div class="rounded-2xl border border-white/80 bg-white/75 p-3 shadow-[0_14px_35px_rgba(30,64,175,0.08)] backdrop-blur-md dark:border-dark-600/70 dark:bg-dark-800/70 dark:shadow-none">
            <div class="flex items-center justify-between px-1 pb-2">
              <span class="text-[10px] font-bold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">
                {{ t('channelStatus.rangeLabel') }}
              </span>
              <span class="font-mono text-[10px] text-gray-400 dark:text-gray-500">
                {{ t('monitorCommon.pollEvery', { n: intervalSeconds }) }}
              </span>
            </div>

            <div
              role="tablist"
              class="grid grid-cols-3 gap-1 rounded-xl bg-gray-100/90 p-1 dark:bg-dark-900/80"
              :aria-label="t('channelStatus.rangeLabel')"
            >
              <button
                v-for="opt in windowOptions"
                :key="opt.value"
                type="button"
                role="tab"
                :aria-selected="window === opt.value"
                class="rounded-lg px-3 py-2.5 text-xs font-semibold transition-all duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-400"
                :class="window === opt.value
                  ? 'bg-white text-gray-950 shadow-sm dark:bg-dark-700 dark:text-white'
                  : 'text-gray-500 hover:bg-white/70 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-dark-700/60 dark:hover:text-gray-200'"
                @click="emit('update:window', opt.value)"
              >
                {{ opt.label }}
              </button>
            </div>

            <div class="mt-3 flex items-center justify-end gap-2">
              <button
                type="button"
                class="inline-flex h-9 items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 text-xs font-semibold text-gray-600 shadow-sm transition-all hover:border-primary-300 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300 dark:hover:border-primary-500/60 dark:hover:text-primary-200"
                :disabled="loading"
                :title="t('common.refresh')"
                @click="emit('refresh')"
              >
                <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
                {{ t('common.refresh') }}
              </button>

              <AutoRefreshButton
                v-if="autoRefresh"
                :enabled="autoRefresh.enabled.value"
                :interval-seconds="autoRefresh.intervalSeconds.value"
                :countdown="autoRefresh.countdown.value"
                :intervals="autoRefresh.intervals"
                @update:enabled="autoRefresh.setEnabled"
                @update:interval="autoRefresh.setInterval"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AutoRefreshButton from '@/components/common/AutoRefreshButton.vue'
export type MonitorWindow = '7d' | '15d' | '30d'
export type OverallStatus = 'operational' | 'degraded'

const props = defineProps<{
  overallStatus: OverallStatus
  intervalSeconds: number
  window: MonitorWindow
  loading: boolean
  channelCount: number
  healthyCount: number
  degradedCount: number
  autoRefresh?: {
    enabled: { value: boolean }
    intervalSeconds: { value: number }
    countdown: { value: number }
    intervals: readonly number[]
    setEnabled: (v: boolean) => void
    setInterval: (v: number) => void
  }
}>()

const emit = defineEmits<{
  (e: 'update:window', value: MonitorWindow): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const windowOptions = computed<{ value: MonitorWindow; label: string }[]>(() => [
  { value: '7d', label: t('channelStatus.windowTab.7d') },
  { value: '15d', label: t('channelStatus.windowTab.15d') },
  { value: '30d', label: t('channelStatus.windowTab.30d') },
])

const overallLabel = computed(() => t(`channelStatus.overall.${props.overallStatus}`))

const overallChipClass = computed(() => {
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'degraded':
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
  }
})

const overallDotClass = computed(() => {
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-500 animate-pulse'
    case 'degraded':
    default:
      return 'bg-amber-500 animate-pulse'
  }
})

</script>
