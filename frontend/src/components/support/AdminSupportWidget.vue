<template>
  <Teleport to="body">
    <div class="fixed bottom-4 right-4 z-[70] flex flex-col items-end gap-3">
      <transition name="admin-support-window">
        <section
          v-if="open"
          class="admin-support-window flex min-h-0 flex-col overflow-hidden border"
          :class="maximized
            ? 'fixed inset-2 rounded-md sm:inset-4'
            : 'h-[min(760px,calc(100dvh-6rem))] w-[min(1120px,calc(100vw-2rem))] rounded-md'"
          role="dialog"
          aria-modal="false"
          aria-label="客服工作台"
        >
          <header class="admin-support-window-header flex h-14 shrink-0 items-center gap-3 border-b px-3.5">
            <span class="admin-support-window-brand flex h-8 w-8 items-center justify-center rounded text-white">
              <Icon name="chatBubble" size="sm" />
            </span>
            <div class="min-w-0 flex-1">
              <h2 class="truncate text-sm font-semibold text-gray-900 dark:text-white">客服工作台</h2>
              <p class="truncate text-[11px] text-gray-500 dark:text-dark-400">用户消息与 Telegram 回复</p>
            </div>
            <span v-if="supportStore.unreadCount" class="flex h-5 min-w-5 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold text-white">
              {{ Math.min(supportStore.unreadCount, 99) }}
            </span>
            <button
              type="button"
              class="admin-support-window-action flex h-8 w-8 items-center justify-center rounded transition focus-visible:outline-none"
              aria-label="Telegram 通知"
              title="Telegram 通知"
              @click="openTelegramSettings"
            >
              <Icon name="paperPlane" size="sm" />
            </button>
            <button
              type="button"
              class="admin-support-window-action flex h-8 w-8 items-center justify-center rounded transition focus-visible:outline-none disabled:cursor-wait disabled:opacity-60"
              aria-label="刷新全部对话"
              title="刷新全部对话"
              :disabled="refreshing"
              @click="refreshConversations"
            >
              <Icon name="refresh" size="sm" :class="refreshing ? 'animate-spin' : ''" />
            </button>
            <button
              type="button"
              class="admin-support-window-action hidden h-8 w-8 items-center justify-center rounded transition focus-visible:outline-none sm:flex"
              :aria-label="maximized ? '还原客服窗口' : '全屏显示客服窗口'"
              :title="maximized ? '还原' : '放大'"
              @click="maximized = !maximized"
            >
              <Icon :name="maximized ? 'collapse' : 'expand'" size="sm" />
            </button>
            <button
              type="button"
              class="admin-support-window-action flex h-8 w-8 items-center justify-center rounded transition focus-visible:outline-none"
              aria-label="关闭客服工作台"
              title="关闭"
              @click="close"
            >
              <Icon name="x" size="sm" />
            </button>
          </header>

          <div class="min-h-0 flex-1">
            <SupportView ref="supportViewRef" />
          </div>
        </section>
      </transition>

      <button
        type="button"
        class="admin-support-launcher relative flex h-12 w-12 items-center justify-center rounded-full text-white transition focus:outline-none"
        :aria-expanded="open"
        :aria-label="open ? '关闭客服工作台' : '打开客服工作台'"
        :title="open ? '关闭客服工作台' : '客服工作台'"
        @click="toggle"
      >
        <Icon :name="open ? 'x' : 'chatBubble'" size="md" />
        <span
          v-if="supportStore.unreadCount && !open"
          class="admin-support-unread absolute -right-1 -top-1 flex h-5 min-w-5 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-bold text-white"
        >
          {{ Math.min(supportStore.unreadCount, 99) }}
        </span>
      </button>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useSupportStore } from '@/stores/support'

const SupportView = defineAsyncComponent(() => import('@/views/admin/SupportView.vue'))
const supportStore = useSupportStore()
const open = ref(false)
const maximized = ref(false)
const refreshing = ref(false)

interface SupportWorkspaceHandle {
  openTelegram: () => Promise<void>
  refreshList: () => Promise<void>
}

const supportViewRef = ref<SupportWorkspaceHandle>()

function openTelegramSettings() {
  void supportViewRef.value?.openTelegram()
}

async function refreshConversations() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await supportViewRef.value?.refreshList()
  } finally {
    refreshing.value = false
  }
}

function toggle() {
  open.value = !open.value
}

function close() {
  open.value = false
  maximized.value = false
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && open.value && !document.body.classList.contains('modal-open')) close()
}

onMounted(() => window.addEventListener('keydown', handleKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', handleKeydown))
</script>

<style scoped>
.admin-support-window {
  color: var(--fv-text);
  background: color-mix(in srgb, var(--fv-surface) 97%, transparent);
  border-color: var(--fv-line);
  border-radius: var(--fv-radius);
  box-shadow: var(--fv-shadow);
  backdrop-filter: blur(18px);
}

.admin-support-window-header {
  background: var(--fv-surface-2);
  border-color: var(--fv-line-soft);
}

.admin-support-window-brand,
.admin-support-launcher {
  background: var(--fv-accent);
}

.admin-support-window-action {
  color: var(--fv-muted);
}

.admin-support-window-action:hover {
  color: var(--fv-text);
  background: var(--fv-accent-wash);
}

.admin-support-launcher {
  box-shadow: var(--fv-shadow-accent);
}

.admin-support-launcher:hover {
  background: var(--fv-accent-hover);
  transform: translateY(-1px);
}

.admin-support-launcher:focus-visible {
  outline: 2px solid var(--fv-accent);
  outline-offset: 3px;
}

.admin-support-unread {
  box-shadow: 0 0 0 2px var(--fv-page);
}

.admin-support-window-enter-active,
.admin-support-window-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
  transform-origin: bottom right;
}

.admin-support-window-enter-from,
.admin-support-window-leave-to {
  opacity: 0;
  transform: translateY(12px) scale(0.985);
}

@media (max-width: 639px) {
  .admin-support-window {
    position: fixed;
    inset: 0.5rem;
    width: auto;
    height: auto;
  }
}
</style>
