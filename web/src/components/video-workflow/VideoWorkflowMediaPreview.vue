<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Close,
  Loading,
  Picture,
  Refresh,
  VideoCamera,
} from '@element-plus/icons-vue'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import { videoWorkflowModelLabel } from '@/utils/videoWorkflowGraph'

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

const dialog = ref<HTMLElement | null>(null)
const video = ref<HTMLVideoElement | null>(null)
const mediaLoading = ref(false)
const mediaFailed = ref(false)
const mediaKey = ref(0)
const imageScale = ref(1)
let previousFocus: HTMLElement | null = null
let previousBodyOverflow = ''
let bodyLocked = false

const source = computed(() => props.src || '')
const kind = computed<'image' | 'video'>(() => props.node?.type === 'video' ? 'video' : 'image')
const title = computed(() => props.node?.title || props.node?.config?.title || props.node?.id || '媒体预览')
const typeLabel = computed(() => kind.value === 'video' ? '视频节点' : '图片节点')
const busy = computed(() => Boolean(props.loading || mediaLoading.value))
const failureMessage = computed(() => props.error || (mediaFailed.value ? '媒体加载失败，请重新获取预览地址。' : ''))
const empty = computed(() => !props.loading && !source.value && !failureMessage.value)
const metaLabel = computed(() => kind.value === 'video'
  ? `${videoWorkflowModelLabel(props.node?.config?.model)} · ${Number(props.node?.duration_seconds || 15).toFixed(3)}s · 30 fps`
  : `${props.node?.scene_id?.replace('scene_', 'S') || '全局'} · 图片预览`)

function close() {
  emit('update:modelValue', false)
}

function lockBody() {
  if (bodyLocked) return
  bodyLocked = true
  previousBodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
}

function unlockBody() {
  if (!bodyLocked) return
  bodyLocked = false
  document.body.style.overflow = previousBodyOverflow
}

function resetMedia() {
  mediaKey.value += 1
  mediaFailed.value = false
  mediaLoading.value = Boolean(source.value && !props.loading)
  imageScale.value = 1
}

function mediaReady() {
  mediaLoading.value = false
  mediaFailed.value = false
}

function handleMediaError() {
  mediaLoading.value = false
  mediaFailed.value = true
}

function retry() {
  mediaFailed.value = false
  mediaLoading.value = true
  emit('retry')
}

function setImageScale(value: number) {
  imageScale.value = Math.min(4, Math.max(.25, Math.round(value * 4) / 4))
}

function zoomImage(direction: 1 | -1) {
  setImageScale(imageScale.value + direction * .25)
}

function handleImageWheel(event: WheelEvent) {
  zoomImage(event.deltaY < 0 ? 1 : -1)
}

function focusableElements() {
  if (!dialog.value) return []
  return Array.from(dialog.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), video[controls], [href], [tabindex]:not([tabindex="-1"])',
  )).filter((element) => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true')
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (!props.modelValue) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close()
    return
  }
  if (event.key !== 'Tab') return
  const focusable = focusableElements()
  if (!focusable.length) {
    event.preventDefault()
    dialog.value?.focus()
    return
  }
  const first = focusable[0]
  const last = focusable.at(-1)!
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

watch(() => props.modelValue, async (visible) => {
  if (visible) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    lockBody()
    resetMedia()
    await nextTick()
    dialog.value?.focus()
    return
  }
  video.value?.pause()
  unlockBody()
  previousFocus?.focus()
  previousFocus = null
}, { immediate: true })

watch(source, async () => {
  if (!props.modelValue) return
  resetMedia()
  await nextTick()
  if (kind.value === 'video' && source.value) video.value?.focus()
})

watch(() => props.loading, (loading) => {
  if (loading) mediaLoading.value = false
})

onMounted(() => document.addEventListener('keydown', handleDocumentKeydown, true))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleDocumentKeydown, true)
  video.value?.pause()
  unlockBody()
})
</script>

<template>
  <Teleport to="body">
    <Transition name="media-preview-fade">
      <section
        v-if="modelValue"
        ref="dialog"
        class="media-preview-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="video-workflow-media-preview-title"
        tabindex="-1"
      >
        <header class="media-preview-header">
          <div class="media-preview-identity">
            <span class="media-preview-kind"><component :is="kind === 'video' ? VideoCamera : Picture" />{{ typeLabel }}</span>
            <div>
              <strong id="video-workflow-media-preview-title">{{ title }}</strong>
              <small>{{ node?.id }} · {{ metaLabel }}</small>
            </div>
          </div>
          <button class="media-preview-close" aria-label="关闭全屏预览" title="关闭（Esc）" @click="close"><Close /></button>
        </header>

        <main
          class="media-preview-stage"
          :class="kind"
          :aria-busy="busy"
          @wheel="kind === 'image' && handleImageWheel($event)"
          @dblclick="kind === 'image' && setImageScale(1)"
        >
          <div class="media-preview-glow" aria-hidden="true" />
          <img
            v-if="kind === 'image' && source && !mediaFailed && !error"
            :key="`image-${mediaKey}`"
            :src="source"
            :alt="title"
            :style="{ transform: `scale(${imageScale})` }"
            draggable="false"
            @load="mediaReady"
            @error="handleMediaError"
          />
          <video
            v-else-if="kind === 'video' && source && !mediaFailed && !error"
            :key="`video-${mediaKey}`"
            ref="video"
            :src="source"
            controls
            playsinline
            preload="metadata"
            :aria-label="`${title} 视频预览`"
            @loadedmetadata="mediaReady"
            @loadeddata="mediaReady"
            @error="handleMediaError"
          />

          <div v-if="busy && !failureMessage" class="media-preview-loading" role="status">
            <Loading /><span>{{ loading ? '正在获取最新预览地址…' : `正在载入${kind === 'video' ? '视频' : '图片'}…` }}</span>
          </div>
          <div v-if="failureMessage || empty" class="media-preview-error" role="alert">
            <component :is="kind === 'video' ? VideoCamera : Picture" />
            <strong>{{ failureMessage ? '预览加载失败' : '当前节点暂无可预览内容' }}</strong>
            <span>{{ failureMessage || '请先运行节点，或为图片节点选择可用素材。' }}</span>
            <button v-if="failureMessage" @click="retry"><Refresh />重新获取</button>
          </div>
          <div v-if="node?.status === 'stale'" class="media-preview-stale" role="status">当前预览来自旧版本，重新运行节点后更新</div>
        </main>

        <footer class="media-preview-footer">
          <span><i />全屏节点预览</span>
          <div v-if="kind === 'image'" class="media-preview-zoom" aria-label="图片缩放控制">
            <button aria-label="缩小图片" :disabled="imageScale <= .25" @click="zoomImage(-1)">−</button>
            <button aria-label="恢复图片为 100%" @click="setImageScale(1)">{{ Math.round(imageScale * 100) }}%</button>
            <button aria-label="放大图片" :disabled="imageScale >= 4" @click="zoomImage(1)">＋</button>
          </div>
          <p v-else>使用播放器控制条播放、暂停、调节音量与进度</p>
          <kbd>Esc</kbd><small>关闭</small>
        </footer>
      </section>
    </Transition>
  </Teleport>
</template>

<style scoped lang="scss">
.media-preview-dialog {
  position: fixed;
  z-index: 5000;
  inset: 0;
  display: grid;
  grid-template-rows: 64px minmax(0, 1fr) 52px;
  color: #f8fafc;
  background: #080b10;
  outline: 0;
}
.media-preview-header {
  position: relative;
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px 0 24px;
  background: rgba(13, 17, 24, .96);
  border-bottom: 1px solid rgba(148, 163, 184, .2);
  box-shadow: 0 8px 28px rgba(0, 0, 0, .28);
}
.media-preview-identity { min-width: 0; display: flex; align-items: center; gap: 14px; }
.media-preview-identity > div { min-width: 0; display: grid; gap: 3px; }
.media-preview-identity strong { overflow: hidden; color: #fff; font-size: 15px; font-weight: 680; text-overflow: ellipsis; white-space: nowrap; }
.media-preview-identity small { overflow: hidden; color: #8d99a8; font: 10px "SFMono-Regular", Consolas, monospace; text-overflow: ellipsis; white-space: nowrap; }
.media-preview-kind { height: 28px; display: inline-flex; align-items: center; gap: 6px; padding: 0 9px; color: #bfdbfe; background: rgba(37, 99, 235, .16); border: 1px solid rgba(96, 165, 250, .38); border-radius: 5px; font-size: 10px; white-space: nowrap; }
.media-preview-kind svg { width: 14px; }
.media-preview-close { width: 36px; height: 36px; display: grid; place-items: center; color: #cbd5e1; background: rgba(255, 255, 255, .04); border: 1px solid rgba(148, 163, 184, .2); border-radius: 6px; cursor: pointer; }
.media-preview-close:hover { color: #fff; background: #dc2626; border-color: #ef4444; }.media-preview-close svg { width: 17px; }
.media-preview-stage {
  position: relative;
  min-width: 0;
  min-height: 0;
  display: grid;
  place-items: center;
  overflow: hidden;
  padding: clamp(16px, 3vw, 42px);
  isolation: isolate;
  background-color: #0a0e14;
  background-image: radial-gradient(circle, rgba(100, 116, 139, .26) 1px, transparent 1px);
  background-size: 24px 24px;
}
.media-preview-glow { position: absolute; z-index: -1; width: 70vw; height: 70vh; background: radial-gradient(ellipse, rgba(37, 99, 235, .14), transparent 68%); filter: blur(28px); pointer-events: none; }
.media-preview-stage img, .media-preview-stage video { position: relative; z-index: 1; display: block; max-width: 100%; max-height: 100%; object-fit: contain; background: #030507; border: 1px solid rgba(148, 163, 184, .24); border-radius: 7px; box-shadow: 0 28px 80px rgba(0, 0, 0, .55); }
.media-preview-stage img { width: auto; height: auto; transition: transform .12s ease; user-select: none; }
.media-preview-stage video { width: min(100%, 1440px); height: 100%; }
.media-preview-loading, .media-preview-error { position: absolute; z-index: 2; display: grid; place-items: center; text-align: center; }
.media-preview-loading { gap: 10px; color: #cbd5e1; font-size: 11px; }.media-preview-loading svg { width: 24px; color: #60a5fa; animation: media-preview-spin 1s linear infinite; }
.media-preview-error { max-width: 360px; gap: 9px; padding: 26px 30px; color: #94a3b8; background: rgba(15, 23, 42, .86); border: 1px solid rgba(148, 163, 184, .24); border-radius: 8px; box-shadow: 0 18px 50px rgba(0, 0, 0, .4); }
.media-preview-error > svg { width: 34px; color: #64748b; }.media-preview-error strong { color: #e2e8f0; font-size: 14px; }.media-preview-error span { font-size: 10px; line-height: 1.6; }
.media-preview-error button { height: 32px; display: flex; align-items: center; gap: 6px; margin-top: 4px; padding: 0 12px; color: #fff; background: #2563eb; border: 0; border-radius: 5px; cursor: pointer; font-size: 10px; }.media-preview-error button svg { width: 13px; }
.media-preview-stale { position: absolute; z-index: 3; top: 16px; left: 50%; transform: translateX(-50%); padding: 6px 10px; color: #fef3c7; background: rgba(120, 53, 15, .92); border: 1px solid rgba(245, 158, 11, .55); border-radius: 5px; font-size: 10px; }
.media-preview-footer { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; align-items: center; gap: 8px; padding: 0 24px; color: #7f8b99; background: #0d1118; border-top: 1px solid rgba(148, 163, 184, .16); font-size: 10px; }
.media-preview-footer > span { display: inline-flex; align-items: center; gap: 7px; color: #cbd5e1; }.media-preview-footer i { width: 7px; height: 7px; background: #22c55e; border-radius: 50%; box-shadow: 0 0 0 3px rgba(34, 197, 94, .12); }
.media-preview-footer p { margin: 0; text-align: center; }.media-preview-footer kbd { padding: 3px 6px; color: #e2e8f0; background: #1d2530; border: 1px solid #3b4654; border-radius: 4px; font: 9px "SFMono-Regular", Consolas, monospace; }.media-preview-footer small { font-size: 9px; }
.media-preview-zoom { justify-self: center; display: flex; align-items: center; gap: 4px; }
.media-preview-zoom button { min-width: 32px; height: 28px; padding: 0 8px; color: #dbeafe; background: #18202b; border: 1px solid #354152; border-radius: 4px; cursor: pointer; font: 10px "SFMono-Regular", Consolas, monospace; }
.media-preview-zoom button:nth-child(2) { min-width: 58px; }.media-preview-zoom button:disabled { opacity: .35; cursor: default; }
.media-preview-fade-enter-active, .media-preview-fade-leave-active { transition: opacity .18s ease; }.media-preview-fade-enter-active .media-preview-stage, .media-preview-fade-leave-active .media-preview-stage { transition: transform .18s ease; }
.media-preview-fade-enter-from, .media-preview-fade-leave-to { opacity: 0; }.media-preview-fade-enter-from .media-preview-stage, .media-preview-fade-leave-to .media-preview-stage { transform: scale(.985); }
button:focus-visible, video:focus-visible { outline: 2px solid #60a5fa; outline-offset: 2px; }
@keyframes media-preview-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { transition: none !important; animation: none !important; } }
</style>
