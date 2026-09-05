<template>
  <BaseDialog :show="!!creation" :title="t('imageStudio.preview')" variant="media" width="full" close-on-click-outside @close="emit('close')">
    <div v-if="creation && picture" class="studio-preview">
      <div class="preview-gallery">
        <div class="preview-canvas" :aria-busy="!loaded.has(picture.url) && !failed.has(picture.url)">
          <!-- Fetch only visited originals, then keep them mounted while switching. -->
          <template v-for="(item, position) in creation.images" :key="`${item.id || position}-${retryVersion}`">
            <img v-if="visited.has(position)" v-show="position === index && !failed.has(item.url)" :src="item.url" :alt="creation.prompt" class="preview-image" referrerpolicy="no-referrer" @load="loaded.add(item.url)" @error="imageError(item)" />
          </template>
          <div v-if="failed.has(picture.url)" class="preview-status" role="alert"><Icon name="exclamationTriangle" /><span>{{ t('imageStudio.imageUnavailable') }}</span><button class="preview-retry" @click="retry">{{ t('imageStudio.retry') }}</button></div>
          <div v-else-if="!loaded.has(picture.url)" class="preview-status" role="status"><span class="preview-spinner" /><span>{{ t('imageStudio.loadingImage') }}</span></div>
          <template v-if="creation.images.length > 1">
            <button class="preview-arrow previous" :aria-label="t('imageStudio.previousImage')" @click="move(-1)"><Icon name="chevronLeft" /></button>
            <button class="preview-arrow next" :aria-label="t('imageStudio.nextImage')" @click="move(1)"><Icon name="chevronRight" /></button>
          </template>
        </div>
        <div class="preview-filmstrip">
          <div v-if="creation.images.length > 1" class="preview-thumbnails" :aria-label="t('imageStudio.batchImages')">
            <button v-for="(item, position) in creation.images" :key="item.id || position" :class="{ selected: position === index }" :aria-label="t('imageStudio.imageNumber', { number: position + 1 })" :aria-pressed="position === index" @click="emit('select', position)"><StudioThumbnail :image="item" alt="" /><span class="thumbnail-number">{{ position + 1 }}</span></button>
          </div>
          <span class="preview-position" aria-live="polite" aria-atomic="true">{{ index + 1 }} <span>/ {{ creation.images.length }}</span></span>
        </div>
      </div>
      <aside class="preview-details">
        <div class="preview-info">
          <div class="preview-model"><Icon name="cube" size="sm" />{{ creation.model }}</div>
          <div class="preview-tags"><span>{{ creation.ratio === 'auto' ? t('imageStudio.autoRatio') : creation.ratio }}</span><span v-if="creation.ratio !== 'auto'">{{ creation.resolution }}</span><span v-if="creation.size">{{ creation.size.replace('x', ' × ') }}</span></div>
          <h4>{{ t('imageStudio.prompt') }}</h4>
          <p class="preview-prompt">{{ picture.revisedPrompt || creation.prompt }}</p>
        </div>
        <div class="preview-actions">
          <button class="btn btn-primary" :disabled="downloading" @click="emit('download')"><Icon :name="downloading ? 'refresh' : 'download'" size="sm" />{{ t('imageStudio.download') }}</button>
          <button class="btn btn-secondary" :disabled="referenceDisabled" @click="emit('reference')"><Icon name="photo" size="sm" />{{ referenceLabel }}</button>
          <a :href="picture.url" target="_blank" rel="noopener noreferrer" class="preview-original">{{ t('imageStudio.openOriginal') }}<Icon name="externalLink" size="sm" /></a>
        </div>
      </aside>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import StudioThumbnail from './StudioThumbnail.vue'
import type { StudioCreation } from '@/stores/imageStudio'

const props = defineProps<{
  creation: StudioCreation | null
  index: number
  downloading: boolean
  referenceDisabled: boolean
  referenceLabel: string
}>()
type Picture = StudioCreation['images'][number]
const emit = defineEmits<{
  close: []
  select: [index: number]
  download: []
  reference: []
  error: [picture: Picture, force?: boolean]
}>()
const { t } = useI18n()
const picture = computed(() => props.creation?.images[props.index])
const loaded = ref(new Set<string>())
const failed = ref(new Set<string>())
const retryVersion = ref(0)
const visited = ref(new Set<number>())
watch(() => props.creation?.id, () => { loaded.value.clear(); failed.value.clear(); visited.value.clear() })
watch(() => [props.creation?.id, props.index], () => { if (props.creation) visited.value.add(props.index) }, { immediate: true })

function imageError(item: Picture) {
  failed.value.add(item.url)
  emit('error', item)
}
function retry() {
  if (!picture.value) return
  failed.value.delete(picture.value.url)
  loaded.value.delete(picture.value.url)
  retryVersion.value++
  emit('error', picture.value, true)
}
function move(offset: number) {
  const total = props.creation?.images.length || 0
  if (total > 1) emit('select', (props.index + offset + total) % total)
}
function handleKeydown(event: KeyboardEvent) {
  if (!props.creation || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
  const target = event.target
  if (target instanceof Element && target.closest('input, textarea, select, [contenteditable="true"]')) return
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault()
    move(event.key === 'ArrowLeft' ? -1 : 1)
  }
}
onMounted(() => document.addEventListener('keydown', handleKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', handleKeydown))
</script>

<style scoped>
.studio-preview { display: grid; grid-template-columns: minmax(0, 1fr) 288px; height: 100%; min-height: 0; color: var(--fv-text); }
.preview-gallery { min-width: 0; min-height: 0; display: flex; flex-direction: column; background: #111318; color: #f3f4f6; }
.preview-canvas { position: relative; flex: 1; min-height: 0; margin: 24px 20px 0; }
.preview-image { position: absolute; width: 100%; height: 100%; object-fit: contain; }
.preview-arrow { position: absolute; top: 50%; transform: translateY(-50%); display: grid; place-items: center; width: 40px; height: 40px; border-radius: 50%; border: 1px solid rgb(255 255 255 / .14); background: rgb(20 22 28 / .7); color: white; backdrop-filter: blur(8px); transition: background .15s; }
.preview-arrow:hover { background: rgb(70 72 80 / .9); }
.preview-arrow.previous { left: 0; }
.preview-arrow.next { right: 0; }
.preview-filmstrip { position: relative; display: flex; justify-content: center; align-items: center; flex-shrink: 0; min-height: 56px; padding: 16px 64px; }
.preview-thumbnails { display: flex; gap: 8px; }
.preview-thumbnails button { position: relative; width: 56px; height: 56px; padding: 3px; border-radius: 10px; border: 1px solid transparent; opacity: .55; transition: opacity .15s, border-color .15s; }
.preview-thumbnails button:hover, .preview-thumbnails button.selected { opacity: 1; }
.preview-thumbnails button.selected { border-color: #b5a6ff; background: rgb(181 166 255 / .13); }
.preview-thumbnails :deep(img) { width: 100%; height: 100%; object-fit: cover; border-radius: 6px; }
.preview-thumbnails .thumbnail-number { position: absolute; bottom: 5px; right: 6px; font-size: 10px; line-height: 15px; min-width: 15px; border-radius: 4px; background: rgb(0 0 0 / .6); }
.preview-position { position: absolute; right: 20px; font-size: 13px; font-variant-numeric: tabular-nums; }
.preview-position span { color: #9397a3; }
.preview-details { display: flex; flex-direction: column; min-height: 0; padding: 24px; background: var(--fv-surface); border-left: 1px solid var(--fv-line); }
.preview-info { flex: 1; min-height: 0; overflow-y: auto; scrollbar-width: thin; }
.preview-model { display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; }
.preview-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 12px; }
.preview-tags span { padding: 4px 7px; border-radius: 5px; background: var(--fv-surface-2); color: var(--fv-muted); font-size: 11px; }
.preview-info h4 { margin: 28px 0 10px; font-size: 12px; color: var(--fv-muted); font-weight: 400; }
.preview-prompt { font-size: 13px; line-height: 1.8; white-space: pre-wrap; overflow-wrap: anywhere; }
.preview-actions { display: flex; flex-direction: column; gap: 10px; padding-top: 24px; }
.preview-actions .btn { justify-content: center; gap: 8px; font-size: 13px; }
.preview-original { display: flex; justify-content: center; align-items: center; gap: 6px; margin-top: 6px; color: var(--fv-muted); font-size: 12px; }
.preview-original:hover { color: var(--fv-text); }
.preview-status { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; flex-direction: column; gap: 12px; color: #a3a6b2; font-size: 13px; pointer-events: none; }
.preview-retry { pointer-events: auto; text-decoration: underline; text-underline-offset: 4px; }
.preview-spinner { width: 24px; height: 24px; border: 2px solid #363944; border-top-color: #b5a6ff; border-radius: 50%; animation: preview-spin .8s linear infinite; }
.studio-preview button:focus-visible, .studio-preview a:focus-visible { outline: 2px solid #a594ff; outline-offset: 3px; }
@keyframes preview-spin { to { transform: rotate(360deg); } }
@media (max-width: 1000px) {
  .studio-preview { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; }
  .preview-details { padding: 16px; max-height: 210px; border-left: 0; border-top: 1px solid var(--fv-line); }
  .preview-info h4, .preview-tags { display: none; }
  .preview-prompt { margin-top: 8px; max-height: 48px; overflow-y: auto; }
  .preview-actions { flex-direction: row; flex-wrap: wrap; align-items: center; padding-top: 12px; gap: 8px; }
  .preview-original { margin: 0 0 0 auto; }
  .preview-canvas { margin: 12px 12px 0; }
  .preview-filmstrip { padding: 10px 48px; }
  .preview-thumbnails button { width: 44px; height: 44px; }
  .preview-position { right: 12px; font-size: 12px; }
}
@media (prefers-reduced-motion: reduce) { .preview-spinner { animation-duration: 2s; } }
</style>
