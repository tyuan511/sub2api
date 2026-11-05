<template>
  <div class="rounded-lg bg-[color-mix(in_srgb,var(--fv-surface-2)_70%,transparent)] px-3 py-2.5">
    <div class="text-xs text-gray-500 dark:text-gray-400">{{ label }}</div>
    <div class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ value }}</div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ label: string; value: string }>()
</script>
