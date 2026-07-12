import type {
  VideoAsset,
  VideoAssetVersion,
  VideoWorkflowEdge,
  VideoWorkflowGraph,
  VideoWorkflowImageTransform,
  VideoWorkflowNode,
  VideoWorkflowNodeRun,
  VideoWorkflowPort,
  VideoWorkflowPortType,
  VideoWorkflowTimelineClip,
  VideoWorkflowValidationIssue,
} from '@/api/videoWorkflow'

export interface VideoWorkflowAssetBinding {
  asset: VideoAsset
  version?: VideoAssetVersion
  assetID: string
  versionID: string
}

export const VIDEO_WORKFLOW_SCHEMA_VERSION = 2
export const VIDEO_WORKFLOW_MAX_NODES = 64
export const VIDEO_WORKFLOW_MAX_EDGES = 128
export const VIDEO_WORKFLOW_MAX_TIMELINE_CLIPS = 4
export const VIDEO_WORKFLOW_SCENE_DURATION_MS = 15_000
export const VIDEO_WORKFLOW_TIMELINE_STEP_MS = 100
export const VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS = 1_000
export const DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL = 'wan2.7-r2v'

export const DEFAULT_VIDEO_WORKFLOW_IMAGE_TRANSFORM: VideoWorkflowImageTransform = {
  crop: { x: 0, y: 0, width: 1, height: 1 },
  rotation: 0,
  flip_horizontal: false,
  flip_vertical: false,
}

export const VIDEO_WORKFLOW_NODE_CATALOG = [
  { type: 'story_brief', label: '故事简报', group: '文本', color: '#67e8f9' },
  { type: 'character', label: '角色护照', group: '角色', color: '#fbbf24' },
  { type: 'script', label: '分镜剧本', group: '文本', color: '#a7f3d0' },
  { type: 'background', label: '场景图片', group: '图像', color: '#fb7185' },
  { type: 'video', label: '场景视频', group: '视频', color: '#60a5fa' },
  { type: 'timeline', label: '顺序时间线', group: '合成', color: '#c4b5fd' },
  { type: 'compose', label: '成片输出', group: '合成', color: '#34d399' },
] as const

interface PortDefinition {
  id: string
  label: string
  type: VideoWorkflowPortType
  required?: boolean
}

interface NodePortDefinition {
  inputs: PortDefinition[]
  outputs: PortDefinition[]
}

const ports: Record<string, NodePortDefinition> = {
  story_brief: { inputs: [], outputs: [{ id: 'text', label: '故事要求', type: 'text' }] },
  character: {
    inputs: [{ id: 'brief', label: '故事要求', type: 'text' }],
    outputs: [{ id: 'selected', label: '角色定妆', type: 'image' }],
  },
  script: {
    inputs: [{ id: 'brief', label: '故事要求', type: 'text' }],
    outputs: [{ id: 'script', label: '分镜剧本', type: 'script' }],
  },
  scene: {
    inputs: [{ id: 'script', label: '分镜剧本', type: 'script' }],
    outputs: [{ id: 'scene', label: '场景描述', type: 'scene' }],
  },
  background: {
    inputs: [{ id: 'scene', label: '场景描述', type: 'scene' }],
    outputs: [{ id: 'image', label: '场景图片', type: 'image' }],
  },
  video: {
    inputs: [
      { id: 'scene', label: '分镜场景', type: 'scene' },
      { id: 'background', label: '背景', type: 'image' },
    ],
    outputs: [{ id: 'video', label: '场景视频', type: 'video' }],
  },
  timeline: {
    inputs: [],
    outputs: [{ id: 'videos', label: '有序片段', type: 'video_list' }],
  },
  compose: {
    inputs: [{ id: 'videos', label: '有序片段', type: 'video_list', required: true }],
    outputs: [{ id: 'video', label: '成片', type: 'video' }],
  },
}

let nodeSequence = 0

export function normalizeImageTransform(value: unknown): VideoWorkflowImageTransform {
  const transform = isRecord(value) ? value : DEFAULT_VIDEO_WORKFLOW_IMAGE_TRANSFORM
  const rawCrop = isRecord(transform.crop) ? transform.crop : DEFAULT_VIDEO_WORKFLOW_IMAGE_TRANSFORM.crop
  const numberOr = (candidate: unknown, fallback: number) => typeof candidate === 'number' && Number.isFinite(candidate)
    ? candidate
    : fallback
  const x = Math.min(0.999, Math.max(0, numberOr(rawCrop.x, 0)))
  const y = Math.min(0.999, Math.max(0, numberOr(rawCrop.y, 0)))
  const width = Math.min(1 - x, Math.max(0.001, numberOr(rawCrop.width, 1 - x)))
  const height = Math.min(1 - y, Math.max(0.001, numberOr(rawCrop.height, 1 - y)))
  const rotation = [0, 90, 180, 270].includes(transform.rotation) ? transform.rotation as 0 | 90 | 180 | 270 : 0
  return {
    crop: { x, y, width, height },
    rotation,
    flip_horizontal: Boolean(transform.flip_horizontal),
    flip_vertical: Boolean(transform.flip_vertical),
  }
}

function cloneImageTransform(transform = DEFAULT_VIDEO_WORKFLOW_IMAGE_TRANSFORM): VideoWorkflowImageTransform {
  return normalizeImageTransform(transform)
}

export function rotateImageTransform(
  transform: VideoWorkflowImageTransform,
  degrees: 90 | -90 = 90,
): VideoWorkflowImageTransform {
  const current = normalizeImageTransform(transform)
  const rotation = ((current.rotation + degrees + 360) % 360) as 0 | 90 | 180 | 270
  return { ...current, crop: { ...current.crop }, rotation }
}

export function toggleImageTransformFlip(
  transform: VideoWorkflowImageTransform,
  axis: 'horizontal' | 'vertical',
): VideoWorkflowImageTransform {
  const current = normalizeImageTransform(transform)
  return axis === 'horizontal'
    ? { ...current, crop: { ...current.crop }, flip_horizontal: !current.flip_horizontal }
    : { ...current, crop: { ...current.crop }, flip_vertical: !current.flip_vertical }
}

export interface VideoWorkflowImageTransformPatch extends Record<string, any> {
  reset?: boolean
  rotation_delta?: number
  flip_horizontal_toggle?: boolean
  flip_vertical_toggle?: boolean
  crop?: VideoWorkflowImageTransform['crop']
}

export function nextVideoWorkflowImageTransform(
  currentValue: unknown,
  patch: VideoWorkflowImageTransformPatch,
): VideoWorkflowImageTransform {
  const current = normalizeImageTransform(currentValue)
  if (patch.reset) return normalizeImageTransform(null)
  if (patch.rotation_delta) return rotateImageTransform(current, patch.rotation_delta > 0 ? 90 : -90)
  if (patch.flip_horizontal_toggle) return toggleImageTransformFlip(current, 'horizontal')
  if (patch.flip_vertical_toggle) return toggleImageTransformFlip(current, 'vertical')
  return normalizeImageTransform({ ...current, ...patch })
}

export function videoWorkflowImageVersionTransformState(versionID: string, version?: VideoAssetVersion) {
  const storedTransform = version?.image_transform || version?.metadata?.image_transform
  if (!storedTransform || !version?.parent_version_id) {
    return {
      transformBaseVersionID: versionID,
      imageTransform: normalizeImageTransform(null),
    }
  }
  return {
    transformBaseVersionID: version.parent_version_id,
    imageTransform: normalizeImageTransform(storedTransform),
  }
}

export function clearVideoWorkflowImageAssetBinding(node: VideoWorkflowNode): VideoWorkflowNode {
  const config: VideoWorkflowNode['config'] = { ...node.config, image_transform: normalizeImageTransform(null) }
  delete config.asset_id
  delete config.asset_version_id
  delete config.transform_base_version_id
  delete config.selected_version_id
  delete config.transform_versions
  delete config.preview_transform_pending
  delete config.preview_url
  return {
    ...node,
    version: (node.version || 1) + 1,
    asset_id: undefined,
    asset_version_id: undefined,
    status: 'stale',
    stale_reason: '图片素材绑定已清除，请重新生成',
    config,
  }
}

export function resolveVideoWorkflowDisplayedStatus(
  nodeStatus: VideoWorkflowNode['status'],
  runStatus: VideoWorkflowNode['status'],
) {
  if (nodeStatus === 'stale') return 'stale' as const
  return runStatus || nodeStatus
}

function portFromDefinition(port: PortDefinition): VideoWorkflowPort {
  return { ...port }
}

function defaultNodeConfig(type: string, title: string): Record<string, any> {
  if (type === 'character') {
    return { title, name: '新角色', adult_age: 22, prompt: '', candidate_count: 2 }
  }
  if (type === 'video') {
    return { title, duration_seconds: 15, duration_ms: VIDEO_WORKFLOW_SCENE_DURATION_MS, model: DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL }
  }
  if (type === 'background') {
    return { title, prompt: '', image_transform: cloneImageTransform() }
  }
  if (type === 'timeline') {
    return { title, clips: [], clip_node_ids: [] }
  }
  return { title, prompt: '' }
}

export function resolveVideoWorkflowVideoModel(settingsValue?: unknown, nodeValue?: unknown) {
  const normalize = (value: unknown) => {
    if (typeof value !== 'string' || !value.trim()) return ''
    const model = value.trim()
    return ['default', 'seedance-2.0', 'seedance 2.0'].includes(model.toLowerCase())
      ? DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL
      : model
  }
  const settingsModel = normalize(settingsValue)
  if (settingsModel) return settingsModel
  const nodeModel = normalize(nodeValue)
  if (nodeModel) return nodeModel
  return DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL
}

export function videoWorkflowModelLabel(value?: unknown) {
  const model = resolveVideoWorkflowVideoModel(undefined, value)
  return model.replace(/^wan2\.7(?=-|$)/i, 'Wan2.7')
}

export function makeVideoWorkflowNode(
  type: string,
  position = { x: 120, y: 120 },
  sceneID?: string,
): VideoWorkflowNode {
  const item = VIDEO_WORKFLOW_NODE_CATALOG.find((entry) => entry.type === type)
  const definition = ports[type] || { inputs: [], outputs: [] }
  nodeSequence += 1
  const title = item?.label || '自定义节点'
  const mediaNode = type === 'background' || type === 'video'
  const node: VideoWorkflowNode = {
    id: `${type}_${Date.now().toString(36)}_${nodeSequence}`,
    type,
    title,
    position,
    scene_id: sceneID,
    status: 'idle',
    enabled: true,
    locked: type === 'timeline' || type === 'compose',
    size: mediaNode ? { width: 208, height: 176 } : { width: 188, height: 108 },
    config: defaultNodeConfig(type, title),
    inputs: definition.inputs.map(portFromDefinition),
    outputs: definition.outputs.map(portFromDefinition),
  }
  if (type === 'scene' || type === 'video') node.duration_seconds = 15
  return node
}

export function cloneWorkflowGraph(graph: VideoWorkflowGraph): VideoWorkflowGraph {
  return JSON.parse(JSON.stringify(graph)) as VideoWorkflowGraph
}

export function videoWorkflowNodeRunOutputVersionID(nodeRun?: VideoWorkflowNodeRun | null) {
  if (!nodeRun) return ''
  const output = nodeRun.output || {}
  const candidates = output.candidate_version_ids || output.candidates || output.images || []
  const firstCandidate = Array.isArray(candidates) ? candidates[0] : undefined
  return String(
    nodeRun.output_version_id
    || output.selected_version_id
    || (typeof firstCandidate === 'string' ? firstCandidate : firstCandidate?.version_id || firstCandidate?.id)
    || '',
  )
}

export function resolveVideoWorkflowAssetBinding(
  node: VideoWorkflowNode | null | undefined,
  nodeRun: VideoWorkflowNodeRun | null | undefined,
  assets: VideoAsset[],
): VideoWorkflowAssetBinding | null {
  if (!node) return null
  const versionID = String(
    node.asset_version_id
    || node.config.asset_version_id
    || videoWorkflowNodeRunOutputVersionID(nodeRun)
    || '',
  )
  const configuredAssetID = String(node.asset_id || node.config.asset_id || '')
  const asset = assets.find((item) => item.id === configuredAssetID)
    || assets.find((item) => item.versions?.some((version) => version.id === versionID))
  if (!asset || !versionID) return null
  return {
    asset,
    version: asset.versions?.find((item) => item.id === versionID),
    assetID: asset.id,
    versionID,
  }
}

export function createTimelineClip(
  sourceNodeID: string,
  index = 0,
  sourcePort = 'video',
): VideoWorkflowTimelineClip {
  return {
    id: `clip_${index + 1}`,
    source_node_id: sourceNodeID,
    source_port: sourcePort,
    trim_in_ms: 0,
    trim_out_ms: VIDEO_WORKFLOW_SCENE_DURATION_MS,
  }
}

export function normalizeTimelineClip(clip: VideoWorkflowTimelineClip): VideoWorkflowTimelineClip {
  const snap = (value: number) => Math.round(value / VIDEO_WORKFLOW_TIMELINE_STEP_MS) * VIDEO_WORKFLOW_TIMELINE_STEP_MS
  const trimIn = Math.min(
    VIDEO_WORKFLOW_SCENE_DURATION_MS - VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS,
    Math.max(0, snap(Number.isFinite(clip.trim_in_ms) ? clip.trim_in_ms : 0)),
  )
  const trimOut = Math.min(
    VIDEO_WORKFLOW_SCENE_DURATION_MS,
    Math.max(
      trimIn + VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS,
      snap(Number.isFinite(clip.trim_out_ms) ? clip.trim_out_ms : VIDEO_WORKFLOW_SCENE_DURATION_MS),
    ),
  )
  return { ...clip, trim_in_ms: trimIn, trim_out_ms: trimOut }
}

export function updateTimelineClipTrim(
  clip: VideoWorkflowTimelineClip,
  trimInMS: number,
  trimOutMS: number,
): VideoWorkflowTimelineClip {
  return normalizeTimelineClip({ ...clip, trim_in_ms: trimInMS, trim_out_ms: trimOutMS })
}

export function createStarterVideoWorkflowGraph(): VideoWorkflowGraph {
  const brief = makeVideoWorkflowNode('story_brief', { x: 40, y: 120 })
  brief.id = 'brief'
  brief.title = '古风短剧创意简报'
  brief.config.title = brief.title
  brief.config.prompt = '围绕人物关系与身份反差，创作四段连续古风小剧场。'

  const roleNames = ['女主', '男主', '表小姐']
  const roles = roleNames.map((name, index) => {
    const node = makeVideoWorkflowNode('character', { x: 300, y: 20 + index * 150 })
    node.id = `character_${index + 1}`
    node.title = name
    node.config = {
      title: name,
      name,
      adult_age: 22 + index,
      prompt: `${name}，古风影视定妆，正面、侧面、背面三视图，角色一致性`,
      candidate_count: 2,
    }
    return node
  })

  const script = makeVideoWorkflowNode('script', { x: 580, y: 170 })
  script.id = 'script'
  script.title = '四场 15 秒分镜剧本'
  script.config.title = script.title
  script.config.prompt = '输出结构化 JSON；每场包含镜头、动作、表情、灯光、台词和声音时间段。'

  const groups = Array.from({ length: 4 }, (_, index) => ({
    id: `group_scene_${index + 1}`,
    type: 'scene' as const,
    scene_id: `scene_${index + 1}`,
    enabled: true,
    duration_seconds: 15 as const,
    node_ids: [`scene_${index + 1}`, `background_${index + 1}`, `video_${index + 1}`],
    position: { x: 810, y: index * 170 },
    size: { width: 700, height: 150 },
    collapsed: false,
  }))

  const sceneNodes = groups.map((group, index) => {
    const node = makeVideoWorkflowNode('scene', { x: 850, y: 20 + index * 170 }, group.scene_id)
    node.id = `scene_${index + 1}`
    node.title = `场景 ${String(index + 1).padStart(2, '0')} · 描述`
    node.config = { title: node.title, prompt: `承接上一场剧情的第 ${index + 1} 场场景，固定 15 秒` }
    return node
  })

  const backgrounds = groups.map((group, index) => {
    const node = makeVideoWorkflowNode('background', { x: 1070, y: 20 + index * 170 }, group.scene_id)
    node.id = `background_${index + 1}`
    node.title = `场景 ${String(index + 1).padStart(2, '0')} · 图片`
    node.config.title = node.title
    node.config.prompt = `承接上一场剧情的古风实景背景，场景 ${index + 1}`
    return node
  })

  const videos = groups.map((group, index) => {
    const node = makeVideoWorkflowNode('video', { x: 1290, y: 20 + index * 170 }, group.scene_id)
    node.id = `video_${index + 1}`
    node.title = `场景 ${String(index + 1).padStart(2, '0')} · 15s`
    node.config.title = node.title
    return node
  })

  const timeline = makeVideoWorkflowNode('timeline', { x: 1540, y: 240 })
  timeline.id = 'timeline'
  const clips = videos.map((node, index) => createTimelineClip(node.id, index))
  timeline.config = {
    title: '顺序时间线',
    clips,
    clip_node_ids: clips.map((clip) => clip.source_node_id),
  }
  timeline.inputs = clips.map((clip, index) => ({ id: clip.id, label: `片段 ${index + 1}`, type: 'video', required: true }))

  const compose = makeVideoWorkflowNode('compose', { x: 1780, y: 240 })
  compose.id = 'compose'

  const edges: VideoWorkflowEdge[] = [
    ...roles.map((role) => ({ id: `brief-${role.id}`, source: brief.id, source_port: 'text', target: role.id, target_port: 'brief' })),
    { id: 'brief-script', source: brief.id, source_port: 'text', target: script.id, target_port: 'brief' },
    ...roles.map((role, index) => {
      const portID = `role_${index + 1}`
      script.inputs!.push({ id: portID, label: role.title || portID, type: 'image', required: true })
      return { id: `${role.id}-script`, source: role.id, source_port: 'selected', target: script.id, target_port: portID }
    }),
    ...videos.flatMap((video, index) => [
      { id: `script-scene_${index + 1}`, source: script.id, source_port: 'script', target: `scene_${index + 1}`, target_port: 'script' },
      { id: `scene_${index + 1}-${video.id}`, source: `scene_${index + 1}`, source_port: 'scene', target: video.id, target_port: 'scene' },
      { id: `scene_${index + 1}-${backgrounds[index].id}`, source: `scene_${index + 1}`, source_port: 'scene', target: backgrounds[index].id, target_port: 'scene' },
      { id: `${backgrounds[index].id}-${video.id}`, source: backgrounds[index].id, source_port: 'image', target: video.id, target_port: 'background' },
      { id: `${video.id}-timeline`, source: video.id, source_port: 'video', target: timeline.id, target_port: clips[index].id },
    ]),
    { id: 'timeline-compose', source: timeline.id, source_port: 'videos', target: compose.id, target_port: 'videos' },
  ]

  return {
    schema_version: VIDEO_WORKFLOW_SCHEMA_VERSION,
    settings: {
      aspect_ratio: '9:16',
      resolution: '1080p',
      fps: 30,
      scene_duration_ms: VIDEO_WORKFLOW_SCENE_DURATION_MS,
      scene_duration_seconds: 15,
      character_approval_policy: 'manual',
      storyboard_approval_policy: 'manual',
      video_model: DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
    },
    nodes: [brief, ...roles, script, ...sceneNodes, ...backgrounds, ...videos, timeline, compose],
    edges,
    groups,
  }
}

function isRecord(value: unknown): value is Record<string, any> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function normalizeNodePorts(node: VideoWorkflowNode): VideoWorkflowNode {
  const definition = ports[node.type]
  if (!definition) return node
  if (!node.inputs?.length && definition.inputs.length) node.inputs = definition.inputs.map(portFromDefinition)
  if (!node.outputs?.length && definition.outputs.length) node.outputs = definition.outputs.map(portFromDefinition)
  return node
}

/** Upgrades a saved Graph v1 draft to the editable Graph v2 shape without changing node IDs. */
export function migrateVideoWorkflowGraph(value: unknown): VideoWorkflowGraph {
  if (!isRecord(value)) return createStarterVideoWorkflowGraph()
  const raw = JSON.parse(JSON.stringify(value)) as Record<string, any>
  const rawSettings = isRecord(raw.settings) ? raw.settings : {}
  const nodes: VideoWorkflowNode[] = (Array.isArray(raw.nodes) ? raw.nodes : [])
    .filter(isRecord)
    .map((item, index) => normalizeNodePorts({
      ...item,
      id: typeof item.id === 'string' && item.id ? item.id : `node_${index + 1}`,
      type: typeof item.type === 'string' ? item.type : 'custom',
      position: isRecord(item.position)
        ? { x: Number(item.position.x) || 0, y: Number(item.position.y) || 0 }
        : { x: 120, y: 120 },
      config: isRecord(item.config) ? item.config : {},
      locked: item.type === 'timeline' || item.type === 'compose' ? true : Boolean(item.locked),
    } as VideoWorkflowNode))

  const maxX = nodes.reduce((value, node) => Math.max(value, node.position.x), 120)
  let timeline = nodes.find((node) => node.type === 'timeline')
  if (!timeline) {
    timeline = makeVideoWorkflowNode('timeline', { x: maxX + 240, y: 240 })
    timeline.id = 'timeline'
    nodes.push(timeline)
  }
  let compose = nodes.find((node) => node.type === 'compose')
  if (!compose) {
    compose = makeVideoWorkflowNode('compose', { x: maxX + 480, y: 240 })
    compose.id = 'compose'
    nodes.push(compose)
  }

  const edges: VideoWorkflowEdge[] = (Array.isArray(raw.edges) ? raw.edges : [])
    .filter(isRecord)
    .map((edge, index) => ({
      id: typeof edge.id === 'string' && edge.id ? edge.id : `edge_${index + 1}`,
      source: String(edge.source || ''),
      source_port: String(edge.source_port || ''),
      target: String(edge.target || ''),
      target_port: String(edge.target_port || ''),
    }))
  if (!edges.some((edge) => edge.source === timeline!.id && edge.target === compose!.id)) {
    edges.push({
      id: 'timeline-compose',
      source: timeline.id,
      source_port: 'videos',
      target: compose.id,
      target_port: 'videos',
    })
  }

  const legacyClipIDs = Array.isArray(timeline.config.clip_node_ids)
    ? timeline.config.clip_node_ids.filter((id: unknown): id is string => typeof id === 'string')
    : edges
      .filter((edge) => edge.target === timeline!.id && edge.target_port === 'clip')
      .map((edge) => edge.source)
  const rawClips = Array.isArray(timeline.config.clips) ? timeline.config.clips : []
  const clips = (rawClips.length ? rawClips : legacyClipIDs.map((sourceNodeID, index) => createTimelineClip(sourceNodeID, index)))
    .filter(isRecord)
    .map((clip, index) => normalizeTimelineClip({
      id: typeof clip.id === 'string' && clip.id ? clip.id : `clip_${index + 1}`,
      source_node_id: String(clip.source_node_id || legacyClipIDs[index] || ''),
      source_port: String(clip.source_port || 'video'),
      trim_in_ms: Number(clip.trim_in_ms ?? 0),
      trim_out_ms: Number(clip.trim_out_ms ?? VIDEO_WORKFLOW_SCENE_DURATION_MS),
    }))
  timeline.config = {
    ...timeline.config,
    clips,
    clip_node_ids: clips.map((clip) => clip.source_node_id),
  }
  timeline.inputs = clips.map((clip, index) => ({ id: clip.id, label: `片段 ${index + 1}`, type: 'video', required: true }))
  const nonTimelineEdges = edges.filter((edge) => edge.target !== timeline!.id || edge.source_port !== 'video')
  edges.length = 0
  edges.push(...nonTimelineEdges)
  clips.forEach((clip, index) => {
    edges.push({
      id: `timeline_${clip.id}_${index + 1}`,
      source: clip.source_node_id,
      source_port: clip.source_port,
      target: timeline!.id,
      target_port: clip.id,
    })
  })

  for (const node of nodes) {
    if (node.type !== 'background') continue
    node.config.image_transform = normalizeImageTransform(node.config.image_transform)
  }

  return {
    schema_version: VIDEO_WORKFLOW_SCHEMA_VERSION,
    settings: {
      aspect_ratio: rawSettings.aspect_ratio === '16:9' || rawSettings.aspect_ratio === '1:1' ? rawSettings.aspect_ratio : '9:16',
      resolution: rawSettings.resolution === '1080p' ? '1080p' : '720p',
      fps: 30,
      scene_duration_ms: VIDEO_WORKFLOW_SCENE_DURATION_MS,
      scene_duration_seconds: 15,
      character_approval_policy: rawSettings.character_approval_policy === 'auto_first' ? 'auto_first' : 'manual',
      storyboard_approval_policy: rawSettings.storyboard_approval_policy === 'auto' ? 'auto' : 'manual',
      ...(typeof rawSettings.text_model === 'string' ? { text_model: rawSettings.text_model } : {}),
      ...(typeof rawSettings.image_model === 'string' ? { image_model: rawSettings.image_model } : {}),
      video_model: typeof rawSettings.video_model === 'string' && rawSettings.video_model.trim()
        ? rawSettings.video_model
        : DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
    },
    nodes,
    edges,
    groups: (Array.isArray(raw.groups) ? raw.groups : []) as VideoWorkflowGraph['groups'],
  }
}

function hasGraphCycle(nodes: VideoWorkflowNode[], edges: VideoWorkflowEdge[]): boolean {
  const indegree = new Map(nodes.map((node) => [node.id, 0]))
  const adjacency = new Map<string, string[]>()
  for (const edge of edges) {
    if (!indegree.has(edge.source) || !indegree.has(edge.target)) continue
    adjacency.set(edge.source, [...(adjacency.get(edge.source) || []), edge.target])
    indegree.set(edge.target, (indegree.get(edge.target) || 0) + 1)
  }
  const queue = [...indegree.entries()].filter(([, degree]) => degree === 0).map(([id]) => id)
  let visited = 0
  while (queue.length) {
    const id = queue.shift()!
    visited += 1
    for (const target of adjacency.get(id) || []) {
      const degree = (indegree.get(target) || 0) - 1
      indegree.set(target, degree)
      if (degree === 0) queue.push(target)
    }
  }
  return visited !== indegree.size
}

function validImageTransform(transform: unknown): boolean {
  if (!isRecord(transform) || !isRecord(transform.crop)) return false
  const crop = transform.crop
  const finite = [crop.x, crop.y, crop.width, crop.height].every((value) => typeof value === 'number' && Number.isFinite(value))
  return finite
    && crop.x >= 0 && crop.y >= 0 && crop.width > 0 && crop.height > 0
    && crop.x + crop.width <= 1 && crop.y + crop.height <= 1
    && [0, 90, 180, 270].includes(transform.rotation)
    && typeof transform.flip_horizontal === 'boolean'
    && typeof transform.flip_vertical === 'boolean'
}

export function validateVideoWorkflowGraph(
  graph: VideoWorkflowGraph,
  options: { requireComplete?: boolean } = {},
): VideoWorkflowValidationIssue[] {
  const issues: VideoWorkflowValidationIssue[] = []
  const add = (code: string, message: string, target: Partial<VideoWorkflowValidationIssue> = {}) => {
    issues.push({ code, message, ...target })
  }

  if (graph.nodes.length > VIDEO_WORKFLOW_MAX_NODES) add('node_limit_exceeded', `节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  if (graph.edges.length > VIDEO_WORKFLOW_MAX_EDGES) add('edge_limit_exceeded', `连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  if (graph.settings.fps !== 30) add('invalid_fps', '输出帧率必须为 30fps')
  if (!['9:16', '16:9', '1:1'].includes(graph.settings.aspect_ratio)) add('invalid_aspect_ratio', '画幅仅支持 9:16、16:9 或 1:1')
  if (graph.schema_version >= 2 && !['720p', '1080p'].includes(graph.settings.resolution || '')) add('invalid_resolution', '分辨率仅支持 720p 或 1080p')
  const durationMS = graph.settings.scene_duration_ms || (graph.settings.scene_duration_seconds || 0) * 1000
  if (durationMS !== VIDEO_WORKFLOW_SCENE_DURATION_MS) add('invalid_scene_duration', '视频节点生成时长必须为 15000ms')
  if (graph.schema_version >= 2 && !['manual', 'auto_first'].includes(graph.settings.character_approval_policy || '')) {
    add('invalid_character_approval_policy', '角色审批策略仅支持 manual 或 auto_first')
  }
  if (graph.schema_version >= 2 && !['manual', 'auto'].includes(graph.settings.storyboard_approval_policy || '')) {
    add('invalid_storyboard_approval_policy', '分镜审批策略仅支持 manual 或 auto')
  }

  const nodes = new Map<string, VideoWorkflowNode>()
  for (const node of graph.nodes) {
    if (!node.id || nodes.has(node.id)) {
      add('duplicate_node_id', '节点 ID 必须唯一且非空', { node_id: node.id })
      continue
    }
    nodes.set(node.id, node)
    if (node.type === 'background' && node.config.image_transform && !validImageTransform(node.config.image_transform)) {
      add('invalid_image_transform', '图片变换参数超出有效范围', { node_id: node.id })
    }
  }
  if (graph.nodes.filter((node) => node.type === 'character').length > 4) add('character_limit_exceeded', '角色节点不能超过 4 个')
  if (graph.nodes.filter((node) => node.type === 'scene').length > 4) add('scene_limit_exceeded', '场景节点不能超过 4 个')
  if (graph.nodes.filter((node) => node.type === 'timeline').length !== 1) add('invalid_timeline_count', '画布必须且只能包含 1 个时间线节点')
  if (graph.nodes.filter((node) => node.type === 'compose').length !== 1) add('invalid_compose_count', '画布必须且只能包含 1 个成片节点')

  const edgeIDs = new Set<string>()
  const incoming = new Map<string, number>()
  for (const edge of graph.edges) {
    if (!edge.id || edgeIDs.has(edge.id)) {
      add('duplicate_edge_id', '连线 ID 必须唯一且非空', { edge_id: edge.id })
      continue
    }
    edgeIDs.add(edge.id)
    const source = nodes.get(edge.source)
    const target = nodes.get(edge.target)
    if (!source || !target) {
      add('node_not_found', '连线引用了不存在的节点', { edge_id: edge.id })
      continue
    }
    if (source.id === target.id) add('graph_cycle', '节点不能连接自身', { edge_id: edge.id })
    const output = source.outputs?.find((port) => port.id === edge.source_port)
    const input = target.inputs?.find((port) => port.id === edge.target_port)
    if (!output || !input) {
      add('port_not_found', '连线引用了不存在的端口', { edge_id: edge.id })
      continue
    }
    if (output.type !== input.type) {
      add('port_type_mismatch', `${output.type} 不能连接 ${input.type}`, { edge_id: edge.id })
      continue
    }
    const key = `${target.id}\u0000${input.id}`
    const count = (incoming.get(key) || 0) + 1
    incoming.set(key, count)
    if (count > 1) {
      add('single_input_multiple_edges', '单值输入端口只能连接 1 条边', { node_id: target.id, edge_id: edge.id })
    }
  }

  if (hasGraphCycle(graph.nodes, graph.edges)) add('graph_cycle', '工作流图不能包含循环依赖')
  if (options.requireComplete) {
    for (const node of graph.nodes) {
      for (const input of node.inputs || []) {
        if (input.required && !incoming.has(`${node.id}\u0000${input.id}`)) {
          add('required_input_missing', `必需输入端口未连接：${input.id}`, { node_id: node.id })
        }
      }
    }
  }

  const timeline = graph.nodes.find((node) => node.type === 'timeline')
  const clips = Array.isArray(timeline?.config.clips) ? timeline!.config.clips as VideoWorkflowTimelineClip[] : []
  if (graph.schema_version >= 2 && clips.length > VIDEO_WORKFLOW_MAX_TIMELINE_CLIPS) {
    add('timeline_clip_count', `时间线片段不能超过 ${VIDEO_WORKFLOW_MAX_TIMELINE_CLIPS} 个`, { node_id: timeline?.id })
  }
  if (graph.schema_version >= 2 && options.requireComplete && clips.length < 1) {
    add('timeline_clip_count', `时间线必须包含 1–${VIDEO_WORKFLOW_MAX_TIMELINE_CLIPS} 个片段`, { node_id: timeline?.id })
  }
  const clipIDs = new Set<string>()
  for (const clip of clips) {
    if (!clip.id || clipIDs.has(clip.id)) {
      add('duplicate_timeline_clip_id', '时间线片段 ID 必须唯一且非空', { node_id: timeline?.id })
    }
    clipIDs.add(clip.id)
    const source = nodes.get(clip.source_node_id)
    const output = source?.outputs?.find((port) => port.id === clip.source_port)
    if (!source || !output || output.type !== 'video') {
      add('invalid_timeline_source', '时间线片段必须引用视频输出端口', { node_id: timeline?.id })
    }
    const duration = clip.trim_out_ms - clip.trim_in_ms
    if (
      !Number.isInteger(clip.trim_in_ms) || !Number.isInteger(clip.trim_out_ms)
      || clip.trim_in_ms < 0 || clip.trim_out_ms > VIDEO_WORKFLOW_SCENE_DURATION_MS
      || clip.trim_in_ms % VIDEO_WORKFLOW_TIMELINE_STEP_MS !== 0
      || clip.trim_out_ms % VIDEO_WORKFLOW_TIMELINE_STEP_MS !== 0
      || duration < VIDEO_WORKFLOW_MIN_CLIP_DURATION_MS
    ) {
      add('invalid_timeline_trim', '裁剪点须按 100ms 对齐，片段时长为 1000–15000ms', { node_id: timeline?.id })
    }
  }
  return issues
}

export function connectionError(graph: VideoWorkflowGraph, edge: Omit<VideoWorkflowEdge, 'id'>): string {
  if (graph.edges.length >= VIDEO_WORKFLOW_MAX_EDGES) return `连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`
  if (edge.source === edge.target) return '节点不能连接自身'
  const source = graph.nodes.find((node) => node.id === edge.source)
  const target = graph.nodes.find((node) => node.id === edge.target)
  if (!source || !target) return '连接节点不存在'
  const output = source.outputs?.find((port) => port.id === edge.source_port)
  const input = target.inputs?.find((port) => port.id === edge.target_port)
  if (!output || !input) return '连接端口不存在'
  if (output.type !== input.type) return `${output.type} 不能连接 ${input.type}`
  if (graph.edges.some((item) => item.source === edge.source && item.source_port === edge.source_port
    && item.target === edge.target && item.target_port === edge.target_port)) return '连接已存在'
  if (graph.edges.some((item) => item.target === edge.target && item.target_port === edge.target_port)) {
    return '单值输入端口只能连接 1 条边'
  }

  const adjacency = new Map<string, string[]>()
  for (const item of graph.edges) {
    adjacency.set(item.source, [...(adjacency.get(item.source) || []), item.target])
  }
  const stack = [edge.target]
  const seen = new Set<string>()
  while (stack.length) {
    const current = stack.pop()!
    if (current === edge.source) return '连接会形成循环'
    if (seen.has(current)) continue
    seen.add(current)
    stack.push(...(adjacency.get(current) || []))
  }
  return ''
}

export function removeNodeFromGraph(graph: VideoWorkflowGraph, nodeID: string): VideoWorkflowGraph {
  const next = cloneWorkflowGraph(graph)
  const target = next.nodes.find((node) => node.id === nodeID)
  if (target?.type === 'timeline' || target?.type === 'compose') return next
  next.nodes = next.nodes.filter((node) => node.id !== nodeID)
  next.edges = next.edges.filter((edge) => edge.source !== nodeID && edge.target !== nodeID)
  const timeline = next.nodes.find((node) => node.type === 'timeline')
  if (Array.isArray(timeline?.config.clip_node_ids)) {
    timeline!.config.clip_node_ids = timeline!.config.clip_node_ids.filter((id: string) => id !== nodeID)
  }
  if (Array.isArray(timeline?.config.clips)) {
    timeline!.config.clips = (timeline!.config.clips as VideoWorkflowTimelineClip[])
      .filter((clip) => clip.source_node_id !== nodeID)
    timeline!.inputs = (timeline!.config.clips as VideoWorkflowTimelineClip[])
      .map((clip, index) => ({ id: clip.id, label: `片段 ${index + 1}`, type: 'video', required: true }))
  }
  next.groups = next.groups.map((group) => ({ ...group, node_ids: group.node_ids.filter((id) => id !== nodeID) }))
  return next
}

export function moveTimelineClip<T>(items: T[], from: number, to: number): T[] {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return [...items]
  const next = [...items]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

export function mobileWorkspaceView(width: number, requested: 'canvas' | 'timeline' | 'inspector') {
  return width < 768 ? requested : 'all'
}

export function desktopVideoWorkflowReady(width: number) {
  return width >= 1280
}

export function nodeStatusLabel(status?: string) {
  return ({
    idle: '待配置', queued: '排队中', running: '生成中', awaiting_approval: '待审批',
    cancel_pending: '停止中', succeeded: '已就绪', failed: '失败', canceled: '已停止', stale: '需更新',
  } as Record<string, string>)[status || 'idle'] || status || '待配置'
}
