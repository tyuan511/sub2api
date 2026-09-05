<template><img v-if="url" :src="url" :alt="file.name" /></template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
const props = defineProps<{ file: File }>()
const url = ref('')
watch(() => props.file, file => {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = URL.createObjectURL(file)
}, { immediate: true })
onBeforeUnmount(() => { if (url.value) URL.revokeObjectURL(url.value) })
</script>
