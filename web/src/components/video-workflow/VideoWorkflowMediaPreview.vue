<script setup lang="ts">
import { computed } from 'vue'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import ImagePreviewDialog from '@/components/ImagePreviewDialog.vue'
import VideoPreviewDialog from '@/components/VideoPreviewDialog.vue'

const props = defineProps<{
  modelValue: boolean
  node: VideoWorkflowNode | null
  src: string
  loading?: boolean
  error?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  retry: []
}>()

const title = computed(() => props.node?.title || props.node?.config?.title || props.node?.id || '媒体预览')
const isVideo = computed(() => props.node?.type === 'video')
</script>

<template>
  <VideoPreviewDialog
    v-if="isVideo"
    :model-value="modelValue"
    :src="src"
    :title="title"
    :loading="loading"
    :error="error"
    @update:model-value="emit('update:modelValue', $event)"
    @retry="emit('retry')"
  />
  <ImagePreviewDialog
    v-else
    :model-value="modelValue"
    :src="src"
    :original-src="src"
    :title="title"
    :alt="title"
    :loading="loading"
    :error="error"
    @update:model-value="emit('update:modelValue', $event)"
    @retry="emit('retry')"
  />
</template>
