<template>
  <div v-if="!readOnly || references.length" ref="container" class="reference-picker" :class="{ 'is-expanded': expanded && references.length, 'is-readonly': readOnly, 'is-single': references.length === 1 }" @pointerenter="pointerEnter" @pointerleave="pointerLeave" @focusin="expanded = true" @focusout="focusOut" @keydown.esc.stop.prevent="collapse">
    <button v-if="!references.length" ref="emptyButton" type="button" class="reference-upload reference-empty" :aria-label="t('imageStudio.reference')" :title="t('imageStudio.referenceHint')" @click="$emit('add')">
      <Icon name="plus" /><span>{{ t('imageStudio.reference') }}</span>
    </button>
    <template v-else>
      <button ref="expandButton" type="button" class="reference-expand" :tabindex="expanded ? -1 : 0" :aria-label="t('imageStudio.manageReferences', { count: references.length })" :aria-expanded="expanded" @click="expanded = true" />
      <div class="reference-tray">
        <div class="reference-cards">
          <div v-for="(reference, index) in references" :key="isFile(reference) ? index : reference.url" class="reference-thumbnail" :style="{ '--index': index, '--angle': `${[-7, 5, -3, 7][index % 4]}deg`, zIndex: references.length - index }">
            <StudioReferenceThumbnail v-if="isFile(reference)" :file="reference" />
            <StudioThumbnail v-else-if="'id' in reference" :image="reference" :alt="referenceName(reference)" />
            <img v-else :src="reference.url" :alt="referenceName(reference)" referrerpolicy="no-referrer" />
            <button v-if="!readOnly" type="button" class="reference-remove" :tabindex="expanded ? 0 : -1" :aria-label="t('imageStudio.removeReferenceNamed', { name: referenceName(reference) })" @click="remove(index)"><Icon name="x" size="sm" /></button>
          </div>
          <button v-if="!readOnly && references.length < 4" type="button" class="reference-upload reference-add" :tabindex="expanded ? 0 : -1" :aria-label="t('imageStudio.reference')" :title="t('imageStudio.referenceHint')" @click="$emit('add')"><Icon name="plus" /></button>
        </div>
      </div>
      <button v-if="!readOnly && references.length < 4" type="button" class="reference-add-badge" :tabindex="expanded ? -1 : 0" :aria-label="t('imageStudio.reference')" @click="$emit('add')"><Icon name="plus" size="sm" /></button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import StudioReferenceThumbnail from './StudioReferenceThumbnail.vue'
import StudioThumbnail from './StudioThumbnail.vue'
import type { StudioAsset } from '@/api/imageStudio'

type Reference = File | StudioAsset | { file: File; url: string }
const props = defineProps<{ references: Reference[]; readOnly?: boolean }>()
const isFile = (reference: Reference): reference is File => reference instanceof File
const referenceName = (reference: Reference) => isFile(reference) ? reference.name : 'file' in reference ? reference.file.name : reference.filename
const emit = defineEmits<{ add: []; remove: [index: number] }>()
const { t } = useI18n()
const container = ref<HTMLElement>()
const expandButton = ref<HTMLButtonElement>()
const emptyButton = ref<HTMLButtonElement>()
const expanded = ref(false)
let hovered = false

function pointerEnter(event: PointerEvent) {
  if (event.pointerType === 'touch') return
  hovered = true
  expanded.value = true
}
function pointerLeave() {
  hovered = false
  if (!container.value?.contains(document.activeElement)) expanded.value = false
}
function focusOut(event: FocusEvent) {
  if (!hovered && !container.value?.contains(event.relatedTarget as Node | null)) expanded.value = false
}
function collapse() {
  expandButton.value?.focus()
  expanded.value = false
}
async function remove(index: number) {
  emit('remove', index)
  await nextTick()
  const buttons = container.value?.querySelectorAll<HTMLButtonElement>('.reference-remove')
  const next = buttons?.[Math.min(index, props.references.length - 1)] || emptyButton.value
  next?.focus()
}
watch(() => props.references.length, count => { if (!count) expanded.value = false })
</script>

<style scoped>
.reference-picker { --card-width: 70px; --card-height: 82px; position: relative; flex: 0 0 92px; width: 92px; height: 96px; }
.reference-tray { position: absolute; top: -8px; left: -8px; padding: 8px; border: 1px solid transparent; border-radius: 16px; transition: background .2s, box-shadow .2s, border-color .2s; }
.reference-cards { position: relative; width: 90px; height: 96px; padding: 7px; }
.reference-thumbnail { position: absolute; top: 7px; left: 7px; width: var(--card-width); height: var(--card-height); flex-shrink: 0; padding: 3px; background: var(--studio-surface); border: 1px solid var(--studio-line); border-radius: 9px; box-shadow: 0 3px 9px #17172b12; transform: translate(calc(var(--index) * 5px), calc(var(--index) * -2px)) rotate(var(--angle)); transition: transform .2s; }
.reference-thumbnail :deep(img) { width: 100%; height: 100%; object-fit: contain; border-radius: 6px; background: #fff; }
.reference-expand { position: absolute; inset: 0; z-index: 5; border-radius: 10px; }
.reference-expand:focus-visible { outline: 2px solid var(--studio-accent); outline-offset: 2px; }
.reference-add-badge { position: absolute; right: 0; bottom: 0; z-index: 6; width: 28px; height: 28px; display: grid; place-items: center; background: var(--studio-bg); color: var(--studio-ink); border: 1px solid var(--studio-line); border-radius: 50%; }
.reference-remove { position: absolute; top: -7px; right: -7px; width: 24px; height: 24px; display: grid; place-items: center; background: #29282f; color: #fff; border: 2px solid var(--studio-surface); border-radius: 50%; visibility: hidden; }
.reference-remove:hover { background: #514c61; }
.reference-upload { display: flex; align-items: center; justify-content: center; width: var(--card-width); height: var(--card-height); flex-shrink: 0; border: 1px dashed color-mix(in srgb, var(--studio-muted) 35%, var(--studio-line)); border-radius: 9px; background: var(--studio-bg); color: var(--studio-muted); }
.reference-upload:hover { color: var(--studio-accent); border-color: var(--studio-accent); }
.reference-empty { flex-direction: column; gap: 6px; }
.reference-empty span { font-size: 10px; }
.reference-add { display: none; }
.is-expanded .reference-tray { z-index: 10; }
.is-expanded .reference-cards { display: flex; gap: 4px; width: max-content; max-width: calc(100vw - 82px); overflow-x: auto; height: auto; padding: 10px 8px; }
.is-expanded .reference-thumbnail { position: relative; top: auto; left: auto; transform: rotate(0deg); }
.is-expanded .reference-remove { visibility: visible; }
.is-expanded .reference-add { display: flex; }
.is-expanded .reference-expand, .is-expanded .reference-add-badge { opacity: 0; pointer-events: none; }
.reference-picker button:focus-visible { outline: 2px solid var(--studio-accent); outline-offset: 2px; }
@media (max-width: 767px) {
  .reference-picker { --card-width: 60px; --card-height: 70px; flex-basis: auto; width: 80px; height: 80px; }
  .reference-cards { width: 78px; height: 80px; }
}
.reference-picker.is-readonly { --card-width: 44px; --card-height: 44px; flex: 0 0 58px; width: 58px; height: 48px; }
.is-readonly.is-single { flex-basis: 46px; width: 46px; }
.is-readonly .reference-tray { top: -6px; left: -6px; padding: 6px; border-radius: 12px; }
.is-readonly:not(.is-expanded) .reference-cards { width: 58px; height: 48px; padding: 0; }
.is-readonly:not(.is-expanded) .reference-thumbnail { top: 0; left: 0; padding: 2px; }
.is-readonly.is-single .reference-thumbnail { transform: none; }
.is-readonly.is-expanded { --card-width: clamp(44px, calc((100vw - 122px) / 4), 64px); --card-height: var(--card-width); }
.is-readonly.is-expanded .reference-cards { padding: 6px; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; } }
</style>
