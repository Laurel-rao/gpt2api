<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import { BaseEdge, Position } from '@vue-flow/core'
import type { CSSProperties } from 'vue'
import type { VideoWorkflowPosition } from '@/api/videoWorkflow'
import { adjustableEdgeGeometry, normalizeEdgeCurve } from '@/utils/videoWorkflowEdge'

interface CurveEdgeData {
  curve?: VideoWorkflowPosition
  route?: VideoWorkflowPosition[]
}

const props = withDefaults(defineProps<{
  id: string
  sourceX: number
  sourceY: number
  sourcePosition: Position
  targetX: number
  targetY: number
  targetPosition: Position
  selected?: boolean
  markerStart?: string
  markerEnd?: string
  interactionWidth?: number
  style?: CSSProperties
  data?: CurveEdgeData
  zoom: number
}>(), {
  selected: false,
  markerStart: undefined,
  markerEnd: undefined,
  interactionWidth: 20,
  style: undefined,
  data: () => ({}),
  zoom: 1,
})

const emit = defineEmits<{
  'curve-change-start': []
  'curve-change': [curve: VideoWorkflowPosition]
  'curve-change-end': [changed: boolean]
}>()

const geometry = computed(() => adjustableEdgeGeometry({
  sourceX: props.sourceX,
  sourceY: props.sourceY,
  sourcePosition: props.sourcePosition,
  targetX: props.targetX,
  targetY: props.targetY,
  targetPosition: props.targetPosition,
  curve: props.data?.curve,
  route: props.data?.route,
}))

let dragging: {
  pointerID: number
  clientX: number
  clientY: number
  initial: VideoWorkflowPosition
  changed: boolean
} | null = null

function sameCurve(left: VideoWorkflowPosition, right: VideoWorkflowPosition) {
  return left.x === right.x && left.y === right.y
}

function beginCurveChange() {
  emit('curve-change-start')
}

function changeCurve(curve: VideoWorkflowPosition) {
  emit('curve-change', normalizeEdgeCurve(curve))
}

function finishCurveChange(changed: boolean) {
  emit('curve-change-end', changed)
}

function onPointerMove(event: PointerEvent) {
  if (!dragging || event.pointerId !== dragging.pointerID) return
  const zoom = Math.max(.05, props.zoom)
  const next = normalizeEdgeCurve({
    x: dragging.initial.x + (event.clientX - dragging.clientX) / zoom,
    y: dragging.initial.y + (event.clientY - dragging.clientY) / zoom,
  })
  if (!sameCurve(next, dragging.initial)) dragging.changed = true
  changeCurve(next)
}

function removeDragListeners() {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
}

function onPointerUp(event: PointerEvent) {
  if (!dragging || event.pointerId !== dragging.pointerID) return
  const changed = dragging.changed
  dragging = null
  removeDragListeners()
  finishCurveChange(changed)
}

function onPointerDown(event: PointerEvent) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const initial = normalizeEdgeCurve(props.data?.curve)
  dragging = {
    pointerID: event.pointerId,
    clientX: event.clientX,
    clientY: event.clientY,
    initial,
    changed: false,
  }
  beginCurveChange()
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
}

function setCurveWithHistory(curve: VideoWorkflowPosition) {
  const current = normalizeEdgeCurve(props.data?.curve)
  const next = normalizeEdgeCurve(curve)
  if (sameCurve(current, next)) return
  beginCurveChange()
  changeCurve(next)
  finishCurveChange(true)
}

function onKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home'].includes(event.key)) return
  const current = normalizeEdgeCurve(props.data?.curve)
  const step = event.shiftKey ? 20 : 4
  const next = { ...current }
  if (event.key === 'ArrowLeft') next.x -= step
  if (event.key === 'ArrowRight') next.x += step
  if (event.key === 'ArrowUp') next.y -= step
  if (event.key === 'ArrowDown') next.y += step
  if (event.key === 'Home') Object.assign(next, { x: 0, y: 0 })
  setCurveWithHistory(next)
  event.preventDefault()
  event.stopPropagation()
}

function resetCurve(event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()
  setCurveWithHistory({ x: 0, y: 0 })
}

onBeforeUnmount(removeDragListeners)
</script>

<template>
  <BaseEdge
    :id="id"
    :path="geometry.path"
    :marker-start="markerStart"
    :marker-end="markerEnd"
    :interaction-width="interactionWidth"
    :style="style"
  />
  <g v-if="selected" class="curve-editor">
    <line
      v-if="data?.curve?.x || data?.curve?.y"
      class="curve-guide"
      :x1="geometry.defaultCenter.x"
      :y1="geometry.defaultCenter.y"
      :x2="geometry.control.x"
      :y2="geometry.control.y"
    />
    <g
      class="curve-handle nodrag nopan"
      role="button"
      tabindex="0"
      aria-label="调整连线曲线位置"
      @pointerdown="onPointerDown"
      @keydown="onKeydown"
      @dblclick="resetCurve"
      @click.stop
    >
      <title>拖动调整曲线，方向键微调，双击或 Home 复位</title>
      <circle class="curve-handle-hit" :cx="geometry.control.x" :cy="geometry.control.y" r="22" />
      <circle class="curve-handle-ring" :cx="geometry.control.x" :cy="geometry.control.y" r="7" />
      <circle class="curve-handle-dot" :cx="geometry.control.x" :cy="geometry.control.y" r="2.5" />
    </g>
  </g>
</template>

<style scoped>
.curve-guide { stroke: #60a5fa; stroke-width: 1; stroke-dasharray: 3 3; opacity: .72; pointer-events: none; }
.curve-handle { cursor: grab; outline: none; }
.curve-handle:active { cursor: grabbing; }
.curve-handle-hit { fill: transparent; pointer-events: all; }
.curve-handle-ring { fill: #172033; stroke: #93c5fd; stroke-width: 2; transition: fill 160ms ease, stroke 160ms ease; }
.curve-handle-dot { fill: #dbeafe; pointer-events: none; }
.curve-handle:hover .curve-handle-ring, .curve-handle:focus-visible .curve-handle-ring { fill: #2563eb; stroke: #dbeafe; }
@media (prefers-reduced-motion: reduce) { .curve-handle-ring { transition: none; } }
</style>
