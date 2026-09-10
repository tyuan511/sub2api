<template>
  <span class="studio-prompt-rich">
    <template v-for="(part, index) in parts" :key="index">
      <span v-if="part.type === 'text'">{{ part.value }}</span>
      <span v-else class="prompt-mention">@{{ name(part.index) }}</span>
    </template>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { parseStudioPrompt, studioMentionToken } from '@/utils/studioPromptMentions'
import type { StudioAsset } from '@/api/imageStudio'

type Reference = File | StudioAsset | { file: File; url: string }
const props = defineProps<{ prompt: string; references: Reference[] }>()
const parts = computed(() => parseStudioPrompt(props.prompt))
function name(index: number) {
  const reference = props.references[index - 1]
  if (!reference) return studioMentionToken(index)
  if (reference instanceof File) return reference.name
  if ('file' in reference) return reference.file.name
  return reference.filename || studioMentionToken(index)
}
</script>
