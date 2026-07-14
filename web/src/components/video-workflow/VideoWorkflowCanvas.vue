<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  VueFlow,
  ConnectionMode,
  type Connection,
  type Edge,
  type EdgeChange,
  type EdgeUpdateEvent,
  type Node,
  type NodeChange,
  type NodeDragEvent,
  type NodeMouseEvent,
  type OnConnectStartParams,
  type ViewportTransform,
} from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import {
  Aim,
  Connection as ConnectionIcon,
  Delete,
  Fold,
  FullScreen,
  Hide,
  Lock,
  Loading,
  Minus,
  Operation,
  Plus,
  Rank,
  Refresh,
  RefreshLeft,
  RefreshRight,
  Sort,
  SwitchButton,
  Unlock,
  View,
} from '@element-plus/icons-vue'
import VideoWorkflowNodeCard from './VideoWorkflowNodeCard.vue'
import VideoWorkflowBezierEdge from './VideoWorkflowBezierEdge.vue'
import VideoWorkflowAssetBus from './VideoWorkflowAssetBus.vue'
import type { VideoWorkflowNode, VideoWorkflowPosition } from '@/api/videoWorkflow'
import type { VideoWorkflowEdgeDisplayMode } from '@/utils/videoWorkflowPresentation'

type CanvasTool = 'select' | 'pan' | 'connect'
type FlowData = {
  node?: VideoWorkflowNode
  zone?: { title: string; subtitle: string; enabled?: boolean }
  bus?: { sourceCount: number; targetCount: number }
  collapsedInputPortIDs?: string[]
}
type QuickConnectItem = { type: string; label: string }
type QuickConnectMenu = { x: number; y: number }

const props = withDefaults(defineProps<{
  backendAvailable: boolean
  activeTool: CanvasTool
  modKeyCode: string
  nodes: Node<FlowData>[]
  edges: Edge[]
  alignmentGuides: { x: number | null; y: number | null }
  quickConnectMenu: QuickConnectMenu | null
  quickConnectTypes: QuickConnectItem[]
  graphNodes: VideoWorkflowNode[]
  selectedNodeIDs: string[]
  selectedNodeLocked: boolean
  edgeDisplayMode: VideoWorkflowEdgeDisplayMode
  layoutBusy: boolean
  autoArrange: boolean
  canUndo: boolean
  canRedo: boolean
  canOperateWorkflow?: boolean
  zoom: number
  viewport: ViewportTransform
}>(), {
  canOperateWorkflow: true,
})

const selectedCount = computed(() => props.selectedNodeIDs.length)
const workflowReady = computed(() => props.canOperateWorkflow)
const canAlign = computed(() => workflowReady.value && selectedCount.value >= 2)
const canDistribute = computed(() => workflowReady.value && selectedCount.value >= 3)
const canModifySelection = computed(() => workflowReady.value && selectedCount.value >= 1)

function toolHint(action: string, min = 1) {
  if (!workflowReady.value) return `${action} · 请先从模板创建工作流`
  if (selectedCount.value < min) return `${action} · 需选中 ${min} 个及以上节点`
  return action
}

const tooltips = computed(() => ({
  select: '选择工具 · 框选/点选节点（V）',
  pan: '抓手工具 · 拖动画布（H / 空格）',
  connect: '连线工具 · 从输出端口拖到输入端口',
  undo: props.canUndo ? '撤销上一步（⌘/Ctrl+Z）' : '撤销 · 当前没有可撤销操作',
  redo: props.canRedo ? '重做（⇧⌘/Ctrl+Z）' : '重做 · 当前没有可重做操作',
  layout: props.layoutBusy ? '正在整理画布…' : `整理画布${props.autoArrange ? ' · 自动排布已开启' : ''}`,
  alignX: toolHint('左对齐选中节点', 2),
  alignY: toolHint('顶部对齐选中节点', 2),
  distributeX: toolHint('水平等距分布', 3),
  distributeY: toolHint('垂直等距分布', 3),
  toggleEnabled: toolHint('启用或停用选中节点'),
  toggleLock: toolHint(props.selectedNodeLocked ? '解锁选中节点' : '锁定选中节点'),
  collapse: toolHint('折叠或展开选中节点'),
  delete: toolHint('删除选中节点或连线'),
  edgeSmart: '智能显示连线',
  edgeAll: '显示全部连线',
  edgeHidden: '隐藏非相关连线',
}))

const emit = defineEmits<{
  'update:activeTool': [tool: CanvasTool]
  'update:nodes': [nodes: Node<FlowData>[]]
  'update:edges': [edges: Edge[]]
  'interaction': []
  'drop-node': [event: DragEvent]
  'auto-layout': [mode: 'auto' | 'selected_auto' | 'all']
  'toggle-auto-arrange': []
  'undo': []
  'redo': []
  'update:edgeDisplayMode': [mode: VideoWorkflowEdgeDisplayMode]
  'align-selected': [axis: 'x' | 'y']
  'distribute-selected': [axis: 'x' | 'y']
  'toggle-enabled': []
  'toggle-lock': []
  'toggle-collapse': []
  'delete-selected': []
  'nodes-change': [changes: NodeChange[]]
  'edges-change': [changes: EdgeChange[]]
  'node-click': [nodeID: string, additive: boolean]
  'preview-media': [node: VideoWorkflowNode]
  'node-drag-start': [event: NodeDragEvent]
  'node-drag': [event: NodeDragEvent]
  'node-drag-stop': [event: NodeDragEvent]
  'pane-click': []
  'connect': [connection: Connection]
  'connect-start': [payload: { event?: MouseEvent } & OnConnectStartParams]
  'connect-end': [event?: MouseEvent]
  'edge-update': [event: EdgeUpdateEvent]
  'edge-click': [edgeID: string]
  'edge-curve-change-start': [edgeID: string]
  'edge-curve-change': [edgeID: string, curve: VideoWorkflowPosition]
  'edge-curve-change-end': [edgeID: string, changed: boolean]
  'viewport-change': [viewport: ViewportTransform]
  'minimap-navigate': [position: { x: number; y: number }]
  'create-connected-node': [type: string]
  'cancel-quick-connect': []
  'zoom-out': []
  'zoom-in': []
  'fit-view': []
}>()

function onLayoutCommand(mode: 'auto' | 'selected_auto' | 'all' | 'toggle_arrange') {
  if (mode === 'toggle_arrange') emit('toggle-auto-arrange')
  else emit('auto-layout', mode)
}

const MINIMAP_NODE_WIDTH = 208
const MINIMAP_NODE_HEIGHT = 112
const MINIMAP_PADDING_X = 80
const MINIMAP_PADDING_Y = 60
const MINIMAP_KEYBOARD_STEP = .1
const canvasStage = ref<HTMLElement | null>(null)
const canvasSize = reactive({ width: 0, height: 0 })
const minimapDragging = ref(false)
let canvasResizeObserver: ResizeObserver | null = null

const minimapBounds = computed(() => {
  if (!props.graphNodes.length) return { minX: -500, minY: -300, width: 1000, height: 600 }
  const minX = Math.min(...props.graphNodes.map((node) => node.position.x)) - MINIMAP_PADDING_X
  const minY = Math.min(...props.graphNodes.map((node) => node.position.y)) - MINIMAP_PADDING_Y
  const maxX = Math.max(...props.graphNodes.map((node) => node.position.x + MINIMAP_NODE_WIDTH)) + MINIMAP_PADDING_X
  const maxY = Math.max(...props.graphNodes.map((node) => node.position.y + MINIMAP_NODE_HEIGHT)) + MINIMAP_PADDING_Y
  return { minX, minY, width: Math.max(1, maxX - minX), height: Math.max(1, maxY - minY) }
})

const minimapViewportStyle = computed(() => {
  const bounds = minimapBounds.value
  const zoom = Math.max(.01, props.viewport.zoom)
  const visible = {
    x: -props.viewport.x / zoom,
    y: -props.viewport.y / zoom,
    width: canvasSize.width / zoom,
    height: canvasSize.height / zoom,
  }
  const left = Math.max(0, Math.min(100, (visible.x - bounds.minX) / bounds.width * 100))
  const top = Math.max(0, Math.min(100, (visible.y - bounds.minY) / bounds.height * 100))
  const right = Math.max(0, Math.min(100, (visible.x + visible.width - bounds.minX) / bounds.width * 100))
  const bottom = Math.max(0, Math.min(100, (visible.y + visible.height - bounds.minY) / bounds.height * 100))
  return {
    left: `${left}%`,
    top: `${top}%`,
    width: `${Math.max(4, right - left)}%`,
    height: `${Math.max(6, bottom - top)}%`,
  }
})

function minimapNodeStyle(node: VideoWorkflowNode) {
  const bounds = minimapBounds.value
  return {
    left: `${(node.position.x + MINIMAP_NODE_WIDTH / 2 - bounds.minX) / bounds.width * 100}%`,
    top: `${(node.position.y + MINIMAP_NODE_HEIGHT / 2 - bounds.minY) / bounds.height * 100}%`,
  }
}

function minimapPosition(clientX: number, clientY: number, target: HTMLElement) {
  const rect = target.getBoundingClientRect()
  const bounds = minimapBounds.value
  const xRatio = Math.max(0, Math.min(1, (clientX - rect.left) / Math.max(1, rect.width)))
  const yRatio = Math.max(0, Math.min(1, (clientY - rect.top) / Math.max(1, rect.height)))
  return { x: bounds.minX + bounds.width * xRatio, y: bounds.minY + bounds.height * yRatio }
}

function navigateMinimap(event: PointerEvent) {
  emit('minimap-navigate', minimapPosition(event.clientX, event.clientY, event.currentTarget as HTMLElement))
}

function startMinimapDrag(event: PointerEvent) {
  if (event.button !== 0) return
  minimapDragging.value = true
  ;(event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId)
  navigateMinimap(event)
  event.preventDefault()
}

function moveMinimapDrag(event: PointerEvent) {
  if (minimapDragging.value) navigateMinimap(event)
}

function stopMinimapDrag(event: PointerEvent) {
  minimapDragging.value = false
  const target = event.currentTarget as HTMLElement
  if (target.hasPointerCapture?.(event.pointerId)) target.releasePointerCapture(event.pointerId)
}

function handleMinimapKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'Enter', ' '].includes(event.key)) return
  const bounds = minimapBounds.value
  const zoom = Math.max(.01, props.viewport.zoom)
  const current = {
    x: (-props.viewport.x + canvasSize.width / 2) / zoom,
    y: (-props.viewport.y + canvasSize.height / 2) / zoom,
  }
  if (event.key === 'ArrowLeft') current.x -= bounds.width * MINIMAP_KEYBOARD_STEP
  if (event.key === 'ArrowRight') current.x += bounds.width * MINIMAP_KEYBOARD_STEP
  if (event.key === 'ArrowUp') current.y -= bounds.height * MINIMAP_KEYBOARD_STEP
  if (event.key === 'ArrowDown') current.y += bounds.height * MINIMAP_KEYBOARD_STEP
  if (['Home', 'Enter', ' '].includes(event.key)) {
    current.x = bounds.minX + bounds.width / 2
    current.y = bounds.minY + bounds.height / 2
  }
  emit('minimap-navigate', current)
  event.preventDefault()
  event.stopPropagation()
}

function updateCanvasSize() {
  canvasSize.width = canvasStage.value?.clientWidth || 0
  canvasSize.height = canvasStage.value?.clientHeight || 0
}

onMounted(() => {
  updateCanvasSize()
  if (typeof ResizeObserver !== 'undefined' && canvasStage.value) {
    canvasResizeObserver = new ResizeObserver(updateCanvasSize)
    canvasResizeObserver.observe(canvasStage.value)
  }
})

onBeforeUnmount(() => canvasResizeObserver?.disconnect())

function emitNodeClick(payload: NodeMouseEvent) {
  emit('node-click', payload.node.id, Boolean((payload.event as MouseEvent).shiftKey))
}
</script>

<template>
  <main ref="canvasStage" class="canvas-stage" @pointerdown="emit('interaction')" @dragover.prevent @drop="emit('drop-node', $event)">
    <div v-if="!backendAvailable" class="offline-banner" role="status"><Refresh />API 暂不可用，编辑内容已保存在本机，恢复连接后自动重试。</div>
    <div class="canvas-label">视频画布</div>
    <div class="canvas-tools" role="toolbar" aria-label="画布工具">
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.select" placement="right" :show-after="250">
          <button :class="{ active: activeTool === 'select' }" aria-label="选择" @click="emit('update:activeTool', 'select')"><Aim /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.pan" placement="right" :show-after="250">
          <button :class="{ active: activeTool === 'pan' }" aria-label="抓手" @click="emit('update:activeTool', 'pan')"><Rank /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.connect" placement="right" :show-after="250">
          <button :class="{ active: activeTool === 'connect' }" aria-label="连线" @click="emit('update:activeTool', 'connect')"><ConnectionIcon /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.undo" placement="right" :show-after="250">
          <button aria-label="撤销" :class="{ 'is-disabled': !canUndo }" @click="canUndo && emit('undo')"><RefreshLeft /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.redo" placement="right" :show-after="250">
          <button aria-label="重做" :class="{ 'is-disabled': !canRedo }" @click="canRedo && emit('redo')"><RefreshRight /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-dropdown
          trigger="click"
          placement="right-start"
          :disabled="layoutBusy || !workflowReady"
          :teleported="true"
          @command="onLayoutCommand"
        >
          <button
            type="button"
            aria-label="整理画布"
            :title="tooltips.layout"
            :class="{ active: autoArrange, 'is-disabled': layoutBusy || !workflowReady }"
            :aria-busy="layoutBusy"
          >
            <component :is="layoutBusy ? Loading : Sort" />
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="auto">整理自动节点</el-dropdown-item>
              <el-dropdown-item command="selected_auto">所选恢复自动布局</el-dropdown-item>
              <el-dropdown-item command="toggle_arrange">{{ autoArrange ? '关闭自动排布' : '开启自动排布' }}</el-dropdown-item>
              <el-dropdown-item command="all" divided>重新整理全部…</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
      <div class="canvas-tool-separator" />
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.alignX" placement="right" :show-after="250">
          <button aria-label="左对齐" :class="{ 'is-disabled': !canAlign }" @click="canAlign && emit('align-selected', 'x')"><Operation /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.alignY" placement="right" :show-after="250">
          <button aria-label="顶部对齐" :class="{ 'is-disabled': !canAlign }" @click="canAlign && emit('align-selected', 'y')"><Operation class="rotate-icon" /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.distributeX" placement="right" :show-after="250">
          <button aria-label="水平等距分布" :class="{ 'is-disabled': !canDistribute }" @click="canDistribute && emit('distribute-selected', 'x')"><Sort /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.distributeY" placement="right" :show-after="250">
          <button aria-label="垂直等距分布" :class="{ 'is-disabled': !canDistribute }" @click="canDistribute && emit('distribute-selected', 'y')"><Sort class="rotate-icon" /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.toggleEnabled" placement="right" :show-after="250">
          <button aria-label="启用或停用" :class="{ 'is-disabled': !canModifySelection }" @click="canModifySelection && emit('toggle-enabled')"><SwitchButton /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.toggleLock" placement="right" :show-after="250">
          <button aria-label="锁定或解锁" :class="{ 'is-disabled': !canModifySelection }" @click="canModifySelection && emit('toggle-lock')"><component :is="selectedNodeLocked ? Unlock : Lock" /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.collapse" placement="right" :show-after="250">
          <button aria-label="折叠节点" :class="{ 'is-disabled': !canModifySelection }" @click="canModifySelection && emit('toggle-collapse')"><Fold /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.delete" placement="right" :show-after="250">
          <button aria-label="删除选中项" :class="{ 'is-disabled': !canModifySelection }" @click="canModifySelection && emit('delete-selected')"><Delete /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-separator" />
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.edgeSmart" placement="right" :show-after="250">
          <button title="智能显示连线" aria-label="智能显示连线" :aria-pressed="edgeDisplayMode === 'smart'" :class="{ active: edgeDisplayMode === 'smart' }" @click="emit('update:edgeDisplayMode', 'smart')"><ConnectionIcon /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.edgeAll" placement="right" :show-after="250">
          <button title="显示全部连线" aria-label="显示全部连线" :aria-pressed="edgeDisplayMode === 'all'" :class="{ active: edgeDisplayMode === 'all' }" @click="emit('update:edgeDisplayMode', 'all')"><View /></button>
        </el-tooltip>
      </div>
      <div class="canvas-tool-item">
        <el-tooltip :content="tooltips.edgeHidden" placement="right" :show-after="250">
          <button title="隐藏非相关连线" aria-label="隐藏非相关连线" :aria-pressed="edgeDisplayMode === 'hidden'" :class="{ active: edgeDisplayMode === 'hidden' }" @click="emit('update:edgeDisplayMode', 'hidden')"><Hide /></button>
        </el-tooltip>
      </div>
    </div>

    <VueFlow
      id="video-workflow-flow"
      class="workflow-flow"
      :nodes="nodes"
      :edges="edges"
      :min-zoom=".2"
      :max-zoom="1.8"
      :fit-view-on-init="true"
      :pan-on-drag="activeTool === 'pan' ? true : [1]"
      :pan-on-scroll="true"
      :zoom-on-scroll="false"
      :zoom-on-pinch="true"
      :zoom-activation-key-code="modKeyCode"
      :pan-activation-key-code="'Space'"
      :selection-key-code="activeTool === 'select' ? true : 'Shift'"
      :multi-selection-key-code="'Shift'"
      :delete-key-code="null"
      :snap-to-grid="true"
      :snap-grid="[10, 10]"
      :edges-updatable="true"
      :edge-updater-radius="12"
      :connect-on-click="true"
      :connection-mode="ConnectionMode.Loose"
      :nodes-focusable="true"
      :edges-focusable="true"
      @update:nodes="emit('update:nodes', $event)"
      @update:edges="emit('update:edges', $event)"
      @nodes-change="emit('nodes-change', $event)"
      @edges-change="emit('edges-change', $event)"
      @node-click="emitNodeClick"
      @node-drag-start="emit('node-drag-start', $event)"
      @node-drag="emit('node-drag', $event)"
      @node-drag-stop="emit('node-drag-stop', $event)"
      @pane-click="emit('pane-click')"
      @connect="emit('connect', $event)"
      @connect-start="emit('connect-start', $event)"
      @connect-end="emit('connect-end', $event)"
      @edge-update="emit('edge-update', $event)"
      @edge-click="emit('edge-click', $event.edge.id)"
      @viewport-change="emit('viewport-change', $event)"
    >
      <template #node-zone="{ data }"><section :class="['flow-zone', { disabled: data.zone.enabled === false }]"><header><b>{{ data.zone.title }}</b><span>{{ data.zone.subtitle }}</span></header></section></template>
      <template #node-workflow="{ data, selected }"><VideoWorkflowNodeCard v-if="data.node" :node="data.node" :selected="selected" :collapsed-input-port-i-ds="data.collapsedInputPortIDs" @preview-media="emit('preview-media', $event)" /></template>
      <template #node-assetBus="{ data, selected }"><VideoWorkflowAssetBus v-if="data.bus" v-bind="data.bus" :selected="selected" /></template>
      <template #edge-adjustable="edgeProps">
        <VideoWorkflowBezierEdge
          v-bind="edgeProps"
          :zoom="zoom"
          @curve-change-start="emit('edge-curve-change-start', edgeProps.id)"
          @curve-change="emit('edge-curve-change', edgeProps.id, $event)"
          @curve-change-end="emit('edge-curve-change-end', edgeProps.id, $event)"
        />
      </template>
    </VueFlow>

    <i v-if="alignmentGuides.x !== null" class="alignment-guide vertical" :style="{ left: `${alignmentGuides.x}px` }" />
    <i v-if="alignmentGuides.y !== null" class="alignment-guide horizontal" :style="{ top: `${alignmentGuides.y}px` }" />
    <div v-if="quickConnectMenu" class="quick-connect-menu" :style="{ left: `${quickConnectMenu.x}px`, top: `${quickConnectMenu.y}px` }">
      <strong>创建兼容节点</strong>
      <button v-for="item in quickConnectTypes" :key="item.type" @click="emit('create-connected-node', item.type)">{{ item.label }}</button>
      <button class="cancel" @click="emit('cancel-quick-connect')">取消</button>
    </div>

    <div class="canvas-nav-stack">
      <div
        :class="['canvas-minimap', { dragging: minimapDragging }]"
        role="button"
        tabindex="0"
        aria-label="画布小地图，点击或拖动定位画布"
        title="点击或拖动定位画布"
        @pointerdown="startMinimapDrag"
        @pointermove="moveMinimapDrag"
        @pointerup="stopMinimapDrag"
        @pointercancel="stopMinimapDrag"
        @keydown="handleMinimapKeydown"
      >
        <i v-for="node in graphNodes" :key="node.id" :class="{ selected: selectedNodeIDs.includes(node.id) }" :style="minimapNodeStyle(node)" />
        <span class="minimap-viewport" :style="minimapViewportStyle" />
      </div>
      <div class="zoom-controls"><button title="缩小" @click="emit('zoom-out')"><Minus /></button><b>{{ Math.round(zoom * 100) }}%</b><button title="放大" @click="emit('zoom-in')"><Plus /></button><button title="适应全部" @click="emit('fit-view')"><FullScreen /></button></div>
    </div>
  </main>
</template>

<style scoped lang="scss">
.canvas-stage { grid-area: canvas; position: relative; min-width: 0; min-height: 0; overflow: hidden; background: var(--canvas, #111318); }
.offline-banner { position: absolute; z-index: 20; left: 50%; top: 12px; transform: translateX(-50%); display: flex; align-items: center; gap: 6px; padding: 7px 11px; color: #fef3c7; background: rgba(120, 53, 15, .92); border: 1px solid #b45309; border-radius: 5px; font-size: 10px; }.offline-banner svg { width: 13px; }
.canvas-label { position: absolute; z-index: 4; left: 16px; top: 12px; color: #e2e8f0; font-size: 13px; font-weight: 650; pointer-events: none; }
.workflow-flow { width: 100%; height: 100%; background-color: #111318; background-image: radial-gradient(circle, #323844 1px, transparent 1px); background-size: 24px 24px; }
.workflow-flow :deep(.vue-flow__edge-path) { stroke: #8b96a5; stroke-width: 1.5; vector-effect: non-scaling-stroke; }
.workflow-flow :deep(.vue-flow__edge:hover .vue-flow__edge-path) { stroke-width: 2.5; }
.workflow-flow :deep(.vue-flow__edge.selected .vue-flow__edge-path) { stroke: #60a5fa !important; stroke-width: 2.5; }
.workflow-flow :deep(.vue-flow__edge.animated .vue-flow__edge-path) { stroke: #38bdf8 !important; }
.workflow-flow :deep(.vue-flow__selection) { background: rgba(37, 99, 235, .1); border: 1px solid #60a5fa; }
.workflow-flow :deep(.vue-flow__node) { cursor: default; }
.workflow-flow :deep(.vue-flow__handle.valid::after), .workflow-flow :deep(.vue-flow__handle.connecting::after) { background: #16a34a; border-color: #86efac; }
.alignment-guide { position: absolute; z-index: 6; pointer-events: none; background: #60a5fa; box-shadow: 0 0 0 1px rgba(96, 165, 250, .18); }.alignment-guide.vertical { top: 0; bottom: 0; width: 1px; }.alignment-guide.horizontal { left: 0; right: 0; height: 1px; }
.quick-connect-menu { position: absolute; z-index: 30; width: 174px; display: grid; gap: 4px; padding: 8px; color: #e2e8f0; background: #222831; border: 1px solid #64748b; border-radius: 6px; box-shadow: 0 14px 32px rgba(0, 0, 0, .4); }.quick-connect-menu strong { padding: 3px 5px 6px; font-size: 10px; }.quick-connect-menu button { height: 30px; padding: 0 8px; color: #e2e8f0; text-align: left; background: #2b323c; border: 0; border-radius: 4px; cursor: pointer; font-size: 10px; }.quick-connect-menu button:hover { background: #2563eb; }.quick-connect-menu button.cancel { color: #94a3b8; background: transparent; }
.flow-zone { width: 100%; height: 100%; box-sizing: border-box; color: #94a3b8; background: rgba(30, 35, 43, .28); border: 1px dashed #3c4653; border-radius: 8px; }.flow-zone.disabled { opacity: .36; }.flow-zone header { height: 34px; display: flex; align-items: center; gap: 8px; padding: 0 10px; border-bottom: 1px solid rgba(71, 85, 105, .45); }.flow-zone b { font-size: 10px; }.flow-zone span { color: #64748b; font-size: 9px; }
.canvas-tools { position: absolute; z-index: 8; left: 16px; top: 48px; width: 38px; display: flex; flex-direction: column; gap: 2px; padding: 4px; background: rgba(30, 35, 43, .94); border: 1px solid #47515e; border-radius: 6px; box-shadow: 0 10px 24px rgba(0, 0, 0, .26); }.canvas-tool-item { display: flex; width: 100%; min-height: 30px; }.canvas-tool-item :deep(.el-tooltip__trigger), .canvas-tool-item :deep(.el-dropdown) { display: flex; width: 100%; }.canvas-tool-separator { height: 1px; margin: 4px 2px; background: #47515e; flex: 0 0 auto; }.canvas-tools button { width: 100%; height: 30px; display: grid; place-items: center; color: #b9c2ce; background: transparent; border: 0; border-radius: 4px; cursor: pointer; }.canvas-tools button:hover:not(.is-disabled), .canvas-tools button.active { color: #fff; background: #2563eb; }.canvas-tools button.is-disabled { opacity: .35; cursor: not-allowed; }.canvas-tools button.is-disabled:hover { color: #b9c2ce; background: transparent; }.canvas-tools button:focus-visible { outline: 2px solid #93c5fd; outline-offset: 1px; }.canvas-tools button[aria-busy="true"] svg { animation: tool-spin 1s linear infinite; }.canvas-tools svg { width: 15px; }.rotate-icon { transform: rotate(90deg); }
.canvas-nav-stack { position: absolute; z-index: 8; right: 16px; bottom: 14px; display: flex; flex-direction: column; align-items: stretch; gap: 8px; }
.canvas-minimap { position: relative; width: 150px; height: 92px; box-sizing: border-box; overflow: hidden; background: rgba(23, 28, 35, .94); border: 1px solid #66717f; border-radius: 5px; cursor: crosshair; touch-action: none; user-select: none; }.canvas-minimap:hover, .canvas-minimap:focus-visible, .canvas-minimap.dragging { border-color: #93c5fd; box-shadow: 0 0 0 2px rgba(96, 165, 250, .18); }.canvas-minimap.dragging { cursor: grabbing; }.canvas-minimap i { position: absolute; width: 16px; height: 7px; pointer-events: none; transform: translate(-50%, -50%); background: #7b8795; border-radius: 1px; }.canvas-minimap i.selected { background: #3b82f6; box-shadow: 0 0 0 1px #93c5fd; }.minimap-viewport { position: absolute; box-sizing: border-box; pointer-events: none; background: rgba(37, 99, 235, .08); border: 1px solid #60a5fa; }
.zoom-controls { height: 32px; display: flex; align-items: center; color: #dbe2ea; background: rgba(23, 28, 35, .94); border: 1px solid #66717f; border-radius: 5px; }.zoom-controls button { width: 32px; height: 30px; display: grid; place-items: center; color: #dbe2ea; background: transparent; border: 0; border-right: 1px solid #47515e; cursor: pointer; }.zoom-controls button:last-child { border: 0; border-left: 1px solid #47515e; }.zoom-controls b { min-width: 50px; text-align: center; font-size: 10px; }.zoom-controls svg { width: 13px; }
button { font-family: inherit; } button:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
@keyframes tool-spin { to { transform: rotate(360deg); } }
</style>
