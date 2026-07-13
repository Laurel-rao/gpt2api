import ELK from 'elkjs/lib/elk.bundled.js'
import type { ElkNode, ElkPort } from 'elkjs/lib/elk-api'
import type { VideoWorkflowGraph, VideoWorkflowNode, VideoWorkflowPosition } from '@/api/videoWorkflow'
import {
  applyVideoWorkflowLayoutProposal,
  type VideoWorkflowLayoutMode,
  videoWorkflowNodeSize,
} from './videoWorkflowLayout'

const elk = new ELK()

function portID(nodeID: string, kind: 'input' | 'output', id: string) {
  return `${nodeID}:${kind}:${id}`
}

function nodePorts(node: VideoWorkflowNode): ElkPort[] {
  const inputs = (node.inputs || []).map((port, index) => ({
    id: portID(node.id, 'input', port.id),
    width: 8,
    height: 8,
    layoutOptions: { 'elk.port.side': 'WEST', 'elk.port.index': String(index) },
  }))
  const outputs = (node.outputs || []).map((port, index) => ({
    id: portID(node.id, 'output', port.id),
    width: 8,
    height: 8,
    layoutOptions: { 'elk.port.side': 'EAST', 'elk.port.index': String(index) },
  }))
  return [...inputs, ...outputs]
}

function elkNode(node: VideoWorkflowNode): ElkNode {
  return {
    id: node.id,
    ...videoWorkflowNodeSize(node),
    ports: nodePorts(node),
    layoutOptions: {
      'elk.portConstraints': 'FIXED_ORDER',
    },
  }
}

function sceneOrder(value: string) {
  return Number(value.replace(/\D/g, '')) || Number.MAX_SAFE_INTEGER
}

function elkGraph(source: VideoWorkflowGraph): ElkNode {
  const groupedNodeIDs = new Set(source.groups.flatMap((group) => group.node_ids))
  const groups = [...source.groups]
    .sort((left, right) => sceneOrder(left.scene_id) - sceneOrder(right.scene_id) || left.id.localeCompare(right.id))
    .map((group) => ({
      id: group.id,
      children: group.node_ids
        .map((id) => source.nodes.find((node) => node.id === id))
        .filter(Boolean)
        .map((node) => elkNode(node!)),
      layoutOptions: {
        'elk.algorithm': 'layered',
        'elk.direction': 'RIGHT',
        'elk.edgeRouting': 'ORTHOGONAL',
        'elk.padding': '[top=46,left=30,bottom=18,right=30]',
        'elk.spacing.nodeNode': '42',
        'elk.layered.spacing.nodeNodeBetweenLayers': '64',
      },
    }))
  const ungrouped = source.nodes.filter((node) => !groupedNodeIDs.has(node.id)).map(elkNode)

  return {
    id: 'video-workflow-layout',
    children: [...ungrouped, ...groups],
    edges: source.edges.map((edge) => ({
      id: edge.id,
      sources: [portID(edge.source, 'output', edge.source_port)],
      targets: [portID(edge.target, 'input', edge.target_port)],
    })),
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
      'elk.padding': '[top=52,left=52,bottom=52,right=52]',
      'elk.spacing.nodeNode': '48',
      'elk.spacing.edgeNode': '24',
      'elk.layered.spacing.nodeNodeBetweenLayers': '72',
      'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
      'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
      'elk.layered.nodePlacement.favorStraightEdges': 'true',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
    },
  }
}

function collectPositions(layout: ElkNode) {
  const positions = new Map<string, VideoWorkflowPosition>()
  for (const child of layout.children || []) {
    const parent = { x: child.x || 0, y: child.y || 0 }
    if (child.children?.length) {
      for (const node of child.children) {
        positions.set(node.id, { x: parent.x + (node.x || 0), y: parent.y + (node.y || 0) })
      }
    } else positions.set(child.id, parent)
  }
  return positions
}

export async function elkLayoutVideoWorkflowGraph(source: VideoWorkflowGraph, mode: VideoWorkflowLayoutMode) {
  const layout = await elk.layout(elkGraph(source))
  return applyVideoWorkflowLayoutProposal(source, collectPositions(layout), mode)
}
