<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Close, Mute, VideoPause, VideoPlay, View } from '@element-plus/icons-vue'
import type { VideoWorkflowNode, VideoWorkflowTimelineClip } from '@/api/videoWorkflow'

const props = defineProps<{
  clips: VideoWorkflowTimelineClip[]
  nodes: VideoWorkflowNode[]
  selectedClipID?: string
}>()
const emit = defineEmits<{
  'select': [clip: VideoWorkflowTimelineClip]
  'move': [from: number, to: number]
  'trim': [clipID: string, trimInMS: number, trimOutMS: number]
  'remove': [clipID: string]
  'close': []
}>()

const playing = ref(false)
const playheadMS = ref(20_300)
const draggedIndex = ref<number | null>(null)
let playbackTimer: number | null = null
let trimSession: { clip: VideoWorkflowTimelineClip; side: 'in' | 'out'; startX: number; width: number } | null = null

const totalMS = computed(() => props.clips.reduce((sum, clip) => sum + Math.max(0, clip.trim_out_ms - clip.trim_in_ms), 0))
const totalLabel = computed(() => `${(totalMS.value / 1000).toFixed(1)}秒`)
const ticks = computed(() => Array.from({ length: 7 }, (_, index) => index * 10))
const clipNode = (clip: VideoWorkflowTimelineClip) => props.nodes.find((node) => node.id === clip.source_node_id)
const clipTitle = (clip: VideoWorkflowTimelineClip, index: number) => clipNode(clip)?.title || `场景${String(index + 1).padStart(2, '0')}`
const clipPreview = (clip: VideoWorkflowTimelineClip) => String(
  clipNode(clip)?.output?.preview_url
  || clipNode(clip)?.output?.url
  || clipNode(clip)?.config?.preview_url
  || '',
)
const clipDuration = (clip: VideoWorkflowTimelineClip) => clip.trim_out_ms - clip.trim_in_ms

watch(totalMS, (duration) => {
  if (duration <= 0 || playheadMS.value >= duration) playheadMS.value = 0
}, { immediate: true })

function togglePlayback() {
  playing.value = !playing.value
  if (playbackTimer) window.clearInterval(playbackTimer)
  playbackTimer = null
  if (!playing.value) return
  playbackTimer = window.setInterval(() => {
    playheadMS.value = Math.min(totalMS.value, playheadMS.value + 100)
    if (playheadMS.value >= totalMS.value) {
      playing.value = false
      if (playbackTimer) window.clearInterval(playbackTimer)
      playbackTimer = null
    }
  }, 100)
}

function formatTime(ms: number) {
  const seconds = Math.max(0, ms) / 1000
  return `00:${seconds.toFixed(1).padStart(4, '0')}`
}

function startTrim(event: PointerEvent, clip: VideoWorkflowTimelineClip, side: 'in' | 'out') {
  const target = (event.currentTarget as HTMLElement).parentElement
  if (!target) return
  trimSession = { clip: { ...clip }, side, startX: event.clientX, width: target.getBoundingClientRect().width }
  window.addEventListener('pointermove', moveTrim)
  window.addEventListener('pointerup', stopTrim, { once: true })
  event.preventDefault()
  event.stopPropagation()
}

function moveTrim(event: PointerEvent) {
  if (!trimSession) return
  const delta = Math.round(((event.clientX - trimSession.startX) / Math.max(1, trimSession.width) * 15_000) / 100) * 100
  if (trimSession.side === 'in') {
    const trimIn = Math.max(0, Math.min(trimSession.clip.trim_out_ms - 1_000, trimSession.clip.trim_in_ms + delta))
    emit('trim', trimSession.clip.id, trimIn, trimSession.clip.trim_out_ms)
  } else {
    const trimOut = Math.min(15_000, Math.max(trimSession.clip.trim_in_ms + 1_000, trimSession.clip.trim_out_ms + delta))
    emit('trim', trimSession.clip.id, trimSession.clip.trim_in_ms, trimOut)
  }
}

function stopTrim() {
  trimSession = null
  window.removeEventListener('pointermove', moveTrim)
}

function keyboardMove(event: KeyboardEvent, index: number) {
  if (!event.altKey || !['ArrowLeft', 'ArrowRight'].includes(event.key)) return
  event.preventDefault()
  emit('move', index, index + (event.key === 'ArrowLeft' ? -1 : 1))
}

function sourceTimeAtPlayhead(clip: VideoWorkflowTimelineClip) {
  const index = props.clips.findIndex((item) => item.id === clip.id)
  const clipStart = props.clips.slice(0, Math.max(0, index))
    .reduce((sum, item) => sum + Math.max(0, item.trim_out_ms - item.trim_in_ms), 0)
  const elapsed = Math.max(0, Math.min(clip.trim_out_ms - clip.trim_in_ms, playheadMS.value - clipStart))
  return clip.trim_in_ms + elapsed
}

function handleTimelineKeydown(event: KeyboardEvent) {
  const target = event.target as HTMLElement | null
  if (target?.matches('input, textarea, select, [contenteditable="true"]')) return
  if (event.altKey) return
  const selected = props.clips.find((clip) => clip.id === props.selectedClipID)
  if (event.key === ' ') {
    event.preventDefault()
    event.stopPropagation()
    togglePlayback()
    return
  }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault()
    event.stopPropagation()
    const amount = event.shiftKey ? 1000 : 100
    playheadMS.value = Math.max(0, Math.min(totalMS.value, playheadMS.value + (event.key === 'ArrowLeft' ? -amount : amount)))
    return
  }
  if (!selected) return
  const sourceTime = sourceTimeAtPlayhead(selected)
  if (event.key === '[') {
    event.preventDefault()
    emit('trim', selected.id, Math.min(selected.trim_out_ms - 1000, Math.max(0, sourceTime)), selected.trim_out_ms)
  }
  if (event.key === ']') {
    event.preventDefault()
    emit('trim', selected.id, selected.trim_in_ms, Math.max(selected.trim_in_ms + 1000, Math.min(15_000, sourceTime)))
  }
  if (event.key === 'Delete' || event.key === 'Backspace') {
    event.preventDefault()
    event.stopPropagation()
    emit('remove', selected.id)
  }
}

function updateInput(clip: VideoWorkflowTimelineClip, side: 'in' | 'out', value: string) {
  const ms = Math.round(Number(value) / 100) * 100
  emit('trim', clip.id, side === 'in' ? ms : clip.trim_in_ms, side === 'out' ? ms : clip.trim_out_ms)
}

defineExpose({ togglePlayback })

onBeforeUnmount(() => {
  if (playbackTimer) window.clearInterval(playbackTimer)
  window.removeEventListener('pointermove', moveTrim)
})
</script>

<template>
  <section class="workflow-timeline" aria-label="单轨视频时间线" tabindex="0" @keydown="handleTimelineKeydown">
    <header class="timeline-toolbar">
      <div class="timeline-title"><strong>时间线</strong><span>单轨剪辑</span></div>
      <button class="play-button" :title="playing ? '暂停（Space）' : '播放（Space）'" @click="togglePlayback">
        <component :is="playing ? VideoPause : VideoPlay" />
      </button>
      <span class="timecode">{{ formatTime(playheadMS) }} / {{ formatTime(totalMS) }}</span>
      <span class="timeline-hint">拖动片段排序 · 拖动两侧裁剪 · 100ms 精度</span>
      <strong class="total-duration">{{ totalLabel }}</strong>
      <button class="collapse-timeline" title="关闭时间线" aria-label="关闭时间线" @click="emit('close')"><Close /><span>关闭</span></button>
    </header>

    <div class="ruler-row">
      <span class="track-label" />
      <div class="timeline-ruler"><span v-for="tick in ticks" :key="tick">{{ tick.toString().padStart(2, '0') }}:00</span></div>
    </div>

    <div class="video-row">
      <div class="track-label"><b>V1</b><small>视频轨道</small><View /></div>
      <div class="clip-track">
        <article
          v-for="(clip, index) in clips"
          :key="clip.id"
          :class="['timeline-clip', { selected: clip.id === selectedClipID }]"
          draggable="true"
          tabindex="0"
          :aria-label="`${clipTitle(clip, index)}，${(clipDuration(clip) / 1000).toFixed(1)}秒`"
          @dragstart="draggedIndex = index"
          @dragover.prevent
          @drop="draggedIndex !== null && emit('move', draggedIndex, index); draggedIndex = null"
          @click="emit('select', clip)"
          @keydown="keyboardMove($event, index)"
        >
          <button class="trim-handle trim-in" aria-label="拖动设置入点" @pointerdown="startTrim($event, clip, 'in')" />
          <img v-if="clipPreview(clip)" :src="clipPreview(clip)" :alt="clipTitle(clip, index)" />
          <span v-else class="clip-image" :class="`scene-${index + 1}`">S{{ String(index + 1).padStart(2, '0') }}</span>
          <div><b>{{ clipTitle(clip, index) }}</b><span>{{ (clipDuration(clip) / 1000).toFixed(1) }}秒</span></div>
          <button class="remove-clip" title="从时间线移除" @click.stop="emit('remove', clip.id)"><Close /></button>
          <button class="trim-handle trim-out" aria-label="拖动设置出点" @pointerdown="startTrim($event, clip, 'out')" />
          <div class="trim-inputs" @click.stop>
            <label>入点<input :value="clip.trim_in_ms" type="number" min="0" :max="clip.trim_out_ms - 1000" step="100" @change="updateInput(clip, 'in', ($event.target as HTMLInputElement).value)" /></label>
            <label>出点<input :value="clip.trim_out_ms" type="number" :min="clip.trim_in_ms + 1000" max="15000" step="100" @change="updateInput(clip, 'out', ($event.target as HTMLInputElement).value)" /></label>
          </div>
        </article>
        <div v-if="!clips.length" class="empty-track">从视频节点添加 1–4 个片段</div>
        <div class="playhead" :style="{ left: `${Math.min(100, totalMS ? playheadMS / totalMS * 100 : 0)}%` }"><span>{{ formatTime(playheadMS) }}</span></div>
      </div>
    </div>

    <div class="audio-row">
      <div class="track-label"><b>A1</b><small>音频轨道</small><Mute /></div>
      <div class="waveform" aria-label="片段原声音轨"><i v-for="index in 52" :key="index" :style="{ height: `${4 + (index * 7) % 14}px` }" /></div>
    </div>
  </section>
</template>

<style scoped lang="scss">
.workflow-timeline { height: 100%; min-height: 0; display: grid; grid-template-rows: 40px 24px minmax(70px, 1fr) 52px; color: #0f172a; background: #fff; }
.timeline-toolbar { display: grid; grid-template-columns: 150px 32px 140px minmax(0, 1fr) 70px 56px; align-items: center; gap: 8px; padding: 0 12px; border-bottom: 1px solid #e2e8f0; }
.timeline-title { display: flex; align-items: baseline; gap: 8px; }.timeline-title strong { font-size: 13px; }.timeline-title span, .timeline-hint { color: #94a3b8; font-size: 9px; }
.play-button { width: 30px; height: 30px; display: grid; place-items: center; color: #334155; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 5px; cursor: pointer; }.play-button svg { width: 14px; }
.collapse-timeline { width: 56px; height: 30px; display: flex; align-items: center; justify-content: center; gap: 4px; color: #475569; background: #f8fafc; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; font-size: 10px; }.collapse-timeline:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.collapse-timeline svg { width: 12px; }
.timecode { color: #475569; font-family: "SFMono-Regular", Consolas, monospace; font-size: 10px; }.total-duration { color: #334155; text-align: right; font-size: 11px; }
.ruler-row, .video-row, .audio-row { display: grid; grid-template-columns: 92px minmax(0, 1fr); }
.ruler-row { border-bottom: 1px solid #e2e8f0; }.timeline-ruler { display: flex; align-items: center; }.timeline-ruler span { flex: 1; padding-left: 4px; color: #94a3b8; border-left: 1px solid #dbe2ea; font: 8px "SFMono-Regular", monospace; }
.track-label { display: grid; grid-template-columns: 30px minmax(0, 1fr) 18px; align-items: center; gap: 3px; padding: 0 8px 0 12px; border-right: 1px solid #e2e8f0; }.track-label b { font-size: 11px; }.track-label small { color: #64748b; font-size: 9px; }.track-label svg { width: 13px; color: #64748b; }
.video-row { min-height: 70px; border-bottom: 1px solid #e2e8f0; }.clip-track { position: relative; display: flex; align-items: stretch; gap: 7px; min-width: 0; padding: 7px 12px; overflow-x: auto; background: #fbfdff; }
.timeline-clip { position: relative; flex: 1 0 180px; min-width: 160px; max-width: 240px; display: grid; grid-template-columns: 68px minmax(0, 1fr) 22px; align-items: center; gap: 7px; padding: 4px 8px; color: #334155; background: #fff; border: 1px solid #cbd5e1; border-radius: 5px; cursor: grab; box-shadow: 0 2px 6px rgba(15, 23, 42, .04); }
.timeline-clip:hover { border-color: #60a5fa; }.timeline-clip.selected { border-color: #2563eb; box-shadow: 0 0 0 2px rgba(37, 99, 235, .16); }.timeline-clip:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
.timeline-clip > img, .clip-image { width: 68px; height: 48px; object-fit: cover; border-radius: 3px; }.clip-image { display: grid; place-items: center; color: #e2e8f0; background: linear-gradient(135deg, #334155, #0f172a); font-size: 11px; }.clip-image.scene-2 { background: linear-gradient(135deg, #1d4ed8, #172554); }.clip-image.scene-3 { background: linear-gradient(135deg, #475569, #1c1917); }.clip-image.scene-4 { background: linear-gradient(135deg, #7c2d12, #292524); }
.timeline-clip > div:not(.trim-inputs) { min-width: 0; display: grid; gap: 5px; }.timeline-clip div b { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.timeline-clip div span { color: #64748b; font-size: 9px; }
.remove-clip { width: 22px; height: 22px; display: grid; place-items: center; color: #94a3b8; background: transparent; border: 0; border-radius: 4px; cursor: pointer; }.remove-clip:hover { color: #dc2626; background: #fef2f2; }.remove-clip svg { width: 12px; }
.trim-handle { position: absolute; z-index: 3; top: 0; bottom: 0; width: 24px; background: transparent; border: 0; cursor: ew-resize; }.trim-handle::after { position: absolute; top: 7px; bottom: 7px; width: 5px; content: ''; background: #3b82f6; border-radius: 3px; opacity: .72; }.trim-in { left: -8px; }.trim-in::after { left: 8px; }.trim-out { right: -8px; }.trim-out::after { right: 8px; }.timeline-clip:hover .trim-handle::after, .trim-handle:focus-visible::after { opacity: 1; box-shadow: 0 0 0 2px #bfdbfe; }
.trim-inputs { position: absolute; z-index: 5; left: 8px; right: 8px; top: calc(100% + 4px); display: none; grid-template-columns: 1fr 1fr; gap: 5px; padding: 6px; background: #fff; border: 1px solid #cbd5e1; border-radius: 4px; box-shadow: 0 8px 20px rgba(15, 23, 42, .16); }.timeline-clip:focus-within .trim-inputs { display: grid; }.trim-inputs label { display: grid; gap: 2px; color: #64748b; font-size: 8px; }.trim-inputs input { width: 100%; height: 24px; box-sizing: border-box; padding: 0 3px; border: 1px solid #dbe2ea; border-radius: 3px; font-size: 9px; }
.empty-track { flex: 1; display: grid; place-items: center; color: #94a3b8; border: 1px dashed #cbd5e1; border-radius: 5px; font-size: 10px; }
.playhead { position: absolute; z-index: 6; top: 0; bottom: 0; width: 1px; pointer-events: none; background: #2563eb; }.playhead::before { position: absolute; left: -4px; top: -1px; content: ''; border: 4px solid transparent; border-top-color: #2563eb; }.playhead span { position: absolute; left: -26px; top: -25px; padding: 2px 4px; color: #fff; background: #2563eb; border-radius: 3px; font: 8px monospace; }
.audio-row { min-height: 0; }.waveform { display: flex; align-items: center; gap: 3px; padding: 0 14px; overflow: hidden; }.waveform i { flex: 1; min-width: 2px; max-width: 8px; background: #cbd5e1; border-radius: 2px; }
button:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { * { scroll-behavior: auto !important; } }
</style>
