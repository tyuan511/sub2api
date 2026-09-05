<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-5 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.routingOptimization.title') }}</h1>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.routingOptimization.description') }}</p>
          </div>
          <button class="btn btn-secondary" :disabled="loading" @click="loadExperiments">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
            {{ t('common.refresh') }}
          </button>
        </div>
        <div v-if="errorMessage" class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
          {{ errorMessage }}
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.routingOptimization.experiments') }}</h2>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-4 py-3">{{ t('admin.routingOptimization.experiment') }}</th>
                <th class="px-4 py-3">{{ t('admin.routingOptimization.scope') }}</th>
                <th class="px-4 py-3">{{ t('admin.routingOptimization.versions') }}</th>
                <th class="px-4 py-3">{{ t('admin.routingOptimization.bucket') }}</th>
                <th class="px-4 py-3">{{ t('admin.routingOptimization.status') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr
                v-for="item in experiments"
                :key="item.id"
                class="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/70"
                :class="selected?.id === item.id ? 'bg-primary-50/60 dark:bg-primary-950/20' : ''"
                @click="selectExperiment(item)"
              >
                <td class="px-4 py-3 font-mono text-xs text-gray-800 dark:text-gray-200">{{ item.experiment_key }}</td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                  {{ item.platform }} / {{ item.model_family }} / {{ item.endpoint_kind }} / {{ item.preference }}
                </td>
                <td class="px-4 py-3 font-mono text-xs text-gray-500">
                  {{ item.baseline_strategy_version }} → {{ item.candidate_strategy_version }}
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ formatPercent(item.allocation_bps / 10000) }}</td>
                <td class="px-4 py-3"><span :class="statusClass(item.status)">{{ item.status }}</span></td>
              </tr>
              <tr v-if="!loading && experiments.length === 0">
                <td colspan="5" class="px-4 py-12 text-center text-gray-400">{{ t('admin.routingOptimization.empty') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <template v-if="selected">
        <section class="card p-5 sm:p-6">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ selected.experiment_key }}</h2>
              <p class="mt-1 text-xs text-gray-500">{{ selected.bucket_salt_checksum }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button v-if="selected.status === 'draft'" class="btn btn-primary" :disabled="acting" @click="runReplay">
                {{ t('admin.routingOptimization.runReplay') }}
              </button>
              <button v-if="selected.status === 'paused'" class="btn btn-primary" :disabled="acting" @click="resumeSelected">
                {{ t('admin.routingOptimization.resumeShadow') }}
              </button>
              <button v-if="['draft', 'shadow', 'canary', 'active'].includes(selected.status)" class="btn btn-secondary" :disabled="acting" @click="pauseSelected">
                {{ t('admin.routingOptimization.pause') }}
              </button>
              <button v-if="['canary', 'active'].includes(selected.status)" class="btn btn-danger" :disabled="acting" @click="rollbackSelected">
                {{ t('admin.routingOptimization.rollback') }}
              </button>
            </div>
          </div>

          <div class="mt-5 grid grid-cols-2 gap-3 md:grid-cols-4">
            <VersionCard label="baseline" :value="pointers?.baseline_version || selected.baseline_strategy_version" />
            <VersionCard label="active" :value="pointers?.active_version || '—'" />
            <VersionCard label="canary" :value="pointers?.canary_version || '—'" />
            <VersionCard label="shadow" :value="pointers?.shadow_version || '—'" />
          </div>

          <div v-if="selected.stop_reason" class="mt-4 rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
            {{ t('admin.routingOptimization.stopReason') }}: {{ selected.stop_reason }}
          </div>
        </section>

        <section class="grid gap-6 xl:grid-cols-2">
          <div class="card p-5 sm:p-6">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.routingOptimization.offlineReplay') }}</h3>
            <div v-if="offlineReplay?.replay_version" class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <MetricCard :label="t('admin.routingOptimization.passed')" :value="offlineReplay.passed ? t('common.yes') : t('common.no')" />
              <MetricCard :label="t('admin.routingOptimization.decisions')" :value="formatNumber(offlineReplay.decision_count)" />
              <MetricCard :label="t('admin.routingOptimization.replayed')" :value="formatNumber(offlineReplay.replayed_decisions)" />
              <MetricCard :label="t('admin.routingOptimization.changedRank')" :value="formatNumber(offlineReplay.changed_top_choice)" />
              <MetricCard :label="t('admin.routingOptimization.missingFeatures')" :value="formatNumber(offlineReplay.missing_feature_decisions)" />
              <MetricCard :label="t('admin.routingOptimization.invalid')" :value="formatNumber(offlineReplay.invalid_decisions)" />
              <MetricCard :label="t('admin.routingOptimization.hardViolations')" :value="formatNumber(offlineReplay.hard_constraint_violations)" />
              <p class="col-span-2 text-xs leading-relaxed text-gray-500">{{ offlineReplay.selection_bias_notice }}</p>
              <div v-if="datasetManifest" class="col-span-2 mt-2 rounded-lg border border-gray-200 p-3 text-xs dark:border-dark-700">
                <div class="grid gap-2 sm:grid-cols-2">
                  <p><span class="text-gray-500">{{ t('admin.routingOptimization.queryVersion') }}:</span> <span class="font-mono">{{ datasetManifest.query_version }}</span></p>
                  <p><span class="text-gray-500">{{ t('admin.routingOptimization.datasetRows') }}:</span> {{ formatNumber(datasetManifest.row_count) }}</p>
                  <p class="sm:col-span-2"><span class="text-gray-500">{{ t('admin.routingOptimization.replayWindow') }}:</span> {{ formatDateRange(datasetManifest.since, datasetManifest.until) }}</p>
                  <p><span class="text-gray-500">{{ t('admin.routingOptimization.featureSchemas') }}:</span> {{ datasetManifest.feature_schema_versions?.join(', ') || '—' }}</p>
                  <p><span class="text-gray-500">{{ t('admin.routingOptimization.samplingRange') }}:</span> {{ formatProbabilityRange(datasetManifest.minimum_sample_probability, datasetManifest.maximum_sample_probability) }}</p>
                  <p class="sm:col-span-2"><span class="text-gray-500">{{ t('admin.routingOptimization.pointInTimeJoin') }}:</span> {{ datasetManifest.point_in_time_join }}</p>
                  <p class="sm:col-span-2"><span class="text-gray-500">{{ t('admin.routingOptimization.exclusions') }}:</span> {{ datasetManifest.exclusion_rules?.join('; ') || '—' }}</p>
                  <p class="sm:col-span-2 break-all"><span class="text-gray-500">SHA-256:</span> <span class="font-mono">{{ datasetManifest.checksum }}</span></p>
                </div>
              </div>
            </div>
            <p v-else class="mt-4 text-sm text-gray-400">{{ t('admin.routingOptimization.noEvidence') }}</p>
          </div>

          <div class="card p-5 sm:p-6">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.routingOptimization.canaryEvidence') }}</h3>
            <div v-if="evaluation?.metric_policy_version" class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <MetricCard :label="t('admin.routingOptimization.metric')" :value="evaluation.primary_metric || '—'" />
              <MetricCard :label="t('admin.routingOptimization.promotionEligible')" :value="evaluation.promotion_eligible ? t('common.yes') : t('common.no')" />
              <MetricCard :label="t('admin.routingOptimization.baselineSamples')" :value="formatNumber(evaluation.baseline_samples)" />
              <MetricCard :label="t('admin.routingOptimization.candidateSamples')" :value="formatNumber(evaluation.candidate_samples)" />
              <MetricCard :label="t('admin.routingOptimization.observationWindow')" :value="formatDuration(evaluation.candidate_metrics?.observation_duration)" />
              <MetricCard :label="t('admin.routingOptimization.experimentCoverage')" :value="formatExperimentCoverage(evaluation.candidate_metrics)" />
              <MetricCard :label="t('admin.routingOptimization.eventCoverage')" :value="formatPercent(evaluation.candidate_metrics?.event_coverage)" />
              <MetricCard :label="t('admin.routingOptimization.successRate')" :value="formatPercent(evaluation.candidate_metrics?.final_success_rate)" />
              <MetricCard label="95% CI" :value="formatCI(evaluation.candidate_metrics)" />
              <MetricCard :label="t('admin.routingOptimization.failureRisk')" :value="formatPercent(evaluation.candidate_metrics?.failure_risk)" />
              <MetricCard label="P95 TTFT" :value="formatMS(evaluation.candidate_metrics?.p95_ttft_ms)" />
              <MetricCard label="P99 TTFT" :value="formatMS(evaluation.candidate_metrics?.p99_ttft_ms)" />
              <MetricCard label="P95 Duration" :value="formatMS(evaluation.candidate_metrics?.p95_latency_ms)" />
              <MetricCard label="P99 Duration" :value="formatMS(evaluation.candidate_metrics?.p99_latency_ms)" />
              <MetricCard :label="t('admin.routingOptimization.expectedSuccessfulCost')" :value="formatDecimal(evaluation.candidate_metrics?.expected_successful_cost)" />
              <MetricCard :label="t('admin.routingOptimization.expectedTimeToSuccess')" :value="formatMS(evaluation.candidate_metrics?.expected_time_to_success_ms)" />
              <MetricCard :label="t('admin.routingOptimization.stabilityLoss')" :value="formatDecimal(evaluation.candidate_metrics?.stability_loss)" />
              <MetricCard :label="t('admin.routingOptimization.calibrationError')" :value="formatDecimal(evaluation.candidate_metrics?.prediction_calibration_error)" />
              <MetricCard :label="t('admin.routingOptimization.missingFeatureRate')" :value="formatPercent(evaluation.candidate_metrics?.missing_feature_rate)" />
              <MetricCard :label="t('admin.routingOptimization.featureDrift')" :value="formatBoolean(evaluation.candidate_metrics?.feature_drift_detected)" />
              <MetricCard :label="t('admin.routingOptimization.featureSchemas')" :value="evaluation.candidate_metrics?.feature_schema_version || '—'" />
              <MetricCard :label="t('admin.routingOptimization.successDrift')" :value="formatSignedPercent(evaluation.success_rate_drift)" />
              <MetricCard :label="t('admin.routingOptimization.costDrift')" :value="formatSignedPercent(evaluation.cost_drift_ratio)" />
              <MetricCard :label="t('admin.routingOptimization.latencyDrift')" :value="formatSignedPercent(evaluation.latency_drift_ratio)" />
              <MetricCard :label="t('admin.routingOptimization.mappingVersion')" :value="evaluation.score_loss_mapping_version || '—'" />
              <div v-if="evaluation.violations?.length" class="col-span-2 rounded-lg bg-red-50 p-3 text-xs text-red-700 dark:bg-red-950/30 dark:text-red-300">
                {{ evaluation.violations.join(', ') }}
              </div>
              <div class="col-span-2 mt-2 overflow-x-auto">
                <table class="min-w-full divide-y divide-gray-200 text-xs dark:divide-dark-700">
                  <thead class="text-left text-gray-500">
                    <tr><th class="py-2 pr-3">{{ t('admin.routingOptimization.metric') }}</th><th class="px-3 py-2">Baseline</th><th class="px-3 py-2">Candidate</th></tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                    <tr v-for="row in metricComparisonRows" :key="row.label">
                      <td class="py-2 pr-3 text-gray-600 dark:text-gray-300">{{ row.label }}</td>
                      <td class="px-3 py-2 font-mono">{{ row.baseline }}</td>
                      <td class="px-3 py-2 font-mono">{{ row.candidate }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <p v-else class="mt-4 text-sm text-gray-400">{{ t('admin.routingOptimization.noEvidence') }}</p>
          </div>
        </section>

        <section v-if="criticalSlices.length" class="card overflow-hidden">
          <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.routingOptimization.criticalSlices') }}</h3>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800">
                <tr><th class="px-4 py-3">Key ID</th><th class="px-4 py-3">{{ t('admin.routingOptimization.decisions') }}</th><th class="px-4 py-3">{{ t('admin.routingOptimization.successRate') }}</th><th class="px-4 py-3">95% CI</th></tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="slice in criticalSlices" :key="slice.api_key_id">
                  <td class="px-4 py-3 font-mono">{{ slice.api_key_id }}</td>
                  <td class="px-4 py-3">{{ formatNumber(slice.decisions) }}</td>
                  <td class="px-4 py-3">{{ formatPercent(slice.final_success_rate) }}</td>
                  <td class="px-4 py-3">{{ formatPercent(slice.success_rate_lower_bound) }} – {{ formatPercent(slice.success_rate_upper_bound) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import MetricCard from '@/components/common/MetricCard.vue'
import VersionCard from '@/components/common/VersionCard.vue'
import { adminAPI, type RoutingArtifactPointers, type RoutingCanaryMetrics, type RoutingExperiment } from '@/api/admin'

const { t } = useI18n()
const loading = ref(false)
const acting = ref(false)
const experiments = ref<RoutingExperiment[]>([])
const selected = ref<RoutingExperiment | null>(null)
const pointers = ref<RoutingArtifactPointers | null>(null)
const errorMessage = ref('')

const offlineReplay = computed(() => selected.value?.offline_replay)
const datasetManifest = computed(() => offlineReplay.value?.dataset_manifest)
const evaluation = computed(() => selected.value?.last_evaluation)
const criticalSlices = computed(() => evaluation.value?.candidate_metrics?.critical_slices || [])
const metricComparisonRows = computed(() => {
  const baseline = evaluation.value?.baseline_metrics
  const candidate = evaluation.value?.candidate_metrics
  return [
    { label: t('admin.routingOptimization.successRate'), baseline: formatPercent(baseline?.final_success_rate), candidate: formatPercent(candidate?.final_success_rate) },
    { label: t('admin.routingOptimization.failureRisk'), baseline: formatPercent(baseline?.failure_risk), candidate: formatPercent(candidate?.failure_risk) },
    { label: t('admin.routingOptimization.expectedSuccessfulCost'), baseline: formatDecimal(baseline?.expected_successful_cost), candidate: formatDecimal(candidate?.expected_successful_cost) },
    { label: t('admin.routingOptimization.expectedTimeToSuccess'), baseline: formatMS(baseline?.expected_time_to_success_ms), candidate: formatMS(candidate?.expected_time_to_success_ms) },
    { label: 'P95 / P99 Duration', baseline: formatPercentiles(baseline?.p95_latency_ms, baseline?.p99_latency_ms), candidate: formatPercentiles(candidate?.p95_latency_ms, candidate?.p99_latency_ms) },
    { label: 'P95 / P99 TTFT', baseline: formatPercentiles(baseline?.p95_ttft_ms, baseline?.p99_ttft_ms), candidate: formatPercentiles(candidate?.p95_ttft_ms, candidate?.p99_ttft_ms) },
    { label: t('admin.routingOptimization.stabilityLoss'), baseline: formatDecimal(baseline?.stability_loss), candidate: formatDecimal(candidate?.stability_loss) },
  ]
})

async function loadExperiments() {
  loading.value = true
  errorMessage.value = ''
  try {
    experiments.value = await adminAPI.routingOptimization.listExperiments()
    if (selected.value) {
      selected.value = experiments.value.find(item => item.id === selected.value?.id) || null
    }
    if (!selected.value && experiments.value.length) await selectExperiment(experiments.value[0])
  } catch (error: any) {
    errorMessage.value = error?.message || String(error)
  } finally {
    loading.value = false
  }
}

async function selectExperiment(item: RoutingExperiment) {
  selected.value = item
  pointers.value = null
  try {
    pointers.value = await adminAPI.routingOptimization.getPointers(item)
  } catch {
    // A draft scope may not have been published yet; its lifecycle remains visible.
  }
}

async function runAction(action: () => Promise<unknown>) {
  if (!selected.value) return
  acting.value = true
  errorMessage.value = ''
  const selectedID = selected.value.id
  try {
    await action()
    await loadExperiments()
    const refreshed = experiments.value.find(item => item.id === selectedID)
    if (refreshed) await selectExperiment(refreshed)
  } catch (error: any) {
    errorMessage.value = error?.message || String(error)
  } finally {
    acting.value = false
  }
}

function runReplay() {
  if (selected.value) void runAction(() => adminAPI.routingOptimization.runOfflineReplay(selected.value!.experiment_key))
}

function pauseSelected() {
  if (!selected.value || !window.confirm(t('admin.routingOptimization.pauseConfirm'))) return
  void runAction(() => adminAPI.routingOptimization.pauseExperiment(selected.value!.experiment_key, 'manual_admin_pause'))
}

function resumeSelected() {
  if (selected.value) void runAction(() => adminAPI.routingOptimization.resumeExperiment(selected.value!.experiment_key))
}

function rollbackSelected() {
  if (!selected.value || !window.confirm(t('admin.routingOptimization.rollbackConfirm'))) return
  void runAction(() => adminAPI.routingOptimization.rollbackExperiment(selected.value!))
}

function statusClass(status: string) {
  if (status === 'active') return 'badge badge-success'
  if (status === 'canary' || status === 'shadow') return 'badge badge-warning'
  if (status === 'paused') return 'badge badge-danger'
  return 'badge badge-gray'
}

function formatPercent(value?: number) {
  return typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(2)}%` : '—'
}
function formatNumber(value?: number) { return typeof value === 'number' ? value.toLocaleString() : '—' }
function formatMS(value?: number) { return typeof value === 'number' && value > 0 ? `${value.toFixed(0)} ms` : '—' }
function formatDecimal(value?: number) { return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(4) : '—' }
function formatSignedPercent(value?: number) {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  const formatted = `${(value * 100).toFixed(2)}%`
  return value > 0 ? `+${formatted}` : formatted
}
function formatBoolean(value?: boolean) {
  return typeof value === 'boolean' ? (value ? t('common.yes') : t('common.no')) : '—'
}
function formatCI(metrics?: Partial<RoutingCanaryMetrics>) {
  if (!metrics) return '—'
  return `${formatPercent(metrics.success_rate_lower_bound)} – ${formatPercent(metrics.success_rate_upper_bound)}`
}
function formatDuration(nanoseconds?: number) {
  if (typeof nanoseconds !== 'number' || nanoseconds <= 0) return '—'
  const seconds = nanoseconds / 1_000_000_000
  if (seconds >= 86400) return `${(seconds / 86400).toFixed(1)} d`
  if (seconds >= 3600) return `${(seconds / 3600).toFixed(1)} h`
  return `${(seconds / 60).toFixed(1)} min`
}
function formatExperimentCoverage(metrics?: Partial<RoutingCanaryMetrics>) {
  if (!metrics || !metrics.expected_decisions || typeof metrics.decisions !== 'number') return '—'
  return `${formatPercent(metrics.decisions / metrics.expected_decisions)} (${formatNumber(metrics.decisions)} / ${formatNumber(metrics.expected_decisions)})`
}
function formatPercentiles(p95?: number, p99?: number) {
  return `${formatMS(p95)} / ${formatMS(p99)}`
}
function formatDateRange(since?: string, until?: string) {
  if (!since || !until) return '—'
  return `${new Date(since).toLocaleString()} – ${new Date(until).toLocaleString()}`
}
function formatProbabilityRange(minimum?: number, maximum?: number) {
  if (typeof minimum !== 'number' || typeof maximum !== 'number') return '—'
  return `${formatPercent(minimum)} – ${formatPercent(maximum)}`
}

onMounted(loadExperiments)
</script>
