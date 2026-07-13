import type {
  VideoWorkflowEdge,
  VideoWorkflowGraph,
  VideoWorkflowNode,
  VideoWorkflowPosition,
} from '@/api/videoWorkflow'
import { videoWorkflowNodeSize } from './videoWorkflowLayout'

export const SHARED_CHARACTER_BUS_ID = '__shared_character_bus'
export const SHARED_CHARACTER_BUS_WIDTH = 132
export const SHARED_CHARACTER_BUS_HEIGHT = 44

export type VideoWorkflowEdgeDisplayMode = 'smart' | 'all' | 'hidden'

export interface SharedCharacterBus {
  id: typeof SHARED_CHARACTER_BUS_ID
  position: VideoWorkflowPosition
  logicalEdges: VideoWorkflowEdge[]
  sourceNodeIDs: string[]
  targetNodeIDs: string[]
  targetPortIDs: Map<string, string[]>
}

function firstVerticalCorridor(graph: VideoWorkflowGraph, left: number, right: number) {
  const padding = 18
  const intervals = graph.nodes
    .map((node) => {
      const size = videoWorkflowNodeSize(node)
      return { left: node.position.x - padding, right: node.position.x + size.width + padding }
    })
    .filter((interval) => interval.right > left && interval.left < right)
    .sort((a, b) => a.left - b.left)
  let cursor = left
  for (const interval of intervals) {
    if (interval.left - cursor >= 42) return cursor + (interval.left - cursor) / 2
    cursor = Math.max(cursor, interval.right)
  }
  return cursor < right ? cursor + (right - cursor) / 2 : left
}

function isCharacterVideoEdge(edge: VideoWorkflowEdge, nodes: Map<string, VideoWorkflowNode>) {
  const source = nodes.get(edge.source)
  const target = nodes.get(edge.target)
  if (source?.type !== 'character' || target?.type !== 'video') return false
  const port = target.inputs?.find((item) => item.id === edge.target_port)
  return port?.type === 'image' && edge.target_port !== 'background'
}

function overlapsNode(graph: VideoWorkflowGraph, position: VideoWorkflowPosition) {
  return graph.nodes.some((node) => {
    const size = videoWorkflowNodeSize(node)
    return position.x < node.position.x + size.width + 28
      && position.x + SHARED_CHARACTER_BUS_WIDTH + 28 > node.position.x
      && position.y < node.position.y + size.height + 28
      && position.y + SHARED_CHARACTER_BUS_HEIGHT + 28 > node.position.y
  })
}

export function defaultSharedCharacterBusPosition(graph: VideoWorkflowGraph) {
  const characters = graph.nodes.filter((node) => node.type === 'character')
  const videos = graph.nodes.filter((node) => node.type === 'video')
  const characterRight = Math.max(...characters.map((node) => node.position.x + videoWorkflowNodeSize(node).width), 80)
  const videoLeft = Math.min(...videos.map((node) => node.position.x), characterRight + 520)
  const centerY = videos.length
    ? videos.reduce((sum, node) => sum + node.position.y + videoWorkflowNodeSize(node).height / 2, 0) / videos.length
    : 260
  const preferred = {
    x: characterRight + Math.max(72, (videoLeft - characterRight - SHARED_CHARACTER_BUS_WIDTH) * .35),
    y: centerY - SHARED_CHARACTER_BUS_HEIGHT / 2,
  }
  const candidates = [preferred]
  for (let ring = 1; ring <= 24; ring += 1) {
    candidates.push(
      { x: preferred.x, y: preferred.y - ring * 52 },
      { x: preferred.x, y: preferred.y + ring * 52 },
      { x: preferred.x + ring * 52, y: preferred.y },
      { x: preferred.x - ring * 52, y: preferred.y },
    )
  }
  return candidates.find((position) => !overlapsNode(graph, position)) || preferred
}

export function ensureSharedCharacterBusLayout(graph: VideoWorkflowGraph, reset = false) {
  const current = graph.layout?.shared_character_bus
  if (current?.position_mode === 'manual' && !reset) return current
  const anchor = { position: defaultSharedCharacterBusPosition(graph), position_mode: 'auto' as const }
  graph.layout = { ...(graph.layout || {}), shared_character_bus: anchor }
  return anchor
}

export function sharedCharacterBus(graph: VideoWorkflowGraph): SharedCharacterBus | null {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]))
  const logicalEdges = graph.edges.filter((edge) => isCharacterVideoEdge(edge, nodes))
  const sourceNodeIDs = [...new Set(logicalEdges.map((edge) => edge.source))].sort()
  const targetNodeIDs = [...new Set(logicalEdges.map((edge) => edge.target))]
    .sort((left, right) => {
      const leftNode = nodes.get(left)
      const rightNode = nodes.get(right)
      return (leftNode?.position.y || 0) - (rightNode?.position.y || 0) || left.localeCompare(right)
    })
  if (logicalEdges.length < 4 || sourceNodeIDs.length < 2 || targetNodeIDs.length < 2) return null
  const targetPortIDs = new Map<string, string[]>()
  targetNodeIDs.forEach((targetID) => targetPortIDs.set(
    targetID,
    logicalEdges.filter((edge) => edge.target === targetID).map((edge) => edge.target_port),
  ))
  const anchor = graph.layout?.shared_character_bus || {
    position: defaultSharedCharacterBusPosition(graph),
    position_mode: 'auto' as const,
  }
  return {
    id: SHARED_CHARACTER_BUS_ID,
    position: { ...anchor.position },
    logicalEdges,
    sourceNodeIDs,
    targetNodeIDs,
    targetPortIDs,
  }
}

export function sharedCharacterBusTargetRoute(
  graph: VideoWorkflowGraph,
  bus: SharedCharacterBus,
  targetID: string,
  targetIndex: number,
) {
  const target = graph.nodes.find((node) => node.id === targetID)
  if (!target) return []
  const collapsed = new Set(bus.targetPortIDs.get(targetID) || [])
  const visibleInputCount = (target.inputs || []).filter((port) => !collapsed.has(port.id)).length
  const sourceY = bus.position.y + SHARED_CHARACTER_BUS_HEIGHT / 2
  const targetY = target.position.y + Math.min(videoWorkflowNodeSize(target).height - 12, 48 + visibleInputCount * 24)
  const busRight = bus.position.x + SHARED_CHARACTER_BUS_WIDTH
  const targetLaneX = target.position.x - 48
  const corridorX = firstVerticalCorridor(graph, busRight + 28, targetLaneX - 28)
  const top = Math.min(...graph.nodes.map((node) => node.position.y), target.position.y)
  const laneY = top - 42 - targetIndex * 18
  return [
    { x: corridorX, y: sourceY },
    { x: corridorX, y: laneY },
    { x: targetLaneX, y: laneY },
    { x: targetLaneX, y: targetY },
  ]
}
