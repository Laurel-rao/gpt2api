<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Download, FullScreen, Refresh, RefreshLeft, RefreshRight, View, ZoomIn, ZoomOut } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'

const MIN_SCALE = 0.2
const MAX_SCALE = 6
const SCALE_STEP = 0.2

const props = withDefaults(defineProps<{
  modelValue: boolean
  src?: string
  originalSrc?: string
  title?: string
  alt?: string
  downloadName?: string
  loading?: boolean
  error?: string
  width?: string
}>(), {
  src: '',
  originalSrc: '',
  title: '图片预览',
  alt: '图片预览',
  downloadName: 'image.png',
  loading: false,
  error: '',
  width: 'min(1120px, 94vw)',
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  retry: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const activeSrc = ref('')
const usingOriginal = ref(false)
const imageLoading = ref(false)
const scale = ref(1)
const rotation = ref(0)
const offset = reactive({ x: 0, y: 0 })
const dragging = ref(false)
const dragStart = reactive({ pointerID: 0, x: 0, y: 0, originX: 0, originY: 0 })

const currentLoading = computed(() => props.loading || imageLoading.value)
const scaleLabel = computed(() => `${Math.round(scale.value * 100)}%`)
const rotationLabel = computed(() => `${((rotation.value % 360) + 360) % 360}°`)
const imageStyle = computed(() => ({
  transform: `translate3d(${offset.x}px, ${offset.y}px, 0) rotate(${rotation.value}deg) scale(${scale.value})`,
}))

watch(
  () => [props.modelValue, props.src] as const,
  ([isVisible, src]) => {
    if (!isVisible || usingOriginal.value) return
    activeSrc.value = src || ''
    imageLoading.value = Boolean(src)
  },
  { immediate: true },
)

watch(
  () => props.modelValue,
  (isVisible) => {
    if (isVisible) {
      usingOriginal.value = false
      resetTransform()
      activeSrc.value = props.src || ''
      imageLoading.value = Boolean(props.src)
      return
    }
    stopDrag()
  },
)

function clampScale(value: number) {
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, Number(value.toFixed(2))))
}

function resetTransform() {
  scale.value = 1
  rotation.value = 0
  offset.x = 0
  offset.y = 0
}

function zoom(delta: number) {
  scale.value = clampScale(scale.value + delta)
}

function rotate(delta: number) {
  rotation.value += delta
}

function showOriginal() {
  const target = props.originalSrc || props.src
  if (!target) return
  if (usingOriginal.value && activeSrc.value === target) {
    window.open(target, '_blank', 'noopener,noreferrer')
    return
  }
  usingOriginal.value = true
  if (activeSrc.value === target) {
    // 原图与当前预览地址相同，不会触发新的 img load 事件。
    imageLoading.value = false
    resetTransform()
    return
  }
  activeSrc.value = target
  imageLoading.value = true
  resetTransform()
}

function openOriginal() {
  const target = props.originalSrc || activeSrc.value || props.src
  if (!target) return
  window.open(target, '_blank', 'noopener,noreferrer')
}

function onWheel(event: WheelEvent) {
  if (!activeSrc.value) return
  zoom(event.deltaY < 0 ? SCALE_STEP : -SCALE_STEP)
}

function onPointerDown(event: PointerEvent) {
  if (!activeSrc.value) return
  dragging.value = true
  dragStart.pointerID = event.pointerId
  dragStart.x = event.clientX
  dragStart.y = event.clientY
  dragStart.originX = offset.x
  dragStart.originY = offset.y
  ;(event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId)
}

function onPointerMove(event: PointerEvent) {
  if (!dragging.value || event.pointerId !== dragStart.pointerID) return
  offset.x = dragStart.originX + event.clientX - dragStart.x
  offset.y = dragStart.originY + event.clientY - dragStart.y
}

function stopDrag() {
  dragging.value = false
}

function toggleZoom() {
  if (scale.value > 1) {
    scale.value = 1
    offset.x = 0
    offset.y = 0
    return
  }
  scale.value = 2
}

function onImageLoad() {
  imageLoading.value = false
}

function onImageError() {
  imageLoading.value = false
  ElMessage.error('图片加载失败')
  if (usingOriginal.value && props.src && activeSrc.value !== props.src) {
    usingOriginal.value = false
    activeSrc.value = props.src
  }
}

async function downloadImage() {
  const target = props.originalSrc || activeSrc.value || props.src
  if (!target) return
  try {
    const res = await fetch(target)
    if (!res.ok) throw new Error(`download failed: ${res.status}`)
    const blob = await res.blob()
    const objectURL = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectURL
    link.download = props.downloadName
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(objectURL)
  } catch (err) {
    console.error('download image preview failed:', err)
    openOriginal()
  }
}
</script>

<template>
  <el-dialog v-model="visible" :width="width" append-to-body align-center :title="title" class="image-preview-dialog">
    <div class="image-preview-shell">
      <div class="image-preview-toolbar">
        <div class="image-preview-tools">
          <el-tooltip content="载入原图" placement="top">
            <el-button :icon="View" :disabled="!originalSrc && !src" @click="showOriginal">原图</el-button>
          </el-tooltip>
          <el-tooltip content="新窗口打开" placement="top">
            <el-button :icon="FullScreen" :disabled="!originalSrc && !activeSrc && !src" @click="openOriginal" />
          </el-tooltip>
          <el-tooltip content="缩小" placement="top">
            <el-button :icon="ZoomOut" :disabled="scale <= MIN_SCALE" @click="zoom(-SCALE_STEP)" />
          </el-tooltip>
          <span class="image-preview-state">{{ scaleLabel }}</span>
          <el-tooltip content="放大" placement="top">
            <el-button :icon="ZoomIn" :disabled="scale >= MAX_SCALE" @click="zoom(SCALE_STEP)" />
          </el-tooltip>
          <el-tooltip content="向左旋转" placement="top">
            <el-button :icon="RefreshLeft" @click="rotate(-90)" />
          </el-tooltip>
          <el-tooltip content="向右旋转" placement="top">
            <el-button :icon="RefreshRight" @click="rotate(90)" />
          </el-tooltip>
          <el-tooltip content="重置" placement="top">
            <el-button :icon="Refresh" @click="resetTransform" />
          </el-tooltip>
        </div>
        <div class="image-preview-tools image-preview-tools-right">
          <span class="image-preview-mode">{{ usingOriginal ? '原图' : '预览图' }} · {{ rotationLabel }}</span>
          <el-button type="primary" :icon="Download" :disabled="!originalSrc && !activeSrc && !src" @click="downloadImage">下载</el-button>
        </div>
      </div>

      <div
        class="image-preview-stage"
        :class="{ 'is-dragging': dragging }"
        v-loading="currentLoading"
        :aria-busy="currentLoading"
        @wheel.prevent="onWheel"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="stopDrag"
        @pointercancel="stopDrag"
        @dblclick="toggleZoom"
      >
        <div v-if="activeSrc" class="image-preview-frame" :style="imageStyle">
          <img
            class="image-preview-image"
            :src="activeSrc"
            :alt="alt || title"
            draggable="false"
            @load="onImageLoad"
            @error="onImageError"
          >
        </div>
        <el-empty v-else-if="!currentLoading" :description="error || '暂无图片'">
          <el-button v-if="error" :icon="Refresh" @click.stop="emit('retry')">重新获取</el-button>
        </el-empty>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped lang="scss">
.image-preview-dialog {
  :deep(.el-dialog) {
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 32px);
    margin: 0;
  }

  :deep(.el-dialog__header) {
    flex: 0 0 auto;
  }

  :deep(.el-dialog__body) {
    flex: 1 1 auto;
    min-height: 0;
    overflow: hidden;
    padding-top: 8px;
  }
}

.image-preview-shell {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 12px;
  min-height: 0;
}

.image-preview-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  padding: 8px;
  border: 1px solid #d8e0ee;
  border-radius: 8px;
  background: #f8fafc;
}

.image-preview-tools {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.image-preview-tools :deep(.el-button) {
  min-width: 34px;
  height: 32px;
  margin-left: 0;
}

.image-preview-state,
.image-preview-mode {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 62px;
  height: 32px;
  padding: 0 10px;
  border: 1px solid #d8e0ee;
  border-radius: 6px;
  background: #fff;
  color: #334155;
  font-size: 13px;
  line-height: 1;
  white-space: nowrap;
}

.image-preview-mode {
  min-width: 92px;
  color: #64748b;
}

.image-preview-stage {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  min-height: 0;
  height: clamp(320px, calc(100vh - 230px), 680px);
  padding: 18px;
  overflow: hidden;
  border: 1px solid #d8e0ee;
  border-radius: 8px;
  background-color: #f1f5f9;
  background-image:
    linear-gradient(45deg, rgba(148, 163, 184, .22) 25%, transparent 25%),
    linear-gradient(-45deg, rgba(148, 163, 184, .22) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, rgba(148, 163, 184, .22) 75%),
    linear-gradient(-45deg, transparent 75%, rgba(148, 163, 184, .22) 75%);
  background-position: 0 0, 0 10px, 10px -10px, -10px 0;
  background-size: 20px 20px;
  cursor: grab;
  touch-action: none;
  user-select: none;
}

.image-preview-stage.is-dragging {
  cursor: grabbing;
}

.image-preview-frame {
  display: flex;
  align-items: center;
  justify-content: center;
  max-width: 100%;
  max-height: 100%;
  transform-origin: center center;
  transition: transform .12s ease;
  will-change: transform;
}

.image-preview-image {
  display: block;
  width: auto;
  height: auto;
  max-width: 100%;
  max-height: calc(clamp(320px, calc(100vh - 230px), 680px) - 36px);
  object-fit: contain;
  border-radius: 6px;
  box-shadow: 0 12px 32px rgba(15, 23, 42, .16);
}

.image-preview-stage.is-dragging .image-preview-frame {
  transition: none;
}

@media (max-width: 720px) {
  .image-preview-toolbar {
    align-items: stretch;
  }

  .image-preview-tools,
  .image-preview-tools-right {
    width: 100%;
  }

  .image-preview-tools-right {
    justify-content: space-between;
  }

  .image-preview-stage {
    height: clamp(280px, calc(100vh - 250px), 62vh);
    padding: 12px;
  }

  .image-preview-image {
    max-height: calc(clamp(280px, calc(100vh - 250px), 62vh) - 24px);
  }
}
</style>
