<template>
  <AppLayout>
    <div class="flex h-[calc(100dvh-6rem)] min-h-0 flex-col overflow-hidden lg:h-[calc(100dvh-8rem)]">
      <section class="chat-workspace flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-gray-200 shadow-sm dark:border-dark-700 lg:flex-row">
        <aside class="chat-contact-panel flex h-[238px] min-h-0 w-full shrink-0 flex-col border-b border-gray-200 dark:border-dark-700 lg:h-auto lg:w-[318px] lg:border-b-0 lg:border-r">
          <div class="flex h-16 shrink-0 items-center border-b border-gray-200 px-3 dark:border-dark-700">
            <div class="flex h-9 min-w-0 flex-1 overflow-hidden rounded-md bg-white ring-1 ring-inset ring-gray-200 transition focus-within:ring-2 focus-within:ring-primary-500 dark:bg-dark-900 dark:ring-dark-700">
              <div class="relative min-w-0 flex-1">
                <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input v-model="search" class="h-full w-full border-0 bg-transparent pl-9 pr-2 text-sm text-gray-800 outline-none placeholder:text-gray-400 dark:text-dark-100" placeholder="搜索用户名或邮箱" @keyup.enter="searchUsers()" />
              </div>
              <button type="button" class="flex h-full w-9 shrink-0 items-center justify-center border-l border-gray-200 text-gray-500 transition hover:bg-gray-100 hover:text-primary-600 dark:border-dark-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-primary-400" title="Telegram 通知" aria-label="Telegram 通知" @click="openTelegram">
                <Icon name="paperPlane" size="sm" />
              </button>
              <button type="button" class="flex h-full w-9 shrink-0 items-center justify-center border-l border-gray-200 text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 dark:border-dark-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white" title="刷新全部对话" aria-label="刷新全部对话" @click="refreshList">
                <Icon name="refresh" size="sm" :class="loading || searchingUsers ? 'animate-spin' : ''" />
              </button>
            </div>
          </div>

          <div class="min-h-0 flex-1 overflow-y-auto">
            <template v-if="search.trim()">
              <div v-if="searchingUsers" class="flex h-40 items-center justify-center text-sm text-gray-500 dark:text-dark-400">正在搜索...</div>
              <div v-else-if="!userSearchResults.length" class="flex h-40 items-center justify-center px-6 text-center text-sm text-gray-500 dark:text-dark-400">未找到用户</div>
              <div v-else>
                <button
                  v-for="item in userSearchResults"
                  :key="item.user_id"
                  type="button"
                  class="group flex h-[72px] w-full items-center gap-3 border-b border-gray-100 px-3 text-left transition dark:border-dark-800"
                  :class="(item.ticket_id && selectedID === item.ticket_id) || pendingUser?.user_id === item.user_id ? 'bg-[#dedede] dark:bg-dark-700' : 'hover:bg-[#ededed] dark:hover:bg-dark-800'"
                  @click="selectSearchUser(item)"
                >
                  <span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-sm font-semibold text-white shadow-sm" :class="avatarClass(item.user_id)">{{ userInitial(item) }}</span>
                  <span class="min-w-0 flex-1">
                    <span class="flex items-center justify-between gap-2">
                      <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ item.user_email || `用户 #${item.user_id}` }}</span>
                      <span v-if="item.last_message_at" class="shrink-0 text-[11px] text-gray-400 dark:text-dark-500">{{ formatListTime(item.last_message_at) }}</span>
                    </span>
                    <span class="mt-1 block truncate text-xs text-gray-500 dark:text-dark-400">{{ item.last_message_preview || '尚无消息' }}</span>
                  </span>
                </button>
              </div>
            </template>
            <div v-else-if="loading" class="flex h-40 items-center justify-center text-sm text-gray-500 dark:text-dark-400">正在加载...</div>
            <div v-else-if="!conversationItems.length" class="flex h-40 items-center justify-center px-6 text-center text-sm text-gray-500 dark:text-dark-400">暂无会话</div>
            <div v-else>
              <button
                v-for="item in conversationItems"
                :key="item.id"
                type="button"
                class="group flex h-[72px] w-full items-center gap-3 border-b border-gray-100 px-3 text-left transition dark:border-dark-800"
                :class="selectedID === item.id ? 'bg-[#dedede] dark:bg-dark-700' : 'hover:bg-[#ededed] dark:hover:bg-dark-800'"
                @click="selectTicket(item.id)"
              >
                <span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-sm font-semibold text-white shadow-sm" :class="avatarClass(item.user_id)">{{ userInitial(item) }}</span>
                <span class="min-w-0 flex-1">
                  <span class="flex items-center justify-between gap-2">
                    <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ item.user_email || `用户 #${item.user_id}` }}</span>
                    <span class="shrink-0 text-[11px] text-gray-400 dark:text-dark-500">{{ formatListTime(item.last_message_at) }}</span>
                  </span>
                  <span class="mt-1 flex items-center justify-between gap-2">
                    <span class="min-w-0 truncate text-xs text-gray-500 dark:text-dark-400">{{ item.last_message_preview || '尚无消息' }}</span>
                    <span v-if="item.unread_count" class="flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold text-white">{{ Math.min(item.unread_count, 99) }}</span>
                  </span>
                </span>
              </button>
            </div>
          </div>

          <footer v-if="!search.trim() && result.pages > 1" class="flex shrink-0 items-center justify-between border-t border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">
            <button class="btn btn-secondary btn-sm" :disabled="page <= 1" @click="page--; load()">上一页</button>
            <span>{{ page }} / {{ result.pages }}</span>
            <button class="btn btn-secondary btn-sm" :disabled="page >= result.pages" @click="page++; load()">下一页</button>
          </footer>
        </aside>

        <main class="flex min-h-0 min-w-0 flex-1 flex-col">
          <template v-if="activeUser">
            <header class="chat-conversation-header flex h-16 shrink-0 items-center gap-3 border-b border-gray-200 px-4 dark:border-dark-700 sm:px-5">
              <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-xs font-semibold text-white shadow-sm lg:hidden" :class="avatarClass(activeUser.user_id)">{{ userInitial(activeUser) }}</span>
              <div class="min-w-0 flex-1">
                <h2 class="flex min-w-0 items-center gap-2 text-[17px] font-semibold text-gray-900 dark:text-white">
                  <span class="truncate">{{ activeUser.user_email || `用户 #${activeUser.user_id}` }}</span>
                  <span class="shrink-0 rounded bg-primary-600 px-1.5 py-0.5 text-[11px] font-medium leading-4 text-white dark:bg-primary-700">ID: {{ activeUser.user_id }}</span>
                </h2>
              </div>
              <button v-if="ticket" type="button" class="flex h-9 w-9 items-center justify-center rounded-md text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white" title="刷新当前对话" aria-label="刷新当前对话" @click="loadTicket(ticket.id)">
                <Icon name="refresh" size="sm" :class="detailLoading ? 'animate-spin' : ''" />
              </button>
            </header>

            <div ref="messageStream" class="chat-message-stream min-h-0 flex-1 space-y-5 overflow-y-auto px-3 py-5 sm:px-6 lg:px-8">
              <div v-if="detailLoading" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-dark-400">正在加载...</div>
              <div v-else-if="!messages.length" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-dark-400">暂无消息</div>
              <article v-for="message in messages" v-else :key="message.id" class="flex items-start gap-2.5" :class="message.sender_role === 'admin' ? 'justify-end' : 'justify-start'">
                <span v-if="message.sender_role !== 'admin'" class="mt-[18px] flex h-10 w-10 shrink-0 items-center justify-center rounded-md text-xs font-semibold text-white shadow-sm" :class="avatarClass(activeUser.user_id)">{{ userInitial(activeUser) }}</span>
                <div :class="message.sender_role === 'admin' ? 'max-w-[84%] sm:max-w-[74%]' : 'max-w-[calc(100%_-_3.25rem)] sm:max-w-[74%]'">
                  <div class="mb-1 text-[11px] text-gray-400 dark:text-dark-500" :class="message.sender_role === 'admin' ? 'text-right' : ''">
                    {{ formatTime(message.created_at) }}
                  </div>
                  <div v-if="message.attachments?.length" class="mb-2 flex flex-col gap-2" :class="message.sender_role === 'admin' ? 'items-end' : 'items-start'">
                    <div v-for="attachment in message.attachments" :key="attachment.id" class="max-w-full overflow-hidden rounded-md bg-gray-200 dark:bg-dark-800">
                      <button type="button" class="block max-w-full cursor-zoom-in" :aria-label="`放大预览 ${attachment.original_name}`" @click="openImage(attachment.id, attachment.original_name)">
                        <img v-if="imageURLs[attachment.id]" :src="imageURLs[attachment.id]" :alt="attachment.original_name" data-support-attachment class="block max-h-[320px] max-w-full object-contain sm:max-w-[280px]" />
                        <span v-else class="flex h-32 w-48 items-center justify-center text-xs text-gray-500">加载图片...</span>
                      </button>
                    </div>
                  </div>
                  <div
                    v-if="message.content"
                    class="chat-bubble whitespace-pre-wrap break-words px-3.5 py-2.5 text-sm leading-6"
                    :class="message.sender_role === 'admin' ? 'chat-bubble-out bg-primary-600 text-white dark:bg-primary-700 dark:text-white' : 'chat-bubble-in text-gray-900 dark:text-dark-100'"
                  >{{ message.content }}</div>
                </div>
              </article>
            </div>

            <form class="chat-composer flex h-[176px] shrink-0 flex-col border-t border-gray-200 dark:border-dark-700" @submit.prevent="sendReply">
              <div class="flex h-11 shrink-0 items-center gap-1 px-3 sm:px-4">
                <label class="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-gray-600 transition hover:bg-gray-100 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white" :class="sending ? 'pointer-events-none opacity-50' : ''" title="发送图片">
                  <Icon name="photo" size="md" />
                  <span class="sr-only">添加图片</span>
                  <input ref="fileInput" type="file" class="hidden" multiple accept="image/jpeg,image/png,image/webp" :disabled="sending" @change="selectFiles" />
                </label>
                <span v-if="files.length" class="text-xs text-gray-500 dark:text-dark-400">{{ files.length }} 张图片待发送</span>
                <button v-if="files.length" type="button" class="ml-1 flex h-7 w-7 items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-800 dark:hover:text-white" title="清除图片" aria-label="清除图片" @click="clearFiles">
                  <Icon name="x" size="sm" />
                </button>
              </div>
              <div v-if="filePreviews.length" class="flex h-14 shrink-0 gap-2 overflow-x-auto px-4 pb-2">
                <div v-for="(preview, index) in filePreviews" :key="preview.url" class="group/preview relative h-12 w-12 shrink-0 overflow-hidden rounded-md border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-800">
                  <img :src="preview.url" :alt="preview.file.name" class="h-full w-full object-cover" />
                  <button type="button" class="absolute right-0 top-0 flex h-5 w-5 items-center justify-center bg-black/65 text-white opacity-0 transition group-hover/preview:opacity-100 focus:opacity-100" title="移除图片" aria-label="移除图片" @click="removeFile(index)">
                    <Icon name="x" size="xs" />
                  </button>
                </div>
              </div>
              <div class="relative min-h-0 flex-1">
                <textarea v-model="reply" maxlength="10000" class="h-full w-full resize-none border-0 bg-transparent px-4 pb-12 pt-1 text-sm leading-6 text-gray-900 outline-none placeholder:text-gray-400 dark:text-dark-100" placeholder="输入消息，Enter 发送" :disabled="sending" @keydown.enter.exact.prevent="sendReply"></textarea>
                <button class="absolute bottom-3 right-4 rounded-md bg-[#e9e9e9] px-5 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-[#dcdcdc] disabled:cursor-not-allowed disabled:opacity-50 dark:bg-dark-700 dark:text-dark-100 dark:hover:bg-dark-600" type="submit" :disabled="sending || (!reply.trim() && !files.length)">
                  {{ sending ? '发送中...' : '发送' }}
                </button>
              </div>
            </form>
          </template>
          <div v-else class="flex flex-1 flex-col items-center justify-center px-6 text-center text-gray-500 dark:text-dark-400">
            <span class="flex h-14 w-14 items-center justify-center rounded-md bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-dark-300"><Icon name="chatBubble" size="lg" /></span>
            <p class="mt-4 text-sm font-medium text-gray-800 dark:text-dark-100">选择一个用户开始对话</p>
          </div>
        </main>
      </section>
    </div>

    <SupportImagePreview :src="previewImageURL" :alt="previewImageName" @close="closeImagePreview" />

    <BaseDialog :show="showTelegram" title="Telegram 客服通知" width="wide" @close="showTelegram = false">
      <div class="space-y-6">
        <section class="space-y-4">
          <div class="flex items-center justify-between"><div><h4 class="font-medium text-gray-900 dark:text-white">通知机器人</h4><p class="text-sm text-gray-500">配置全局 Bot，并通过 webhook 接收管理员绑定。</p></div><label class="flex items-center gap-2 text-sm"><input v-model="telegramForm.enabled" type="checkbox" class="h-4 w-4" />启用</label></div>
          <div><label class="input-label">Bot Token</label><input v-model="telegramForm.bot_token" type="password" class="input" :placeholder="telegramConfig?.token_set ? '已配置，留空保持不变' : '123456:ABC...'" autocomplete="off" /></div>
          <div><label class="input-label">Webhook 公网地址</label><input v-model="telegramForm.webhook_base_url" type="url" class="input" placeholder="https://api.example.com" /><p class="mt-1 text-xs text-gray-500">必须是能访问当前 Sub2API 的 HTTPS 地址，系统会自动注册 webhook。</p></div>
          <div class="flex items-center gap-2"><button class="btn btn-primary" :disabled="telegramSaving" @click="saveTelegram">保存配置</button><span v-if="telegramConfig?.bot_username" class="text-sm text-gray-500">@{{ telegramConfig.bot_username }} · {{ telegramConfig.webhook_set ? 'Webhook 已注册' : 'Webhook 未注册' }}</span></div>
        </section>
        <section class="border-t border-gray-200 pt-5 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-2"><div><h4 class="font-medium text-gray-900 dark:text-white">我的通知绑定</h4><p class="text-sm text-gray-500">每个管理员绑定自己的 Telegram 私聊。</p></div><div class="flex gap-2"><button v-if="binding?.bound" class="btn btn-secondary" @click="testBinding">发送测试</button><button v-if="!binding?.bound" class="btn btn-primary" @click="bindTelegram">生成绑定链接</button></div></div>
          <div v-if="binding?.bound" class="mt-4 grid gap-3 sm:grid-cols-2">
            <label class="flex items-center gap-2 text-sm"><input v-model="binding.enabled" type="checkbox" @change="saveBinding" />启用通知</label>
            <label class="flex items-center gap-2 text-sm"><input v-model="binding.notify_new_ticket" type="checkbox" @change="saveBinding" />新消息</label>
            <label class="flex items-center gap-2 text-sm"><input v-model="binding.notify_user_reply" type="checkbox" @change="saveBinding" />用户回复</label>
            <div class="flex items-center justify-between text-sm text-gray-500 sm:col-span-2"><span>已绑定 @{{ binding.telegram_username || 'Telegram 用户' }}</span><button class="text-red-600 hover:underline" @click="unbindTelegram">解除绑定</button></div>
            <p v-if="binding.last_error" class="text-sm text-red-600 sm:col-span-2">最近错误：{{ binding.last_error }}</p>
          </div>
          <p v-else class="mt-4 text-sm text-gray-500">尚未绑定。保存并启用机器人后生成链接，在 Telegram 中点击 Start 即可。</p>
        </section>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import SupportImagePreview from '@/components/support/SupportImagePreview.vue'
import { getSupportAttachmentBlob, type SupportMessage, type SupportTicket, type SupportListResult } from '@/api/support'
import { getAdminSupportTicket, listAdminSupport, replyAdminSupportTicket, searchAdminSupportUsers, startAdminSupportConversation, getTelegramConfig, saveTelegramConfig, getTelegramBinding, createTelegramBindLink, updateTelegramBinding, deleteTelegramBinding, testTelegramBinding, type SupportUserSearchItem, type TelegramBinding, type TelegramConfig } from '@/api/admin/support'
import { useAppStore } from '@/stores/app'
import { useSupportStore } from '@/stores/support'

const appStore = useAppStore()
const supportStore = useSupportStore()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const detailLoading = ref(false)
const page = ref(1)
const search = ref('')
const searchingUsers = ref(false)
const userSearchResults = ref<SupportUserSearchItem[]>([])
const pendingUser = ref<SupportUserSearchItem>()
const result = reactive<SupportListResult>({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
const selectedID = ref<number>()
const ticket = ref<SupportTicket>()
const reply = ref('')
const sending = ref(false)
const files = ref<File[]>([])
const fileInput = ref<HTMLInputElement>()
const filePreviews = ref<Array<{ file: File; url: string }>>([])
const imageURLs = ref<Record<number, string>>({})
const previewImageURL = ref('')
const previewImageName = ref('')
const messageStream = ref<HTMLElement>()
const messages = computed<SupportMessage[]>(() => ticket.value?.messages || [])
const activeUser = computed(() => ticket.value ? {
  user_id: ticket.value.user_id,
  user_name: ticket.value.user_name || '',
  user_email: ticket.value.user_email || ''
} : pendingUser.value)
const conversationItems = computed<SupportTicket[]>(() => result.items)

const showTelegram = ref(false)
const telegramSaving = ref(false)
const telegramConfig = ref<TelegramConfig>()
const binding = ref<TelegramBinding>()
const telegramForm = reactive({ enabled: false, bot_token: '', webhook_base_url: '' })

function requestedUserID() {
  const raw = Array.isArray(route.query.user_id) ? route.query.user_id[0] : route.query.user_id
  const value = Number(raw)
  return Number.isFinite(value) && value > 0 ? value : undefined
}

function revokeImages() {
  closeImagePreview()
  Object.values(imageURLs.value).forEach((url) => URL.revokeObjectURL(url))
  imageURLs.value = {}
}

async function loadImages() {
  for (const message of messages.value) {
    for (const attachment of message.attachments || []) {
      if (imageURLs.value[attachment.id]) continue
      try {
        imageURLs.value[attachment.id] = URL.createObjectURL(await getSupportAttachmentBlob(attachment.id, true))
      } catch {
        // Leave a placeholder when the attachment cannot be loaded.
      }
    }
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

async function loadTicket(id: number, silent = false) {
  if (!silent) detailLoading.value = true
  try {
    const previousID = ticket.value?.id
    const loaded = await getAdminSupportTicket(id)
    if (previousID !== loaded.id) revokeImages()
    ticket.value = loaded
    const preview = result.items.find((item) => item.id === id)
    if (preview) preview.unread_count = 0
    await loadImages()
    await supportStore.refreshUnread(true)
  } catch (error: any) {
    appStore.showError(error?.message || '加载对话失败')
  } finally {
    if (!silent) detailLoading.value = false
    await scrollToBottom()
  }
}

async function load(silent: boolean | Event = false) {
  const isSilent = silent === true
  if (!isSilent) loading.value = true
  try {
    Object.assign(result, await listAdminSupport({ page: page.value, page_size: 20 }))
    if (pendingUser.value) return
    const requested = requestedUserID()
    let target = result.items.find((item) => item.user_id === requested) || result.items.find((item) => item.id === selectedID.value) || result.items[0]
    if (!result.items.some((item) => item.user_id === requested) && requested && !search.value) {
      const routed = await listAdminSupport({ user_id: requested, page: 1, page_size: 1 })
      target = routed.items[0]
    }
    if (target) {
      selectedID.value = target.id
      if (String(route.query.user_id || '') !== String(target.user_id)) await router.replace({ query: { ...route.query, ticket: undefined, user_id: String(target.user_id) } })
      await loadTicket(target.id, isSilent)
    } else {
      selectedID.value = undefined
      ticket.value = undefined
      revokeImages()
    }
  } catch (error: any) {
    appStore.showError(error?.message || '加载客服消息失败')
  } finally {
    if (!isSilent) loading.value = false
  }
}

async function selectTicket(id: number, userID?: number) {
  if (selectedID.value === id && ticket.value?.id === id) return
  pendingUser.value = undefined
  selectedID.value = id
  const targetUserID = userID || result.items.find((item) => item.id === id)?.user_id
  await router.replace({ query: { ...route.query, ticket: undefined, user_id: targetUserID ? String(targetUserID) : undefined } })
  await loadTicket(id)
}

async function selectSearchUser(item: SupportUserSearchItem) {
  if (item.ticket_id) {
    pendingUser.value = undefined
    await selectTicket(item.ticket_id, item.user_id)
    return
  }
  selectedID.value = undefined
  ticket.value = undefined
  pendingUser.value = item
  revokeImages()
  await router.replace({ query: { ...route.query, ticket: undefined, user_id: undefined } })
}

let searchTimer: ReturnType<typeof setTimeout> | undefined
let searchRequest = 0

async function searchUsers(silent = false) {
  const query = search.value.trim()
  const request = ++searchRequest
  if (!query) {
    userSearchResults.value = []
    searchingUsers.value = false
    return
  }
  if (!silent) searchingUsers.value = true
  try {
    const items = await searchAdminSupportUsers(query)
    if (request === searchRequest) userSearchResults.value = items
  } catch (error: any) {
    if (request === searchRequest) appStore.showError(error?.message || '搜索用户失败')
  } finally {
    if (request === searchRequest) searchingUsers.value = false
  }
}

function refreshList() {
  if (search.value.trim()) {
    void searchUsers()
  } else {
    void load()
  }
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

async function sendReply() {
  if (!activeUser.value || sending.value || (!reply.value.trim() && !files.value.length)) return
  sending.value = true
  try {
    if (ticket.value) {
      ticket.value = await replyAdminSupportTicket(ticket.value.id, reply.value, files.value)
    } else if (pendingUser.value) {
      ticket.value = await startAdminSupportConversation(pendingUser.value.user_id, reply.value, files.value)
      pendingUser.value = undefined
      selectedID.value = ticket.value.id
      search.value = ''
      userSearchResults.value = []
      await router.replace({ query: { ...route.query, ticket: undefined, user_id: String(ticket.value.user_id) } })
    }
    reply.value = ''
    clearFiles()
    await loadImages()
    const preview = result.items.find((item) => item.id === ticket.value?.id)
    if (preview && ticket.value) preview.last_message_at = ticket.value.last_message_at
    await scrollToBottom()
    void load(true)
  } catch (error: any) {
    appStore.showError(error?.message || '发送失败')
  } finally {
    sending.value = false
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

function userInitial(item: Pick<SupportTicket, 'user_name' | 'user_id'>) {
  const value = (item.user_name || `用户${item.user_id}`).trim()
  return Array.from(value).slice(0, 2).join('')
}

const avatarPalettes = [
  'bg-[#457b9d]',
  'bg-[#7b6d8d]',
  'bg-[#4f7c68]',
  'bg-[#b06c49]',
  'bg-[#59677a]',
  'bg-[#8a5d66]'
]

function avatarClass(userID: number) {
  return avatarPalettes[Math.abs(userID) % avatarPalettes.length]
}

function formatTime(value: string) {
  return new Date(value).toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function formatListTime(value: string) {
  const date = new Date(value)
  const now = new Date()
  if (date.toDateString() === now.toDateString()) return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return date.toLocaleDateString([], { month: 'numeric', day: 'numeric' })
}

async function openTelegram() {
  showTelegram.value = true
  try {
    telegramConfig.value = await getTelegramConfig()
    binding.value = await getTelegramBinding()
    telegramForm.enabled = telegramConfig.value.enabled
    telegramForm.webhook_base_url = telegramConfig.value.webhook_base_url
  } catch (error: any) {
    appStore.showError(error?.message || '加载 Telegram 配置失败')
  }
}

async function saveTelegram() {
  telegramSaving.value = true
  try {
    telegramConfig.value = await saveTelegramConfig(telegramForm)
    telegramForm.bot_token = ''
    appStore.showSuccess('Telegram 配置已保存')
  } catch (error: any) {
    appStore.showError(error?.message || '保存失败')
  } finally {
    telegramSaving.value = false
  }
}

async function bindTelegram() {
  try {
    const link = await createTelegramBindLink()
    window.open(link.url, '_blank', 'noopener,noreferrer')
    appStore.showInfo('绑定链接 10 分钟内有效，完成后重新打开此面板查看状态')
  } catch (error: any) {
    appStore.showError(error?.message || '生成绑定链接失败')
  }
}

async function saveBinding() {
  if (!binding.value) return
  try {
    binding.value = await updateTelegramBinding({ enabled: binding.value.enabled, notify_new_ticket: binding.value.notify_new_ticket, notify_user_reply: binding.value.notify_user_reply })
  } catch (error: any) {
    appStore.showError(error?.message || '更新绑定失败')
  }
}

async function testBinding() {
  try {
    await testTelegramBinding()
    appStore.showSuccess('测试通知已发送')
  } catch (error: any) {
    appStore.showError(error?.message || '测试发送失败')
  }
}

async function unbindTelegram() {
  try {
    await deleteTelegramBinding()
    binding.value = await getTelegramBinding()
  } catch (error: any) {
    appStore.showError(error?.message || '解除绑定失败')
  }
}

const realtime = () => {
  void load(true)
  if (search.value.trim()) void searchUsers(true)
}
watch(search, (value) => {
  if (searchTimer) clearTimeout(searchTimer)
  if (!value.trim()) {
    searchRequest++
    searchingUsers.value = false
    userSearchResults.value = []
    return
  }
  searchingUsers.value = true
  searchTimer = setTimeout(() => { void searchUsers() }, 250)
})
watch(() => route.query.user_id, (value) => {
  const id = Array.isArray(value) ? Number(value[0]) : Number(value)
  if (id > 0 && id !== ticket.value?.user_id) {
    void load(true)
  }
})
onMounted(() => { void load(); window.addEventListener('support-realtime', realtime) })
onBeforeUnmount(() => {
  window.removeEventListener('support-realtime', realtime)
  if (searchTimer) clearTimeout(searchTimer)
  revokeImages()
  clearFiles()
})
</script>

<style scoped>
.chat-workspace {
  --chat-canvas: #f3f3f3;
  --chat-panel: #f7f7f7;
  --chat-surface: #ffffff;
  --chat-incoming: #ffffff;
  background: var(--chat-surface);
}

.chat-contact-panel,
.chat-conversation-header,
.chat-composer {
  background: var(--chat-panel);
}

.chat-message-stream {
  background: var(--chat-canvas);
}

.chat-bubble {
  position: relative;
  width: fit-content;
  min-width: 2.5rem;
  border-radius: 5px;
}

.chat-bubble-in {
  background: var(--chat-incoming);
  box-shadow: 0 1px 1px rgb(0 0 0 / 5%);
}

.chat-bubble-in::before {
  position: absolute;
  top: 12px;
  left: -7px;
  border-top: 6px solid transparent;
  border-right: 8px solid var(--chat-incoming);
  border-bottom: 6px solid transparent;
  content: '';
}

.chat-bubble-out {
  margin-left: auto;
}

:global(.dark) .chat-workspace {
  --chat-canvas: #171a1f;
  --chat-panel: #20242b;
  --chat-surface: #20242b;
  --chat-incoming: #2a3038;
}
</style>
