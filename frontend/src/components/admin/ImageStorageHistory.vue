<template>
  <div v-if="error || previousProfiles.length" class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700">
    <p v-if="error" class="text-sm text-red-500" role="alert">{{ error }}<button type="button" class="ml-3 underline" :disabled="migrating" @click="load">{{ t('common.retry') }}</button></p>
    <details v-if="previousProfiles.length">
      <summary class="cursor-pointer text-sm font-medium">{{ t('imageStudioStorage.historyTitle') }}</summary>
      <p class="my-3 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('imageStudioStorage.switchNote') }}</p>
      <div class="space-y-3">
        <div v-for="profile in previousProfiles" :key="profile.id" class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="min-w-0"><p class="break-all text-sm">{{ profile.config.bucket }} / {{ profile.config.prefix }}</p><p class="mt-1 break-all text-xs text-gray-500">{{ profile.config.endpoint || 'AWS S3' }} · {{ t('imageStudioStorage.files', { count: profile.file_count }) }}</p></div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="migrating || !settings.enabled || !settings.active_id" @click="migrationSource = profile">{{ t('imageStudioStorage.migrate') }}</button>
        </div>
      </div>
    </details>
    <div v-if="migrating" class="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm" role="status"><span>{{ t('imageStudioStorage.progress', { count: migrated, remaining }) }}</span><button type="button" class="btn btn-secondary btn-sm" @click="stopMigration = true">{{ t('imageStudioStorage.pause') }}</button></div>
    <ConfirmDialog :show="!!migrationSource" :title="t('imageStudioStorage.migrate')" :message="t('imageStudioStorage.migrationConfirm', { from: sourceName, to: targetName })" :confirm-text="t('imageStudioStorage.start')" @cancel="migrationSource = null" @confirm="migrate" />
    <TotpStepUpDialog :controller="stepUp" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpCancelled } from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'
import { imageStorageHistoryAPI, type ImageStorageProfile, type ImageStorageHistory } from '@/api/admin/imageStudio'

const props = defineProps<{ revision: number }>()
const { t } = useI18n()
const app = useAppStore()
const stepUp = useStepUp()
const settings = ref<ImageStorageHistory>({ active_id: 0, enabled: false, profiles: [] })
const error = ref('')
const migrating = ref(false)
const migrated = ref(0)
const remaining = ref(0)
const stopMigration = ref(false)
const migrationSource = ref<ImageStorageProfile | null>(null)
const controller = new AbortController()
const previousProfiles = computed(() => settings.value.profiles.filter(p => p.id !== settings.value.active_id && p.file_count > 0))
const profileName = (profile?: ImageStorageProfile | null) => profile ? `${profile.config.bucket} / ${profile.config.prefix}` : ''
const sourceName = computed(() => profileName(migrationSource.value))
const targetName = computed(() => profileName(settings.value.profiles.find(p => p.id === settings.value.active_id)))
const message = (err: unknown) => (err as { message?: string })?.message || t('imageStudioStorage.failed')
async function load() {
  error.value = ''
  try { settings.value = await imageStorageHistoryAPI.get(controller.signal) }
  catch (err) { if (!controller.signal.aborted) error.value = message(err) }
}
async function migrate() {
  const from = migrationSource.value?.id
  const to = settings.value.active_id
  if (!from || !to || migrating.value) return
  remaining.value = migrationSource.value!.file_count
  migrationSource.value = null
  migrating.value = true
  stopMigration.value = false
  migrated.value = 0
  error.value = ''
  try {
    do {
      const result = await stepUp.run(() => imageStorageHistoryAPI.migrate(from, to, controller.signal))
      migrated.value += result.moved
      remaining.value = result.remaining
    } while (remaining.value > 0 && !stopMigration.value)
    if (!remaining.value) app.showSuccess(t('imageStudioStorage.migrated'))
  } catch (err) { if (!isStepUpCancelled(err) && !controller.signal.aborted) error.value = message(err) }
  finally {
    migrating.value = false
    if (!controller.signal.aborted) {
      const failure = error.value
      await load()
      if (failure) error.value = failure
    }
  }
}
watch(() => props.revision, load, { immediate: true })
onBeforeUnmount(() => { stopMigration.value = true; controller.abort() })
</script>
