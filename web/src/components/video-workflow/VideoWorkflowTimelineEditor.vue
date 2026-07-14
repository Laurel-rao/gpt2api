<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowDown, ArrowUp, Delete } from '@element-plus/icons-vue'
import type { VideoWorkflowTimelineClip } from '@/api/videoWorkflow'
import {
  VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS,
  VIDEO_WORKFLOW_SCENE_DURATION_MS,
  VIDEO_WORKFLOW_TIMELINE_STEP_MS,
  moveTimelineClip,
  updateTimelineClipTrim,
} from '@/utils/videoWorkflowGraph'

const props = withDefaults(defineProps<{
  clips: VideoWorkflowTimelineClip[]
  /** source_node_id → 展示标题 */
  sourceTitles?: Record<string, string>
}>(), {
  sourceTitles: () => ({}),
})

const emit = defineEmits<{
  'update:clips': [clips: VideoWorkflowTimelineClip[]]
}>()

const dragFrom = ref<number | null>(null)

const canDelete = computed(() => props.clips.length > 1)

const totalDurationMs = computed(() => props.clips.reduce((total, clip) => (
  total + Math.max(0, Number(clip.trim_out_ms || 0) - Number(clip.trim_in_ms || 0))
), 0))

function clipTitle(clip: VideoWorkflowTimelineClip, index: number) {
  return props.sourceTitles[clip.source_node_id]
    || clip.id
    || `片段 ${index + 1}`
}

function emitClips(next: VideoWorkflowTimelineClip[]) {
  emit('update:clips', next)
}

function moveUp(index: number) {
  if (index <= 0) return
  emitClips(moveTimelineClip(props.clips, index, index - 1))
}

function moveDown(index: number) {
  if (index >= props.clips.length - 1) return
  emitClips(moveTimelineClip(props.clips, index, index + 1))
}

function removeClip(index: number) {
  if (!canDelete.value) return
  emitClips(props.clips.filter((_, i) => i !== index))
}

function updateTrim(index: number, trimInMS: number, trimOutMS: number) {
  const clip = props.clips[index]
  if (!clip) return
  const next = updateTimelineClipTrim(clip, trimInMS, trimOutMS)
  emitClips(props.clips.map((item, i) => (i === index ? next : item)))
}

function msToSeconds(ms: number) {
  return Math.round(Number(ms || 0) / 100) / 10
}

function secondsToMS(raw: string) {
  const seconds = Number(raw)
  if (!Number.isFinite(seconds)) return 0
  return Math.round(seconds * 1000)
}

function onTrimInChange(index: number, raw: string) {
  const clip = props.clips[index]
  if (!clip) return
  updateTrim(index, secondsToMS(raw), clip.trim_out_ms)
}

function onTrimOutChange(index: number, raw: string) {
  const clip = props.clips[index]
  if (!clip) return
  updateTrim(index, clip.trim_in_ms, secondsToMS(raw))
}

function onDragStart(index: number, event: DragEvent) {
  dragFrom.value = index
  event.dataTransfer?.setData('text/plain', String(index))
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}

function onDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}

function onDrop(index: number, event: DragEvent) {
  event.preventDefault()
  const from = dragFrom.value ?? Number(event.dataTransfer?.getData('text/plain'))
  dragFrom.value = null
  if (!Number.isInteger(from) || from < 0 || from === index) return
  emitClips(moveTimelineClip(props.clips, from, index))
}

function onDragEnd() {
  dragFrom.value = null
}
</script>

<template>
  <section class="timeline-editor" aria-label="顺序时间线编辑器">
    <header class="timeline-editor-heading">
      <div>
        <strong>时间线片段</strong>
        <span>{{ clips.length }} 段 · 合计 {{ (totalDurationMs / 1000).toFixed(1) }} 秒</span>
      </div>
    </header>

    <p
      v-if="!canDelete && clips.length === 1"
      id="timeline-last-clip-hint"
      class="timeline-hint"
      role="status"
      aria-live="polite"
    >
      至少保留 1 个片段，无法删除最后一个片段
    </p>

    <div v-if="!clips.length" class="timeline-empty">
      <b>暂无片段</b>
      <span>从视频节点添加到顺序时间线</span>
    </div>

    <ol v-else class="timeline-clip-list">
      <li
        v-for="(clip, index) in clips"
        :key="clip.id"
        class="timeline-clip"
        :class="{ dragging: dragFrom === index }"
        draggable="true"
        :aria-label="`片段 ${index + 1}：${clipTitle(clip, index)}`"
        @dragstart="onDragStart(index, $event)"
        @dragover="onDragOver"
        @drop="onDrop(index, $event)"
        @dragend="onDragEnd"
      >
        <div class="clip-header">
          <b>{{ clipTitle(clip, index) }}</b>
          <code>{{ clip.source_node_id }}</code>
        </div>

        <div class="clip-actions">
          <button
            type="button"
            :disabled="index === 0"
            :aria-label="`上移片段 ${index + 1}`"
            @click="moveUp(index)"
          >
            <ArrowUp /><span>上移</span>
          </button>
          <button
            type="button"
            :disabled="index === clips.length - 1"
            :aria-label="`下移片段 ${index + 1}`"
            @click="moveDown(index)"
          >
            <ArrowDown /><span>下移</span>
          </button>
          <button
            type="button"
            class="delete-clip"
            :disabled="!canDelete"
            :aria-label="`删除片段 ${index + 1}`"
            :aria-describedby="!canDelete ? 'timeline-last-clip-hint' : undefined"
            :title="!canDelete ? '至少保留 1 个片段' : '删除片段'"
            @click="removeClip(index)"
          >
            <Delete /><span>删除</span>
          </button>
        </div>

        <div class="clip-trim" :aria-label="`片段 ${index + 1} 裁剪`">
          <label>
            入点 (秒)
            <input
              type="number"
              :value="msToSeconds(clip.trim_in_ms)"
              :min="0"
              :max="(VIDEO_WORKFLOW_SCENE_DURATION_MS - VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS) / 1000"
              :step="VIDEO_WORKFLOW_TIMELINE_STEP_MS / 1000"
              :aria-label="`片段 ${index + 1} 入点`"
              @change="onTrimInChange(index, ($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            出点 (秒)
            <input
              type="number"
              :value="msToSeconds(clip.trim_out_ms)"
              :min="VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS / 1000"
              :max="VIDEO_WORKFLOW_SCENE_DURATION_MS / 1000"
              :step="VIDEO_WORKFLOW_TIMELINE_STEP_MS / 1000"
              :aria-label="`片段 ${index + 1} 出点`"
              @change="onTrimOutChange(index, ($event.target as HTMLInputElement).value)"
            />
          </label>
          <span class="clip-duration">
            时长 {{ msToSeconds(Math.max(0, clip.trim_out_ms - clip.trim_in_ms)).toFixed(1) }} 秒
          </span>
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped lang="scss">
.timeline-editor { color: #0f172a; background: #fff; }
.timeline-editor-heading {
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 14px;
  border-bottom: 1px solid #eef2f7;
}
.timeline-editor-heading > div { min-width: 0; display: grid; gap: 2px; }
.timeline-editor-heading strong { color: #334155; font-size: 12px; }
.timeline-editor-heading span { color: #64748b; font-size: 9px; }
.timeline-hint {
  margin: 0;
  padding: 10px 14px;
  color: #b45309;
  background: #fffbeb;
  border-bottom: 1px solid #fde68a;
  font-size: 9px;
  line-height: 1.5;
}
.timeline-empty {
  min-height: 120px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 20px;
  color: #64748b;
  text-align: center;
}
.timeline-empty b { color: #334155; font-size: 12px; }
.timeline-empty span { font-size: 9px; }
.timeline-clip-list { margin: 0; padding: 0; list-style: none; }
.timeline-clip {
  padding: 12px 14px;
  border-bottom: 1px solid #eef2f7;
  background: #fff;
  cursor: grab;
}
.timeline-clip.dragging { opacity: .72; background: #f8fafc; }
.clip-header {
  min-height: 28px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
}
.clip-header b {
  overflow: hidden;
  color: #334155;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.clip-header code { color: #64748b; font-size: 8px; }
.clip-actions {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 7px;
  margin-top: 9px;
}
.clip-actions button {
  min-height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  color: #334155;
  background: #fff;
  border: 1px solid #cbd5e1;
  border-radius: 5px;
  cursor: pointer;
  font-size: 9px;
}
.clip-actions button:hover:not(:disabled) {
  color: #2563eb;
  background: #eff6ff;
  border-color: #93c5fd;
}
.clip-actions button:disabled {
  color: #94a3b8;
  background: #f8fafc;
  border-color: #e2e8f0;
  cursor: not-allowed;
}
.clip-actions button:focus-visible {
  outline: 2px solid #2563eb;
  outline-offset: 1px;
}
.clip-actions svg { width: 13px; }
.clip-actions .delete-clip:hover:not(:disabled) {
  color: #dc2626;
  background: #fef2f2;
  border-color: #fecaca;
}
.clip-trim {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 9px;
  padding: 8px;
  background: #f8fafc;
  border-radius: 5px;
}
.clip-trim label {
  display: grid;
  gap: 5px;
  color: #475569;
  font-size: 9px;
}
.clip-trim input {
  width: 100%;
  box-sizing: border-box;
  height: 34px;
  padding: 0 9px;
  color: #0f172a;
  background: #fff;
  border: 1px solid #cbd5e1;
  border-radius: 5px;
  outline: 0;
  font: inherit;
  font-size: 11px;
}
.clip-trim input:focus {
  border-color: #60a5fa;
  box-shadow: 0 0 0 2px rgba(37, 99, 235, .1);
}
.clip-duration {
  grid-column: 1 / -1;
  color: #64748b;
  font-size: 8px;
}
</style>
