<template>
  <div class="studio-prompt-editor">
    <div
      ref="editorEl"
      class="studio-prompt-input"
      :class="{ 'is-empty': !modelValue }"
      contenteditable="true"
      role="textbox"
      :aria-label="ariaLabel"
      :data-placeholder="placeholder"
      @keydown="onKeydown"
      @input="onInput"
      @paste="onPaste"
      @compositionstart="composing = true"
      @compositionend="onCompositionEnd"
    />
    <Teleport to="body">
      <div v-if="mentionOpen" ref="menuEl" class="studio-mention-layer" :style="menuStyle" role="listbox" @mousedown.prevent>
        <p v-if="!references.length">{{ t('imageStudio.referenceMentionEmpty') }}</p>
        <button v-for="(item, index) in references" :key="index" type="button" role="option" :aria-selected="index === activeIndex" :class="{ 'is-active': index === activeIndex }" @mouseenter="activeIndex = index" @click="choose(index)">{{ item.name }}</button>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseStudioPrompt, studioMentionToken, studioPromptPasteNeedsReferences, studioPromptTokenizeText } from '@/utils/studioPromptMentions'

const mentionStyle = 'display:inline;padding:0 6px;margin:0 1px;border-radius:999px;color:#3b6cff;background:rgba(59,108,255,.12);font-size:inherit;line-height:inherit;white-space:nowrap'
const props = defineProps<{ modelValue: string; references: { name: string }[]; placeholder?: string; ariaLabel?: string }>()
const emit = defineEmits<{ 'update:modelValue': [string]; submit: []; 'mentionsUnresolved': [] }>()
const { t } = useI18n()
const editorEl = ref<HTMLElement>()
const mentionOpen = ref(false)
const menuPos = ref({ left: 0, top: 0 })
const menuEl = ref<HTMLElement>()
const activeIndex = ref(0)
let composing = false
let inner = props.modelValue

const menuStyle = computed(() => ({ left: `${menuPos.value.left}px`, top: `${menuPos.value.top}px` }))

function escapeHtml(value: string) {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function mentionLabel(index: number) {
  const name = props.references[index - 1]?.name
  return name ? `@${name}` : studioMentionToken(index)
}

function mentionHTML(index: number) {
  return `<span class="prompt-mention" data-index="${index}" contenteditable="false" style="${mentionStyle}">${escapeHtml(mentionLabel(index))}</span>`
}

function htmlFromPrompt(prompt: string) {
  if (!prompt) return ''
  return parseStudioPrompt(prompt).map(part => {
    if (part.type === 'text') return escapeHtml(part.value).replace(/\n/g, '<br>')
    return mentionHTML(part.index)
  }).join('')
}

function serialize(root: HTMLElement) {
  const chunks: string[] = []
  const walk = (node: Node, isRoot = false) => {
    if (node.nodeType === Node.TEXT_NODE) {
      chunks.push((node.textContent || '').replace(/\u00a0/g, ' '))
      return
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return
    const el = node as HTMLElement
    if (el.classList.contains('prompt-mention')) {
      const index = Number(el.dataset.index || 0)
      if (index > 0) chunks.push(studioMentionToken(index))
      return
    }
    if (el.tagName === 'BR') {
      chunks.push('\n')
      return
    }
    if (el.tagName === 'DIV' && !isRoot && chunks.length && !chunks[chunks.length - 1].endsWith('\n')) chunks.push('\n')
    Array.from(el.childNodes).forEach(child => walk(child))
  }
  Array.from(root.childNodes).forEach(child => walk(child, true))
  return chunks.join('')
}

function render(prompt: string) {
  const editor = editorEl.value
  if (!editor) return
  inner = prompt
  editor.innerHTML = htmlFromPrompt(prompt)
}

function emitValue(value: string) {
  inner = value
  emit('update:modelValue', value)
}

function caretEndsWithAt() {
  const selection = window.getSelection()
  if (!selection?.rangeCount || !selection.isCollapsed || !editorEl.value?.contains(selection.anchorNode)) return false
  const range = selection.getRangeAt(0)
  if (range.startContainer.nodeType !== Node.TEXT_NODE) return false
  return (range.startContainer.textContent || '').slice(0, range.startOffset).endsWith('@')
}

function updateMenuPosition() {
  const selection = window.getSelection()
  const range = selection?.rangeCount ? selection.getRangeAt(0) : undefined
  const caret = range?.getBoundingClientRect()
  const editor = editorEl.value?.getBoundingClientRect()
  const left = caret && caret.width + caret.height ? caret.left : editor?.left || 16
  const top = (caret && caret.height ? caret.bottom : editor?.bottom || 40) + 6
  menuPos.value = { left, top }
}

function onInput() {
  const editor = editorEl.value
  if (!editor) return
  emitValue(serialize(editor))
  const open = !composing && caretEndsWithAt()
  if (open && !mentionOpen.value) activeIndex.value = 0
  mentionOpen.value = open
  if (open) updateMenuPosition()
}

function scrollActiveIntoView() {
  const active = menuEl.value?.querySelector<HTMLElement>('.is-active')
  active?.scrollIntoView({ block: 'nearest' })
}

function onCompositionEnd() {
  composing = false
  onInput()
}

function onKeydown(event: KeyboardEvent) {
  if (mentionOpen.value && !event.isComposing && props.references.length) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const delta = event.key === 'ArrowDown' ? 1 : -1
      activeIndex.value = (activeIndex.value + delta + props.references.length) % props.references.length
      requestAnimationFrame(scrollActiveIntoView)
      return
    }
    if (event.key === 'Enter' && !event.ctrlKey && !event.metaKey) {
      event.preventDefault()
      choose(activeIndex.value)
      return
    }
    if (event.key === 'Tab') {
      event.preventDefault()
      return
    }
  }
  if (event.key === 'Escape') {
    mentionOpen.value = false
    return
  }
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter' && !event.isComposing) {
    event.preventDefault()
    emit('submit')
  }
}

function onPaste(event: ClipboardEvent) {
  event.preventDefault()
  const raw = event.clipboardData?.getData('text/plain') || ''
  if (studioPromptPasteNeedsReferences(raw, props.references.map(item => item.name))) emit('mentionsUnresolved')
  const text = studioPromptTokenizeText(raw, props.references.map(item => item.name))
  document.execCommand('insertText', false, text)
  const editor = editorEl.value
  if (!editor) return
  const value = serialize(editor)
  emitValue(value)
  render(value)
  mentionOpen.value = false
}

function choose(index: number) {
  const selection = window.getSelection()
  const editor = editorEl.value
  if (!editor) return
  if (selection?.rangeCount && caretEndsWithAt()) {
    const range = selection.getRangeAt(0)
    const node = range.startContainer as Text
    node.deleteData(range.startOffset - 1, 1)
    range.setStart(node, Math.min(range.startOffset, node.length))
    range.collapse(true)
    const wrap = document.createElement('span')
    wrap.innerHTML = mentionHTML(index + 1)
    const mention = wrap.firstElementChild
    if (mention) {
      range.insertNode(mention)
      range.setStartAfter(mention)
      range.collapse(true)
      selection.removeAllRanges()
      selection.addRange(range)
    }
  }
  mentionOpen.value = false
  emitValue(serialize(editor))
  editor.focus()
}

function onPointerDown(event: PointerEvent) {
  const target = event.target as Node | null
  if (mentionOpen.value && target && !(event.target as Element).closest?.('.studio-mention-layer') && !editorEl.value?.contains(target)) mentionOpen.value = false
}

watch(() => props.modelValue, value => {
  if (value !== inner) render(value)
})
watch(() => props.references.map(item => item.name).join('\0'), () => {
  editorEl.value?.querySelectorAll<HTMLElement>('.prompt-mention').forEach(el => {
    el.textContent = mentionLabel(Number(el.dataset.index))
  })
})
onMounted(() => {
  render(props.modelValue)
  document.addEventListener('pointerdown', onPointerDown, true)
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onPointerDown, true))
defineExpose({ focus: () => editorEl.value?.focus() })
</script>

<style scoped>
.studio-prompt-editor { min-width: 0; }
.studio-prompt-input { display: block; width: 100%; min-height: 82px; }
.studio-prompt-input:focus,
.studio-prompt-input:focus-visible {
  outline: none !important;
  box-shadow: none !important;
  border: 0 !important;
}
</style>

<style>
.studio-mention-layer {
  position: fixed;
  z-index: 80;
  min-width: 200px;
  max-width: 320px;
  max-height: 240px;
  overflow: auto;
  padding: 6px;
  background: #fff;
  border: 1px solid #e6e4ef;
  border-radius: 12px;
  box-shadow: 0 16px 40px rgb(20 18 40 / .16);
}
.studio-mention-layer p {
  margin: 0;
  padding: 8px 10px;
  color: #8b8896;
  font-size: 13px;
}
.studio-mention-layer button {
  display: block;
  width: 100%;
  padding: 8px 10px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #2f2c38;
  font-size: 13px;
  line-height: 1.5;
  text-align: left;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.studio-mention-layer button:hover,
.studio-mention-layer button.is-active {
  background: #eef2ff;
  color: #3b6cff;
}
.prompt-mention {
  display: inline;
  padding: 0 6px;
  margin: 0 1px;
  border-radius: 999px;
  color: #3b6cff;
  background: rgba(59, 108, 255, .12);
  font-size: inherit;
  line-height: inherit;
  white-space: nowrap;
}
</style>
