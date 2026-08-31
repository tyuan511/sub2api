<template>
  <Teleport to="body">
    <Transition name="support-image-preview">
      <div
        v-if="src"
        class="fixed inset-0 z-[130] flex items-center justify-center bg-black/80 p-4 backdrop-blur-sm"
        role="dialog"
        aria-modal="true"
        :aria-label="alt || '图片预览'"
        @click.self="emit('close')"
      >
        <button
          ref="closeButton"
          type="button"
          class="absolute right-4 top-4 flex h-10 w-10 items-center justify-center rounded-md bg-black/45 text-white transition hover:bg-black/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-white/80"
          title="关闭预览"
          aria-label="关闭预览"
          @click="emit('close')"
        >
          <Icon name="x" size="lg" />
        </button>
        <img
          :src="src"
          :alt="alt || '图片预览'"
          class="max-h-[calc(100dvh-3rem)] max-w-[calc(100vw-3rem)] rounded-md object-contain shadow-2xl"
          @click.stop
        />
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  src: string
  alt?: string
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()

const closeButton = ref<HTMLButtonElement>()
let previousActiveElement: HTMLElement | null = null

function handleKeydown(event: KeyboardEvent) {
  if (props.src && event.key === 'Escape') emit('close')
}

watch(() => props.src, async (src) => {
  if (src) {
    previousActiveElement = document.activeElement as HTMLElement
    document.body.classList.add('modal-open')
    await nextTick()
    closeButton.value?.focus()
    return
  }
  document.body.classList.remove('modal-open')
  previousActiveElement?.focus()
  previousActiveElement = null
})

onMounted(() => document.addEventListener('keydown', handleKeydown))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleKeydown)
  document.body.classList.remove('modal-open')
})
</script>

<style scoped>
.support-image-preview-enter-active,
.support-image-preview-leave-active {
  transition: opacity 0.16s ease;
}

.support-image-preview-enter-from,
.support-image-preview-leave-to {
  opacity: 0;
}
</style>
