<template>
  <span ref="container" class="studio-thumbnail" :aria-busy="visible && !loaded && !failed">
    <img v-if="url && !failed" :src="url" :alt="alt" decoding="async" referrerpolicy="no-referrer" @load="loaded = true" @error="retry" />
    <span v-else class="thumbnail-placeholder" :role="failed ? 'img' : undefined" :aria-label="failed ? t('imageStudio.imageUnavailable') : undefined"><Icon v-if="failed" name="photo" size="sm" /></span>
  </span>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { StudioImage } from '@/api/imageStudio'
import { studioThumbnailUrl } from '@/utils/studioThumbnail'

const props = defineProps<{ image: StudioImage & { id?: string }; alt: string }>()
const { t } = useI18n()
const container = ref<HTMLElement>()
const visible = ref(false)
const loaded = ref(false)
const failed = ref(false)
const url = ref('')
let observer: IntersectionObserver | undefined
let version = 0
let retried = false
let disposed = false

async function load(force = false) {
  const current = ++version
  try {
    const source = await studioThumbnailUrl(props.image, force)
    if (!disposed && current === version) url.value = source
  } catch {
    if (!disposed && current === version) failed.value = true
  }
}
async function retry() {
  if (retried) { failed.value = true; return }
  retried = true
  const previous = url.value
  await load(true)
  // Avoid repeatedly fetching a missing public object or falling back to a 4K original.
  if (previous === url.value) failed.value = true
}
watch(() => [props.image.id, props.image.url, props.image.thumbnail_url], () => {
  version++
  loaded.value = false; failed.value = false; retried = false; url.value = ''
  if (visible.value) void load()
})
onMounted(() => {
  if (!container.value) return
  const show = () => { visible.value = true; observer?.disconnect(); void load() }
  if (typeof IntersectionObserver === 'undefined') { show(); return }
  observer = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) show()
  }, { root: container.value.closest('.studio-history'), rootMargin: '240px' })
  observer.observe(container.value)
})
onBeforeUnmount(() => { disposed = true; version++; observer?.disconnect() })
</script>

<style scoped>
.studio-thumbnail, .thumbnail-placeholder { display: block; width: 100%; height: 100%; border-radius: inherit; overflow: hidden; }
.studio-thumbnail img { display: block; width: 100%; height: 100%; object-fit: cover; }
.thumbnail-placeholder { display: grid; place-items: center; background: var(--studio-surface, var(--fv-surface-2)); color: var(--studio-muted, var(--fv-muted)); }
</style>
