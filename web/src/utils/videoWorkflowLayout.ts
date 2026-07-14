import * as dagre from '@dagrejs/dagre'
import { Position } from '@vue-flow/core'
import type {
  VideoWorkflowEdge,
  VideoWorkflowGraph,
  VideoWorkflowGroup,
  VideoWorkflowNode,
  VideoWorkflowPosition,
} from '@/api/videoWorkflow'
import { adjustableBezierGeometry } from './videoWorkflowEdge'

const LAYOUT_MARGIN_X = 80
const LAYOUT_MARGIN_Y = 72
const NODE_GAP = 92
const RANK_GAP = 116
const GROUP_PADDING = { left: 30, right: 30, top: 46, bottom: 18 }
const GROUP_GAP = 40
const COLLISION_PADDING = 14
const ROUTE_LANE_PADDING = 30
const PLACEMENT_STEP = 36
const PLACEMENT_ATTEMPTS = 80

export type VideoWorkflowLayoutMode = 'auto' | 'all'

interface NodeBox {
  id: string
  x: number
  y: number
  width: number
  height: number
}

interface EdgeGeometry {
  source: VideoWorkflowPosition
  target: VideoWorkflowPosition
  sourceControl: VideoWorkflowPosition
  targetControl: VideoWorkflowPosition
}

export interface VideoWorkflowLayoutDiagnostics {
  nodeOverlaps: Array<[string, string]>
  groupOverlaps: Array<[string, string]>
  edgeNodeOverlaps: Array<{ edgeID: string; nodeID: string }>
}

export function videoWorkflowNodeSize(node: VideoWorkflowNode) {
  if (node.collapsed) return { width: 188, height: 36 }
  if (['character', 'background', 'image', 'video'].includes(node.type)) return { width: 208, height: 196 }
  return { width: 188, height: 96 }
}

export function cloneLayoutGraph(graph: VideoWorkflowGraph): VideoWorkflowGraph {
  return JSON.parse(JSON.stringify(graph)) as VideoWorkflowGraph
}

function nodeBoxes(graph: VideoWorkflowGraph): NodeBox[] {
  return graph.nodes.map((node) => ({ id: node.id, ...node.position, ...videoWorkflowNodeSize(node) }))
}

function overlaps(left: NodeBox, right: NodeBox, padding = 0) {
  return left.x < right.x + right.width + padding
    && left.x + left.width + padding > right.x
    && left.y < right.y + right.height + padding
    && left.y + left.height + padding > right.y
}

function movableNode(node: VideoWorkflowNode, mode: VideoWorkflowLayoutMode) {
  return !node.locked && (mode === 'all' || node.position_mode === 'auto')
}

export function videoWorkflowMovableNodeIDs(graph: VideoWorkflowGraph, mode: VideoWorkflowLayoutMode) {
  return graph.nodes.filter((node) => movableNode(node, mode)).map((node) => node.id)
}

function nodeBoxAt(node: VideoWorkflowNode, position: VideoWorkflowPosition): NodeBox {
  return { id: node.id, ...position, ...videoWorkflowNodeSize(node) }
}

function nearestAnchorOffset(
  nodeID: string,
  proposed: Map<string, VideoWorkflowPosition>,
  fixed: VideoWorkflowNode[],
) {
  const position = proposed.get(nodeID)
  if (!position || !fixed.length) return { x: 0, y: 0 }
  let nearest: { distance: number; offset: VideoWorkflowPosition } | null = null
  for (const anchor of fixed) {
    const anchorProposal = proposed.get(anchor.id)
    if (!anchorProposal) continue
    const distance = Math.hypot(position.x - anchorProposal.x, position.y - anchorProposal.y)
    const candidate = {
      distance,
      offset: {
        x: anchor.position.x - anchorProposal.x,
        y: anchor.position.y - anchorProposal.y,
      },
    }
    if (!nearest || candidate.distance < nearest.distance) nearest = candidate
  }
  return nearest?.offset || { x: 0, y: 0 }
}

function freePosition(node: VideoWorkflowNode, preferred: VideoWorkflowPosition, occupied: NodeBox[]) {
  const candidates: VideoWorkflowPosition[] = [{ ...preferred }]
  for (let attempt = 1; attempt <= PLACEMENT_ATTEMPTS; attempt += 1) {
    const ring = Math.ceil(attempt / 4)
    const distance = ring * PLACEMENT_STEP
    const direction = attempt % 4
    if (direction === 1) candidates.push({ x: preferred.x, y: preferred.y + distance })
    if (direction === 2) candidates.push({ x: preferred.x, y: preferred.y - distance })
    if (direction === 3) candidates.push({ x: preferred.x + distance, y: preferred.y })
    if (direction === 0) candidates.push({ x: preferred.x - distance, y: preferred.y })
  }
  return candidates.find((position) => !occupied.some((box) => overlaps(nodeBoxAt(node, position), box, 24))) || candidates.at(-1)!
}

export function applyVideoWorkflowLayoutProposal(
  source: VideoWorkflowGraph,
  proposed: Map<string, VideoWorkflowPosition>,
  mode: VideoWorkflowLayoutMode,
) {
  const graph = cloneLayoutGraph(source)
  const movable = graph.nodes.filter((node) => movableNode(node, mode))
  const fixed = graph.nodes.filter((node) => !movableNode(node, mode))
  const occupied = fixed.map((node) => nodeBoxAt(node, node.position))
  const preferred = new Map<string, VideoWorkflowPosition>()

  for (const node of movable) {
    const candidate = proposed.get(node.id) || node.position
    const offset = nearestAnchorOffset(node.id, proposed, fixed)
    preferred.set(node.id, { x: candidate.x + offset.x, y: candidate.y + offset.y })
  }

  movable
    .sort((left, right) => {
      const leftPosition = preferred.get(left.id)!
      const rightPosition = preferred.get(right.id)!
      return leftPosition.x - rightPosition.x || leftPosition.y - rightPosition.y || left.id.localeCompare(right.id)
    })
    .forEach((node) => {
      node.position = freePosition(node, preferred.get(node.id)!, occupied)
      node.position_mode = 'auto'
      occupied.push(nodeBoxAt(node, node.position))
    })

  updateVideoWorkflowGroupBounds(graph)
  routeEdges(graph)
  return graph
}

function centerUngroupedColumns(graph: VideoWorkflowGraph, groupedNodeIDs: Set<string>, sceneTop: number, sceneBottom: number) {
  const columns = new Map<number, VideoWorkflowNode[]>()
  for (const node of graph.nodes.filter((item) => !groupedNodeIDs.has(item.id))) {
    const key = Math.round(node.position.x / 4) * 4
    columns.set(key, [...(columns.get(key) || []), node])
  }
  const sceneCenter = (sceneTop + sceneBottom) / 2
  for (const nodes of columns.values()) {
    nodes.sort((left, right) => left.position.y - right.position.y || left.id.localeCompare(right.id))
    const totalHeight = nodes.reduce((sum, node) => sum + videoWorkflowNodeSize(node).height, 0) + NODE_GAP * Math.max(0, nodes.length - 1)
    let y = Math.max(LAYOUT_MARGIN_Y, sceneCenter - totalHeight / 2)
    for (const node of nodes) {
      node.position.y = y
      y += videoWorkflowNodeSize(node).height + NODE_GAP
    }
  }
}

function alignSceneGroups(graph: VideoWorkflowGraph) {
  const groups = graph.groups
    .map((group) => ({ group, nodes: group.node_ids.map((id) => graph.nodes.find((node) => node.id === id)).filter(Boolean) as VideoWorkflowNode[] }))
    .filter((item) => item.nodes.length)
    .sort((left, right) => {
      const leftIndex = Number(left.group.scene_id.replace(/\D/g, ''))
      const rightIndex = Number(right.group.scene_id.replace(/\D/g, ''))
      return leftIndex - rightIndex || left.group.id.localeCompare(right.group.id)
    })
  if (!groups.length) return

  const maxNodeHeight = Math.max(...groups.flatMap((item) => item.nodes.map((node) => videoWorkflowNodeSize(node).height)))
  const groupHeight = GROUP_PADDING.top + maxNodeHeight + GROUP_PADDING.bottom
  const firstNodeTop = LAYOUT_MARGIN_Y + GROUP_PADDING.top

  groups.forEach(({ nodes }, index) => {
    const centerY = firstNodeTop + index * (groupHeight + GROUP_GAP) + maxNodeHeight / 2
    for (const node of nodes) node.position.y = centerY - videoWorkflowNodeSize(node).height / 2
  })

  const groupedNodeIDs = new Set(groups.flatMap((item) => item.nodes.map((node) => node.id)))
  const sceneTop = firstNodeTop
  const sceneBottom = firstNodeTop + (groups.length - 1) * (groupHeight + GROUP_GAP) + maxNodeHeight
  centerUngroupedColumns(graph, groupedNodeIDs, sceneTop, sceneBottom)
}

export function updateVideoWorkflowGroupBounds(graph: VideoWorkflowGraph) {
  for (const group of graph.groups) {
    const members = group.node_ids.map((id) => graph.nodes.find((node) => node.id === id)).filter(Boolean) as VideoWorkflowNode[]
    if (!members.length) continue
    const minX = Math.min(...members.map((node) => node.position.x))
    const minY = Math.min(...members.map((node) => node.position.y))
    const maxX = Math.max(...members.map((node) => node.position.x + videoWorkflowNodeSize(node).width))
    const maxY = Math.max(...members.map((node) => node.position.y + videoWorkflowNodeSize(node).height))
    group.position = { x: minX - GROUP_PADDING.left, y: minY - GROUP_PADDING.top }
    group.size = {
      width: maxX - minX + GROUP_PADDING.left + GROUP_PADDING.right,
      height: maxY - minY + GROUP_PADDING.top + GROUP_PADDING.bottom,
    }
  }
}

function portY(node: VideoWorkflowNode, portID: string, kind: 'input' | 'output') {
  const ports = kind === 'input' ? node.inputs : node.outputs
  const index = Math.max(0, ports?.findIndex((port) => port.id === portID) ?? 0)
  const height = videoWorkflowNodeSize(node).height
  return node.position.y + Math.min(height - 12, 28 + index * 22)
}

function edgeGeometry(graph: VideoWorkflowGraph, edge: VideoWorkflowEdge, curve = edge.curve): EdgeGeometry | null {
  const sourceNode = graph.nodes.find((node) => node.id === edge.source)
  const targetNode = graph.nodes.find((node) => node.id === edge.target)
  if (!sourceNode || !targetNode) return null
  const sourceSize = videoWorkflowNodeSize(sourceNode)
  const source = { x: sourceNode.position.x + sourceSize.width, y: portY(sourceNode, edge.source_port, 'output') }
  const target = { x: targetNode.position.x, y: portY(targetNode, edge.target_port, 'input') }
  const geometry = adjustableBezierGeometry({
    sourceX: source.x,
    sourceY: source.y,
    sourcePosition: Position.Right,
    targetX: target.x,
    targetY: target.y,
    targetPosition: Position.Left,
    curve,
  })
  return { source, target, sourceControl: geometry.sourceControl, targetControl: geometry.targetControl }
}

function cubicPoint(geometry: EdgeGeometry, t: number) {
  const inverse = 1 - t
  const inverse2 = inverse * inverse
  const t2 = t * t
  return {
    x: inverse2 * inverse * geometry.source.x
      + 3 * inverse2 * t * geometry.sourceControl.x
      + 3 * inverse * t2 * geometry.targetControl.x
      + t2 * t * geometry.target.x,
    y: inverse2 * inverse * geometry.source.y
      + 3 * inverse2 * t * geometry.sourceControl.y
      + 3 * inverse * t2 * geometry.targetControl.y
      + t2 * t * geometry.target.y,
  }
}

function samplePolyline(points: VideoWorkflowPosition[]) {
  return points.slice(0, -1).flatMap((point, index) => Array.from({ length: 9 }, (_, step) => ({
    x: point.x + (points[index + 1].x - point.x) * step / 8,
    y: point.y + (points[index + 1].y - point.y) * step / 8,
  }))).concat(points.at(-1) || [])
}

function edgeSamples(
  graph: VideoWorkflowGraph,
  edge: VideoWorkflowEdge,
  curve = edge.curve,
  route = edge.route,
) {
  const geometry = edgeGeometry(graph, edge, curve)
  if (!geometry) return []
  if (route?.length) {
    const offset = curve || { x: 0, y: 0 }
    return samplePolyline([
      geometry.source,
      ...route.map((point) => ({ x: point.x + offset.x, y: point.y + offset.y })),
      geometry.target,
    ])
  }
  return Array.from({ length: 31 }, (_, index) => cubicPoint(geometry, index / 30))
}

function pointInsideBox(point: VideoWorkflowPosition, box: NodeBox, padding = COLLISION_PADDING) {
  return point.x > box.x - padding
    && point.x < box.x + box.width + padding
    && point.y > box.y - padding
    && point.y < box.y + box.height + padding
}

function edgeNodeCollisions(
  graph: VideoWorkflowGraph,
  edge: VideoWorkflowEdge,
  curve = edge.curve,
  route = edge.route,
) {
  const samples = edgeSamples(graph, edge, curve, route).slice(2, -2)
  return nodeBoxes(graph)
    .filter((box) => box.id !== edge.source && box.id !== edge.target)
    .filter((box) => samples.some((point) => pointInsideBox(point, box)))
    .map((box) => box.id)
}

function compactRoute(points: VideoWorkflowPosition[]) {
  const unique = points.filter((point, index) => !index || point.x !== points[index - 1].x || point.y !== points[index - 1].y)
  return unique.filter((point, index) => {
    if (!index || index === unique.length - 1) return true
    const previous = unique[index - 1]
    const next = unique[index + 1]
    return !((previous.x === point.x && point.x === next.x) || (previous.y === point.y && point.y === next.y))
  })
}

function routeThroughLane(geometry: EdgeGeometry, laneY: number) {
  const direction = geometry.target.x >= geometry.source.x ? 1 : -1
  const horizontalGap = Math.abs(geometry.target.x - geometry.source.x)
  const stub = Math.min(54, Math.max(28, horizontalGap * .12))
  return compactRoute([
    { x: geometry.source.x + direction * stub, y: geometry.source.y },
    { x: geometry.source.x + direction * stub, y: laneY },
    { x: geometry.target.x - direction * stub, y: laneY },
    { x: geometry.target.x - direction * stub, y: geometry.target.y },
  ])
}

function routeEdges(graph: VideoWorkflowGraph) {
  const routedSamples: VideoWorkflowPosition[][] = []
  const boxes = nodeBoxes(graph)
  const edges = [...graph.edges].sort((left, right) => {
    const leftGeometry = edgeGeometry(graph, left)
    const rightGeometry = edgeGeometry(graph, right)
    const leftSpan = leftGeometry ? Math.abs(leftGeometry.target.x - leftGeometry.source.x) : 0
    const rightSpan = rightGeometry ? Math.abs(rightGeometry.target.x - rightGeometry.source.x) : 0
    return leftSpan - rightSpan || left.id.localeCompare(right.id)
  })

  for (const edge of edges) {
    const defaultGeometry = edgeGeometry(graph, edge, undefined)
    if (!defaultGeometry) continue
    edge.route = undefined
    const defaultCollisions = edgeNodeCollisions(graph, edge, undefined, undefined)
    if (!defaultCollisions.length) {
      routedSamples.push(edgeSamples(graph, edge).slice(3, -3))
      continue
    }

    const midpointY = (defaultGeometry.source.y + defaultGeometry.target.y) / 2
    const candidateLanes = [...new Set([
      midpointY,
      defaultGeometry.source.y,
      defaultGeometry.target.y,
      ...boxes.flatMap((box) => [box.y - ROUTE_LANE_PADDING, box.y + box.height + ROUTE_LANE_PADDING]),
    ].map((value) => Math.round(value * 10) / 10))]
      .sort((left, right) => Math.abs(left - midpointY) - Math.abs(right - midpointY) || left - right)
    let best = routeThroughLane(defaultGeometry, candidateLanes[0])
    let bestScore = Number.POSITIVE_INFINITY
    for (const laneY of candidateLanes) {
      const route = routeThroughLane(defaultGeometry, laneY)
      const samples = edgeSamples(graph, edge, undefined, route).slice(3, -3)
      const collisions = edgeNodeCollisions(graph, edge, undefined, route).length
      const congestion = samples.reduce((score, point) => score + routedSamples.reduce((routeScore, route) => (
        routeScore + (route.some((other) => Math.hypot(point.x - other.x, point.y - other.y) < 20) ? 1 : 0)
      ), 0), 0)
      const score = collisions * 10_000 + congestion * 16 + Math.abs(laneY - midpointY) * .05
      if (score < bestScore) {
        best = route
        bestScore = score
      }
    }
    edge.route = best
    routedSamples.push(edgeSamples(graph, edge).slice(3, -3))
  }
}

export function autoLayoutVideoWorkflowGraph(
  source: VideoWorkflowGraph,
  mode: VideoWorkflowLayoutMode = 'all',
): VideoWorkflowGraph {
  const graph = cloneLayoutGraph(source)
  const layoutGraph = new dagre.graphlib.Graph({ directed: true, multigraph: true })
  layoutGraph.setGraph({
    rankdir: 'LR',
    align: 'UL',
    nodesep: NODE_GAP,
    edgesep: 28,
    ranksep: RANK_GAP,
    marginx: LAYOUT_MARGIN_X,
    marginy: LAYOUT_MARGIN_Y,
    acyclicer: 'greedy',
    ranker: 'network-simplex',
  })
  layoutGraph.setDefaultEdgeLabel(() => ({}))
  for (const node of graph.nodes) layoutGraph.setNode(node.id, videoWorkflowNodeSize(node))
  for (const edge of graph.edges) {
    const sameScene = graph.nodes.find((node) => node.id === edge.source)?.scene_id
      === graph.nodes.find((node) => node.id === edge.target)?.scene_id
    layoutGraph.setEdge(edge.source, edge.target, { weight: sameScene ? 8 : 2 }, edge.id)
  }
  dagre.layout(layoutGraph)

  for (const node of graph.nodes) {
    const positioned = layoutGraph.node(node.id)
    const size = videoWorkflowNodeSize(node)
    node.position = { x: positioned.x - size.width / 2, y: positioned.y - size.height / 2 }
  }
  if (mode === 'all') alignSceneGroups(graph)
  const proposed = new Map(graph.nodes.map((node) => [node.id, { ...node.position }]))
  return applyVideoWorkflowLayoutProposal(source, proposed, mode)
}

export function diagnoseVideoWorkflowLayout(graph: VideoWorkflowGraph): VideoWorkflowLayoutDiagnostics {
  const boxes = nodeBoxes(graph)
  const nodeOverlaps: Array<[string, string]> = []
  boxes.forEach((left, index) => boxes.slice(index + 1).forEach((right) => {
    if (overlaps(left, right, 24)) nodeOverlaps.push([left.id, right.id])
  }))

  const groupBoxes: NodeBox[] = graph.groups.map((group: VideoWorkflowGroup) => ({ id: group.id, ...group.position, ...group.size }))
  const groupOverlaps: Array<[string, string]> = []
  groupBoxes.forEach((left, index) => groupBoxes.slice(index + 1).forEach((right) => {
    if (overlaps(left, right, 12)) groupOverlaps.push([left.id, right.id])
  }))

  const edgeNodeOverlaps = graph.edges.flatMap((edge) => edgeNodeCollisions(graph, edge).map((nodeID) => ({ edgeID: edge.id, nodeID })))
  return { nodeOverlaps, groupOverlaps, edgeNodeOverlaps }
}
