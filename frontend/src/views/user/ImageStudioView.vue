<template>
  <AppLayout>
    <section class="image-studio" :aria-label="t('imageStudio.title')">
      <div ref="historyEl" class="studio-history" :aria-busy="studio.historyLoading" @scroll.passive="updateHistoryPosition">
        <div ref="historyContentEl">
        <p v-if="studio.historyUnavailable" class="studio-notice" role="status">{{ t('imageStudio.historyUnavailable') }}<button @click="studio.loadHistory()">{{ t('imageStudio.retry') }}</button></p>
        <button v-if="studio.hasMore" class="btn btn-secondary mx-auto mb-6 block" :disabled="studio.loadingMore" @click="loadOlderHistory">{{ t('imageStudio.loadMore') }}</button>
        <div v-if="!studio.creations.length && !studio.historyLoading" class="studio-empty">
          <div class="empty-art" aria-hidden="true"><div class="art-sheet art-back" /><div class="art-sheet art-front"><svg viewBox="0 0 100 100" fill="none"><circle cx="68" cy="30" r="10" fill="currentColor" opacity=".5"/><path d="M10 77 38 41 61 67 75 50 94 77" stroke="currentColor" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/></svg></div><span class="art-spark">✦</span></div>
          <h2>{{ t('imageStudio.emptyTitle') }}</h2><p>{{ t('imageStudio.emptyDescription') }}</p>
          <div class="prompt-suggestions"><button v-for="index in 3" :key="index" @click="useSuggestion(index)"><Icon name="lightbulb" size="sm" />{{ t(`imageStudio.suggestion${index}`) }}<Icon name="arrowUp" size="sm" /></button></div>
        </div>
        <article v-for="creation in chronologicalCreations" :key="creation.id" class="creation" :data-status="creation.status">
          <div class="creation-description">
            <StudioReferencePicker v-if="creation.references.length" :references="creation.references" read-only />
            <div class="creation-caption"><p>{{ creation.prompt }}</p><div class="creation-meta"><span>{{ creation.model }}</span><span>{{ creation.ratio === 'auto' ? t('imageStudio.autoRatio') : creation.ratio }}</span><span v-if="creation.ratio !== 'auto'" :title="creation.size?.replace('x', '×')">{{ creation.model.startsWith('gpt-image-2') ? creation.resolution : t('imageStudio.standard') }}</span><span>{{ creation.keyName }}</span><span v-if="creation.references.length">{{ t('imageStudio.referenceCount', { count: creation.references.length }) }}</span><time :datetime="new Date(creation.createdAt).toISOString()">{{ formatTime(creation.createdAt) }}</time></div></div>
          </div>
          <div v-if="creation.status === 'generating'" class="creation-grid" :style="{ '--image-count': creation.count }" role="status">
            <StudioGenerationPlaceholder v-for="index in creation.count" :key="index" :ratio="creation.ratio" :index="index - 1" />
          </div>
          <div v-else-if="creation.status === 'failed'" class="creation-error" role="alert"><Icon name="exclamationTriangle" /><div><strong>{{ t('imageStudio.failed') }}</strong><p>{{ creationError(creation) }}</p><RouterLink v-if="creation.uncertain" to="/usage">{{ t('nav.usage') }} →</RouterLink></div></div>
          <div v-else class="creation-grid" :style="{ '--image-count': creation.images.length }">
            <div v-for="(picture, index) in creation.images" :key="index" class="picture-tile" :style="{ aspectRatio: creation.ratio.replace(':', '/') }">
              <button class="picture-open" :aria-label="t('imageStudio.preview')" @click="openPreview(picture, creation, index)">
                <span v-if="brokenImages.has(picture.url)" class="broken-image"><Icon name="exclamationTriangle" />{{ t('imageStudio.imageUnavailable') }}</span>
                <img v-else :src="picture.url" :alt="creation.prompt" loading="lazy" referrerpolicy="no-referrer" @error="refreshImage(picture)" />
              </button>
              <div class="picture-tools">
                <button type="button" class="picture-reference" :disabled="referenceActionDisabled(picture)" @click="useAsReference(picture, creation.id, index)"><Icon :name="addingReference === imageIdentity(picture) ? 'refresh' : 'photo'" :class="{ spinning: addingReference === imageIdentity(picture) }" size="sm" /><span>{{ referenceActionLabel(picture) }}</span></button>
                <button class="picture-download" :aria-label="t('imageStudio.download')" :disabled="downloading === imageIdentity(picture)" @click="download(picture, creation.id, index)"><Icon name="download" size="sm" /></button>
              </div>
            </div>
          </div>
          <p v-if="creation.status === 'completed' && creation.images.length !== creation.count" class="partial-notice">{{ t('imageStudio.partial', { actual: creation.images.length, requested: creation.count }) }}</p>
          <div class="creation-actions"><button v-if="creation.taskId && creation.uncertain" @click="studio.resume(creation.id)"><Icon name="refresh" size="sm" />{{ t('imageStudio.resumeTask') }}</button><button :disabled="editing || !!addingReference" @click="editCreation(creation)"><Icon name="edit" size="sm" />{{ t('imageStudio.edit') }}</button><button :disabled="loading || editing || !!addingReference" @click="regenerate(creation)"><Icon name="refresh" size="sm" />{{ t('imageStudio.regenerate') }}</button><button v-if="creation.status !== 'generating'" class="delete-creation" :aria-label="t('imageStudio.delete')" @click="deleteTarget = creation"><Icon name="trash" size="sm" /></button></div>
        </article>
        </div>
      </div>

      <div class="composer-dock">
        <div v-if="loadError" class="studio-notice error" role="alert">{{ loadError }}<button @click="loadAccess">{{ t('imageStudio.retry') }}</button></div>
        <div v-else-if="!loading && !storageAvailable" class="studio-notice" role="status">{{ t('imageStudio.storageUnavailable') }}<button @click="loadAccess">{{ t('imageStudio.retry') }}</button></div>
        <form class="studio-composer" @submit.prevent="submit">
          <div class="composer-input">
            <StudioReferencePicker :references="references" @add="fileInput?.click()" @remove="removeReference" />
            <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" multiple class="sr-only" tabindex="-1" @change="addReferences" />
            <TextArea ref="promptInput" v-model="prompt" class="composer-prompt" :aria-label="t('imageStudio.prompt')" :placeholder="t('imageStudio.placeholder')" :maxlength="32000" :rows="3" @keydown="promptKeydown" />
          </div>
          <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
          <div class="composer-toolbar">
            <div class="composer-controls">
              <Select class="composer-select key-select" :model-value="selectedKeyId || null" :options="keyOptions" :aria-label="t('imageStudio.key')" :disabled="loading" :searchable="false" @update:model-value="selectKey">
                <template #selected><span class="key-option"><Icon name="key" size="sm" /><span>{{ selectedKey ? `${selectedKey.name} · ${selectedKey.group?.name}` : loading ? t('imageStudio.loading') : t('imageStudio.noKey') }}</span></span></template>
                <template #option="{ option, selected }"><span class="key-option" :class="{ 'create-key-option': option.value === 'create' }"><Icon :name="option.value === 'create' ? 'plus' : 'key'" size="sm" /><span>{{ option.label }}</span><Icon v-if="selected" name="check" size="sm" /></span></template>
              </Select>
              <Select v-model="model" class="composer-select model-select" :options="modelOptions" :aria-label="t('imageStudio.model')" :disabled="loading || !models.length" :searchable="false" :placeholder="loading ? '…' : t('imageStudio.noModel')">
                <template #selected="{ option }"><span class="key-option"><Icon name="cube" size="sm" /><span>{{ option?.label || (loading ? '…' : t('imageStudio.noModel')) }}</span></span></template>
              </Select>
              <StudioImageSettings v-model:ratio="ratio" v-model:resolution="resolution" v-model:count="count" v-model:size="customSize" class="composer-select" :model="model" @validity="sizeValid = $event" />
            </div>
            <div class="composer-submit"><div class="price-info"><strong v-if="price !== null">{{ t('imageStudio.priceEach', { price: formatPrice(price) }) }}</strong><template v-else><strong>{{ t('imageStudio.billing') }}</strong><small>{{ t('imageStudio.shortcut') }}</small></template></div><button type="submit" class="generate-button" :disabled="!canSubmit" :aria-label="t('imageStudio.generate')"><Icon name="arrowUp" /></button></div>
          </div>
        </form>
      </div>
    </section>

    <ConfirmDialog :show="!!deleteTarget" :title="t('imageStudio.deleteTitle')" :message="t('imageStudio.deleteMessage')" :confirm-text="deleting ? t('imageStudio.deleting') : t('common.delete')" :cancel-text="t('common.cancel')" danger :loading="deleting" @confirm="removeCreation" @cancel="deleteTarget = null">
      <p v-if="deleteTarget" class="line-clamp-3 break-words rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-800 dark:text-gray-400">{{ deleteTarget.prompt }}</p>
      <p v-if="deleteError" class="text-sm text-red-500" role="alert">{{ deleteError }}</p>
    </ConfirmDialog>
    <BaseDialog :show="showCreate" :title="t('imageStudio.createKey')" width="normal" :close-on-escape="!creating" :show-close-button="!creating" @close="showCreate = false">
      <form class="create-key-form" @submit.prevent="createKey">
        <p>{{ t('imageStudio.createDescription') }}</p>
        <Input id="studio-key-name" v-model="keyName" :label="t('imageStudio.keyName')" :aria-label="t('imageStudio.keyName')" :maxlength="100" required />
        <div class="create-group-field"><label for="studio-image-group">{{ t('imageStudio.group') }}</label><Select id="studio-image-group" v-model="createGroupId" :options="groupOptions" :aria-label="t('imageStudio.group')" :placeholder="t('imageStudio.selectGroup')" :disabled="!groups.length || creating" :searchable="false" /></div>
        <p v-if="!groups.length" class="studio-notice">{{ t('imageStudio.noGroups') }}</p>
        <p v-if="createError" class="form-error" role="alert">{{ createError }}</p>
        <div class="dialog-actions"><button type="button" class="btn btn-secondary" :disabled="creating" @click="showCreate = false">{{ t('imageStudio.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="creating || !createGroupId || !keyName.trim()">{{ creating ? t('imageStudio.creating') : t('imageStudio.create') }}</button></div>
      </form>
    </BaseDialog>
    <StudioImagePreview :creation="preview?.creation || null" :index="preview?.index || 0" :downloading="!!preview && downloading === imageIdentity(preview.picture)" :reference-disabled="!!preview && referenceActionDisabled(preview.picture)" :reference-label="preview ? referenceActionLabel(preview.picture) : ''" @close="preview = null" @select="selectPreview" @error="refreshImage" @download="preview && download(preview.picture, preview.creation.id, preview.index)" @reference="preview && useAsReference(preview.picture, preview.creation.id, preview.index)" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { keysAPI } from '@/api/keys'
import { userGroupsAPI } from '@/api/groups'
import { buildImageRequest, canGenerateImages, getImageGenerationGroups, getImageStudioStatus, getImageRatios, getImageResolutions, getStudioFile, isValidImageSize, type ImageGenerationGroup, type ImageRatio, type ImageResolution, type StudioImage } from '@/api/imageStudio'
import { useImageStudioStore, type StudioCreation } from '@/stores/imageStudio'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import TextArea from '@/components/common/TextArea.vue'
import Icon from '@/components/icons/Icon.vue'
import StudioReferencePicker from '@/components/image/StudioReferencePicker.vue'
import StudioGenerationPlaceholder from '@/components/image/StudioGenerationPlaceholder.vue'
import StudioImageSettings from '@/components/image/StudioImageSettings.vue'
import StudioImagePreview from '@/components/image/StudioImagePreview.vue'
import { isImageUrlFresh } from '@/utils/imageUrlCache'
import { fetchImage } from '@/utils/fetchImage'

const { t, locale } = useI18n()
const studio = useImageStudioStore()
// The API and store keep newest-first pagination; the timeline reads oldest first.
const chronologicalCreations = computed(() => [...studio.creations].reverse())
const app = useAppStore()
const keys = ref<ApiKey[]>([])
const storageAvailable = ref(false)
const groups = ref<ImageGenerationGroup[]>([])
const rates = ref<Record<number, number> | null>(null)
const selectedKeyId = ref(0)
const selectedKey = computed(() => keys.value.find(key => key.id === selectedKeyId.value))
const models = computed(() => groups.value.find(group => group.id === selectedKey.value?.group_id)?.image_models || [])
const keyOptions = computed<SelectOption[]>(() => [
  ...keys.value.map(key => ({ value: key.id, label: `${key.name} · ${key.group?.name}` })),
  { value: 'create', label: t('imageStudio.createKey'), disabled: !!loadError.value },
])
const model = ref('')
const prompt = ref('')
const ratio = ref<ImageRatio>('1:1')
const resolution = ref<ImageResolution>('1K')
const count = ref(1)
const customSize = ref<string>()
const sizeValid = ref(true)
const availableRatios = computed(() => getImageRatios(model.value))
const availableResolutions = computed(() => getImageResolutions(model.value, ratio.value))
const modelOptions = computed(() => models.value.map(value => ({ value, label: value })))
const groupOptions = computed(() => groups.value.map(group => ({ value: group.id, label: group.name })))
const references = ref<{ file: File; url: string; sourceId?: string }[]>([])
const promptInput = ref<InstanceType<typeof TextArea>>()
const fileInput = ref<HTMLInputElement>()
const historyEl = ref<HTMLElement>()
const historyContentEl = ref<HTMLElement>()
let historyPositioned = false
let followingHistoryBottom = true
let historyResizeObserver: ResizeObserver | undefined
function updateHistoryPosition() {
  const history = historyEl.value
  if (history) followingHistoryBottom = history.scrollHeight - history.scrollTop - history.clientHeight < 48
}
function scrollHistoryToBottom() {
  const history = historyEl.value
  if (!history || disposed) return
  followingHistoryBottom = true
  history.scrollTop = history.scrollHeight
}
async function positionInitialHistory() {
  if (historyPositioned || studio.historyLoading) return
  await nextTick()
  if (disposed || !historyEl.value) return
  historyPositioned = true
  scrollHistoryToBottom()
}
async function loadOlderHistory() {
  const history = historyEl.value
  if (!history || studio.loadingMore) return
  const top = history.scrollTop
  const height = history.scrollHeight
  const viewportTop = history.getBoundingClientRect().top
  const anchor = Array.from(history.querySelectorAll<HTMLElement>('.creation')).find(item => item.getBoundingClientRect().bottom > viewportTop)
  const anchorTop = anchor?.getBoundingClientRect().top ?? 0
  followingHistoryBottom = false
  await studio.loadMore()
  await nextTick()
  if (disposed || history !== historyEl.value) return
  // Preserve the visible record, including when the browser already anchored it.
  if (anchor?.isConnected) history.scrollTop += anchor.getBoundingClientRect().top - anchorTop
  else history.scrollTop = top + history.scrollHeight - height
  updateHistoryPosition()
}
watch(() => studio.historyLoading, loading => { if (!loading) void positionInitialHistory() }, { flush: 'post' })
const loading = ref(true)
const loadError = ref('')
const formError = ref('')
const showCreate = ref(false)
const creating = ref(false)
const keyName = ref(t('imageStudio.defaultKeyName'))
const createGroupId = ref(0)
const createError = ref('')
const brokenImages = ref(new Set<string>())
const downloading = ref('')
const editing = ref(false)
const addingReference = ref('')
let referenceController: AbortController | undefined
const deleteTarget = ref<StudioCreation | null>(null)
const deleting = ref(false)
const deleteError = ref('')
watch(deleteTarget, () => { deleteError.value = '' })
const preview = ref<{ picture: StudioImage & { id?: string }; creation: StudioCreation; index: number } | null>(null)
let disposed = false
const canSubmit = computed(() => sizeValid.value && !!prompt.value.trim() && !!selectedKey.value && models.value.includes(model.value) && availableRatios.value.includes(ratio.value) && availableResolutions.value.includes(resolution.value) && storageAvailable.value && !loading.value && !editing.value && !addingReference.value && !studio.historyLoading && !loadError.value)
const price = computed(() => {
  if (ratio.value === 'auto') return null
  const group = selectedKey.value?.group
  if (!group || rates.value === null || group.peak_rate_enabled || !availableResolutions.value.includes(resolution.value)) return null
  let size: string
  try { size = buildImageRequest(model.value, '', ratio.value, count.value, resolution.value, customSize.value).size || '' }
  catch { return null }
  const edge = Math.max(...size.split('x').map(Number))
  const base = edge <= 1024 ? group.image_price_1k : edge <= 2048 ? group.image_price_2k : group.image_price_4k
  const multiplier = group.image_rate_independent ? group.image_rate_multiplier : rates.value[group.id] ?? group.rate_multiplier
  return typeof base === 'number' && Number.isFinite(base) && base >= 0 && Number.isFinite(multiplier) ? base * Math.max(0, multiplier) : null
})

const formatPrice = (value: number) => value.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
const formatTime = (time: number) => new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(time)
const errorMessage = (error: unknown, fallback: string) => error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' ? error.message : fallback

function creationError(creation: StudioCreation) {
  if (creation.uncertain) return t(creation.taskId ? 'imageStudio.pollInterrupted' : 'imageStudio.uncertain')
  const messages: Record<string, string> = {
    empty_result: 'emptyResult', image_result_not_stored: 'resultNotStored',
    image_task_expired: 'taskExpired', image_task_key_unavailable: 'taskKeyUnavailable',
  }
  return messages[creation.error || ''] ? t(`imageStudio.${messages[creation.error!]}`) : creation.error
}

async function loadAccess() {
  if (!loading.value) loading.value = true
  loadError.value = ''
  try {
    const [availableGroups, allKeys, status] = await Promise.all([getImageGenerationGroups(), loadAllKeys(), getImageStudioStatus()])
    if (disposed) return
    storageAvailable.value = status.available
    groups.value = availableGroups
    // Never fall back to the Key's embedded group: its permission flag alone
    // does not establish that an image model is actually configured.
    keys.value = allKeys.map(key => ({ ...key, group: availableGroups.find(group => group.id === key.group_id) })).filter(canGenerateImages)
    createGroupId.value = groups.value[0]?.id || 0
    if (!keys.value.some(key => key.id === selectedKeyId.value)) selectedKeyId.value = keys.value[0]?.id || 0
  } catch (error) {
    if (!disposed) loadError.value = errorMessage(error, t('imageStudio.loadFailed'))
  } finally { if (!disposed) loading.value = false }
}

async function loadAllKeys() {
  const result: ApiKey[] = []
  for (let page = 1; ; page++) {
    const response = await keysAPI.list(page, 100, { status: 'active' })
    result.push(...response.items)
    if (page >= response.pages || !response.items.length) return result
  }
}

function selectKey(value: SelectOption['value']) {
  if (value === 'create') openCreate()
  else if (typeof value === 'number' && keys.value.some(key => key.id === value)) selectedKeyId.value = value
}
function openCreate() {
  if (loading.value || loadError.value) return
  createError.value = ''
  showCreate.value = true
}
watch(models, available => {
  if (!available.includes(model.value)) model.value = available.find(item => item === 'gpt-image-2') || available[0] || ''
})
watch(model, () => { customSize.value = undefined })
watch(availableRatios, available => {
  if (!available.includes(ratio.value)) ratio.value = '1:1'
})
watch(availableResolutions, available => {
  if (!available.includes(resolution.value)) resolution.value = available[0] || '1K'
})

async function createKey() {
  if (creating.value || !keyName.value.trim() || !groups.value.some(group => group.id === createGroupId.value)) return
  creating.value = true
  createError.value = ''
  try {
    const created = await keysAPI.create(keyName.value.trim(), createGroupId.value)
    const key = { ...created, group: groups.value.find(group => group.id === created.group_id) }
    if (!canGenerateImages(key)) throw new Error(t('imageStudio.noKeysDescription'))
    keys.value.unshift(key)
    selectedKeyId.value = key.id
    showCreate.value = false
  } catch (error) { createError.value = errorMessage(error, t('imageStudio.createFailed')) }
  finally { creating.value = false }
}

function addReferences(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  formError.value = ''
  if (references.value.length + files.length > 4 || files.some(file => !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 10 * 1024 * 1024 || !file.size)) {
    formError.value = t('imageStudio.invalidReference')
  } else { references.value.push(...files.map(file => ({ file, url: URL.createObjectURL(file) }))) }
  input.value = ''
}
function removeReference(index: number) {
  URL.revokeObjectURL(references.value[index].url)
  references.value.splice(index, 1)
}
function replaceReferences(files: File[]) {
  references.value.forEach(item => URL.revokeObjectURL(item.url))
  references.value = files.map(file => ({ file, url: URL.createObjectURL(file) }))
}
function useSuggestion(index: number) { prompt.value = t(`imageStudio.suggestion${index}`); promptInput.value?.focus() }
function promptKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && !event.isComposing) { event.preventDefault(); void submit() }
}
async function submit() {
  if (!canSubmit.value || !selectedKey.value) return
  formError.value = ''
  const key = selectedKey.value
  const extra: [string?] = customSize.value ? [customSize.value] : []
  const pending = studio.generate(key.key, prompt.value, model.value, ratio.value, count.value, resolution.value, references.value.map(item => item.file), key.id, key.name, ...extra)
  // The store has captured its own prompt and File array before its first await.
  // Clear immediately so a late response cannot erase the user's next draft.
  prompt.value = ''
  replaceReferences([])
  await nextTick()
  scrollHistoryToBottom()
  await pending
}
async function editCreation(creation: StudioCreation) {
  if (editing.value || addingReference.value) return false
  editing.value = true
  let files: File[]
  try {
    files = await Promise.all(creation.references.map(async reference => {
      if (reference instanceof File) return reference
      const asset = await getStudioFile(reference.id)
      const response = await fetchImage(asset.url, AbortSignal.timeout(60000))
      if (!response.ok) throw new Error('reference_unavailable')
      return new File([await response.blob()], reference.filename, { type: reference.content_type })
    }))
  } catch { formError.value = t('imageStudio.referenceUnavailable'); return false }
  finally { editing.value = false }
  if (disposed) return false
  formError.value = ''
  prompt.value = creation.prompt
  count.value = creation.count
  replaceReferences(files)
  if (keys.value.some(key => key.id === creation.keyId) && selectedKeyId.value !== creation.keyId) {
    selectedKeyId.value = creation.keyId
    await nextTick()
  }
  if (models.value.includes(creation.model)) model.value = creation.model
  else formError.value = t('imageStudio.modelUnavailable')
  ratio.value = availableRatios.value.includes(creation.ratio) ? creation.ratio : '1:1'
  if (ratio.value !== creation.ratio) formError.value = t('imageStudio.ratioUnavailable')
  await nextTick()
  resolution.value = availableResolutions.value.includes(creation.resolution) ? creation.resolution : availableResolutions.value[0] || '1K'
  customSize.value = model.value.startsWith('gpt-image-2') && creation.size && isValidImageSize(creation.size, ratio.value) ? creation.size : undefined
  promptInput.value?.focus()
  return true
}
async function regenerate(creation: StudioCreation) {
  if (loading.value) return
  if (await editCreation(creation) && model.value === creation.model && ratio.value === creation.ratio) await submit()
}
async function removeCreation() {
  const target = deleteTarget.value
  if (!target || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await studio.remove(target.id)
    if (preview.value?.creation.id === target.id) preview.value = null
    deleteTarget.value = null
  } catch (error) { deleteError.value = errorMessage(error, t('imageStudio.deleteFailed')) }
  finally { deleting.value = false }
}
const imageIdentity = (picture: StudioImage & { id?: string }) => picture.id || picture.url
const isReferenceAdded = (picture: StudioImage & { id?: string }) => references.value.some(reference => reference.sourceId === imageIdentity(picture))
function referenceActionDisabled(picture: StudioImage & { id?: string }) {
  return !!addingReference.value || editing.value || references.value.length >= 4 || isReferenceAdded(picture)
}
function referenceActionLabel(picture: StudioImage & { id?: string }) {
  if (addingReference.value === imageIdentity(picture)) return t('imageStudio.addingReference')
  return t(isReferenceAdded(picture) ? 'imageStudio.referenceAdded' : 'imageStudio.useAsReference')
}
async function useAsReference(picture: StudioImage & { id?: string }, creationId: string, index: number) {
  if (referenceActionDisabled(picture)) return
  const sourceId = imageIdentity(picture)
  const controller = new AbortController()
  referenceController = controller
  const timeout = window.setTimeout(() => controller.abort(), 60000)
  addingReference.value = sourceId
  try {
    // Renew the stored asset URL before fetching; never send API credentials to S3.
    const asset = picture.id ? await getStudioFile(picture.id) : null
    if (asset && asset.size > 10 * 1024 * 1024) throw new Error(t('imageStudio.invalidReference'))
    const response = await fetchImage(asset?.url || picture.url, controller.signal)
    if (!response.ok) throw new Error(t('imageStudio.referenceUnavailable'))
    const blob = await response.blob()
    const mime = blob.type || asset?.content_type || ''
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(mime) || !blob.size || blob.size > 10 * 1024 * 1024) throw new Error(t('imageStudio.invalidReference'))
    if (disposed) return
    // The user may have uploaded more references while the image was loading.
    if (references.value.length >= 4) throw new Error(t('imageStudio.invalidReference'))
    const extension = mime === 'image/webp' ? 'webp' : mime === 'image/jpeg' ? 'jpg' : 'png'
    const file = new File([blob], `image-${creationId}-${index + 1}.${extension}`, { type: mime })
    references.value.push({ file, url: URL.createObjectURL(file), sourceId })
    formError.value = ''
    preview.value = null
    await nextTick()
    promptInput.value?.focus()
  } catch (error) {
    if (!disposed) app.showError(error instanceof TypeError ? t('imageStudio.referenceUnavailable') : errorMessage(error, t('imageStudio.referenceUnavailable')))
  } finally {
    window.clearTimeout(timeout)
    addingReference.value = ''
    if (referenceController === controller) referenceController = undefined
  }
}
const imageRefreshes = new Map<string, Promise<string>>()
async function freshImage(picture: StudioImage & { id?: string }, force = false) {
  if (!picture.id || (!force && isImageUrlFresh(picture.url))) return picture.url
  const id = picture.id
  let request = imageRefreshes.get(id)
  if (!request) {
    request = getStudioFile(id).then(asset => asset.url).finally(() => imageRefreshes.delete(id))
    imageRefreshes.set(id, request)
  }
  const url = await request
  if (!disposed) picture.url = url
  return url
}
const refreshedImages = new Map<string, number>()
async function refreshImage(picture: StudioImage & { id?: string }, force = false) {
  const key = picture.id || picture.url
  if (imageRefreshes.has(key)) return
  if (!picture.id || (!force && Date.now() - (refreshedImages.get(key) || 0) < 30000)) { brokenImages.value.add(picture.url); return }
  refreshedImages.set(key, Date.now())
  try { const previous = picture.url; await freshImage(picture, true); if (previous === picture.url) brokenImages.value.add(picture.url) }
  catch { brokenImages.value.add(picture.url) }
}
async function openPreview(picture: StudioImage & { id?: string }, creation: StudioCreation, index: number) {
  preview.value = { picture, creation, index }
  try { await freshImage(picture) }
  catch { app.showError(t('imageStudio.imageUnavailable')) }
}
function selectPreview(index: number) {
  const creation = preview.value?.creation
  const picture = creation?.images[index]
  if (creation && picture) {
    preview.value = { picture, creation, index }
    void freshImage(picture).catch(() => { /* The image error handler retries unavailable links. */ })
  }
}
async function download(picture: StudioImage & { id?: string }, id: string, index: number) {
  downloading.value = imageIdentity(picture)
  try {
    await freshImage(picture)
    const response = await fetchImage(picture.url, AbortSignal.timeout(60000))
    if (!response.ok) throw new Error('Download failed')
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const extension = blob.type.includes('webp') ? 'webp' : blob.type.includes('jpeg') ? 'jpg' : 'png'
    const link = document.createElement('a')
    link.href = url; link.download = `image-${id}-${index + 1}.${extension}`
    document.body.appendChild(link); link.click(); link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  } catch { app.showError(t('imageStudio.downloadFailed')) }
  finally { downloading.value = '' }
}

onMounted(() => {
  void positionInitialHistory()
  if (typeof ResizeObserver !== 'undefined') {
    historyResizeObserver = new ResizeObserver(() => {
      if (historyPositioned && followingHistoryBottom && !studio.loadingMore) scrollHistoryToBottom()
    })
    if (historyEl.value) historyResizeObserver.observe(historyEl.value)
    if (historyContentEl.value) historyResizeObserver.observe(historyContentEl.value)
  }
  void loadAccess()
  void userGroupsAPI.getUserGroupRates().then(value => { rates.value = value }).catch(() => { rates.value = null })
})
onBeforeUnmount(() => {
  disposed = true
  historyResizeObserver?.disconnect()
  referenceController?.abort()
  references.value.forEach(item => URL.revokeObjectURL(item.url))
})
</script>

<style scoped>
.image-studio { --studio-bg: var(--fv-page); --studio-surface: #fff; --studio-ink: #222530; --studio-muted: #858a97; --studio-line: #e9ebf0; --studio-accent: #8070ed; display: flex; flex-direction: column; height: calc(100dvh - 64px); min-height: 560px; margin: -32px; color: var(--studio-ink); background: var(--studio-bg); overflow: hidden; }
.dark .image-studio { --studio-surface: #222329; --studio-ink: #f2f1f7; --studio-muted: #9b9aa8; --studio-line: #303138; --studio-accent: #b1a5ff; }
.studio-history { flex: 1; overflow-y: auto; overscroll-behavior: contain; padding: 28px 32px 40px; scrollbar-width: thin; }
.studio-empty { min-height: 300px; display: flex; align-items: center; flex-direction: column; justify-content: center; padding: 32px 12px; text-align: center; }
.empty-art { width: 110px; height: 90px; position: relative; margin-bottom: 26px; }
.art-sheet { position: absolute; width: 70px; height: 76px; border: 1px solid color-mix(in srgb, var(--studio-accent) 20%, var(--studio-line)); border-radius: 12px; }
.art-back { transform: rotate(-17deg); left: 10px; top: 6px; background: color-mix(in srgb, var(--studio-accent) 12%, var(--studio-surface)); }
.art-front { left: 34px; top: 10px; transform: rotate(9deg); background: var(--studio-surface); color: color-mix(in srgb, var(--studio-accent) 60%, var(--studio-surface)); padding: 10px; box-shadow: 0 12px 22px #30206308; }
.art-spark { position: absolute; top: -7px; right: -3px; color: var(--studio-accent); font-size: 27px; }
.studio-empty h2 { font-size: 24px; font-weight: 500; letter-spacing: -.7px; }
.studio-empty > p { font-size: 13px; color: var(--studio-muted); margin: 12px 0 28px; }
.prompt-suggestions { display: flex; flex-direction: column; gap: 9px; max-width: 510px; width: 100%; }
.prompt-suggestions button { display: flex; align-items: center; gap: 10px; padding: 11px 14px; text-align: left; font-size: 12px; background: var(--studio-surface); border: 1px solid var(--studio-line); border-radius: 10px; color: var(--studio-muted); transition: border-color .2s, transform .2s; }
.prompt-suggestions button:hover { border-color: var(--studio-accent); transform: translateY(-1px); }
.prompt-suggestions button svg { flex-shrink: 0; color: var(--studio-accent); }
.prompt-suggestions button svg:last-child { margin-left: auto; width: 13px; transform: rotate(45deg); }
.creation { margin-bottom: 32px; padding-bottom: 28px; border-bottom: 1px solid var(--studio-line); }
.creation:last-child { margin-bottom: 0; border-bottom: 0; }
.creation-description { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 16px; }
.creation-caption { min-width: 0; padding-top: 1px; }
.creation-caption > p { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 14px; line-height: 1.7; max-height: 100px; overflow: auto; }
.creation-meta { display: flex; flex-wrap: wrap; gap: 7px 0; align-items: center; color: var(--studio-muted); font-size: 11px; margin-top: 6px; }
.creation-meta > * + *::before { content: '·'; margin: 0 9px; opacity: .6; }
.creation-grid { display: grid; grid-template-columns: repeat(var(--image-count), minmax(0, 1fr)); gap: 8px; max-width: 100%; }
.creation-grid:has(> :only-child) { max-width: 460px; }
.picture-tile { overflow: hidden; background: var(--studio-surface); border-radius: 10px; position: relative; }
.picture-open { width: 100%; height: 100%; display: block; }
.picture-open img { width: 100%; height: 100%; object-fit: contain; }
.picture-tools { position: absolute; bottom: 10px; left: 10px; right: 10px; display: flex; justify-content: space-between; align-items: center; gap: 6px; opacity: 0; pointer-events: none; transition: opacity .2s; }
.picture-tools button { display: inline-flex; align-items: center; justify-content: center; gap: 5px; padding: 8px; background: #fffE; border: 1px solid #fff8; backdrop-filter: blur(10px); border-radius: 9px; color: #35323f; font-size: 11px; white-space: nowrap; }
.picture-reference { min-width: 0; }
.picture-reference span { overflow: hidden; text-overflow: ellipsis; }
.picture-tools svg, .picture-download { flex-shrink: 0; }
.picture-tools button:focus-visible { outline: 2px solid var(--studio-accent); outline-offset: 2px; }
.picture-tile:hover .picture-tools, .picture-tile:focus-within .picture-tools { opacity: 1; pointer-events: auto; }
@media (hover: none), (pointer: coarse) { .picture-tools { opacity: 1; pointer-events: auto; } }
.broken-image { color: var(--studio-muted); display: flex; flex-direction: column; align-items: center; gap: 12px; font-size: 12px; padding: 20px; }
.creation-actions { display: flex; gap: 8px; margin-top: 13px; }
.creation-actions button { display: flex; align-items: center; gap: 6px; padding: 8px 12px; background: color-mix(in srgb, var(--studio-ink) 4%, var(--studio-bg)); border-radius: 8px; font-size: 12px; }
.creation-actions button:hover { background: color-mix(in srgb, var(--studio-accent) 10%, var(--studio-bg)); }
.creation-actions .delete-creation { padding: 8px; color: var(--studio-muted); }
.creation-error { display: flex; gap: 12px; padding: 24px; border: 1px solid #f2b5a744; background: #eb806b08; border-radius: 12px; font-size: 13px; }
.creation-error svg { color: #ca806e; flex-shrink: 0; }
.creation-error p { color: var(--studio-muted); margin-top: 7px; overflow-wrap: anywhere; }
.creation-error a { display: inline-block; margin-top: 12px; color: var(--studio-accent); }
.partial-notice { font-size: 12px; color: var(--studio-muted); margin-top: 10px; }
.composer-dock { flex-shrink: 0; padding: 12px 32px max(16px, env(safe-area-inset-bottom)); background: var(--studio-bg); }
.studio-composer { padding: 12px; border: 1px solid var(--studio-line); border-radius: 20px; background: var(--studio-surface); box-shadow: 0 4px 24px #30304004; }
.composer-input { display: flex; align-items: flex-start; gap: 10px; min-height: 94px; }
.composer-prompt { flex: 1; min-width: 0; }
.studio-composer .composer-prompt :deep(textarea) { resize: none; min-width: 0; min-height: 82px; max-height: 180px; padding: 6px 0; border: 0; color: var(--studio-ink); font-size: 14px; line-height: 1.8; }
/* The deployed site theme uses !important on form surfaces and focus rings. */
.studio-composer .composer-prompt :deep(textarea),
.studio-composer .composer-prompt :deep(textarea:focus),
.studio-composer .composer-prompt :deep(textarea:focus-visible) { background: transparent !important; border-radius: 0 !important; outline: none !important; box-shadow: none !important; }
.composer-prompt :deep(textarea::placeholder) { color: var(--studio-muted); opacity: .8; }
.composer-toolbar { display: flex; justify-content: space-between; align-items: flex-end; gap: 12px; padding-top: 13px; }
.composer-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; min-width: 0; }
.composer-select { min-width: 72px; }
.composer-select :deep(.select-trigger) { min-height: 35px; padding: 7px 9px; font-size: 11px; }
.composer-select :deep(.select-icon svg) { width: 14px; height: 14px; }
.key-select { max-width: 260px; }
.model-select { max-width: 200px; }
.key-option { display: flex; align-items: center; gap: 7px; min-width: 0; }
.key-option > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.key-option > svg { flex-shrink: 0; }
.create-key-option { color: #8070ed; }
.composer-submit { display: flex; align-items: center; gap: 12px; flex-shrink: 0; padding-bottom: 1px; }
.price-info { text-align: right; display: flex; flex-direction: column; gap: 5px; }
.price-info strong { font-size: 12px; font-weight: 500; }
.price-info small { font-size: 9px; color: var(--studio-muted); }
.generate-button { width: 42px; height: 42px; display: grid; place-items: center; border-radius: 50%; background: var(--studio-accent); color: white; transition: transform .2s; }
.generate-button:hover:not(:disabled) { transform: translateY(-2px); }
.image-studio button:disabled { opacity: .4; cursor: not-allowed; }
.generate-button:disabled { background: color-mix(in srgb, var(--studio-ink) 20%, var(--studio-surface)); color: var(--studio-surface); opacity: 1; }
.studio-notice { padding: 10px 12px; font-size: 12px; background: color-mix(in srgb, #a395ea 8%, transparent); color: var(--studio-muted, #777); border-radius: 8px; line-height: 1.7; margin-bottom: 10px; }
.studio-notice button { color: var(--studio-accent, #8070ed); margin-left: 10px; text-decoration: underline; }
.form-error { font-size: 12px; color: #c36861; padding: 8px 0; overflow-wrap: anywhere; }
.create-key-form { display: flex; flex-direction: column; gap: 20px; }
.create-key-form > p { font-size: 14px; color: var(--fv-muted); }
.create-group-field { display: flex; flex-direction: column; gap: 8px; }
.create-group-field > label { font-size: 13px; }
.dialog-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; align-items: center; gap: 10px; margin-top: 10px; }
.spinning { animation: spin 2s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1280px) { .composer-toolbar { flex-wrap: wrap; } .composer-submit { margin-left: auto; } }
@media (min-width: 768px) and (max-width: 1023px) { .image-studio { margin: -24px; } }
@media (max-width: 767px) {
  .image-studio { margin: -16px; }
  .studio-history { padding: 18px 16px; }
  .studio-empty { padding: 24px 0; }
  .studio-empty h2 { font-size: 20px; }
  .studio-empty > p { line-height: 1.8; }
  .prompt-suggestions button { font-size: 11px; }
  .creation-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .creation-grid:has(> :only-child) { grid-template-columns: 1fr; }
  .composer-dock { padding: 8px 12px max(12px, env(safe-area-inset-bottom)); }
  .studio-composer { padding: 10px 10px 12px; border-radius: 14px; }
  .composer-input { gap: 10px; min-height: 94px; }
  .studio-composer .composer-prompt :deep(textarea) { font-size: 13px; min-height: 82px; max-height: 130px; }
  .composer-toolbar { gap: 10px; padding-top: 6px; }
  .composer-select :deep(.select-trigger) { min-height: 32px; padding: 6px 8px; }
  .key-select { max-width: 210px; }
  .model-select { max-width: 170px; }
  .composer-submit { width: 100%; justify-content: flex-end; }
  .generate-button { width: 36px; height: 36px; }
  .picture-download { opacity: 1; padding: 6px; }
}
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation: none !important; transition: none !important; scroll-behavior: auto !important; } }
</style>
