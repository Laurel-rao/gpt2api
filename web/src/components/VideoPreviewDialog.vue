<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Download, Refresh } from '@element-plus/icons-vue'

const props = withDefaults(defineProps<{
  modelValue: boolean
  src?: string
  title?: string
  loading?: boolean
  error?: string
  downloadable?: boolean
}>(), {
  src: '',
  title: '视频预览',
  loading: false,
  error: '',
  downloadable: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  download: []
  retry: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
const video = ref<HTMLVideoElement | null>(null)

watch(() => props.modelValue, (value) => {
  if (!value) video.value?.pause()
})
</script>

<template>
  <el-dialog v-model="visible" width="920px" append-to-body :title="title" class="asset-dialog">
    <div class="asset-dialog-body" v-loading="loading">
      <video v-if="src" ref="video" :src="src" controls playsinline preload="metadata" />
      <el-empty v-else :description="error || '暂无可预览视频'">
        <el-button v-if="error" :icon="Refresh" @click="emit('retry')">重新获取</el-button>
      </el-empty>
    </div>
    <template #footer>
      <el-button v-if="downloadable" :disabled="!src" @click="emit('download')">
        <el-icon><Download /></el-icon>
        下载
      </el-button>
      <el-button type="primary" @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.asset-dialog-body {
  min-height: 320px;
  display: grid;
  place-items: center;
  background: #0b0f14;
}

.asset-dialog-body video {
  display: block;
  width: 100%;
  max-height: 70vh;
  background: #000;
}
</style>
