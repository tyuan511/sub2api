<template>
  <Select class="studio-settings-select" :model-value="ratio" :options="[]" :searchable="false" :panel-width="480" :error="sizeError" :aria-label="t('imageStudio.imageSettings')">
    <template #selected>
      <span class="settings-summary">
        <Icon v-if="automatic" name="expand" size="sm" aria-hidden="true" />
        <span v-else class="ratio-glyph" :style="ratioStyle(ratio, 15)" aria-hidden="true" />
        <span>{{ ratioLabel(ratio) }}</span><template v-if="!automatic"><i>·</i><span :title="formatSize(selectedSize)">{{ resolutionLabel(resolution) }}</span></template><i>·</i><span>{{ count }}</span>
      </span>
    </template>
    <template #panel>
      <div class="image-settings-panel">
        <fieldset>
          <legend>{{ t('imageStudio.chooseRatio') }}</legend>
          <div class="settings-segments ratio-segments" :aria-label="t('imageStudio.ratio')" role="group">
            <button v-for="value in ratios" :key="value" type="button" :aria-pressed="ratio === value" :aria-label="ratioLabel(value)" @click="chooseRatio(value)">
              <span class="ratio-glyph-space" aria-hidden="true"><Icon v-if="value === 'auto'" name="expand" size="sm" /><span v-else class="ratio-glyph" :style="ratioStyle(value, 17)" /></span>
              <span>{{ ratioLabel(value) }}</span>
            </button>
          </div>
        </fieldset>
        <fieldset>
          <legend>{{ t('imageStudio.chooseResolution') }}</legend>
          <div class="settings-segments" :aria-label="t('imageStudio.resolution')" role="group">
            <button v-if="automatic" type="button" disabled>{{ t('imageStudio.autoDimensions') }}</button>
            <template v-else>
              <button v-for="value in resolutions" :key="value" type="button" :data-preset="value" :aria-label="resolutionLabel(value)" :title="formatSize(presetSize(value))" :aria-pressed="selectedSize === presetSize(value)" @click="chooseResolution(value)">{{ resolutionLabel(value) }}</button>
            </template>
          </div>
        </fieldset>
        <fieldset>
          <legend>{{ t('imageStudio.chooseCount') }}</legend>
          <div class="settings-segments" :aria-label="t('imageStudio.imageCount')" role="group">
            <button v-for="value in 4" :key="value" type="button" :aria-pressed="count === value" :aria-label="t('imageStudio.count', { count: value })" @click="emit('update:count', value)">{{ value }}</button>
          </div>
        </fieldset>
        <fieldset>
          <legend>{{ t('imageStudio.dimensions') }}</legend>
          <div class="size-fields" @keydown.enter.prevent>
            <Input :model-value="width" :aria-label="t('imageStudio.width')" :disabled="automatic" :placeholder="automatic ? '—' : ''" :readonly="!supportsSize" :maxlength="4" autocomplete="off" @update:model-value="draft('width', $event)" @blur="commitSize('width')" @enter="commitSize('width')"><template #prefix>W</template></Input>
            <span class="size-link" :title="t('imageStudio.sizeLinked')" :aria-label="t('imageStudio.sizeLinked')"><Icon name="link" size="sm" /></span>
            <Input :model-value="height" :aria-label="t('imageStudio.height')" :disabled="automatic" :placeholder="automatic ? '—' : ''" :readonly="!supportsSize" :maxlength="4" autocomplete="off" @update:model-value="draft('height', $event)" @blur="commitSize('height')" @enter="commitSize('height')"><template #prefix>H</template></Input>
            <span class="size-unit">PX</span>
          </div>
          <p v-if="sizeError" class="size-error" role="alert">{{ t('imageStudio.sizeInvalid') }}</p>
          <p v-else-if="supportsSize" class="size-hint">{{ t(automatic ? 'imageStudio.autoSizeHint' : 'imageStudio.sizeLinked') }}</p>
        </fieldset>
      </div>
    </template>
  </Select>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import Icon from '@/components/icons/Icon.vue'
import { buildImageRequest, getImageRatios, getImageResolutions, imageSizeResolution, isValidImageSize, type ImageRatio, type ImageResolution } from '@/api/imageStudio'

const props = defineProps<{ model: string; ratio: ImageRatio; resolution: ImageResolution; count: number; size?: string }>()
const emit = defineEmits<{
  'update:ratio': [value: ImageRatio]
  'update:resolution': [value: ImageResolution]
  'update:count': [value: number]
  'update:size': [value: string | undefined]
  'validity': [value: boolean]
}>()
const { t } = useI18n()
const supportsSize = computed(() => props.model.startsWith('gpt-image-2'))
const automatic = computed(() => props.ratio === 'auto')
const ratioLabel = (ratio: ImageRatio) => ratio === 'auto' ? t('imageStudio.autoRatio') : ratio
const ratioOrder: ImageRatio[] = ['1:1', '3:4', '16:9', '4:3', '9:16', '2:3', '3:2', '21:9']
const ratios = computed(() => ratioOrder.filter(value => getImageRatios(props.model).includes(value)))
const resolutions = computed(() => getImageResolutions(props.model, props.ratio))
const resolutionLabel = (value: ImageResolution) => supportsSize.value ? value : t('imageStudio.standard')
const formatSize = (size?: string) => size ? size.replace('x', '×') : '—'
function presetSize(resolution: ImageResolution) {
  try { return buildImageRequest(props.model, '', props.ratio, props.count, resolution).size }
  catch { return undefined }
}
const selectedSize = computed(() => props.size ?? presetSize(props.resolution))
const width = ref('')
const height = ref('')
const sizeError = ref(false)
const dirtyEdge = ref<'width' | 'height' | null>(null)

function ratioStyle(ratio: ImageRatio, edge: number) {
  const [w, h] = ratio.split(':').map(Number)
  return { width: `${edge * w / Math.max(w, h)}px`, height: `${edge * h / Math.max(w, h)}px` }
}
function resetDraft() {
  let size = ''
  try { size = buildImageRequest(props.model, '', props.ratio, props.count, props.resolution, props.size).size || '' } catch { /* Parent normalizes model presets on its next update. */ }
  [width.value, height.value] = size ? size.split('x') : ['', '']
  sizeError.value = false
  dirtyEdge.value = null
  emit('validity', automatic.value || !!size)
}
watch(() => [props.model, props.ratio, props.resolution, props.size], resetDraft, { immediate: true })
function chooseRatio(value: ImageRatio) {
  emit('update:size', undefined)
  emit('update:ratio', value)
  const supported = getImageResolutions(props.model, value)
  if (value !== props.ratio || !supported.includes(props.resolution)) emit('update:resolution', supported[0])
  resetDraft()
}
function chooseResolution(value: ImageResolution) {
  emit('update:size', undefined)
  emit('update:resolution', value)
  resetDraft()
}
function draft(edge: 'width' | 'height', value: string) {
  if (edge === 'width') width.value = value
  else height.value = value
  dirtyEdge.value = edge
  emit('validity', false)
}
function commitSize(edge: 'width' | 'height') {
  if (automatic.value || !supportsSize.value || dirtyEdge.value !== edge) return
  const input = edge === 'width' ? width.value : height.value
  let [rw, rh] = props.ratio.split(':').map(Number)
  let a = rw, b = rh
  while (b) [a, b] = [b, a % b]
  rw = rw / a * 16; rh = rh / a * 16
  // Keep the selected ratio exact while snapping both edges to 16px.
  const steps = Math.round(Number(input) / (edge === 'width' ? rw : rh))
  const size = `${rw * steps}x${rh * steps}`
  if (!/^\d+$/.test(input) || !isValidImageSize(size, props.ratio)) {
    sizeError.value = true
    emit('validity', false)
    return
  }
  [width.value, height.value] = size.split('x')
  dirtyEdge.value = null
  sizeError.value = false
  emit('update:resolution', imageSizeResolution(size))
  emit('update:size', size)
  emit('validity', true)
}
</script>

<style scoped>
.settings-summary { display: flex; align-items: center; gap: 8px; white-space: nowrap; }
.settings-summary i { color: #b4b5be; font-style: normal; }
.ratio-glyph { display: inline-block; flex: none; border: 1.6px solid currentColor; border-radius: 3px; }
.image-settings-panel { padding: 14px; color: #282932; }
.image-settings-panel fieldset { min-width: 0; }
.image-settings-panel fieldset + fieldset { margin-top: 10px; }
.image-settings-panel legend { margin-bottom: 8px; font-size: 11px; color: #858a97; }
.settings-segments { display: flex; gap: 3px; padding: 3px; border-radius: 10px; background: #f5f5f7; }
.settings-segments button { display: flex; flex: 1; align-items: center; justify-content: center; gap: 3px; min-width: 0; min-height: 32px; padding: 5px 3px; border-radius: 8px; font-size: 12px; white-space: nowrap; transition: background .18s, box-shadow .18s; }
.settings-segments button:hover { background: #ffffff80; }
.settings-segments button[aria-pressed="true"] { background: #fff; box-shadow: 0 1px 4px #18182506; }
.settings-segments button:focus-visible { outline: 2px solid #9585ed; outline-offset: -2px; }
.settings-segments button:disabled { opacity: .35; cursor: not-allowed; background: transparent; box-shadow: none; }
.ratio-segments { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: 2px; }
.ratio-segments button { flex-direction: column; gap: 6px; min-height: 54px; font-size: 11px; }
.ratio-glyph-space { height: 19px; display: flex; align-items: center; justify-content: center; }
.size-fields { display: grid; grid-template-columns: minmax(0, 1fr) 18px minmax(0, 1fr) 22px; gap: 10px; align-items: center; }
.size-fields :deep(input) { height: 34px; font-size: 12px; text-align: right; background: #f5f5f7; border-color: transparent; border-radius: 9px; box-shadow: none; font-variant-numeric: tabular-nums; }
.size-fields :deep(input:focus) { border-color: #a697ef; }
.size-link, .size-unit { color: #8c8d96; font-size: 12px; }
.size-link { display: grid; place-items: center; }
.size-hint, .size-error { font-size: 10px; margin-top: 8px; }
.size-hint { color: #a0a1ab; }
.size-error { color: #e36767; }
.dark .image-settings-panel { color: #ededf3; }
.dark .settings-segments, .dark .size-fields :deep(input) { background: #191b24; }
.dark .settings-segments button:not(:disabled):hover { background: #30323e; }
.dark .settings-segments button[aria-pressed="true"] { background: #3b3d4b; }
@media (max-width: 480px) {
  .image-settings-panel { padding: 14px; }
  .ratio-segments { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .ratio-segments button { min-height: 50px; gap: 5px; }
  .size-fields { gap: 8px; grid-template-columns: minmax(0, 1fr) 18px minmax(0, 1fr) 22px; }
}
</style>
