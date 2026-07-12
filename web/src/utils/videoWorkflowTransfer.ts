import type {
  VideoWorkflowGraph,
  VideoWorkflowNode,
} from '@/api/videoWorkflow'
import {
  VIDEO_WORKFLOW_MAX_EDGES,
  VIDEO_WORKFLOW_MAX_NODES,
  VIDEO_WORKFLOW_NODE_CATALOG,
  cloneWorkflowGraph,
  migrateVideoWorkflowGraph,
  validateVideoWorkflowGraph,
} from '@/utils/videoWorkflowGraph'

export const VIDEO_WORKFLOW_TRANSFER_FORMAT = 'gpt2api.video-workflow'
export const VIDEO_WORKFLOW_TRANSFER_VERSION = 1
export const VIDEO_WORKFLOW_TRANSFER_MAX_BYTES = 1024 * 1024
export const VIDEO_WORKFLOW_TRANSFER_MAX_DEPTH = 32

type JsonRecord = Record<string, any>

export interface VideoWorkflowTransferDocument {
  format: typeof VIDEO_WORKFLOW_TRANSFER_FORMAT
  format_version: typeof VIDEO_WORKFLOW_TRANSFER_VERSION
  exported_at: string
  name: string
  graph: VideoWorkflowGraph
}

export interface ImportedVideoWorkflowGraph {
  graph: VideoWorkflowGraph
  name: string
  source: 'document' | 'graph'
  asset_reference_count: number
}

export class VideoWorkflowTransferError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'VideoWorkflowTransferError'
  }
}

const allowedNodeTypes = new Set<string>([
  ...VIDEO_WORKFLOW_NODE_CATALOG.map((item) => item.type),
  // 场景描述节点由模板/分镜自动维护，不在左侧可拖入目录中。
  'scene',
])
const allowedPortTypes = new Set(['text', 'script', 'scene', 'image', 'image_set', 'video', 'video_list'])
const transientConfigKeys = new Set([
  'preview_url',
  'signed_url',
  'download_url',
  'transform_versions',
  'preview_transform_pending',
])

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function byteLength(value: string) {
  return new TextEncoder().encode(value).byteLength
}

function assertMaximumDepth(root: unknown) {
  const stack: Array<{ value: unknown; depth: number }> = [{ value: root, depth: 1 }]
  while (stack.length) {
    const current = stack.pop()!
    if (current.depth > VIDEO_WORKFLOW_TRANSFER_MAX_DEPTH) {
      throw new VideoWorkflowTransferError(`JSON 嵌套不能超过 ${VIDEO_WORKFLOW_TRANSFER_MAX_DEPTH} 层`)
    }
    if (Array.isArray(current.value)) {
      current.value.forEach((value) => stack.push({ value, depth: current.depth + 1 }))
    } else if (isRecord(current.value)) {
      Object.values(current.value).forEach((value) => stack.push({ value, depth: current.depth + 1 }))
    }
  }
}

function assertPort(port: unknown, nodeID: string) {
  if (!isRecord(port) || typeof port.id !== 'string' || !port.id || !allowedPortTypes.has(String(port.type))) {
    throw new VideoWorkflowTransferError(`节点 ${nodeID} 包含无效端口`)
  }
  if ('label' in port && typeof port.label !== 'string') {
    throw new VideoWorkflowTransferError(`节点 ${nodeID} 的端口标签必须是字符串`)
  }
  for (const key of ['required', 'multiple']) {
    if (key in port && typeof port[key] !== 'boolean') {
      throw new VideoWorkflowTransferError(`节点 ${nodeID} 的端口 ${key} 必须是布尔值`)
    }
  }
}

function assertOptionalPrimitive(record: JsonRecord, keys: string[], type: 'string' | 'boolean', context: string) {
  for (const key of keys) {
    if (key in record && typeof record[key] !== type) {
      throw new VideoWorkflowTransferError(`${context} 的 ${key} 必须是${type === 'string' ? '字符串' : '布尔值'}`)
    }
  }
}

function assertOptionalInteger(record: JsonRecord, key: string, context: string) {
  if (key in record && !Number.isInteger(record[key])) {
    throw new VideoWorkflowTransferError(`${context} 的 ${key} 必须是整数`)
  }
}

function assertOptionalSize(value: unknown, context: string) {
  if (value === undefined) return
  if (!isRecord(value)
    || ('width' in value && !Number.isFinite(value.width))
    || ('height' in value && !Number.isFinite(value.height))) {
    throw new VideoWorkflowTransferError(`${context} 的 size 必须包含有效数值`)
  }
}

function effectiveAssetBinding(node: JsonRecord) {
  const topLevel = {
    assetID: typeof node.asset_id === 'string' ? node.asset_id.trim() : '',
    versionID: typeof node.asset_version_id === 'string' ? node.asset_version_id.trim() : '',
  }
  if (topLevel.assetID || topLevel.versionID) return topLevel
  const config = isRecord(node.config) ? node.config : {}
  return {
    assetID: typeof config.asset_id === 'string' ? config.asset_id.trim() : '',
    versionID: typeof config.asset_version_id === 'string' ? config.asset_version_id.trim() : '',
  }
}

function assertNode(node: unknown, schemaVersion: number) {
  if (!isRecord(node) || typeof node.id !== 'string' || !node.id || typeof node.type !== 'string') {
    throw new VideoWorkflowTransferError('节点必须包含非空 id 和 type')
  }
  if (!allowedNodeTypes.has(node.type)) {
    throw new VideoWorkflowTransferError(`不支持的节点类型：${node.type}`)
  }
  assertOptionalPrimitive(node, ['title', 'role_id', 'scene_id', 'asset_id', 'asset_version_id'], 'string', `节点 ${node.id}`)
  assertOptionalPrimitive(node, ['collapsed', 'enabled', 'locked'], 'boolean', `节点 ${node.id}`)
  assertOptionalInteger(node, 'version', `节点 ${node.id}`)
  assertOptionalInteger(node, 'duration_seconds', `节点 ${node.id}`)
  assertOptionalSize(node.size, `节点 ${node.id}`)
  if (node.type === 'scene' && node.duration_seconds !== 15) {
    throw new VideoWorkflowTransferError(`场景节点 ${node.id} 的时长必须为 15 秒`)
  }
  if (schemaVersion >= 2) {
    if (!isRecord(node.position) || !Number.isFinite(node.position.x) || !Number.isFinite(node.position.y)) {
      throw new VideoWorkflowTransferError(`节点 ${node.id} 缺少有效画布坐标`)
    }
    if (!isRecord(node.config) || !Array.isArray(node.inputs) || !Array.isArray(node.outputs)) {
      throw new VideoWorkflowTransferError(`节点 ${node.id} 缺少 Graph v2 配置或端口`)
    }
    assertOptionalPrimitive(node.config, ['title', 'prompt', 'model', 'asset_id', 'asset_version_id'], 'string', `节点 ${node.id} 配置`)
    node.inputs.forEach((port: unknown) => assertPort(port, node.id))
    node.outputs.forEach((port: unknown) => assertPort(port, node.id))
    if (node.type === 'character' || node.type === 'background') {
      const { assetID, versionID } = effectiveAssetBinding(node)
      if (Boolean(assetID) !== Boolean(versionID)) {
        throw new VideoWorkflowTransferError(`节点 ${node.id} 的素材 ID 与版本 ID 必须同时存在`)
      }
    }
  }
}

function assertGroups(groups: unknown[], nodes: unknown[]) {
  const nodeIDs = new Set(nodes.filter(isRecord).map((node) => String(node.id || '')))
  const groupIDs = new Set<string>()
  let sceneGroups = 0
  groups.forEach((group) => {
    if (!isRecord(group) || typeof group.id !== 'string' || !group.id || groupIDs.has(group.id)) {
      throw new VideoWorkflowTransferError('分组 ID 必须唯一且非空')
    }
    groupIDs.add(group.id)
    if (group.type !== 'scene' || typeof group.scene_id !== 'string' || group.duration_seconds !== 15 || !Array.isArray(group.node_ids)) {
      throw new VideoWorkflowTransferError(`分组 ${group.id} 不是有效的 15 秒场景分组`)
    }
    assertOptionalPrimitive(group, ['enabled', 'collapsed'], 'boolean', `分组 ${group.id}`)
    if (!isRecord(group.position) || !isRecord(group.size)
      || !Number.isFinite(group.position.x) || !Number.isFinite(group.position.y)
      || !Number.isFinite(group.size.width) || !Number.isFinite(group.size.height)) {
      throw new VideoWorkflowTransferError(`分组 ${group.id} 缺少有效画布范围`)
    }
    if (group.node_ids.some((id: unknown) => typeof id !== 'string' || !nodeIDs.has(id))) {
      throw new VideoWorkflowTransferError(`分组 ${group.id} 引用了不存在的节点`)
    }
    sceneGroups += 1
  })
  if (sceneGroups > 4) throw new VideoWorkflowTransferError('场景分组不能超过 4 个')
}

function assertTimelineSources(graph: VideoWorkflowGraph) {
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]))
  const timeline = graph.nodes.find((node) => node.type === 'timeline')
  const clips = Array.isArray(timeline?.config.clips) ? timeline.config.clips : []
  for (const clip of clips) {
    if (nodes.get(clip.source_node_id)?.type !== 'video') {
      throw new VideoWorkflowTransferError(`时间线片段 ${clip.id || ''} 必须引用视频节点`)
    }
  }
}

function assertRawGraphShape(value: unknown): asserts value is VideoWorkflowGraph {
  if (!isRecord(value) || !isRecord(value.settings) || !Array.isArray(value.nodes) || !Array.isArray(value.edges)
    || (value.groups !== undefined && !Array.isArray(value.groups))) {
    throw new VideoWorkflowTransferError('JSON 不是有效的视频工作流 Graph')
  }
  // Go 的 Graph.Groups 使用 omitempty；零分组图省略该字段时按空数组处理。
  if (value.groups === undefined) value.groups = []
  if (value.schema_version !== 1 && value.schema_version !== 2) {
    throw new VideoWorkflowTransferError('仅支持 Graph v1 或 v2')
  }
  assertOptionalPrimitive(value.settings, ['aspect_ratio', 'resolution', 'character_approval_policy', 'storyboard_approval_policy', 'text_model', 'image_model', 'video_model'], 'string', '画布设置')
  for (const key of ['fps', 'scene_duration_ms', 'scene_duration_seconds']) {
    assertOptionalInteger(value.settings, key, '画布设置')
  }
  if (value.nodes.length > VIDEO_WORKFLOW_MAX_NODES) {
    throw new VideoWorkflowTransferError(`节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  }
  if (value.edges.length > VIDEO_WORKFLOW_MAX_EDGES) {
    throw new VideoWorkflowTransferError(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  }
  value.nodes.forEach((node: unknown) => assertNode(node, value.schema_version))
  value.edges.forEach((edge: unknown) => {
    if (!isRecord(edge) || ['id', 'source', 'source_port', 'target', 'target_port'].some((key) => typeof edge[key] !== 'string' || !edge[key])) {
      throw new VideoWorkflowTransferError('连线必须包含完整的节点与端口标识')
    }
  })
  assertGroups(value.groups, value.nodes)
  if (value.schema_version >= 2) {
    const timeline = value.nodes.filter((node) => node.type === 'timeline')
    const compose = value.nodes.filter((node) => node.type === 'compose')
    if (timeline.length !== 1 || compose.length !== 1) {
      throw new VideoWorkflowTransferError('Graph v2 必须且只能包含一个时间线节点和一个成片节点')
    }
    if (!timeline[0].locked || !compose[0].locked) {
      throw new VideoWorkflowTransferError('时间线节点和成片节点必须保持锁定')
    }
    const rawIssues = validateVideoWorkflowGraph(value as VideoWorkflowGraph, { requireComplete: false })
    if (rawIssues.length) {
      throw new VideoWorkflowTransferError(formatValidationIssues(rawIssues))
    }
  }
}

function stripTransientConfig(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stripTransientConfig)
  if (!isRecord(value)) return value
  return Object.fromEntries(Object.entries(value)
    .filter(([key]) => !transientConfigKeys.has(key))
    .map(([key, child]) => [key, stripTransientConfig(child)]))
}

export function prepareVideoWorkflowGraphForTransfer(graph: VideoWorkflowGraph): VideoWorkflowGraph {
  const prepared = cloneWorkflowGraph(graph)
  prepared.nodes = prepared.nodes.map((node) => {
    const next = stripTransientConfig(node) as VideoWorkflowNode
    delete next.status
    delete next.progress
    delete next.output
    delete next.stale_reason
    return next
  })
  return prepared
}

function formatValidationIssues(issues: Array<{ message: string }>) {
  const visible = issues.slice(0, 3).map((issue) => issue.message).join('；')
  return `画布校验未通过：${visible}${issues.length > 3 ? `；另有 ${issues.length - 3} 项` : ''}`
}

function countAssetReferences(graph: VideoWorkflowGraph) {
  return graph.nodes.filter((node) => {
    const { assetID, versionID } = effectiveAssetBinding(node as JsonRecord)
    return Boolean(assetID && versionID)
  }).length
}

export function createVideoWorkflowTransferDocument(
  name: string,
  graph: VideoWorkflowGraph,
  now = new Date(),
): VideoWorkflowTransferDocument {
  return {
    format: VIDEO_WORKFLOW_TRANSFER_FORMAT,
    format_version: VIDEO_WORKFLOW_TRANSFER_VERSION,
    exported_at: now.toISOString(),
    name: name.trim() || '未命名视频工作流',
    graph: prepareVideoWorkflowGraphForTransfer(migrateVideoWorkflowGraph(graph)),
  }
}

export function serializeVideoWorkflowTransfer(
  name: string,
  graph: VideoWorkflowGraph,
  now = new Date(),
) {
  return `${JSON.stringify(createVideoWorkflowTransferDocument(name, graph, now), null, 2)}\n`
}

export function parseVideoWorkflowTransfer(text: string): ImportedVideoWorkflowGraph {
  if (byteLength(text) > VIDEO_WORKFLOW_TRANSFER_MAX_BYTES) {
    throw new VideoWorkflowTransferError('JSON 文件不能超过 1 MiB')
  }
  let root: unknown
  try {
    root = JSON.parse(text.replace(/^\uFEFF/, ''))
  } catch {
    throw new VideoWorkflowTransferError('JSON 语法错误')
  }
  assertMaximumDepth(root)
  if (!isRecord(root)) throw new VideoWorkflowTransferError('JSON 根节点必须是对象')

  let rawGraph: unknown = root
  let name = ''
  let source: ImportedVideoWorkflowGraph['source'] = 'graph'
  if ('graph' in root) {
    source = 'document'
    if (!('format' in root)) throw new VideoWorkflowTransferError('导入文档缺少 format')
    if (!('format_version' in root)) throw new VideoWorkflowTransferError('导入文档缺少 format_version')
    if (root.format !== VIDEO_WORKFLOW_TRANSFER_FORMAT) {
      throw new VideoWorkflowTransferError(`不支持的导入格式：${String(root.format)}`)
    }
    if (root.format_version !== VIDEO_WORKFLOW_TRANSFER_VERSION) {
      throw new VideoWorkflowTransferError(`不支持的导入格式版本：${String(root.format_version)}`)
    }
    rawGraph = root.graph
    name = typeof root.name === 'string' ? root.name.trim() : ''
  } else if ('format' in root || 'format_version' in root) {
    throw new VideoWorkflowTransferError('导入文档缺少 graph')
  }

  assertRawGraphShape(rawGraph)
  const graph = prepareVideoWorkflowGraphForTransfer(migrateVideoWorkflowGraph(rawGraph))
  graph.nodes.forEach((node) => assertNode(node, 2))
  assertGroups(graph.groups, graph.nodes)
  assertTimelineSources(graph)
  const issues = validateVideoWorkflowGraph(graph, { requireComplete: false })
  if (issues.length) throw new VideoWorkflowTransferError(formatValidationIssues(issues))
  if (graph.nodes.filter((node) => node.type === 'timeline').length !== 1
    || graph.nodes.filter((node) => node.type === 'compose').length !== 1) {
    throw new VideoWorkflowTransferError('导入画布缺少固定时间线或成片节点')
  }
  return { graph, name, source, asset_reference_count: countAssetReferences(graph) }
}

export function videoWorkflowTransferFilename(name: string, revision: number, now = new Date()) {
  const safeName = (name || '未命名视频工作流')
    .normalize('NFKC')
    .replace(/[\u0000-\u001f\u007f\\/:*?"<>|]+/g, '-')
    .replace(/\s+/g, ' ')
    .replace(/^[-.\s]+|[-.\s]+$/g, '')
    .slice(0, 80) || '未命名视频工作流'
  const stamp = now.toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15)
  return `${safeName}-R${Math.max(0, Math.trunc(revision || 0))}-${stamp}.video-workflow.json`
}
