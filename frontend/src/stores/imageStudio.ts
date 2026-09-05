import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { useAuthStore } from './auth'
import { buildImageRequest, deleteStudioCreation, generateImages, getStudioHistory, getLegacyStudioTask, importStudioHistory, pollImageTask, type ImageRatio, type ImageResolution, type StudioAsset, type StudioCreationResponse, type StudioImage, type StudioTask } from '@/api/imageStudio'
import { readImageHistory, writeImageHistory } from '@/utils/imageStudioHistory'

export interface StudioCreation {
  id: string; prompt: string; model: string; ratio: ImageRatio; count: number
  resolution: ImageResolution; references: (File | StudioAsset)[]; keyId: number; keyName: string
  createdAt: number; status: 'generating' | 'completed' | 'failed'; images: (StudioImage & { id?: string })[]
  error?: string; uncertain?: boolean; taskId?: string; size?: string
}
const fromServer = (item: StudioCreationResponse): StudioCreation => ({
  id: item.id, taskId: item.id, prompt: item.prompt, model: item.model, ratio: item.ratio,
  resolution: item.resolution, size: item.size, count: item.count, references: item.references,
  keyId: item.key_id, keyName: item.key_name, createdAt: item.created_at,
  status: item.status === 'processing' ? 'generating' : item.status,
  images: item.images.map(image => ({ ...image, revisedPrompt: image.revised_prompt })), error: item.error,
})

export const useImageStudioStore = defineStore('imageStudio', () => {
  const auth = useAuthStore()
  const creations = ref<StudioCreation[]>([])
  const historyUnavailable = ref(false)
  const historyLoading = ref(false)
  const loadingMore = ref(false)
  const hasMore = ref(false)
  const legacyImportFailed = ref(false)
  const importing = ref(false)
  const generating = computed(() => creations.value.some(item => item.status === 'generating'))
  const controllers = new Map<string, AbortController>()
  let page = 1
  let accountVersion = 0
  let historyController = new AbortController()

  async function loadHistory(more = false) {
    const version = accountVersion
    if (!auth.user?.id || (more && loadingMore.value)) return
    if (more) loadingMore.value = true
    else historyLoading.value = true
    try {
      const result = await getStudioHistory(more ? page + 1 : 1, historyController.signal)
      if (version !== accountVersion) return
      const incoming = result.items.map(fromServer)
      if (more) {
        const ids = new Set(creations.value.map(item => item.id))
        creations.value.push(...incoming.filter(item => !ids.has(item.id)))
        page++
      } else {
        const pending = creations.value.filter(item => item.status === 'generating' && !incoming.some(saved => saved.id === item.id))
        creations.value = [...pending, ...incoming]
        page = 1
      }
      hasMore.value = result.has_more
      historyUnavailable.value = false
      resumePending()
    } catch { if (version === accountVersion) historyUnavailable.value = true }
    finally { if (version === accountVersion) { historyLoading.value = false; loadingMore.value = false } }
  }

  async function importLegacy() {
    if (!auth.user?.id || importing.value) return
    const version = accountVersion
    const userId = auth.user.id
    let importedAny = false
    importing.value = true
    legacyImportFailed.value = false
    try {
      let remaining = await readImageHistory(userId)
      for (const record of [...remaining]) {
        if (version !== accountVersion) return
        try {
          let images = record.images
          let status = record.status
          if (record.taskId) {
            const task = await getLegacyStudioTask(record.taskId, historyController.signal).catch(() => null)
            if (task?.status === 'processing') throw new Error('Legacy task is still processing')
            if (task) { images = task.images; status = task.status }
          }
          const outputs: File[] = []
          for (const [index, picture] of images.entries()) {
            const response = await fetch(picture.url, { credentials: 'omit', referrerPolicy: 'no-referrer', signal: historyController.signal })
            if (!response.ok) throw new Error('Legacy image unavailable')
            const blob = await response.blob()
            outputs.push(new File([blob], `image-${index + 1}.${blob.type.includes('webp') ? 'webp' : blob.type.includes('jpeg') ? 'jpg' : 'png'}`, { type: blob.type }))
          }
          if (version !== accountVersion) return
          await importStudioHistory({ legacy_id: record.id, prompt: record.prompt, model: record.model, ratio: record.ratio,
            resolution: record.resolution || '1K', count: record.count, key_name: record.keyName,
            created_at: record.createdAt, status, error: record.error?.slice(0, 600),
          }, record.references || [], outputs, historyController.signal)
          if (version !== accountVersion) return
          importedAny = true
          remaining = remaining.filter(item => item.id !== record.id)
          await writeImageHistory(userId, remaining)
        } catch { if (version === accountVersion) legacyImportFailed.value = true }
      }
    } catch { /* IndexedDB may be unavailable; new database history still works. */ }
    finally { if (version === accountVersion) { importing.value = false; if (importedAny) await loadHistory() } }
  }

  watch(() => auth.user?.id, async userId => {
    accountVersion++
    controllers.forEach(controller => controller.abort())
    controllers.clear()
    historyController.abort()
    historyController = new AbortController()
    creations.value = []
    historyUnavailable.value = false
    historyLoading.value = !!userId
    loadingMore.value = false
    importing.value = false
    legacyImportFailed.value = false
    hasMore.value = false
    page = 1
    if (userId) { await loadHistory(); void importLegacy() }
  }, { immediate: true, flush: 'sync' })

  async function track(record: StudioCreation, execute: (signal: AbortSignal, submitted: (task: StudioTask) => void) => Promise<StudioCreationResponse>) {
    const version = accountVersion
    const controller = new AbortController()
    let requestID = record.id
    let currentID = record.id
    controllers.set(requestID, controller)
    const current = () => version === accountVersion ? creations.value.find(item => item.id === currentID) : undefined
    try {
      const result = await execute(controller.signal, task => {
        const item = current()
        if (item) {
          item.id = task.taskId; item.taskId = task.taskId; currentID = task.taskId
          controllers.delete(requestID)
          requestID = task.taskId
          controllers.set(requestID, controller)
        }
      })
      const item = current()
      if (item) Object.assign(item, fromServer(result))
    } catch (error) {
      const item = current()
      if (item) {
        item.status = 'failed'
        item.uncertain = error instanceof TypeError || (error instanceof DOMException && ['AbortError', 'TimeoutError'].includes(error.name))
        item.error = error instanceof Error ? error.message : 'generation_failed'
      }
    } finally { if (controllers.get(requestID) === controller) controllers.delete(requestID) }
  }
  async function generate(apiKey: string, prompt: string, model: string, ratio: ImageRatio, count: number, resolution: ImageResolution, references: File[], keyId: number, keyName: string, customSize?: string) {
    if (historyLoading.value || !prompt.trim() || !apiKey || !model) return
    const files = [...references]
    const request = buildImageRequest(model, prompt, ratio, count, resolution, customSize)
    const record: StudioCreation = { id: crypto.randomUUID(), prompt: prompt.trim(), model, ratio, count,
      createdAt: Date.now(), status: 'generating', images: [], resolution, size: request.size, references: files, keyId, keyName }
    creations.value.unshift(record)
    await track(record, (signal, onSubmitted) => generateImages(apiKey, request, signal, files, onSubmitted))
  }
  async function resume(id: string) {
    const record = creations.value.find(item => item.id === id)
    if (!record?.taskId || record.status === 'completed' || controllers.has(id)) return
    record.status = 'generating'; record.uncertain = false; record.error = undefined
    await track(record, signal => pollImageTask(record.taskId!, signal))
  }
  function resumePending() {
    for (const record of creations.value) if (record.taskId && (record.status === 'generating' || record.uncertain)) void resume(record.id)
  }
  async function remove(id: string) {
    const record = creations.value.find(item => item.id === id)
    if (!record || record.status === 'generating') return
    const version = accountVersion
    if (record.taskId) {
      try { await deleteStudioCreation(record.taskId) }
      catch (error) {
        const failure = error as { status?: number; response?: { status?: number } }
        if ((failure.status ?? failure.response?.status) !== 404) throw error
      }
    }
    if (version === accountVersion) creations.value = creations.value.filter(item => item.id !== id)
  }
  return { creations, generating, historyLoading, historyUnavailable, loadingMore, hasMore, legacyImportFailed, importing,
    generate, resume, resumePending, remove, loadHistory, loadMore: () => loadHistory(true), importLegacy }
})
