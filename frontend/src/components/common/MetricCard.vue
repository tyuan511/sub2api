<template>
  <div class="rounded-lg bg-gray-50 px-3 py-2.5 dark:bg-dark-800">
    <div class="text-xs text-gray-500 dark:text-gray-400">{{ label }}</div>
    <div class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ value }}</div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ label: string; value: string }>()
</script>
