import { Position } from '@vue-flow/core'
import type { VideoWorkflowPosition } from '@/api/videoWorkflow'

export const MAX_EDGE_CURVE_OFFSET = 2_000

export interface AdjustableBezierParams {
  sourceX: number
  sourceY: number
  sourcePosition: Position
  targetX: number
  targetY: number
  targetPosition: Position
  curve?: VideoWorkflowPosition
  route?: VideoWorkflowPosition[]
  curvature?: number
}

export interface AdjustableBezierGeometry {
  path: string
  defaultCenter: VideoWorkflowPosition
  control: VideoWorkflowPosition
  sourceControl: VideoWorkflowPosition
  targetControl: VideoWorkflowPosition
}

function clamp(value: number) {
  return Math.max(-MAX_EDGE_CURVE_OFFSET, Math.min(MAX_EDGE_CURVE_OFFSET, value))
}

export function normalizeEdgeCurve(curve?: Partial<VideoWorkflowPosition> | null): VideoWorkflowPosition {
  return {
    x: clamp(Number.isFinite(curve?.x) ? Number(curve!.x) : 0),
    y: clamp(Number.isFinite(curve?.y) ? Number(curve!.y) : 0),
  }
}

function calculateControlOffset(distance: number, curvature: number) {
  return distance >= 0 ? .5 * distance : curvature * 25 * Math.sqrt(-distance)
}

function endpointControl(
  position: Position,
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  curvature: number,
): VideoWorkflowPosition {
  if (position === Position.Left) return { x: x1 - calculateControlOffset(x1 - x2, curvature), y: y1 }
  if (position === Position.Right) return { x: x1 + calculateControlOffset(x2 - x1, curvature), y: y1 }
  if (position === Position.Top) return { x: x1, y: y1 - calculateControlOffset(y1 - y2, curvature) }
  return { x: x1, y: y1 + calculateControlOffset(y2 - y1, curvature) }
}

export function adjustableBezierGeometry(params: AdjustableBezierParams): AdjustableBezierGeometry {
  const curvature = params.curvature ?? .25
  const sourceControl = endpointControl(
    params.sourcePosition,
    params.sourceX,
    params.sourceY,
    params.targetX,
    params.targetY,
    curvature,
  )
  const targetControl = endpointControl(
    params.targetPosition,
    params.targetX,
    params.targetY,
    params.sourceX,
    params.sourceY,
    curvature,
  )
  const defaultCenter = {
    x: params.sourceX * .125 + sourceControl.x * .375 + targetControl.x * .375 + params.targetX * .125,
    y: params.sourceY * .125 + sourceControl.y * .375 + targetControl.y * .375 + params.targetY * .125,
  }
  const curve = normalizeEdgeCurve(params.curve)
  // Moving both cubic controls by 4/3 of the requested offset moves t=.5 by the exact requested amount.
  const controlAdjustment = { x: curve.x / .75, y: curve.y / .75 }
  const adjustedSource = { x: sourceControl.x + controlAdjustment.x, y: sourceControl.y + controlAdjustment.y }
  const adjustedTarget = { x: targetControl.x + controlAdjustment.x, y: targetControl.y + controlAdjustment.y }

  return {
    path: `M${params.sourceX},${params.sourceY} C${adjustedSource.x},${adjustedSource.y} ${adjustedTarget.x},${adjustedTarget.y} ${params.targetX},${params.targetY}`,
    defaultCenter,
    control: { x: defaultCenter.x + curve.x, y: defaultCenter.y + curve.y },
    sourceControl: adjustedSource,
    targetControl: adjustedTarget,
  }
}

function distance(left: VideoWorkflowPosition, right: VideoWorkflowPosition) {
  return Math.hypot(right.x - left.x, right.y - left.y)
}

function toward(from: VideoWorkflowPosition, to: VideoWorkflowPosition, amount: number) {
  const length = Math.max(1, distance(from, to))
  return {
    x: from.x + (to.x - from.x) * amount / length,
    y: from.y + (to.y - from.y) * amount / length,
  }
}

function polylineCenter(points: VideoWorkflowPosition[]) {
  const lengths = points.slice(0, -1).map((point, index) => distance(point, points[index + 1]))
  const total = lengths.reduce((sum, length) => sum + length, 0)
  let remaining = total / 2
  for (let index = 0; index < lengths.length; index += 1) {
    if (remaining <= lengths[index]) return toward(points[index], points[index + 1], remaining)
    remaining -= lengths[index]
  }
  return points.at(-1) || { x: 0, y: 0 }
}

function smoothRoutePath(points: VideoWorkflowPosition[], radius = 18) {
  if (points.length < 2) return ''
  let path = `M${points[0].x},${points[0].y}`
  for (let index = 1; index < points.length - 1; index += 1) {
    const previous = points[index - 1]
    const corner = points[index]
    const next = points[index + 1]
    const bend = Math.min(radius, distance(previous, corner) / 2, distance(corner, next) / 2)
    const entry = toward(corner, previous, bend)
    const exit = toward(corner, next, bend)
    path += ` L${entry.x},${entry.y} Q${corner.x},${corner.y} ${exit.x},${exit.y}`
  }
  const target = points.at(-1)!
  return `${path} L${target.x},${target.y}`
}

export function adjustableEdgeGeometry(params: AdjustableBezierParams): AdjustableBezierGeometry {
  if (!params.route?.length) return adjustableBezierGeometry(params)
  const curve = normalizeEdgeCurve(params.curve)
  const source = { x: params.sourceX, y: params.sourceY }
  const target = { x: params.targetX, y: params.targetY }
  const route = params.route.map((point) => ({ x: point.x + curve.x, y: point.y + curve.y }))
  const defaultPoints = [source, ...params.route, target]
  const defaultCenter = polylineCenter(defaultPoints)

  return {
    path: smoothRoutePath([source, ...route, target]),
    defaultCenter,
    control: { x: defaultCenter.x + curve.x, y: defaultCenter.y + curve.y },
    sourceControl: route[0] || source,
    targetControl: route.at(-1) || target,
  }
}
