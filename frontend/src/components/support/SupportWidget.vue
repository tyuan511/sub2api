<template>
  <div class="fixed bottom-4 right-4 z-40 flex flex-col items-end gap-3">
    <transition name="support-widget">
      <section
        v-if="open"
        class="support-chat flex h-[min(620px,calc(100dvh-6rem))] w-[min(390px,calc(100vw-2rem))] flex-col overflow-hidden border"
        aria-label="联系客服"
      >
        <header class="support-chat-header flex h-14 shrink-0 items-center justify-between border-b border-gray-200 px-3.5 dark:border-dark-700">
          <h2 class="min-w-0 truncate text-sm font-semibold text-gray-900 dark:text-white">FastVibe 客服</h2>
          <button class="flex h-8 w-8 items-center justify-center rounded-md text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white" aria-label="关闭客服窗口" title="关闭" @click="open = false">
            <Icon name="x" size="sm" />
          </button>
        </header>

        <div ref="messageStream" class="support-message-stream min-h-0 flex-1 space-y-4 overflow-y-auto px-3 py-4">
          <div v-if="loading" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-dark-400">正在加载...</div>
          <div v-else-if="!messages.length" class="flex h-full flex-col items-center justify-center px-6 text-center">
            <span class="flex h-12 w-12 items-center justify-center rounded-md bg-white text-gray-500 shadow-sm dark:bg-dark-800 dark:text-dark-300">
              <Icon name="chatBubble" size="lg" />
            </span>
            <p class="mt-3 text-sm font-medium text-gray-800 dark:text-dark-100">有什么可以帮你？</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">发送消息后，管理员会在这里回复。</p>
          </div>

          <article v-for="message in messages" :key="message.id" class="flex items-start gap-2" :class="message.sender_role === 'user' ? 'justify-end' : 'justify-start'">
            <img
              v-if="message.sender_role !== 'user'"
              src="/fastvibe-support-logo.png"
              alt="FastVibe 客服"
              class="mt-[18px] h-8 w-8 shrink-0 rounded-md border border-gray-200 bg-white object-cover shadow-sm dark:border-dark-600"
            />
            <div :class="message.sender_role === 'user' ? 'max-w-[88%] sm:max-w-[78%]' : 'max-w-[calc(100%_-_2.5rem)] sm:max-w-[78%]'">
              <div class="mb-1 text-[10px] text-gray-400 dark:text-dark-500" :class="message.sender_role === 'user' ? 'text-right' : ''">
                {{ formatTime(message.created_at) }}
              </div>
              <div v-if="message.attachments?.length" class="mb-2 flex flex-col gap-2" :class="message.sender_role === 'user' ? 'items-end' : 'items-start'">
                <button v-for="attachment in message.attachments" :key="attachment.id" class="max-w-full cursor-zoom-in overflow-hidden rounded-md bg-gray-200 dark:bg-dark-800" :aria-label="`放大预览 ${attachment.original_name}`" @click="openImage(attachment.id, attachment.original_name)">
                  <img v-if="imageURLs[attachment.id]" :src="imageURLs[attachment.id]" :alt="attachment.original_name" data-support-attachment class="block max-h-60 max-w-full object-contain sm:max-w-[230px]" />
                  <span v-else class="flex h-28 w-40 items-center justify-center text-[11px] text-gray-500">加载中...</span>
                </button>
              </div>
              <div
                v-if="message.content"
                class="support-bubble whitespace-pre-wrap break-words px-3 py-2 text-sm leading-5"
                :class="message.sender_role === 'user' ? 'support-bubble-out bg-primary-600 text-white dark:bg-primary-700 dark:text-white' : 'support-bubble-in text-gray-900 dark:text-dark-100'"
              >{{ message.content }}</div>
            </div>
          </article>
        </div>

        <form class="support-composer flex h-[154px] shrink-0 flex-col border-t border-gray-200 dark:border-dark-700" @submit.prevent="sendMessage">
          <div class="flex h-10 shrink-0 items-center gap-1 px-3">
            <label class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white" :class="sending ? 'pointer-events-none opacity-50' : ''" title="发送图片">
              <Icon name="photo" size="md" />
              <span class="sr-only">添加图片</span>
              <input ref="fileInput" type="file" class="hidden" multiple accept="image/jpeg,image/png,image/webp" :disabled="sending" @change="selectFiles" />
            </label>
            <span v-if="files.length" class="min-w-0 truncate text-xs text-gray-500 dark:text-dark-400">{{ files.length }} 张图片待发送</span>
            <button v-if="files.length" type="button" class="flex h-7 w-7 items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-800 dark:hover:text-white" title="清除图片" aria-label="清除图片" @click="clearFiles">
              <Icon name="x" size="sm" />
            </button>
          </div>
          <div v-if="filePreviews.length" class="flex h-14 shrink-0 gap-2 overflow-x-auto px-3 pb-2">
            <div v-for="(preview, index) in filePreviews" :key="preview.url" class="group/preview relative h-12 w-12 shrink-0 overflow-hidden rounded-md border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-800">
              <img :src="preview.url" :alt="preview.file.name" class="h-full w-full object-cover" />
              <button type="button" class="absolute right-0 top-0 flex h-5 w-5 items-center justify-center bg-black/65 text-white opacity-0 transition group-hover/preview:opacity-100 focus:opacity-100" title="移除图片" aria-label="移除图片" @click="removeFile(index)">
                <Icon name="x" size="xs" />
              </button>
            </div>
          </div>
          <div class="relative min-h-0 flex-1">
            <textarea
              ref="draftInput"
              v-model="draft"
              maxlength="10000"
              class="h-full w-full resize-none border-0 bg-transparent px-3 pb-10 pt-0 text-sm leading-5 text-gray-900 outline-none placeholder:text-gray-400 dark:text-dark-100"
              :disabled="sending"
              placeholder="输入消息，Enter 发送"
              @keydown.enter.exact="handleMessageEnter"
            ></textarea>
            <button class="support-send-button absolute bottom-2.5 right-3 rounded-md px-4 py-1.5 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-50" type="submit" :disabled="sending || (!draft.trim() && !files.length)">
              {{ sending ? '发送中...' : '发送' }}
            </button>
          </div>
        </form>
      </section>
    </transition>

    <SupportImagePreview :src="previewImageURL" :alt="previewImageName" @close="closeImagePreview" />

    <button
      class="support-launcher group relative flex items-center gap-2 rounded-full px-4 py-3 text-sm font-semibold text-white transition focus:outline-none"
      :aria-expanded="open"
      aria-label="联系客服"
      @click="toggle"
    >
      <Icon :name="open ? 'x' : 'chatBubble'" size="md" />
      <span>联系客服</span>
      <span v-if="supportStore.unreadCount && !open" class="support-unread absolute -right-1 -top-1 flex h-5 min-w-5 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-bold text-white">{{ Math.min(supportStore.unreadCount, 99) }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import SupportImagePreview from '@/components/support/SupportImagePreview.vue'
import { createSupportTicket, getSupportAttachmentBlob, getSupportTicket, listSupport, replySupportTicket, type SupportMessage, type SupportTicket } from '@/api/support'
import { useAppStore } from '@/stores/app'
import { useSupportStore } from '@/stores/support'
import { isIMECompositionKeyEvent } from '@/utils/keyboard'

const appStore = useAppStore()
const supportStore = useSupportStore()
const open = ref(false)
const loading = ref(false)
const sending = ref(false)
const draft = ref('')
const draftInput = ref<HTMLTextAreaElement>()
const files = ref<File[]>([])
const fileInput = ref<HTMLInputElement>()
const filePreviews = ref<Array<{ file: File; url: string }>>([])
const ticket = ref<SupportTicket>()
const imageURLs = ref<Record<number, string>>({})
const previewImageURL = ref('')
const previewImageName = ref('')
const messageStream = ref<HTMLElement>()
const messages = computed<SupportMessage[]>(() => ticket.value?.messages || [])

function revokeImages() {
  closeImagePreview()
  Object.values(imageURLs.value).forEach((url) => URL.revokeObjectURL(url))
  imageURLs.value = {}
}

async function loadImages() {
  for (const message of ticket.value?.messages || []) {
    for (const attachment of message.attachments || []) {
      if (imageURLs.value[attachment.id]) continue
      try {
        imageURLs.value[attachment.id] = URL.createObjectURL(await getSupportAttachmentBlob(attachment.id))
      } catch {
        // Keep the attachment placeholder when it is unavailable.
      }
    }
  }
}

async function loadConversation(silent = false) {
  if (!silent) loading.value = true
  try {
    const result = await listSupport({ page: 1, page_size: 20 })
    const latest = result.items[0]
    if (!latest) {
      revokeImages()
      ticket.value = undefined
      return
    }
    const previousID = ticket.value?.id
    ticket.value = await getSupportTicket(latest.id)
    if (previousID !== ticket.value.id) revokeImages()
    await loadImages()
    await supportStore.refreshUnread(false)
  } catch (error: any) {
    appStore.showError(error?.message || '加载客服消息失败')
  } finally {
    if (!silent) loading.value = false
    await scrollToBottom()
  }
}

async function scrollToBottom() {
  await nextTick()
  const stream = messageStream.value
  if (!stream) return
  const pendingImages = Array.from(stream.querySelectorAll<HTMLImageElement>('[data-support-attachment]')).filter((image) => !image.complete)
  await Promise.all(pendingImages.map((image) => new Promise<void>((resolve) => {
    const done = () => {
      image.removeEventListener('load', done)
      image.removeEventListener('error', done)
      resolve()
    }
    image.addEventListener('load', done, { once: true })
    image.addEventListener('error', done, { once: true })
  })))
  await nextTick()
  stream.scrollTop = stream.scrollHeight
}

function toggle() {
  open.value = !open.value
}

function selectFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const selected = Array.from(input.files || [])
  if (selected.length > 2 || selected.some((file) => file.size > 3 * 1024 * 1024)) {
    appStore.showError('最多选择 2 张图片，单张不能超过 3 MB')
    input.value = ''
    return
  }
  clearFiles()
  files.value = selected
  filePreviews.value = selected.map((file) => ({ file, url: URL.createObjectURL(file) }))
}

function clearFiles() {
  filePreviews.value.forEach((preview) => URL.revokeObjectURL(preview.url))
  filePreviews.value = []
  files.value = []
  if (fileInput.value) fileInput.value.value = ''
}

function removeFile(index: number) {
  const preview = filePreviews.value[index]
  if (preview) URL.revokeObjectURL(preview.url)
  filePreviews.value.splice(index, 1)
  files.value.splice(index, 1)
  if (!files.value.length && fileInput.value) fileInput.value.value = ''
}

function handleMessageEnter(event: KeyboardEvent) {
  if (isIMECompositionKeyEvent(event)) return
  event.preventDefault()
  void sendMessage()
}

async function sendMessage() {
  const content = draft.value.trim()
  if (sending.value || (!content && !files.value.length)) return
  sending.value = true
  try {
    const current = ticket.value
    if (!current) {
      ticket.value = await createSupportTicket({ content: content || '发送了图片', files: files.value })
    } else {
      ticket.value = await replySupportTicket(current.id, content, files.value)
    }
    draft.value = ''
    clearFiles()
    await loadImages()
    await supportStore.refreshUnread(false)
    await scrollToBottom()
  } catch (error: any) {
    appStore.showError(error?.message || '发送消息失败')
  } finally {
    sending.value = false
    await nextTick()
    if (open.value) draftInput.value?.focus({ preventScroll: true })
  }
}

function openImage(attachmentID: number, name: string) {
  const url = imageURLs.value[attachmentID]
  if (!url) return
  previewImageURL.value = url
  previewImageName.value = name
}

function closeImagePreview() {
  previewImageURL.value = ''
  previewImageName.value = ''
}

function formatTime(value: string) {
  return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

const realtime = () => {
  if (open.value) void loadConversation(true)
}

watch(open, (value) => {
  if (value) void loadConversation()
})

onMounted(() => window.addEventListener('support-realtime', realtime))
onBeforeUnmount(() => {
  window.removeEventListener('support-realtime', realtime)
  revokeImages()
  clearFiles()
})
</script>

<style scoped>
.support-chat {
  --support-canvas: var(--fv-page);
  --support-panel: var(--fv-surface-2);
  --support-incoming: var(--fv-surface);
  color: var(--fv-text);
  background: var(--support-panel);
  border-color: var(--fv-line);
  border-radius: var(--fv-radius);
  box-shadow: var(--fv-shadow);
}

.support-chat-header,
.support-composer {
  background: var(--support-panel);
  border-color: var(--fv-line-soft);
}

.support-message-stream {
  background: var(--support-canvas);
}

.support-composer textarea:focus,
.support-composer textarea:focus-visible {
  border-color: transparent;
  outline: none;
  box-shadow: none;
}

.support-bubble {
  position: relative;
  width: fit-content;
  min-width: 2.25rem;
  border-radius: 5px;
}

.support-bubble-in {
  background: var(--support-incoming);
  box-shadow: 0 1px 1px rgb(0 0 0 / 5%);
}

.support-bubble-in::before {
  position: absolute;
  top: 10px;
  left: -6px;
  border-top: 5px solid transparent;
  border-right: 7px solid var(--support-incoming);
  border-bottom: 5px solid transparent;
  content: '';
}

.support-bubble-out {
  margin-left: auto;
}

.support-send-button {
  color: var(--fv-text-soft);
  background: var(--fv-surface-3);
}

.support-send-button:hover:not(:disabled) {
  color: var(--fv-text);
  background: var(--fv-accent-soft);
}

.support-launcher {
  background: var(--fv-accent);
  box-shadow: var(--fv-shadow-accent);
}

.support-launcher:hover {
  background: var(--fv-accent-hover);
  transform: translateY(-1px);
}

.support-launcher:focus-visible {
  outline: 2px solid var(--fv-accent);
  outline-offset: 3px;
}

.support-unread {
  box-shadow: 0 0 0 2px var(--fv-page);
}

.support-widget-enter-active,
.support-widget-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
  transform-origin: bottom right;
}

.support-widget-enter-from,
.support-widget-leave-to {
  opacity: 0;
  transform: translateY(10px) scale(0.98);
}
</style>
