<template>
  <div class="rounded-xl border border-gray-200 p-3 dark:border-dark-700">
    <div class="text-xs font-semibold uppercase tracking-wide text-gray-400">{{ label }}</div>
    <div class="mt-1 break-all font-mono text-xs text-gray-800 dark:text-gray-200">{{ value }}</div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ label: string; value: string }>()
</script>
