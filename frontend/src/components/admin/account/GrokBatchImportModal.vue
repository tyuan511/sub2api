<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.grokBatchImport.title')"
    width="wide"
    close-on-click-outside
    @close="handleClose"
  >
    <div class="space-y-5">
      <!-- 账号类型：目前仅 Grok Heavy -->
      <div>
        <label class="input-label">{{ t('admin.accounts.grokBatchImport.typeLabel') }}</label>
        <div class="tabs inline-flex flex-wrap" role="tablist">
          <button
            v-for="tab in accountTypes"
            :key="tab.id"
            type="button"
            role="tab"
            class="tab"
            :class="activeType === tab.id ? 'tab-active' : ''"
            :aria-selected="activeType === tab.id"
            @click="activeType = tab.id"
          >
            {{ tab.label }}
          </button>
        </div>
      </div>

      <!-- 凭证输入 -->
      <div>
        <div class="flex items-end justify-between gap-3">
          <label class="input-label mb-0">{{ t('admin.accounts.grokBatchImport.credentialsLabel') }}</label>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.accounts.grokBatchImport.parsedCount', { count: parsed.items.length }) }}
          </span>
        </div>
        <textarea
          v-model="rawInput"
          rows="8"
          class="input mt-1 w-full font-mono text-xs"
          :placeholder="t('admin.accounts.grokBatchImport.credentialsPlaceholder')"
          :disabled="importing"
        ></textarea>
        <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.accounts.grokBatchImport.credentialsHint') }}
        </p>
        <div
          v-if="parsed.duplicates > 0 || parsed.invalidLines.length > 0"
          class="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
        >
          <span v-if="parsed.duplicates > 0">
            {{ t('admin.accounts.grokBatchImport.duplicateHint', { count: parsed.duplicates }) }}
          </span>
          <span v-if="parsed.duplicates > 0 && parsed.invalidLines.length > 0"> · </span>
          <span v-if="parsed.invalidLines.length > 0">
            {{ t('admin.accounts.grokBatchImport.invalidHint', { lines: parsed.invalidLines.join(', ') }) }}
          </span>
        </div>
      </div>

      <!-- 导入参数 -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div class="sm:col-span-3">
          <GroupSelector v-model="groupIds" :groups="grokGroups" platform="grok" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.concurrency') }}</label>
          <input v-model.number="concurrency" type="number" min="1" class="input w-full" :disabled="importing" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.priority') }}</label>
          <input v-model.number="priority" type="number" min="1" class="input w-full" :disabled="importing" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.grokBatchImport.modelsLabel') }}</label>
          <div class="input flex w-full items-center bg-gray-50 text-xs text-gray-600 dark:bg-dark-800 dark:text-dark-300">
            {{ MODEL_MAPPING_KEYS.join(' / ') }}
          </div>
        </div>
      </div>

      <!-- 进度与结果 -->
      <div v-if="importing || results.length > 0" class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-center justify-between text-sm">
          <span class="font-medium text-gray-900 dark:text-white">
            {{ importing ? t('admin.accounts.grokBatchImport.importing') : t('admin.accounts.grokBatchImport.done') }}
          </span>
          <span class="text-gray-500 dark:text-dark-400">
            {{ t('admin.accounts.grokBatchImport.progress', { done: results.length, total: parsed.items.length }) }}
          </span>
        </div>
        <div class="h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
          <div
            class="h-full rounded-full bg-primary-500 transition-all"
            :style="{ width: `${progressPercent}%` }"
          ></div>
        </div>
        <div class="flex flex-wrap gap-3 text-sm">
          <span class="text-emerald-600 dark:text-emerald-400">
            {{ t('admin.accounts.grokBatchImport.successCount', { count: successCount }) }}
          </span>
          <span class="text-red-600 dark:text-red-400">
            {{ t('admin.accounts.grokBatchImport.failedCount', { count: failureCount }) }}
          </span>
        </div>
        <div
          v-if="failureCount > 0"
          class="max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
        >
          <div v-for="item in failures" :key="item.line" class="whitespace-pre-wrap text-red-600 dark:text-red-400">
            #{{ item.line }} {{ item.name || '-' }} — {{ item.error }}
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" :disabled="importing" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          type="button"
          :disabled="importing || parsed.items.length === 0"
          @click="handleImport"
        >
          {{ importing ? t('admin.accounts.grokBatchImport.importing') : t('admin.accounts.grokBatchImport.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { useGrokOAuth } from '@/composables/useGrokOAuth'
import { parseGrokCredentialLines, type GrokCredentialLine } from '@/utils/grokCredentialLines'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { AdminGroup } from '@/types'

interface Props {
  show: boolean
  groups: AdminGroup[]
}

interface Emits {
  (e: 'close'): void
  (e: 'imported'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()
const grokOAuth = useGrokOAuth()

/** 上号模型白名单：按运营要求固定为 grok-4.6 / grok-4.7。 */
const MODEL_MAPPING_KEYS = ['grok-4.6', 'grok-4.7']
/** 每个账号的 SSO 兑换是同步请求，前端并发与后端批量导入保持一致（3）。 */
const IMPORT_CONCURRENCY = 3

const accountTypes = computed(() => [
  { id: 'grok-heavy', label: t('admin.accounts.grokBatchImport.typeGrokHeavy') }
])
const activeType = ref('grok-heavy')

const rawInput = ref('')
const concurrency = ref(100)
const priority = ref(1)
const groupIds = ref<number[]>([])
const importing = ref(false)

interface ImportResultRow {
  line: number
  name: string
  error: string
}

const results = ref<ImportResultRow[]>([])

const grokGroups = computed(() => props.groups.filter((group) => group.platform === 'grok'))
const parsed = computed(() => parseGrokCredentialLines(rawInput.value))
const successCount = computed(() => results.value.filter((item) => !item.error).length)
const failures = computed(() => results.value.filter((item) => item.error))
const failureCount = computed(() => failures.value.length)
const progressPercent = computed(() => {
  const total = parsed.value.items.length
  if (total === 0) return 0
  return Math.min(100, Math.round((results.value.length / total) * 100))
})

// grok 分组默认选中第一个，用户可改。
watch(
  [() => props.show, grokGroups],
  ([visible, groups]) => {
    if (!visible || groupIds.value.length > 0) return
    if (groups.length > 0) groupIds.value = [groups[0].id]
  },
  { immediate: true }
)

watch(
  () => props.show,
  (visible) => {
    if (visible) return
    importing.value = false
    results.value = []
    rawInput.value = ''
  }
)

function modelMapping(): Record<string, string> {
  return Object.fromEntries(MODEL_MAPPING_KEYS.map((model) => [model, model]))
}

/** 账号名固定为邮箱；同一批次内重名时追加序号以保证可区分。 */
function accountNameFor(item: GrokCredentialLine, email: string, used: Set<string>): string {
  const base = email.trim() || item.email.trim() || t('admin.accounts.grokBatchImport.fallbackName')
  let name = base
  let suffix = 2
  while (used.has(name)) {
    name = `${base} #${suffix++}`
  }
  used.add(name)
  return name
}

async function importOne(item: GrokCredentialLine, usedNames: Set<string>): Promise<ImportResultRow> {
  const tokenInfo = await grokOAuth.validateSSOToken(item.ssoToken)
  if (!tokenInfo) {
    // useGrokOAuth 已把可翻译的错误写进 error.value。
    throw new Error(grokOAuth.error.value || t('admin.accounts.grokBatchImport.ssoFailed'))
  }
  const email = String(tokenInfo.email || item.email || '').trim()
  const credentials = grokOAuth.buildCredentials(tokenInfo)
  credentials.model_mapping = modelMapping()
  const name = accountNameFor(item, email, usedNames)
  await adminAPI.accounts.create({
    name,
    platform: 'grok',
    type: 'oauth',
    credentials,
    extra: grokOAuth.buildExtraInfo(tokenInfo),
    concurrency: concurrency.value,
    priority: priority.value,
    group_ids: [...groupIds.value]
  })
  return { line: item.line, name, error: '' }
}

const handleImport = async () => {
  const items = parsed.value.items
  if (items.length === 0 || importing.value) return
  if (groupIds.value.length === 0) {
    appStore.showError(t('admin.accounts.grokBatchImport.groupRequired'))
    return
  }

  importing.value = true
  results.value = []
  const usedNames = new Set<string>()
  const queue = [...items]

  const worker = async () => {
    for (;;) {
      const item = queue.shift()
      if (!item) return
      try {
        results.value.push(await importOne(item, usedNames))
      } catch (err) {
        results.value.push({
          line: item.line,
          name: item.email,
          error: extractApiErrorMessage(err, t('admin.accounts.grokBatchImport.failed'))
        })
      }
    }
  }

  await Promise.all(Array.from({ length: Math.min(IMPORT_CONCURRENCY, items.length) }, worker))
  importing.value = false
  // 结果按行号回显，避免并发完成顺序打乱阅读。
  results.value = [...results.value].sort((a, b) => a.line - b.line)

  if (failureCount.value === 0) {
    appStore.showSuccess(t('admin.accounts.grokBatchImport.allSucceeded', { count: successCount.value }))
  } else if (successCount.value > 0) {
    appStore.showWarning(
      t('admin.accounts.grokBatchImport.partialSuccess', {
        success: successCount.value,
        failed: failureCount.value
      })
    )
  } else {
    appStore.showError(t('admin.accounts.grokBatchImport.allFailed'))
  }
  if (successCount.value > 0) {
    emit('imported')
  }
}

const handleClose = () => {
  if (importing.value) return
  emit('close')
}
</script>
