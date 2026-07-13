<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import {
  CircleCheckFilled,
  Clock,
  Document,
  Film,
  Grid,
  Loading,
  Picture,
  User,
  VideoCamera,
  VideoPlay,
  WarningFilled,
  ZoomIn,
} from '@element-plus/icons-vue'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import { nodeStatusLabel, videoWorkflowModelLabel, videoWorkflowNodePreviewURL } from '@/utils/videoWorkflowGraph'

const props = defineProps<{
  node: VideoWorkflowNode
  selected?: boolean
  collapsedInputPortIDs?: string[]
}>()
const emit = defineEmits<{
  'preview-media': [node: VideoWorkflowNode]
}>()

const mediaNode = computed(() => ['background', 'image', 'video'].includes(props.node.type))
const previewURL = computed(() => videoWorkflowNodePreviewURL(props.node))
const status = computed(() => props.node.status || 'idle')
const visibleInputs = computed(() => (props.node.inputs || []).filter((port) => !props.collapsedInputPortIDs?.includes(port.id)))
const sharedCharacterCount = computed(() => props.collapsedInputPortIDs?.length || 0)
const statusText = computed(() => {
  if (props.node.enabled === false) return '已停用'
  if (status.value === 'stale') return '需更新'
  if (status.value === 'idle' && ['timeline', 'compose'].includes(props.node.type)) return '待生成'
  return nodeStatusLabel(status.value)
})
const icon = computed(() => ({
  story_brief: Document,
  character: User,
  script: Grid,
  scene: Grid,
  background: Picture,
  image: Picture,
  video: VideoCamera,
  timeline: Clock,
  compose: Film,
} as Record<string, any>)[props.node.type] || Document)
const statusIcon = computed(() => ({
  succeeded: CircleCheckFilled,
  running: Loading,
  queued: Loading,
  stale: WarningFilled,
  failed: WarningFilled,
} as Record<string, any>)[status.value] || Clock)
const nodeTitle = computed(() => props.node.title || props.node.config?.title || props.node.config?.name || props.node.id)
const summary = computed(() => {
  if (props.node.type === 'video') return `${videoWorkflowModelLabel(props.node.config?.model)} · 15 秒`
  if (props.node.type === 'compose') return 'H.264 · 30fps · AAC'
  if (props.node.type === 'timeline') return `${props.node.config?.clip_node_ids?.length || 0} 个片段 · 单轨`
  return props.node.config?.prompt || statusText.value
})
</script>

<template>
  <article
    :class="['workflow-node-card', node.type, status, { selected, media: mediaNode, collapsed: node.collapsed, disabled: node.enabled === false }]"
    :aria-label="`${nodeTitle}，${statusText}`"
    tabindex="0"
  >
    <Handle
      v-for="(port, index) in visibleInputs"
      :id="port.id"
      :key="`input-${port.id}`"
      type="target"
      :position="Position.Left"
      :style="{ top: `${48 + index * 24}px` }"
      class="node-port input"
      :title="`${port.label || port.id} · ${port.type}`"
    />
    <Handle
      v-if="sharedCharacterCount"
      id="__shared_characters"
      type="target"
      :position="Position.Left"
      :connectable="false"
      :style="{ top: `${48 + visibleInputs.length * 24}px` }"
      class="node-port input shared-character-port"
      :title="`公共角色资产 · ${sharedCharacterCount} 个角色`"
    />
    <Handle
      v-for="(port, index) in node.outputs || []"
      :id="port.id"
      :key="`output-${port.id}`"
      type="source"
      :position="Position.Right"
      :style="{ top: `${48 + index * 24}px` }"
      class="node-port output"
      :title="`${port.label || port.id} · ${port.type}`"
    />

    <header>
      <span class="node-icon"><component :is="icon" /></span>
      <strong>{{ nodeTitle }}</strong>
      <span v-if="node.locked" class="node-lock" title="节点已锁定">锁定</span>
      <component :is="statusIcon" :class="['status-icon', status]" />
    </header>

    <template v-if="!node.collapsed">
      <div v-if="mediaNode" class="node-media">
        <video v-if="previewURL && node.type === 'video'" :src="previewURL" muted playsinline preload="metadata" aria-hidden="true" />
        <img v-else-if="previewURL" :src="previewURL" :alt="nodeTitle" draggable="false" />
        <div v-else class="media-placeholder" aria-hidden="true">
          <component :is="node.type === 'video' ? VideoCamera : Picture" />
          <span>{{ node.scene_id?.replace('scene_', 'S') || '9:16' }}</span>
        </div>
        <button
          :class="['node-preview-button', 'nodrag', 'nopan', 'nowheel', { video: node.type === 'video' }]"
          type="button"
          :aria-label="`全屏预览：${nodeTitle}`"
          :title="`全屏预览：${nodeTitle}`"
          @pointerdown.stop
          @keydown.stop
          @keyup.stop
          @click.stop="emit('preview-media', node)"
        >
          <component :is="node.type === 'video' ? VideoPlay : ZoomIn" />
        </button>
        <em>9:16</em>
      </div>
      <p v-else>{{ summary }}</p>
      <footer>
        <span :class="['node-status', status]"><component :is="statusIcon" />{{ statusText }}</span>
        <b v-if="node.progress !== undefined">{{ node.progress }}%</b>
        <span v-else>{{ node.scene_id?.replace('scene_', 'S') || '全局' }}</span>
      </footer>
    </template>
  </article>
</template>

<style scoped lang="scss">
.workflow-node-card {
  width: 188px;
  height: 108px;
  box-sizing: border-box;
  overflow: visible;
  color: #eef2f6;
  background: linear-gradient(180deg, #242930 0%, #1d2228 100%);
  border: 1px solid #555e69;
  border-radius: 8px;
  box-shadow: 0 10px 24px rgba(0, 0, 0, .28);
  transition: border-color .18s ease, box-shadow .18s ease, transform .18s ease;
}
.workflow-node-card.media { width: 208px; height: 176px; }
.workflow-node-card.collapsed { height: 44px; }
.workflow-node-card:hover { border-color: #77818c; }
.workflow-node-card:focus-visible { outline: 2px solid #60a5fa; outline-offset: 2px; }
.workflow-node-card.selected {
  border-color: #3b82f6;
  box-shadow: 0 0 0 2px rgba(37, 99, 235, .42), 0 12px 28px rgba(0, 0, 0, .36);
}
.workflow-node-card.stale { border-color: #f59e0b; border-style: dashed; }
.workflow-node-card.failed { border-color: #ef4444; }
.workflow-node-card.running { border-color: #38bdf8; }
.workflow-node-card.disabled { opacity: .5; filter: grayscale(.45); }
header {
  height: 43px;
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 7px;
  padding: 0 10px;
  border-bottom: 1px solid rgba(148, 163, 184, .16);
}
.node-icon { width: 22px; height: 22px; display: grid; place-items: center; color: #dbeafe; }
.node-icon :deep(svg) { width: 17px; height: 17px; }
header strong { overflow: hidden; color: #f8fafc; font-size: 12px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.node-lock { color: #94a3b8; font-size: 9px; }
.status-icon { width: 14px; height: 14px; color: #94a3b8; }
.status-icon.succeeded { color: #22c55e; }
.status-icon.running, .status-icon.queued { color: #38bdf8; animation: node-spin 1.1s linear infinite; }
.status-icon.stale { color: #f59e0b; }
.status-icon.failed { color: #ef4444; }
p {
  height: 31px;
  margin: 7px 10px 5px;
  overflow: hidden;
  color: #aeb8c4;
  font-size: 10px;
  line-height: 15px;
}
.node-media { position: relative; height: 96px; margin: 0 10px 5px; overflow: hidden; background: #111318; border-radius: 5px; }
.node-media img, .node-media video { width: 100%; height: 100%; object-fit: cover; pointer-events: none; transition: transform .18s ease; }
.node-preview-button {
  position: absolute; z-index: 2; left: 50%; top: 50%; width: 36px; height: 36px;
  display: grid; place-items: center; padding: 0; color: #fff;
  background: rgba(15, 23, 42, .88); border: 1px solid rgba(255, 255, 255, .7); border-radius: 50%;
  opacity: 0; cursor: pointer; transform: translate(-50%, -50%) scale(.92);
  transition: opacity .18s ease, background .18s ease, border-color .18s ease, transform .18s ease;
}
.node-preview-button.video, .node-media:hover .node-preview-button, .node-media:focus-within .node-preview-button { opacity: 1; transform: translate(-50%, -50%) scale(1); }
.node-preview-button:hover { background: #2563eb; border-color: #bfdbfe; }
.node-preview-button:focus-visible { outline: 2px solid #93c5fd; outline-offset: 2px; }
.node-preview-button svg { width: 16px; height: 16px; }
.media-placeholder {
  width: 100%; height: 100%; display: grid; place-items: center;
  color: #8492a2;
  background:
    radial-gradient(circle at 28% 25%, rgba(59, 130, 246, .28), transparent 35%),
    linear-gradient(145deg, #222b36, #14191f);
}
.media-placeholder svg { width: 28px; }
.media-placeholder span { position: absolute; left: 8px; bottom: 5px; font-size: 9px; }
.node-media em { position: absolute; right: 5px; bottom: 5px; padding: 2px 4px; color: #e2e8f0; background: rgba(15, 23, 42, .8); border-radius: 3px; font-size: 9px; font-style: normal; }
footer { height: 22px; display: flex; align-items: center; justify-content: space-between; padding: 0 10px; color: #9aa6b2; font-size: 9px; }
.media footer { display: none; }
.node-status { display: inline-flex; align-items: center; gap: 4px; }
.node-status svg { width: 11px; }
.node-status.succeeded { color: #4ade80; }
.node-status.stale { color: #fbbf24; }
.node-status.failed { color: #f87171; }
footer b { color: #60a5fa; }

:global(.vue-flow__handle.node-port) {
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  background: transparent;
  border: 0;
}
:global(.vue-flow__handle.node-port::after) {
  content: '';
  width: 10px;
  height: 10px;
  box-sizing: border-box;
  background: #222831;
  border: 2px solid #a5b1be;
  border-radius: 50%;
  transition: background .15s ease, border-color .15s ease, transform .15s ease;
}
:global(.vue-flow__handle.node-port:hover::after) { background: #2563eb; border-color: #93c5fd; transform: scale(1.2); }
:global(.vue-flow__handle.node-port.connecting::after) { border-color: #22c55e; }

@keyframes node-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) {
  .workflow-node-card, .status-icon { transition: none; animation: none !important; }
}
</style>
