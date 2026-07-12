<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  useVueFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type EdgeUpdateEvent,
  type Node,
  type NodeChange,
  type NodeDragEvent,
} from '@vue-flow/core'
import {
  Aim,
  ArrowLeft,
  ArrowUp,
  Check,
  Clock,
  Download,
  EditPen,
  Grid,
  Menu,
  MoreFilled,
  Plus,
  RefreshLeft,
  RefreshRight,
  Upload,
  VideoPause,
  VideoPlay,
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import axios from 'axios'
import VideoWorkflowCanvas from '@/components/video-workflow/VideoWorkflowCanvas.vue'
import VideoWorkflowInspector from '@/components/video-workflow/VideoWorkflowInspector.vue'
import VideoWorkflowLibrary from '@/components/video-workflow/VideoWorkflowLibrary.vue'
import VideoWorkflowOutline from '@/components/video-workflow/VideoWorkflowOutline.vue'
import VideoWorkflowRunHistory from '@/components/video-workflow/VideoWorkflowRunHistory.vue'
import VideoWorkflowRunConfirm from '@/components/video-workflow/VideoWorkflowRunConfirm.vue'
import VideoWorkflowTimeline from '@/components/video-workflow/VideoWorkflowTimeline.vue'
import {
  approveVideoWorkflowCharacters,
  approveVideoWorkflowStoryboard,
  cancelVideoWorkflowRun,
  createVideoWorkflow,
  estimateVideoWorkflowRun,
  getVideoWorkflow,
  getVideoWorkflowRun,
  listVideoAssets,
  listVideoWorkflowRuns,
  listVideoWorkflowTemplates,
  listVideoWorkflows,
  runVideoWorkflow,
  signVideoAssetVersion,
  transformVideoAssetVersion,
  updateVideoWorkflow,
  uploadVideoAsset,
  validateVideoWorkflow,
  type VideoAsset,
  type VideoAssetVersion,
  type VideoWorkflow,
  type VideoWorkflowEdge,
  type VideoWorkflowGraph,
  type VideoWorkflowNode,
  type VideoWorkflowRun,
  type VideoWorkflowRunMode,
  type VideoWorkflowTemplate,
  type VideoWorkflowTimelineClip,
} from '@/api/videoWorkflow'
import {
  VIDEO_WORKFLOW_MAX_EDGES,
  VIDEO_WORKFLOW_MAX_NODES,
  VIDEO_WORKFLOW_NODE_CATALOG,
  clearVideoWorkflowImageAssetBinding,
  cloneWorkflowGraph,
  connectionError,
  createTimelineClip,
  createStarterVideoWorkflowGraph,
  desktopVideoWorkflowReady,
  migrateVideoWorkflowGraph,
  makeVideoWorkflowNode,
  moveTimelineClip,
  nextVideoWorkflowImageTransform,
  normalizeImageTransform,
  removeNodeFromGraph,
  resolveVideoWorkflowAssetBinding,
  resolveVideoWorkflowDisplayedStatus,
  resolveVideoWorkflowVideoModel,
  updateTimelineClipTrim,
  validateVideoWorkflowGraph,
  videoWorkflowImageVersionTransformState,
  videoWorkflowNodeRunOutputVersionID,
} from '@/utils/videoWorkflowGraph'
import { SerialVideoWorkflowOperationQueue, runConfirmedVideoWorkflowMutation } from '@/utils/videoWorkflowAsync'
import {
  VIDEO_WORKFLOW_TRANSFER_MAX_BYTES,
  parseVideoWorkflowTransfer,
  serializeVideoWorkflowTransfer,
  videoWorkflowTransferFilename,
} from '@/utils/videoWorkflowTransfer'

type PanelTab = 'nodes' | 'assets'
type CanvasTool = 'select' | 'pan' | 'connect'
type FlowData = { node?: VideoWorkflowNode; zone?: { title: string; subtitle: string; enabled?: boolean } }
type WorkspaceMenuCommand = 'outline' | 'import_json' | 'export_json'

const POLL_INTERVAL = 2500
const AUTO_SAVE_DELAY = 800
const HISTORY_LIMIT = 50
const RUN_HISTORY_PAGE_SIZE = 20
const RUN_STORAGE_KEY = 'gpt2api.video-workflow-runs'
const LAYOUT_STORAGE_KEY = 'gpt2api.video-workflow-layout.v2'
const LOCAL_DRAFT_PREFIX = 'gpt2api.video-workflow-draft.'
const PENDING_RUN_PREFIX = 'gpt2api.video-workflow-pending-run.'

const router = useRouter()
const loading = ref(true)
const saving = ref(false)
const creating = ref(false)
const runningAction = ref(false)
const backendAvailable = ref(true)
const revisionConflict = ref(false)
const viewportWidth = ref(typeof window === 'undefined' ? 1440 : window.innerWidth)
const templates = ref<VideoWorkflowTemplate[]>([])
const workflows = ref<VideoWorkflow[]>([])
const assets = ref<VideoAsset[]>([])
const activeWorkflow = ref<VideoWorkflow | null>(null)
const graph = ref<VideoWorkflowGraph>(createStarterVideoWorkflowGraph())
const activeRun = ref<VideoWorkflowRun | null>(null)
const panelTab = ref<PanelTab>('nodes')
const activeTool = ref<CanvasTool>('select')
const selectedNodeID = ref('background_2')
const selectedNodeIDs = ref<string[]>(['background_2'])
const selectedEdgeIDs = ref<string[]>([])
const selectedClipID = ref('clip_2')
const dirty = ref(false)
const autosaveReady = ref(false)
const changeSequence = ref(0)
const undoStack = ref<VideoWorkflowGraph[]>([])
const redoStack = ref<VideoWorkflowGraph[]>([])
const clipboard = ref<{ nodes: VideoWorkflowNode[]; edges: VideoWorkflowEdge[] } | null>(null)
const flowNodes = ref<Node<FlowData>[]>([])
const flowEdges = ref<Edge[]>([])
const createDialogVisible = ref(false)
const characterDialogVisible = ref(false)
const storyboardDialogVisible = ref(false)
const outlineVisible = ref(false)
const connectionDialogVisible = ref(false)
const connectionTarget = ref<{ nodeID: string; portID: string } | null>(null)
const connectionSource = ref('')
const pickingAsset = ref(false)
const pendingConnection = ref<{ nodeID: string; portID: string } | null>(null)
const quickConnectMenu = ref<{ x: number; y: number; position: { x: number; y: number } } | null>(null)
const alignmentGuides = reactive<{ x: number | null; y: number | null }>({ x: null, y: null })
const interactionArea = ref<'canvas' | 'timeline' | 'other'>('canvas')
const selectedTemplateID = ref<string | number>('')
const newWorkflowName = ref('雨夜重逢 · 60秒短剧')
const characterSelections = ref<Record<string, string>>({})
const storyboardJSON = ref('{}')
const deletionToast = ref<{ count: number } | null>(null)
const conflictDraftKey = ref('')
const timelineRef = ref<InstanceType<typeof VideoWorkflowTimeline> | null>(null)
const runConfirmRef = ref<InstanceType<typeof VideoWorkflowRunConfirm> | null>(null)
const runHistoryVisible = ref(false)
const runHistoryLoading = ref(false)
const runHistoryItems = ref<VideoWorkflowRun[]>([])
const runHistoryTotal = ref(0)
const runHistoryActionID = ref('')
const runHistoryDetail = ref<VideoWorkflowRun | null>(null)
const jsonFileInput = ref<HTMLInputElement | null>(null)
const jsonTransferBusy = ref(false)
const panelLayout = reactive({ left: 248, right: 320, timeline: 196, inspectorOpen: true, timelineOpen: true })
const canvasViewport = reactive({ x: 0, y: 0, zoom: 1 })

let saveTimer: number | null = null
let retryTimer: number | null = null
let pollingTimer: number | null = null
let deletionTimer: number | null = null
let trimHistoryOpen = false
let trimHistoryTimer: number | null = null
let resizing: { kind: 'left' | 'right' | 'timeline'; start: number; value: number } | null = null
let spaceGesture: { startedAt: number; moved: boolean } | null = null
let activeSavePromise: Promise<VideoWorkflow | null> | null = null
let restoredLocalDraft = false
const imageTransformQueue = new SerialVideoWorkflowOperationQueue()
let imageTransformContextGeneration = 0
let deletedRecord: { before: VideoWorkflowGraph; nodeIDs: string[] } | null = null
let pollingGeneration = 0
let workspaceGeneration = 0
let componentUnmounted = false

const {
  fitView,
  zoomIn,
  zoomOut,
  setViewport,
  setCenter,
  screenToFlowCoordinate,
} = useVueFlow('video-workflow-flow')

const desktopReady = computed(() => desktopVideoWorkflowReady(viewportWidth.value))
const selectedNode = computed(() => graph.value.nodes.find((node) => node.id === selectedNodeID.value) || null)
const timelineNode = computed(() => graph.value.nodes.find((node) => node.type === 'timeline') || null)
const timelineClips = computed<VideoWorkflowTimelineClip[]>(() => Array.isArray(timelineNode.value?.config.clips)
  ? timelineNode.value!.config.clips as VideoWorkflowTimelineClip[]
  : [])
const activeRunNodeMap = computed(() => new Map((activeRun.value?.node_runs || []).map((item) => [item.node_id, item])))
const isRunActive = computed(() => ['queued', 'running', 'awaiting_character_approval', 'awaiting_storyboard_approval', 'cancel_pending'].includes(activeRun.value?.status || ''))
const saveState = computed(() => {
  if (revisionConflict.value) return '修订冲突 · 已保留本地副本'
  if (saving.value) return '保存中…'
  if (dirty.value && !backendAvailable.value) return '离线草稿已保留'
  if (dirty.value) return '待保存'
  return `已保存 · R${activeWorkflow.value?.revision || 0}`
})
const runStatus = computed(() => ({
  queued: '排队中', running: '生成中', awaiting_character_approval: '待选角色', awaiting_storyboard_approval: '待确认分镜',
  cancel_pending: '停止中', canceled: '已停止', succeeded: '已完成', failed: '运行失败',
} as Record<string, string>)[activeRun.value?.status || ''] || '')
const workspaceStyle = computed(() => ({
  '--left-panel': `${panelLayout.left}px`,
  '--right-panel': panelLayout.inspectorOpen ? `${panelLayout.right}px` : '0px',
  '--timeline-height': panelLayout.timelineOpen ? `${panelLayout.timeline}px` : '0px',
}))
const modKeyCode = computed(() => typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? 'Meta' : 'Control')
const selectedCompatibleSources = computed(() => {
  if (!connectionTarget.value) return []
  const target = graph.value.nodes.find((node) => node.id === connectionTarget.value?.nodeID)
  const input = target?.inputs?.find((port) => port.id === connectionTarget.value?.portID)
  if (!input) return []
  return graph.value.nodes.flatMap((node) => (node.outputs || [])
    .filter((port) => port.type === input.type && node.id !== target?.id)
    .map((port) => ({ value: `${node.id}\u0000${port.id}`, label: `${nodeTitle(node)} · ${port.label || port.id}` })))
})
const quickConnectTypes = computed(() => {
  if (!pendingConnection.value) return []
  const source = graph.value.nodes.find((node) => node.id === pendingConnection.value?.nodeID)
  const output = source?.outputs?.find((port) => port.id === pendingConnection.value?.portID)
  if (!output) return []
  return VIDEO_WORKFLOW_NODE_CATALOG.filter((item) => !['timeline', 'compose'].includes(item.type)).filter((item) => {
    const candidate = makeVideoWorkflowNode(item.type)
    return candidate.inputs?.some((port) => port.type === output.type)
  })
})

const characterCandidates = computed(() => (activeRun.value?.node_runs || [])
  .filter((item) => item.node_type === 'character' || graph.value.nodes.find((node) => node.id === item.node_id)?.type === 'character')
  .map((item) => {
    const node = graph.value.nodes.find((entry) => entry.id === item.node_id)
    const raw = item.output?.candidates || item.output?.images || item.output?.candidate_version_ids || []
    const candidates = (Array.isArray(raw) ? raw : []).map((candidate: any, index: number) => ({
      id: typeof candidate === 'string' ? candidate : String(candidate.asset_version_id || candidate.version_id || candidate.id || `${item.node_id}_${index}`),
      url: typeof candidate === 'string' ? '' : String(candidate.preview_url || candidate.url || ''),
    }))
    return { nodeID: item.node_id, nodeRunID: item.id, title: nodeTitle(node), hash: item.input_hash || '', candidates }
  }).filter((item) => item.candidates.length))
const canApproveCharacters = computed(() => characterCandidates.value.length > 0 && characterCandidates.value.every((item) => characterSelections.value[item.nodeID]))
const canApproveStoryboard = computed(() => { try { JSON.parse(storyboardJSON.value); return true } catch { return false } })

function nodeTitle(node?: VideoWorkflowNode | null) {
  return node?.title || node?.config?.title || node?.config?.name
    || VIDEO_WORKFLOW_NODE_CATALOG.find((item) => item.type === node?.type)?.label || node?.id || '未命名节点'
}

function currentVideoModel(node?: VideoWorkflowNode | null) {
  return resolveVideoWorkflowVideoModel(graph.value.settings.video_model, node?.config?.model)
}

function nodeRunOutputVersionID(nodeID: string) {
  return videoWorkflowNodeRunOutputVersionID(activeRunNodeMap.value.get(nodeID))
}

function assetBindingForNode(node: VideoWorkflowNode | null) {
  return resolveVideoWorkflowAssetBinding(node, node ? activeRunNodeMap.value.get(node.id) : null, assets.value)
}

function invalidateImageTransformContext() { imageTransformContextGeneration += 1 }

async function refreshAssets() {
  const items = await listVideoAssets({ limit: 100 })
  assets.value = items
  return items
}

async function ensureAssetBinding(nodeID: string) {
  let node = graph.value.nodes.find((item) => item.id === nodeID) || null
  let binding = assetBindingForNode(node)
  if (!binding && nodeRunOutputVersionID(nodeID)) {
    try { await refreshAssets() } catch { /* 后续给出明确的素材绑定错误。 */ }
    node = graph.value.nodes.find((item) => item.id === nodeID) || null
    binding = assetBindingForNode(node)
  }
  return binding
}

function applyVideoModelDefault(node: VideoWorkflowNode) {
  if (node.type === 'video') node.config.model = currentVideoModel(node)
  return node
}

function displayNode(node: VideoWorkflowNode): VideoWorkflowNode {
  const displayed = node.type === 'video'
    ? { ...node, config: { ...node.config, model: currentVideoModel(node) } }
    : node
  const runNode = activeRunNodeMap.value.get(node.id)
  const binding = assetBindingForNode(node)
  const enriched = binding ? {
    ...displayed,
    asset_id: binding.assetID,
    asset_version_id: binding.versionID,
    config: {
      ...displayed.config,
      asset_id: binding.assetID,
      asset_version_id: binding.versionID,
      preview_url: binding.version?.preview_url || displayed.config.preview_url,
    },
  } : displayed
  if (!runNode) return enriched
  const status = resolveVideoWorkflowDisplayedStatus(node.status, runNode.status)
  return {
    ...enriched,
    status,
    progress: status === 'stale' ? undefined : runNode.progress,
    output: runNode.output,
    stale_reason: status === 'stale' ? node.stale_reason : runNode.error || node.stale_reason,
  }
}

function syncFlow() {
  const zones: Node<FlowData>[] = graph.value.groups.map((group) => ({
    id: `__${group.scene_id}`,
    type: 'zone',
    position: { ...group.position },
    draggable: false,
    selectable: false,
    data: { zone: { title: `场景 ${group.scene_id.replace(/\D/g, '').padStart(2, '0')}`, subtitle: `15 秒 · ${group.enabled ? '已启用' : '已停用'}`, enabled: group.enabled } },
    style: { width: `${group.size.width}px`, height: `${group.size.height}px`, zIndex: -2 },
  }))
  flowNodes.value = [
    ...zones,
    ...graph.value.nodes.map((node) => ({
      id: node.id,
      type: 'workflow',
      position: { ...node.position },
      data: { node: displayNode(node) },
      selected: selectedNodeIDs.value.includes(node.id),
      draggable: !node.locked,
      style: { width: `${['background', 'image', 'video'].includes(node.type) ? 208 : 188}px`, zIndex: 2 },
    } as Node<FlowData>)),
  ]
  flowEdges.value = graph.value.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    sourceHandle: edge.source_port,
    targetHandle: edge.target_port,
    type: 'smoothstep',
    selected: selectedEdgeIDs.value.includes(edge.id),
    animated: activeRunNodeMap.value.get(edge.target)?.status === 'running',
  }))
}

function selectEdge(edgeID: string) {
  selectedEdgeIDs.value = [edgeID]
  selectedNodeIDs.value = []
  selectedNodeID.value = ''
  syncFlow()
}

function updateCanvasViewport(viewport: { x: number; y: number; zoom: number }) {
  Object.assign(canvasViewport, viewport)
}

function navigateFromMinimap(position: { x: number; y: number }) {
  interactionArea.value = 'canvas'
  void setCenter(position.x, position.y, { zoom: canvasViewport.zoom, duration: 0 })
}

function draftKey(workflowID = activeWorkflow.value?.id || 'preview') { return `${LOCAL_DRAFT_PREFIX}${workflowID}` }
function persistLocalDraft() {
  try {
    localStorage.setItem(draftKey(), JSON.stringify({ revision: activeWorkflow.value?.revision || 0, graph: graph.value, dirty: dirty.value, saved_at: Date.now() }))
  } catch { /* 浏览器存储不足时仍保留内存草稿。 */ }
}

function restoreLocalDraft(workflow: VideoWorkflow | null, fallback: VideoWorkflowGraph) {
  restoredLocalDraft = false
  conflictDraftKey.value = ''
  try {
    const key = draftKey(workflow?.id || 'preview')
    const value = JSON.parse(localStorage.getItem(key) || 'null')
    if (value?.dirty && (!workflow || value.revision === workflow.revision)) {
      restoredLocalDraft = true
      return migrateVideoWorkflowGraph(value.graph)
    }
    if (value?.dirty && workflow && value.revision !== workflow.revision) {
      conflictDraftKey.value = `${key}.conflict`
      localStorage.setItem(conflictDraftKey.value, JSON.stringify(value))
    }
  } catch { /* 忽略损坏的本地草稿。 */ }
  return migrateVideoWorkflowGraph(fallback)
}

function recoverConflictDraft() {
  if (!conflictDraftKey.value) return
  try {
    const value = JSON.parse(localStorage.getItem(conflictDraftKey.value) || 'null')
    if (!value?.graph) return
    invalidateImageTransformContext()
    pushUndo()
    graph.value = migrateVideoWorkflowGraph(value.graph)
    conflictDraftKey.value = ''
    markDirty()
    syncFlow()
    ElMessage.success('已恢复冲突草稿，请核对后保存')
  } catch { ElMessage.error('冲突草稿无法恢复') }
}

async function loadWorkspace(workflow?: VideoWorkflow | null) {
  if (componentUnmounted) return
  invalidateImageTransformContext()
  const generation = ++workspaceGeneration
  stopPolling()
  const next = workflow === undefined
    ? (workflows.value[0]?.id ? await getVideoWorkflow(workflows.value[0].id) : null)
    : workflow
  if (componentUnmounted || generation !== workspaceGeneration) return
  autosaveReady.value = false
  activeWorkflow.value = next
  runHistoryItems.value = []
  runHistoryTotal.value = 0
  runHistoryDetail.value = null
  activeRun.value = null
  if (next) {
    const latestPage = await listVideoWorkflowRuns(next.id, { limit: 1, offset: 0 }).catch(() => null)
    if (componentUnmounted || generation !== workspaceGeneration) return
    if (latestPage) {
      runHistoryItems.value = latestPage.items
      runHistoryTotal.value = latestPage.total
    }
    const runID = latestPage?.items[0]?.id || next.latest_run?.id || storedRunIDs()[next.id]
    if (runID) activeRun.value = await getVideoWorkflowRun(runID, true).catch(() => next.latest_run || null)
  }
  if (componentUnmounted || generation !== workspaceGeneration) return
  graph.value = restoreLocalDraft(next, next?.graph || createStarterVideoWorkflowGraph())
  const preferred = graph.value.nodes.find((node) => node.id === 'background_2') || graph.value.nodes[0]
  selectedNodeID.value = preferred?.id || ''
  selectedNodeIDs.value = preferred ? [preferred.id] : []
  selectedEdgeIDs.value = []
  selectedClipID.value = timelineClips.value[1]?.id || timelineClips.value[0]?.id || ''
  undoStack.value = []
  redoStack.value = []
  revisionConflict.value = false
  dirty.value = restoredLocalDraft
  syncFlow()
  await nextTick()
  fitView({ padding: .14, duration: 280 })
  autosaveReady.value = true
  if (activeRun.value && isRunActive.value) startPolling()
}

function storedRunIDs(): Record<string, string> { try { return JSON.parse(localStorage.getItem(RUN_STORAGE_KEY) || '{}') } catch { return {} } }
function rememberRun(workflowID: string, runID: string) { localStorage.setItem(RUN_STORAGE_KEY, JSON.stringify({ ...storedRunIDs(), [workflowID]: runID })) }

function upsertRunHistory(run: VideoWorkflowRun) {
  if (!activeWorkflow.value || run.workflow_id !== activeWorkflow.value.id) return
  const existed = runHistoryItems.value.some((item) => item.id === run.id)
  runHistoryItems.value = [run, ...runHistoryItems.value.filter((item) => item.id !== run.id)]
    .sort((a, b) => new Date(b.created_at || 0).getTime() - new Date(a.created_at || 0).getTime())
  if (!existed) runHistoryTotal.value = Math.max(runHistoryTotal.value + 1, runHistoryItems.value.length)
}

async function loadRunHistory(reset = true) {
  const workflowID = activeWorkflow.value?.id
  if (!workflowID || runHistoryLoading.value) return
  runHistoryLoading.value = true
  try {
    const offset = reset ? 0 : runHistoryItems.value.length
    const page = await listVideoWorkflowRuns(workflowID, { limit: RUN_HISTORY_PAGE_SIZE, offset })
    if (workflowID !== activeWorkflow.value?.id) return
    const merged = reset ? page.items : [...runHistoryItems.value, ...page.items]
    runHistoryItems.value = [...new Map(merged.map((item) => [item.id, item])).values()]
    runHistoryTotal.value = page.total
  } catch {
    ElMessage.error('生成历史加载失败，请重试')
  } finally {
    runHistoryLoading.value = false
  }
}

function openRunHistory() {
  runHistoryVisible.value = true
  runHistoryDetail.value = null
  void loadRunHistory(true)
}

async function inspectHistoryRun(run: VideoWorkflowRun) {
  runHistoryActionID.value = run.id
  try {
    runHistoryDetail.value = await getVideoWorkflowRun(run.id, true)
  } catch { ElMessage.error('运行详情加载失败') } finally { runHistoryActionID.value = '' }
}

async function bootstrap() {
  loading.value = true
  try {
    const [templateItems, workflowItems, assetItems] = await Promise.all([
      listVideoWorkflowTemplates(), listVideoWorkflows(), listVideoAssets({ limit: 100 }).catch(() => []),
    ])
    if (componentUnmounted) return
    templates.value = templateItems
    workflows.value = workflowItems
    assets.value = assetItems
    selectedTemplateID.value = templates.value[0]?.id || ''
    await loadWorkspace()
  } catch {
    backendAvailable.value = false
    await loadWorkspace(null)
  } finally {
    loading.value = false
  }
}

function pushUndo(snapshot = cloneWorkflowGraph(graph.value)) {
  undoStack.value = [...undoStack.value, snapshot].slice(-HISTORY_LIMIT)
  redoStack.value = []
}

function markDirty() {
  if (!autosaveReady.value) return
  dirty.value = true
  changeSequence.value += 1
  persistLocalDraft()
  scheduleSave()
}

function commitGraph(mutator: (target: VideoWorkflowGraph) => void, options: { history?: boolean; sync?: boolean } = {}) {
  if (options.history !== false) pushUndo()
  mutator(graph.value)
  markDirty()
  if (options.sync !== false) syncFlow()
}

function undo() {
  const previous = undoStack.value.pop()
  if (!previous) return
  invalidateImageTransformContext()
  redoStack.value.push(cloneWorkflowGraph(graph.value))
  graph.value = previous
  markDirty()
  syncFlow()
}

function redo() {
  const next = redoStack.value.pop()
  if (!next) return
  invalidateImageTransformContext()
  undoStack.value.push(cloneWorkflowGraph(graph.value))
  graph.value = next
  markDirty()
  syncFlow()
}

function scheduleSave(delay = AUTO_SAVE_DELAY) {
  if (!activeWorkflow.value || revisionConflict.value) return
  if (saveTimer) window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => { void saveWorkflow(true) }, delay)
}

function saveWorkflow(silent = false): Promise<VideoWorkflow | null> {
  if (activeSavePromise) return activeSavePromise
  if (!activeWorkflow.value || !dirty.value || revisionConflict.value) return Promise.resolve(activeWorkflow.value)
  activeSavePromise = performSaveWorkflow(silent).finally(() => { activeSavePromise = null })
  return activeSavePromise
}

async function performSaveWorkflow(silent = false): Promise<VideoWorkflow | null> {
  if (!activeWorkflow.value) return null
  saving.value = true
  const sequence = changeSequence.value
  const snapshot = cloneWorkflowGraph(graph.value)
  try {
    const updated = await updateVideoWorkflow(activeWorkflow.value.id, {
      name: activeWorkflow.value.name,
      revision: activeWorkflow.value.revision,
      graph: snapshot,
    })
    activeWorkflow.value = { ...activeWorkflow.value, ...updated, graph: migrateVideoWorkflowGraph(updated.graph || snapshot) }
    const index = workflows.value.findIndex((item) => item.id === updated.id)
    if (index >= 0) workflows.value[index] = activeWorkflow.value
    backendAvailable.value = true
    if (changeSequence.value === sequence) {
      dirty.value = false
      localStorage.removeItem(draftKey())
    } else scheduleSave()
    if (!silent) ElMessage.success(`已保存 R${activeWorkflow.value.revision}`)
    return activeWorkflow.value
  } catch (error) {
    persistLocalDraft()
    if (axios.isAxiosError(error) && error.response?.status === 409) {
      revisionConflict.value = true
      localStorage.setItem(`${draftKey()}.conflict`, JSON.stringify({ graph: cloneWorkflowGraph(graph.value), saved_at: Date.now() }))
      ElMessage.error('修订冲突，本地改动已另存为浏览器副本')
    } else {
      backendAvailable.value = false
      if (!retryTimer) retryTimer = window.setTimeout(() => { retryTimer = null; void saveWorkflow(true) }, 5000)
      if (!silent) ElMessage.warning('网络不可用，草稿已保存在本机')
    }
    return null
  } finally {
    saving.value = false
  }
}

async function flushSave(): Promise<boolean> {
  for (let attempt = 0; attempt < 4 && dirty.value; attempt += 1) {
    const saved = await saveWorkflow(true)
    if (!saved || revisionConflict.value) return false
  }
  return !dirty.value
}

async function reloadWorkflow() {
  if (!activeWorkflow.value) return
  await loadWorkspace(await getVideoWorkflow(activeWorkflow.value.id))
  ElMessage.success('已载入服务器版本')
}

function handleWorkspaceMenu(command: WorkspaceMenuCommand) {
  if (command === 'outline') {
    outlineVisible.value = true
    return
  }
  if (command === 'export_json') {
    exportWorkflowJSON()
    return
  }
  openWorkflowJSONImport()
}

function exportWorkflowJSON() {
  if (!activeWorkflow.value) return ElMessage.warning('请先创建或选择工作流')
  try {
    const content = serializeVideoWorkflowTransfer(activeWorkflow.value.name, graph.value)
    const blob = new Blob([content], { type: 'application/json;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = videoWorkflowTransferFilename(activeWorkflow.value.name, activeWorkflow.value.revision)
    link.style.display = 'none'
    document.body.appendChild(link)
    link.click()
    link.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
    ElMessage.success(`已导出 ${graph.value.nodes.length} 个节点`)
  } catch (error: any) {
    ElMessage.error(error?.message || '导出 JSON 失败')
  }
}

function openWorkflowJSONImport() {
  if (!activeWorkflow.value) return ElMessage.warning('请先创建或选择工作流')
  if (revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')
  if (isRunActive.value || runningAction.value) return ElMessage.warning('请先停止当前运行再导入')
  if (!jsonFileInput.value) return
  jsonFileInput.value.value = ''
  jsonFileInput.value.click()
}

async function importWorkflowJSON(event: Event) {
  const input = event.currentTarget as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || jsonTransferBusy.value) return
  if (!activeWorkflow.value) return ElMessage.warning('请先创建或选择工作流')
  if (file.size > VIDEO_WORKFLOW_TRANSFER_MAX_BYTES) return ElMessage.error('JSON 文件不能超过 1 MiB')
  if (isRunActive.value || runningAction.value) return ElMessage.warning('请先停止当前运行再导入')
  if (revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')

  const context = { workflowID: activeWorkflow.value.id, generation: workspaceGeneration }
  jsonTransferBusy.value = true
  try {
    const imported = parseVideoWorkflowTransfer(await file.text())
    if (activeWorkflow.value?.id !== context.workflowID || workspaceGeneration !== context.generation) {
      throw new Error('读取文件期间工作流已切换，请重新导入')
    }
    if (dirty.value && !await flushSave()) throw new Error('当前草稿尚未保存，已取消导入')
    if (activeWorkflow.value?.id !== context.workflowID || workspaceGeneration !== context.generation) {
      throw new Error('保存草稿期间工作流已切换，请重新导入')
    }
    const sequence = changeSequence.value
    const clipCount = imported.graph.nodes.find((node) => node.type === 'timeline')?.config.clips?.length || 0
    const sourceName = imported.name ? `“${imported.name}”` : '该 JSON'
    const assetNotice = imported.asset_reference_count
      ? `\n检测到 ${imported.asset_reference_count} 个素材引用；JSON 不包含媒体文件，跨账号导入后需重新选择素材。`
      : ''
    await ElMessageBox.confirm(
      `${sourceName}包含 ${imported.graph.nodes.length} 个节点、${imported.graph.edges.length} 条连线和 ${clipCount} 个片段。导入将替换当前画布，可通过撤销恢复。${assetNotice}`,
      '确认导入视频工作流',
      { type: 'warning', confirmButtonText: '替换画布', cancelButtonText: '取消' },
    )
    if (
      activeWorkflow.value?.id !== context.workflowID
      || workspaceGeneration !== context.generation
      || changeSequence.value !== sequence
      || isRunActive.value
      || runningAction.value
      || revisionConflict.value
    ) throw new Error('确认期间画布已发生变化，请重新导入')

    invalidateImageTransformContext()
    pushUndo()
    graph.value = cloneWorkflowGraph(imported.graph)
    const preferred = graph.value.nodes.find((node) => node.id === 'background_2') || graph.value.nodes[0]
    selectedNodeID.value = preferred?.id || ''
    selectedNodeIDs.value = preferred ? [preferred.id] : []
    selectedEdgeIDs.value = []
    selectedClipID.value = timelineClips.value[0]?.id || ''
    markDirty()
    syncFlow()
    await nextTick()
    fitView({ padding: .14, duration: 280 })

    const saved = await flushSave()
    if (saved) ElMessage.success(`已导入并保存为 R${activeWorkflow.value?.revision || 0}`)
    else if (!revisionConflict.value) ElMessage.warning('已导入本地草稿，尚未同步到服务器')
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.message || '导入 JSON 失败')
  } finally {
    jsonTransferBusy.value = false
  }
}

async function createFromTemplate() {
  if (!selectedTemplateID.value || !newWorkflowName.value.trim()) return
  creating.value = true
  try {
    const item = await createVideoWorkflow({ name: newWorkflowName.value.trim(), template_id: selectedTemplateID.value })
    workflows.value.unshift(item)
    createDialogVisible.value = false
    await loadWorkspace(await getVideoWorkflow(item.id))
  } finally { creating.value = false }
}

async function selectWorkflow(id: string) {
  if (id === activeWorkflow.value?.id) return
  if (runningAction.value) return ElMessage.warning('运行准备期间不能切换工作流')
  if (dirty.value && !await flushSave()) return ElMessage.error('当前草稿尚未保存，已取消切换')
  await loadWorkspace(await getVideoWorkflow(id))
}

function selectNode(id: string, additive = false) {
  if (id.startsWith('__')) return
  if (!panelLayout.inspectorOpen) setInspectorOpen(true)
  selectedNodeID.value = id
  if (additive) {
    selectedNodeIDs.value = selectedNodeIDs.value.includes(id)
      ? selectedNodeIDs.value.filter((item) => item !== id)
      : [...selectedNodeIDs.value, id]
  } else selectedNodeIDs.value = [id]
  selectedEdgeIDs.value = []
  syncFlow()
}

function clearSelection() {
  selectedNodeID.value = ''
  selectedNodeIDs.value = []
  selectedEdgeIDs.value = []
  quickConnectMenu.value = null
  pendingConnection.value = null
  syncFlow()
}

function onNodeChanges(changes: NodeChange[]) {
  for (const change of changes) {
    if (change.type !== 'select' || change.id.startsWith('__')) continue
    if (change.selected && !selectedNodeIDs.value.includes(change.id)) selectedNodeIDs.value.push(change.id)
    if (!change.selected) selectedNodeIDs.value = selectedNodeIDs.value.filter((id) => id !== change.id)
  }
}

function onEdgeChanges(changes: EdgeChange[]) {
  for (const change of changes) {
    if (change.type !== 'select') continue
    if (change.selected && !selectedEdgeIDs.value.includes(change.id)) selectedEdgeIDs.value.push(change.id)
    if (!change.selected) selectedEdgeIDs.value = selectedEdgeIDs.value.filter((id) => id !== change.id)
  }
}

function addNode(type: string, position?: { x: number; y: number }) {
  if (['timeline', 'compose'].includes(type)) return ElMessage.info('时间线和最终成片为固定系统节点')
  if (graph.value.nodes.length >= VIDEO_WORKFLOW_MAX_NODES) return ElMessage.warning(`节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  const count = graph.value.nodes.filter((node) => node.type === type).length
  if (type === 'character' && count >= 4) return ElMessage.warning('角色节点不能超过 4 个')
  const sceneID = ['background', 'video'].includes(type) ? selectedNode.value?.scene_id : undefined
  const node = applyVideoModelDefault(makeVideoWorkflowNode(type, position || { x: 320 + count * 32, y: 180 + count * 28 }, sceneID))
  commitGraph((target) => { target.nodes.push(node) })
  selectedNodeID.value = node.id
  selectedNodeIDs.value = [node.id]
  syncFlow()
}

function dropNode(event: DragEvent) {
  const type = event.dataTransfer?.getData('application/video-workflow-node')
  if (!type) return
  addNode(type, screenToFlowCoordinate({ x: event.clientX, y: event.clientY }))
}

function updateNodeTitle(value: string) {
  if (!selectedNode.value) return
  const id = selectedNode.value.id
  commitGraph((target) => {
    const node = target.nodes.find((item) => item.id === id)
    if (node) { node.title = value; node.config.title = value }
  })
}

function markDownstreamStale(target: VideoWorkflowGraph, sourceID: string) {
  const queue = [sourceID]
  const seen = new Set<string>()
  while (queue.length) {
    const source = queue.shift()!
    for (const edge of target.edges.filter((item) => item.source === source)) {
      if (seen.has(edge.target)) continue
      seen.add(edge.target)
      const node = target.nodes.find((item) => item.id === edge.target)
      if (node) { node.status = 'stale'; node.stale_reason = '上游输入已修改，请重新运行' }
      queue.push(edge.target)
    }
  }
}

function updateNodeConfig(key: string, value: any) {
  if (!selectedNode.value) return
  const id = selectedNode.value.id
  commitGraph((target) => {
    const node = target.nodes.find((item) => item.id === id)
    if (node) { node.config[key] = value; markDownstreamStale(target, id) }
  })
}

function copySelectedNodes() {
  const ids = new Set(selectedNodeIDs.value.filter((id) => !['timeline', 'compose'].includes(graph.value.nodes.find((node) => node.id === id)?.type || '')))
  if (!ids.size) return
  clipboard.value = {
    nodes: cloneWorkflowGraph({ ...graph.value, nodes: graph.value.nodes.filter((node) => ids.has(node.id)), edges: [], groups: [] }).nodes,
    edges: graph.value.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)).map((edge) => ({ ...edge })),
  }
}

function pasteNodes(offset = 28) {
  if (!clipboard.value) return
  if (graph.value.nodes.length + clipboard.value.nodes.length > VIDEO_WORKFLOW_MAX_NODES) return ElMessage.warning(`节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  if (graph.value.edges.length + clipboard.value.edges.length > VIDEO_WORKFLOW_MAX_EDGES) return ElMessage.warning(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  const characterCount = graph.value.nodes.filter((node) => node.type === 'character').length
  const pastedCharacters = clipboard.value.nodes.filter((node) => node.type === 'character').length
  if (characterCount + pastedCharacters > 4) return ElMessage.warning('角色节点不能超过 4 个')
  const idMap = new Map<string, string>()
  const clones = clipboard.value.nodes.map((node) => {
    const id = `${node.type}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`
    idMap.set(node.id, id)
    return { ...node, id, title: `${nodeTitle(node)} 副本`, position: { x: node.position.x + offset, y: node.position.y + offset }, config: { ...node.config, title: `${nodeTitle(node)} 副本` }, status: 'idle' as const }
  })
  const edges = clipboard.value.edges.map((edge) => ({ ...edge, id: `edge_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`, source: idMap.get(edge.source)!, target: idMap.get(edge.target)! }))
  commitGraph((target) => { target.nodes.push(...clones); target.edges.push(...edges) })
  selectedNodeIDs.value = clones.map((node) => node.id)
  selectedNodeID.value = clones.at(-1)?.id || ''
  syncFlow()
}

function duplicateSelectedNodes() { copySelectedNodes(); pasteNodes() }

async function deleteSelectedNodes() {
  const ids = selectedNodeIDs.value.filter((id) => !['timeline', 'compose'].includes(graph.value.nodes.find((node) => node.id === id)?.type || ''))
  if (!ids.length) return ElMessage.info('时间线与最终成片不可删除')
  if (timelineClips.value.length && timelineClips.value.every((clip) => ids.includes(clip.source_node_id))) {
    return ElMessage.warning('时间线至少保留 1 个视频片段')
  }
  const impact = graph.value.edges.filter((edge) => ids.includes(edge.source) || ids.includes(edge.target)).length
    + timelineClips.value.filter((clip) => ids.includes(clip.source_node_id)).length
  if (impact) {
    try {
      await ElMessageBox.confirm(`将同时移除 ${impact} 个连接或时间线引用。`, '删除节点', { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' })
    } catch { return }
  }
  const before = cloneWorkflowGraph(graph.value)
  commitGraph((target) => {
    let next = target
    for (const id of ids) next = removeNodeFromGraph(next, id)
    graph.value = next
  })
  deletedRecord = { before, nodeIDs: [...ids] }
  selectedNodeIDs.value = []
  selectedNodeID.value = ''
  syncFlow()
  deletionToast.value = { count: ids.length }
  if (deletionTimer) window.clearTimeout(deletionTimer)
  deletionTimer = window.setTimeout(() => { deletionToast.value = null }, 5000)
}

function restoreDeletedNodes() {
  if (!deletedRecord) return
  const record = deletedRecord
  pushUndo()
  const ids = new Set(record.nodeIDs)
  for (const node of record.before.nodes.filter((item) => ids.has(item.id))) {
    if (!graph.value.nodes.some((item) => item.id === node.id)) graph.value.nodes.push(node)
  }
  for (const edge of record.before.edges.filter((item) => ids.has(item.source) || ids.has(item.target))) {
    if (!graph.value.edges.some((item) => item.id === edge.id)
      && graph.value.nodes.some((node) => node.id === edge.source)
      && graph.value.nodes.some((node) => node.id === edge.target)) graph.value.edges.push(edge)
  }
  graph.value.groups.forEach((group) => {
    const previous = record.before.groups.find((item) => item.id === group.id)
    if (previous) group.node_ids = [...new Set([...group.node_ids, ...previous.node_ids.filter((id) => ids.has(id))])]
  })
  const previousTimeline = record.before.nodes.find((node) => node.type === 'timeline')
  const deletedClips = (previousTimeline?.config.clips || []).filter((clip: VideoWorkflowTimelineClip) => ids.has(clip.source_node_id))
  if (deletedClips.length) setTimelineClips(graph.value, [...timelineClips.value, ...deletedClips].slice(0, 4))
  selectedNodeIDs.value = record.nodeIDs
  selectedNodeID.value = record.nodeIDs.at(-1) || ''
  deletedRecord = null
  deletionToast.value = null
  markDirty()
  syncFlow()
}

function onNodeDragStart(event: NodeDragEvent) {
  if ((event.event as MouseEvent).altKey) duplicateSelectedNodes()
}

function onNodeDragStop(event: NodeDragEvent) {
  const moved = event.nodes?.length ? event.nodes : [event.node]
  commitGraph((target) => {
    for (const item of moved) {
      const node = target.nodes.find((entry) => entry.id === item.id)
      if (node && !node.locked) node.position = { ...item.position }
    }
  })
  alignmentGuides.x = null
  alignmentGuides.y = null
}

function onNodeDrag(event: NodeDragEvent) {
  const others = flowNodes.value.filter((node) => !node.id.startsWith('__') && node.id !== event.node.id)
  const matchX = others.find((node) => Math.abs(node.position.x - event.node.position.x) <= 6)
  const matchY = others.find((node) => Math.abs(node.position.y - event.node.position.y) <= 6)
  alignmentGuides.x = matchX ? matchX.position.x * canvasViewport.zoom + canvasViewport.x : null
  alignmentGuides.y = matchY ? matchY.position.y * canvasViewport.zoom + canvasViewport.y : null
}

async function onConnect(connection: Connection) {
  if (!connection.source || !connection.target || !connection.sourceHandle || !connection.targetHandle) return
  const candidate = { source: connection.source, source_port: connection.sourceHandle, target: connection.target, target_port: connection.targetHandle }
  let error = connectionError(graph.value, candidate)
  const old = graph.value.edges.find((edge) => edge.target === candidate.target && edge.target_port === candidate.target_port)
  if (error === '单值输入端口只能连接 1 条边' && old) {
    try {
      await ElMessageBox.confirm('该输入端口已有连接，是否替换？', '替换连接', { confirmButtonText: '替换', cancelButtonText: '取消' })
    } catch { return }
    const withoutOld = { ...graph.value, edges: graph.value.edges.filter((edge) => edge.id !== old.id) }
    error = connectionError(withoutOld, candidate)
    if (!error) commitGraph((target) => { target.edges = target.edges.filter((edge) => edge.id !== old.id); target.edges.push({ id: `edge_${Date.now().toString(36)}`, ...candidate }) })
    return
  }
  if (error) return ElMessage.warning(error)
  commitGraph((target) => { target.edges.push({ id: `edge_${Date.now().toString(36)}`, ...candidate }) })
  pendingConnection.value = null
  quickConnectMenu.value = null
}

function onConnectStart(payload: { nodeId?: string; handleId?: string | null; handleType?: string | null }) {
  if (payload.handleType !== 'source' || !payload.nodeId || !payload.handleId) return
  pendingConnection.value = { nodeID: payload.nodeId, portID: payload.handleId }
}

function onConnectEnd(event?: MouseEvent | TouchEvent) {
  const mouse = event as MouseEvent | undefined
  if (!pendingConnection.value || !mouse || (mouse.target as HTMLElement | null)?.closest('.vue-flow__handle')) {
    pendingConnection.value = null
    return
  }
  quickConnectMenu.value = {
    x: mouse.clientX - panelLayout.left,
    y: mouse.clientY - 64,
    position: screenToFlowCoordinate({ x: mouse.clientX, y: mouse.clientY }),
  }
}

function createConnectedNode(type: string) {
  if (!pendingConnection.value || !quickConnectMenu.value) return
  if (graph.value.nodes.length >= VIDEO_WORKFLOW_MAX_NODES) return ElMessage.warning(`节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  if (graph.value.edges.length >= VIDEO_WORKFLOW_MAX_EDGES) return ElMessage.warning(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  if (type === 'character' && graph.value.nodes.filter((node) => node.type === 'character').length >= 4) return ElMessage.warning('角色节点不能超过 4 个')
  const source = graph.value.nodes.find((node) => node.id === pendingConnection.value?.nodeID)
  const output = source?.outputs?.find((port) => port.id === pendingConnection.value?.portID)
  if (!source || !output) return
  const node = applyVideoModelDefault(makeVideoWorkflowNode(type, quickConnectMenu.value.position, source.scene_id))
  const input = node.inputs?.find((port) => port.type === output.type)
  if (!input) return
  const edge: VideoWorkflowEdge = {
    id: `edge_${Date.now().toString(36)}`,
    source: source.id,
    source_port: output.id,
    target: node.id,
    target_port: input.id,
  }
  commitGraph((target) => { target.nodes.push(node); target.edges.push(edge) })
  selectedNodeID.value = node.id
  selectedNodeIDs.value = [node.id]
  pendingConnection.value = null
  quickConnectMenu.value = null
  syncFlow()
}

function onEdgeUpdate(event: EdgeUpdateEvent) {
  const connection = event.connection
  if (!connection.source || !connection.target || !connection.sourceHandle || !connection.targetHandle) return
  const candidate = { source: connection.source, source_port: connection.sourceHandle, target: connection.target, target_port: connection.targetHandle }
  const draft = { ...graph.value, edges: graph.value.edges.filter((edge) => edge.id !== event.edge.id) }
  const error = connectionError(draft, candidate)
  if (error) return ElMessage.warning(error)
  commitGraph((target) => {
    const edge = target.edges.find((item) => item.id === event.edge.id)
    if (edge) Object.assign(edge, candidate)
  })
}

function removeSelectedEdges() {
  if (!selectedEdgeIDs.value.length) return
  commitGraph((target) => { target.edges = target.edges.filter((edge) => !selectedEdgeIDs.value.includes(edge.id)) })
  selectedEdgeIDs.value = []
}

function autoLayout() {
  const columns: Record<string, number> = { story_brief: 0, character: 1, script: 2, scene: 3, background: 4, video: 5, timeline: 6, compose: 7 }
  const rows = new Map<number, number>()
  commitGraph((target) => {
    for (const node of target.nodes) {
      const column = columns[node.type] ?? 3
      const row = rows.get(column) || 0
      node.position = { x: 60 + column * 260, y: 80 + row * 205 }
      rows.set(column, row + 1)
    }
  })
  nextTick(() => fitView({ padding: .12, duration: 260 }))
}

function alignSelected(axis: 'x' | 'y') {
  const nodes = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id))
  if (nodes.length < 2) return
  const value = Math.min(...nodes.map((node) => node.position[axis]))
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).forEach((node) => { node.position[axis] = value }))
}

function distributeSelected(axis: 'x' | 'y') {
  const nodes = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).sort((a, b) => a.position[axis] - b.position[axis])
  if (nodes.length < 3) return
  const gap = (nodes.at(-1)!.position[axis] - nodes[0].position[axis]) / (nodes.length - 1)
  commitGraph((target) => nodes.forEach((source, index) => { const node = target.nodes.find((item) => item.id === source.id); if (node) node.position[axis] = nodes[0].position[axis] + gap * index }))
}

function toggleLock() {
  if (!selectedNodeIDs.value.length) return
  const shouldLock = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).some((node) => !node.locked)
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !['timeline', 'compose'].includes(node.type)).forEach((node) => { node.locked = shouldLock }))
}

function toggleCollapse() {
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).forEach((node) => { node.collapsed = !node.collapsed }))
}

function groupSelected() {
  if (selectedNodeIDs.value.length < 2) return ElMessage.info('至少选择 2 个节点')
  const groupID = `canvas_group_${Date.now().toString(36)}`
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).forEach((node) => { node.config.canvas_group_id = groupID }))
  ElMessage.success('已建立画布分组')
}

function ungroupSelected() {
  const grouped = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && node.config.canvas_group_id)
  if (!grouped.length) return ElMessage.info('选中节点不属于画布分组')
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).forEach((node) => { delete node.config.canvas_group_id }))
  ElMessage.success('已解除画布分组')
}

function canSetTimelineClips(target: VideoWorkflowGraph, clips: VideoWorkflowTimelineClip[]) {
  const timeline = target.nodes.find((node) => node.type === 'timeline')
  if (!timeline) return false
  return target.edges.filter((edge) => edge.target !== timeline.id).length + clips.length <= VIDEO_WORKFLOW_MAX_EDGES
}

function toggleEnabled() {
  const editable = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !['timeline', 'compose'].includes(node.type))
  if (!editable.length) return
  const enabled = editable.some((node) => node.enabled === false)
  const selectedVideoIDs = new Set(editable.filter((node) => node.type === 'video').map((node) => node.id))
  if (!enabled && selectedVideoIDs.size && timelineClips.value.every((clip) => selectedVideoIDs.has(clip.source_node_id))) {
    return ElMessage.warning('至少保留 1 个启用的视频片段')
  }
  let nextClips = timelineClips.value.filter((clip) => enabled || !selectedVideoIDs.has(clip.source_node_id))
  if (enabled) {
    for (const nodeID of selectedVideoIDs) {
      if (nextClips.length >= 4 || nextClips.some((clip) => clip.source_node_id === nodeID)) continue
      let index = 1
      while (nextClips.some((clip) => clip.id === `clip_${index}`)) index += 1
      nextClips = [...nextClips, createTimelineClip(nodeID, index - 1)]
    }
  }
  if (selectedVideoIDs.size && !canSetTimelineClips(graph.value, nextClips)) return ElMessage.warning(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  commitGraph((target) => {
    const selected = target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !['timeline', 'compose'].includes(node.type))
    selected.forEach((node) => { node.enabled = enabled })
    const videoIDs = new Set(selected.filter((node) => node.type === 'video').map((node) => node.id))
    if (!videoIDs.size) return
    setTimelineClips(target, nextClips)
  })
}

function setTimelineClips(target: VideoWorkflowGraph, clips: VideoWorkflowTimelineClip[]): boolean {
  const timeline = target.nodes.find((node) => node.type === 'timeline')
  if (!timeline || !canSetTimelineClips(target, clips)) return false
  timeline.config.clips = clips
  timeline.config.clip_node_ids = clips.map((clip) => clip.source_node_id)
  timeline.inputs = clips.map((clip, index) => ({ id: clip.id, label: `片段 ${index + 1}`, type: 'video', required: true }))
  target.edges = target.edges.filter((edge) => edge.target !== timeline.id)
  target.edges.push(...clips.map((clip) => ({
    id: `edge_timeline_${clip.id}`,
    source: clip.source_node_id,
    source_port: clip.source_port,
    target: timeline.id,
    target_port: clip.id,
  })))
  return true
}

function moveClip(from: number, to: number) {
  if (to < 0 || to >= timelineClips.value.length) return
  commitGraph((target) => setTimelineClips(target, moveTimelineClip(timelineClips.value, from, to)), { sync: false })
}

function trimClip(clipID: string, trimInMS: number, trimOutMS: number) {
  if (!trimHistoryOpen) { pushUndo(); trimHistoryOpen = true }
  const next = timelineClips.value.map((clip) => clip.id === clipID ? updateTimelineClipTrim(clip, trimInMS, trimOutMS) : clip)
  setTimelineClips(graph.value, next)
  markDirty()
  if (trimHistoryTimer) window.clearTimeout(trimHistoryTimer)
  trimHistoryTimer = window.setTimeout(() => { trimHistoryOpen = false }, 400)
}

function removeClip(clipID: string) {
  if (timelineClips.value.length <= 1) return ElMessage.warning('时间线至少保留 1 个片段')
  commitGraph((target) => {
    setTimelineClips(target, timelineClips.value.filter((clip) => clip.id !== clipID))
  }, { sync: false })
  if (selectedClipID.value === clipID) selectedClipID.value = timelineClips.value[0]?.id || ''
}

function addSelectedVideoToTimeline() {
  const node = selectedNode.value
  if (!node || node.type !== 'video') return
  if (timelineClips.value.some((clip) => clip.source_node_id === node.id)) return ElMessage.info('该视频已在时间线中')
  if (timelineClips.value.length >= 4) return ElMessage.warning('时间线最多包含 4 个片段')
  let index = 1
  while (timelineClips.value.some((item) => item.id === `clip_${index}`)) index += 1
  const clip = createTimelineClip(node.id, index - 1)
  const nextClips = [...timelineClips.value, clip]
  if (!canSetTimelineClips(graph.value, nextClips)) return ElMessage.warning(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  commitGraph((target) => {
    setTimelineClips(target, nextClips)
    const source = target.nodes.find((item) => item.id === node.id)
    if (source) source.enabled = true
  }, { sync: false })
  selectedClipID.value = clip.id
}

function selectClip(clip: VideoWorkflowTimelineClip) { selectedClipID.value = clip.id; selectNode(clip.source_node_id) }

async function replaceImage(file: File) {
  if (!selectedNode.value) return
  try {
    const asset = await uploadVideoAsset(file)
    assets.value.unshift(asset)
    applyAssetToSelected(asset)
    ElMessage.success('图片已上传为新素材版本')
  } catch { ElMessage.error('图片上传失败') }
}

function applyAssetToSelected(asset: VideoAsset) {
  if (!selectedNode.value || asset.kind !== 'image') return
  const nodeID = selectedNode.value.id
  invalidateImageTransformContext()
  const version = asset.versions?.find((item) => item.id === asset.current_version_id) || asset.versions?.at(-1)
  const versionTransform = videoWorkflowImageVersionTransformState(version?.id || '', version)
  commitGraph((target) => {
    const node = target.nodes.find((item) => item.id === nodeID)
    if (!node) return
    node.asset_id = asset.id
    node.asset_version_id = version?.id
    node.config.asset_id = asset.id
    node.config.asset_version_id = version?.id
    node.config.transform_base_version_id = versionTransform.transformBaseVersionID
    node.config.image_transform = versionTransform.imageTransform
    node.config.preview_url = version?.preview_url || asset.preview_url || ''
    delete node.config.transform_versions
    delete node.config.preview_transform_pending
    node.status = 'succeeded'
    node.stale_reason = undefined
    markDownstreamStale(target, nodeID)
  })
  panelTab.value = 'nodes'
  pickingAsset.value = false
}

function chooseAsset() { pickingAsset.value = true; panelTab.value = 'assets'; ElMessage.info('在左侧素材库选择一张图片') }
function selectAsset(asset: VideoAsset) {
  if ((pickingAsset.value || ['background', 'image'].includes(selectedNode.value?.type || '')) && asset.kind === 'image') applyAssetToSelected(asset)
}

function applyImageTransform(patch: Record<string, any>) {
  if (!selectedNode.value) return Promise.resolve()
  const nodeID = selectedNode.value.id
  const contextGeneration = imageTransformContextGeneration
  return imageTransformQueue.enqueue(nodeID, async () => {
    if (componentUnmounted || contextGeneration !== imageTransformContextGeneration) return
    const binding = await ensureAssetBinding(nodeID)
    if (componentUnmounted || contextGeneration !== imageTransformContextGeneration) return
    const sourceNode = graph.value.nodes.find((item) => item.id === nodeID)
    if (!sourceNode || !binding) {
      ElMessage.error('当前图片尚未生成可编辑的素材版本，请先运行或上传图片')
      return
    }
    const sourceAssetID = binding.assetID
    const selectedVersionID = binding.versionID
    const baseVersionID = String(sourceNode.config.transform_base_version_id || selectedVersionID)
    const currentTransform = normalizeImageTransform(sourceNode.config.image_transform)
    const transform = nextVideoWorkflowImageTransform(currentTransform, patch)
    const transformSnapshot = JSON.stringify(currentTransform)

    let version: VideoAssetVersion
    try {
      version = await transformVideoAssetVersion(sourceAssetID, baseVersionID, transform)
      try {
        const signed = await signVideoAssetVersion(sourceAssetID, version.id, 'preview')
        version.preview_url = signed.url
      } catch { /* 变换接口通常已携带预览签名，缺失时素材刷新会补齐。 */ }
    } catch {
      if (contextGeneration === imageTransformContextGeneration) ElMessage.error('图片变换失败，当前版本保持不变')
      return
    }

    if (componentUnmounted || contextGeneration !== imageTransformContextGeneration) return
    const currentNode = graph.value.nodes.find((item) => item.id === nodeID)
    const currentBinding = assetBindingForNode(currentNode || null)
    const currentBaseVersionID = String(currentNode?.config.transform_base_version_id || currentBinding?.versionID || '')
    if (
      !currentNode
      || currentBinding?.assetID !== sourceAssetID
      || currentBinding?.versionID !== selectedVersionID
      || currentBaseVersionID !== baseVersionID
      || JSON.stringify(normalizeImageTransform(currentNode.config.image_transform)) !== transformSnapshot
    ) return

    const asset = assets.value.find((item) => item.id === sourceAssetID)
    if (asset) {
      asset.versions = [version, ...(asset.versions || []).filter((item) => item.id !== version.id)]
      asset.current_version_id = version.id
      asset.current_version = version.version
      asset.preview_url = version.preview_url
    }
    commitGraph((target) => {
      const targetNode = target.nodes.find((item) => item.id === nodeID)
      if (!targetNode) return
      targetNode.asset_id = sourceAssetID
      targetNode.asset_version_id = version.id
      targetNode.config.asset_id = sourceAssetID
      targetNode.config.asset_version_id = version.id
      targetNode.config.transform_base_version_id = baseVersionID
      targetNode.config.selected_version_id = version.id
      targetNode.config.image_transform = transform
      delete targetNode.config.transform_versions
      delete targetNode.config.preview_transform_pending
      if (version.preview_url) targetNode.config.preview_url = version.preview_url
      targetNode.status = 'succeeded'
      targetNode.stale_reason = undefined
      markDownstreamStale(target, nodeID)
    })
  })
}

function selectImageVersion(versionID: string) {
  if (!selectedNode.value || !versionID) return
  const nodeID = selectedNode.value.id
  invalidateImageTransformContext()
  const currentBinding = assetBindingForNode(selectedNode.value)
  const asset = assets.value.find((item) => item.versions?.some((entry) => entry.id === versionID)) || currentBinding?.asset
  const version = asset?.versions?.find((item) => item.id === versionID)
  const versionTransform = videoWorkflowImageVersionTransformState(versionID, version)
  commitGraph((target) => {
    const node = target.nodes.find((item) => item.id === nodeID)
    if (!node) return
    if (asset) {
      node.asset_id = asset.id
      node.config.asset_id = asset.id
    }
    node.asset_version_id = versionID
    node.config.asset_version_id = versionID
    node.config.selected_version_id = versionID
    if (version?.preview_url) node.config.preview_url = version.preview_url
    node.config.image_transform = versionTransform.imageTransform
    node.config.transform_base_version_id = versionTransform.transformBaseVersionID
    delete node.config.transform_versions
    delete node.config.preview_transform_pending
    node.status = 'succeeded'
    node.stale_reason = undefined
    markDownstreamStale(target, nodeID)
  })
}

async function regenerateSelectedImage() {
  const nodeID = selectedNodeID.value
  const node = graph.value.nodes.find((item) => item.id === nodeID)
  if (!node || !['background', 'image'].includes(node.type)) return startRun('node_only', nodeID)
  await startRun('node_only', nodeID, (target) => {
    const index = target.nodes.findIndex((item) => item.id === nodeID)
    if (index < 0) return
    target.nodes[index] = clearVideoWorkflowImageAssetBinding(target.nodes[index])
    markDownstreamStale(target, nodeID)
  })
}

function openConnectionPicker(portID: string) {
  if (!selectedNode.value) return
  connectionTarget.value = { nodeID: selectedNode.value.id, portID }
  connectionSource.value = selectedCompatibleSources.value[0]?.value || ''
  connectionDialogVisible.value = true
}

async function addKeyboardConnection() {
  if (!connectionTarget.value || !connectionSource.value) return
  const [source, sourcePort] = connectionSource.value.split('\u0000')
  await onConnect({ source, sourceHandle: sourcePort, target: connectionTarget.value.nodeID, targetHandle: connectionTarget.value.portID })
  connectionDialogVisible.value = false
}

function pendingRunRequestID(targetKey: string) {
  const storageKey = `${PENDING_RUN_PREFIX}${activeWorkflow.value?.id || 'unknown'}`
  try {
    const pending = JSON.parse(localStorage.getItem(storageKey) || 'null')
    if (pending?.target_key === targetKey && Date.now() - Number(pending.created_at || 0) < 10 * 60 * 1000) {
      return { id: String(pending.id), storageKey }
    }
  } catch { /* 创建新的幂等请求。 */ }
  const id = typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : `run_${Date.now().toString(36)}`
  localStorage.setItem(storageKey, JSON.stringify({ id, target_key: targetKey, created_at: Date.now() }))
  return { id, storageKey }
}

async function startRun(
  mode: VideoWorkflowRunMode = 'full',
  nodeID?: string,
  deferredMutation?: (target: VideoWorkflowGraph) => void,
) {
  if (runningAction.value) return
  if (!activeWorkflow.value) return ElMessage.warning('请先从模板创建工作流')
  const startNodeID = mode === 'full' ? undefined : (nodeID || selectedNodeID.value)
  if (!startNodeID && mode !== 'full') return ElMessage.warning('请先选择起始节点')
  runningAction.value = true
  try {
    if (dirty.value && !await flushSave()) return ElMessage.error('画布尚未保存，已阻止运行旧修订')
    const localIssues = validateVideoWorkflowGraph(graph.value, { requireComplete: mode === 'full' })
    if (localIssues.length) {
      const first = localIssues[0]
      if (first.node_id) selectNode(first.node_id)
      return ElMessage.error(first.message)
    }
    const validation = await validateVideoWorkflow(activeWorkflow.value.id, graph.value)
    if (!validation.valid) {
      const first = validation.issues?.[0]
      if (first?.node_id) selectNode(first.node_id)
      return ElMessage.error(first?.message || '画布校验未通过')
    }
    let target = { revision: activeWorkflow.value.revision, run_mode: mode, ...(startNodeID ? { start_node_id: startNodeID } : {}) }
    const estimate = await estimateVideoWorkflowRun(activeWorkflow.value.id, target)
    if (!runConfirmRef.value) throw new Error('运行确认组件尚未就绪')
    const confirmEstimate = async (currentEstimate = estimate) => {
      const confirmationContext = {
        workflowID: activeWorkflow.value!.id,
        revision: activeWorkflow.value!.revision,
        changeSequence: changeSequence.value,
      }
      await runConfirmRef.value!.open(mode, currentEstimate)
      if (
        dirty.value
        || activeWorkflow.value?.id !== confirmationContext.workflowID
        || activeWorkflow.value?.revision !== confirmationContext.revision
        || changeSequence.value !== confirmationContext.changeSequence
      ) {
        throw new Error('画布在费用确认期间发生变更，请保存后重新确认')
      }
    }

    const submit = async (currentEstimate = estimate) => {
      const requestTarget = `${activeWorkflow.value!.id}:${target.revision}:${mode}:${startNodeID || ''}`
      const pending = pendingRunRequestID(requestTarget)
      const run = await runVideoWorkflow(activeWorkflow.value!.id, { ...target, estimate_token: currentEstimate.token, request_id: pending.id, node_id: startNodeID })
      localStorage.removeItem(pending.storageKey)
      return run
    }

    if (deferredMutation) {
      const originalGraph = cloneWorkflowGraph(graph.value)
      const run = await runConfirmedVideoWorkflowMutation({
        confirm: () => confirmEstimate(estimate),
        apply: async () => {
          invalidateImageTransformContext()
          commitGraph(deferredMutation)
          if (!await flushSave()) throw new Error('重新生成准备失败，原图片版本已恢复')
        },
        submit: async () => {
          if (!activeWorkflow.value) throw new Error('工作流已切换，已取消重新生成')
          const validationAfterMutation = await validateVideoWorkflow(activeWorkflow.value.id, graph.value)
          if (!validationAfterMutation.valid) throw new Error(validationAfterMutation.issues?.[0]?.message || '重新生成画布校验未通过')
          target = { revision: activeWorkflow.value.revision, run_mode: mode, ...(startNodeID ? { start_node_id: startNodeID } : {}) }
          const refreshedEstimate = await estimateVideoWorkflowRun(activeWorkflow.value.id, target)
          if (
            refreshedEstimate.total_credits !== estimate.total_credits
            || refreshedEstimate.cached_credits !== estimate.cached_credits
          ) await confirmEstimate(refreshedEstimate)
          return submit(refreshedEstimate)
        },
        rollback: async () => {
          invalidateImageTransformContext()
          graph.value = cloneWorkflowGraph(originalGraph)
          markDirty()
          syncFlow()
          await flushSave()
        },
      })
      activeRun.value = run
    } else {
      await confirmEstimate(estimate)
      activeRun.value = await submit(estimate)
    }
    rememberRun(activeWorkflow.value.id, activeRun.value.id)
    upsertRunHistory(activeRun.value)
    syncFlow()
    startPolling()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.message || '启动运行失败')
  } finally { runningAction.value = false }
}

async function stopRun() {
  if (!activeRun.value) return
  runningAction.value = true
  try { await cancelVideoWorkflowRun(activeRun.value.id); await refreshRun() } finally { runningAction.value = false }
}
function startPolling() {
  if (componentUnmounted) return
  stopPolling()
  const generation = pollingGeneration
  const runID = activeRun.value?.id
  const workflowID = activeWorkflow.value?.id
  if (!runID || !workflowID) return
  const poll = async () => {
    if (componentUnmounted) return
    await refreshRun(runID, workflowID, generation)
    if (generation === pollingGeneration && activeRun.value?.id === runID && isRunActive.value) {
      pollingTimer = window.setTimeout(poll, POLL_INTERVAL)
    }
  }
  pollingTimer = window.setTimeout(poll, POLL_INTERVAL)
}
function stopPolling() {
  pollingGeneration += 1
  if (pollingTimer) window.clearTimeout(pollingTimer)
  pollingTimer = null
}
async function refreshRun(
  runID = activeRun.value?.id,
  workflowID = activeWorkflow.value?.id,
  generation = pollingGeneration,
) {
  if (!runID || !workflowID) return
  try {
    const detail = await getVideoWorkflowRun(runID, true)
    if (componentUnmounted || generation !== pollingGeneration || activeWorkflow.value?.id !== workflowID || activeRun.value?.id !== runID) return
    activeRun.value = detail
    if (!dirty.value && detail.workflow_revision === activeWorkflow.value?.revision) {
      for (const nodeRun of detail.node_runs || []) {
        const node = graph.value.nodes.find((item) => item.id === nodeRun.node_id)
        if (node?.status === 'stale') {
          node.status = undefined
          node.stale_reason = undefined
        }
      }
    }
    const missingGeneratedImage = (detail.node_runs || []).some((nodeRun) => {
      if (!['background', 'character'].includes(nodeRun.node_type || '')) return false
      const versionID = nodeRun.output_version_id || String(nodeRun.output?.selected_version_id || '')
      return Boolean(versionID) && !assets.value.some((asset) => asset.versions?.some((version) => version.id === versionID))
    })
    if (missingGeneratedImage) {
      try { await refreshAssets() } catch { /* 运行详情仍可继续刷新。 */ }
      if (componentUnmounted || generation !== pollingGeneration || activeWorkflow.value?.id !== workflowID || activeRun.value?.id !== runID) return
    }
    upsertRunHistory(activeRun.value)
    syncFlow()
    if (activeRun.value.status === 'awaiting_character_approval') openCharacterApproval()
    if (activeRun.value.status === 'awaiting_storyboard_approval') openStoryboardApproval()
    if (!isRunActive.value) stopPolling()
  } catch { /* 下一轮继续查询。 */ }
}

function openCharacterApproval() {
  for (const role of characterCandidates.value) if (!characterSelections.value[role.nodeID] && role.candidates[0]) characterSelections.value[role.nodeID] = role.candidates[0].id
  characterDialogVisible.value = true
}
async function approveCharacters() {
  if (!activeRun.value || !canApproveCharacters.value) return
  await approveVideoWorkflowCharacters(activeRun.value.id, { selections: characterCandidates.value.map((item) => ({ node_run_id: item.nodeRunID, input_hash: item.hash, selected_version_id: characterSelections.value[item.nodeID] })) })
  characterDialogVisible.value = false
  await refreshRun(); startPolling()
}
function openStoryboardApproval() {
  const nodeRun = activeRun.value?.node_runs?.find((item) => item.node_type === 'script' || graph.value.nodes.find((node) => node.id === item.node_id)?.type === 'script')
  storyboardJSON.value = JSON.stringify(nodeRun?.output?.script || nodeRun?.output?.storyboard || nodeRun?.output || {}, null, 2)
  storyboardDialogVisible.value = true
}
async function approveStoryboard() {
  if (!activeRun.value || !canApproveStoryboard.value) return
  const nodeRun = activeRun.value.node_runs?.find((item) => item.node_type === 'script' || graph.value.nodes.find((node) => node.id === item.node_id)?.type === 'script')
  await approveVideoWorkflowStoryboard(activeRun.value.id, { node_run_id: nodeRun?.id || '', input_hash: nodeRun?.input_hash || '', script: JSON.parse(storyboardJSON.value) })
  storyboardDialogVisible.value = false
  await refreshRun(); startPolling()
}

function createPreviewWindow() {
  const previewWindow = window.open('about:blank', '_blank')
  if (previewWindow) previewWindow.opener = null
  return previewWindow
}

async function previewRunOutput(run: VideoWorkflowRun | null, previewWindow: Window | null, allowCanvasFallback: boolean) {
  const navigatePreview = (url: string) => {
    if (!previewWindow) {
      ElMessage.error('浏览器阻止了预览窗口，请允许本站打开弹窗后重试')
      return false
    }
    previewWindow.location.replace(url)
    return true
  }
  const directURL = run?.output_url
  if (directURL) { navigatePreview(String(directURL)); return }
  const assetID = run?.output?.asset_id
  const versionID = run?.output_version_id || run?.output_asset_version_id
  if (assetID && versionID) {
    try {
      const signed = await signVideoAssetVersion(assetID, versionID, 'preview')
      navigatePreview(signed.url)
    } catch {
      previewWindow?.close()
      ElMessage.error('预览链接签发失败')
    }
    return
  }
  const compose = allowCanvasFallback ? graph.value.nodes.find((node) => node.type === 'compose') : null
  const url = compose?.output?.url || compose?.output?.output_url
  if (url) { navigatePreview(String(url)); return }
  previewWindow?.close()
  selectNode(compose?.id || 'compose')
  ElMessage.info('最终成片生成后可在此预览')
}

function previewOutput() { void previewRunOutput(activeRun.value, createPreviewWindow(), true) }

async function previewHistoryOutput(run: VideoWorkflowRun) {
  const previewWindow = createPreviewWindow()
  runHistoryActionID.value = run.id
  try {
    const detail = run.node_runs ? run : await getVideoWorkflowRun(run.id, true)
    await previewRunOutput(detail, previewWindow, false)
  } catch {
    previewWindow?.close()
    ElMessage.error('历史成片预览失败')
  } finally { runHistoryActionID.value = '' }
}

async function downloadRunOutput(run: VideoWorkflowRun | null) {
  const assetID = run?.output?.asset_id
  const versionID = run?.output_version_id || run?.output_asset_version_id
  if (!assetID || !versionID) return ElMessage.info('最终成片生成后可下载')
  try {
    const signed = await signVideoAssetVersion(assetID, versionID, 'download')
    const link = document.createElement('a')
    link.href = signed.url
    link.download = `${activeWorkflow.value?.name || 'video'}.mp4`
    link.rel = 'noopener'
    link.click()
  } catch { ElMessage.error('下载链接签发失败') }
}

function downloadOutput() { void downloadRunOutput(activeRun.value) }

async function downloadHistoryOutput(run: VideoWorkflowRun) {
  runHistoryActionID.value = run.id
  try {
    const detail = run.node_runs ? run : await getVideoWorkflowRun(run.id, true)
    await downloadRunOutput(detail)
  } catch { ElMessage.error('历史成片下载失败') } finally { runHistoryActionID.value = '' }
}

function generateFromHistory() {
  runHistoryVisible.value = false
  void startRun('full')
}

function handleCanvasKeydown(event: KeyboardEvent) {
  if (!desktopReady.value) return
  const target = event.target as HTMLElement | null
  if (target?.matches('input, textarea, select, [contenteditable="true"]') || target?.closest('.el-dialog, .el-message-box')) return
  const mod = event.metaKey || event.ctrlKey
  if (mod && event.key.toLowerCase() === 'z') { event.preventDefault(); event.shiftKey ? redo() : undo(); return }
  if (mod && event.key.toLowerCase() === 'y') { event.preventDefault(); redo(); return }
  if (mod && event.key.toLowerCase() === 'c') { event.preventDefault(); copySelectedNodes(); return }
  if (mod && event.key.toLowerCase() === 'v') { event.preventDefault(); pasteNodes(); return }
  if (mod && event.key.toLowerCase() === 'd') { event.preventDefault(); duplicateSelectedNodes(); return }
  if (mod && event.key.toLowerCase() === 'a') { event.preventDefault(); selectedNodeIDs.value = graph.value.nodes.map((node) => node.id); selectedNodeID.value = selectedNodeIDs.value.at(-1) || ''; syncFlow(); return }
  if (mod && event.key.toLowerCase() === 'g') { event.preventDefault(); event.shiftKey ? ungroupSelected() : groupSelected(); return }
  if (mod && event.key === 'Enter') { event.preventDefault(); void startRun('full'); return }
  if (event.key === 'Delete' || event.key === 'Backspace') { event.preventDefault(); selectedEdgeIDs.value.length ? removeSelectedEdges() : void deleteSelectedNodes(); return }
  if (event.key === 'Escape') { clearSelection(); return }
  if (event.key.toLowerCase() === 'v') { activeTool.value = 'select'; return }
  if (event.key.toLowerCase() === 'h') { activeTool.value = 'pan'; return }
  if (event.key === '+' || event.key === '=') { zoomIn(); return }
  if (event.key === '-') { zoomOut(); return }
  if (event.key === '0') { setViewport({ x: 0, y: 0, zoom: 1 }, { duration: 180 }); return }
  if (event.shiftKey && event.key === '1') { fitView({ padding: .12, duration: 220 }); return }
  if (event.shiftKey && event.key === '2') {
    const ids = new Set(selectedNodeIDs.value)
    fitView({ nodes: flowNodes.value.filter((node) => ids.has(node.id)).map((node) => node.id), padding: .2, duration: 220 }); return
  }
  if (event.key === 'F2' && selectedNode.value) {
    event.preventDefault()
    void ElMessageBox.prompt('输入新的节点名称', '重命名节点', { inputValue: nodeTitle(selectedNode.value), inputValidator: (value) => Boolean(value.trim()) || '名称不能为空' })
      .then(({ value }) => updateNodeTitle(value)).catch(() => undefined)
    return
  }
  if (event.key === ' ' && !event.repeat) {
    const inCanvas = Boolean(target?.closest('.canvas-stage')) || interactionArea.value === 'canvas'
    if (inCanvas) {
      spaceGesture = { startedAt: performance.now(), moved: false }
      return
    }
    event.preventDefault()
    timelineRef.value?.togglePlayback()
    return
  }
  if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key) && selectedNodeIDs.value.length) {
    event.preventDefault()
    const amount = event.shiftKey ? 10 : 1
    commitGraph((targetGraph) => targetGraph.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !node.locked).forEach((node) => {
      if (event.key === 'ArrowLeft') node.position.x -= amount
      if (event.key === 'ArrowRight') node.position.x += amount
      if (event.key === 'ArrowUp') node.position.y -= amount
      if (event.key === 'ArrowDown') node.position.y += amount
    }))
  }
}

function handleCanvasKeyup(event: KeyboardEvent) {
  if (!desktopReady.value) { spaceGesture = null; return }
  if (event.key !== ' ' || !spaceGesture) return
  const gesture = spaceGesture
  spaceGesture = null
  if (!gesture.moved && performance.now() - gesture.startedAt < 220) timelineRef.value?.togglePlayback()
}

function trackSpacePan(event: PointerEvent) {
  if (spaceGesture && event.buttons) spaceGesture.moved = true
}

function startResize(kind: 'left' | 'right' | 'timeline', event: PointerEvent) {
  resizing = { kind, start: kind === 'timeline' ? event.clientY : event.clientX, value: panelLayout[kind] }
  window.addEventListener('pointermove', resizePanel)
  window.addEventListener('pointerup', stopResize, { once: true })
  event.preventDefault()
}
function resizePanel(event: PointerEvent) {
  if (!resizing) return
  const current = resizing.kind === 'timeline' ? event.clientY : event.clientX
  const delta = current - resizing.start
  if (resizing.kind === 'left') panelLayout.left = Math.max(208, Math.min(340, resizing.value + delta))
  if (resizing.kind === 'right') panelLayout.right = Math.max(280, Math.min(420, resizing.value - delta))
  if (resizing.kind === 'timeline') panelLayout.timeline = Math.max(160, Math.min(320, resizing.value - delta))
}
function stopResize() {
  resizing = null
  window.removeEventListener('pointermove', resizePanel)
  persistPanelLayout()
}

function persistPanelLayout() {
  localStorage.setItem(LAYOUT_STORAGE_KEY, JSON.stringify(panelLayout))
}

function setInspectorOpen(open: boolean) {
  panelLayout.inspectorOpen = open
  interactionArea.value = 'canvas'
  persistPanelLayout()
}

function setTimelineOpen(open: boolean) {
  panelLayout.timelineOpen = open
  interactionArea.value = 'canvas'
  persistPanelLayout()
}

function restoreLayout() {
  try {
    const stored = JSON.parse(localStorage.getItem(LAYOUT_STORAGE_KEY) || '{}')
    if (Number.isFinite(stored.left)) panelLayout.left = Math.max(208, Math.min(340, stored.left))
    if (Number.isFinite(stored.right)) panelLayout.right = Math.max(280, Math.min(420, stored.right))
    if (Number.isFinite(stored.timeline)) panelLayout.timeline = Math.max(160, Math.min(320, stored.timeline))
    if (typeof stored.inspectorOpen === 'boolean') panelLayout.inspectorOpen = stored.inspectorOpen
    if (typeof stored.timelineOpen === 'boolean') panelLayout.timelineOpen = stored.timelineOpen
  } catch { /* 使用默认布局 */ }
}
function checkViewport() { viewportWidth.value = window.innerWidth }
function beforeUnload(event: BeforeUnloadEvent) { persistLocalDraft(); if (dirty.value) { event.preventDefault(); event.returnValue = '' } }

watch(() => activeRun.value?.status, (status) => {
  if (status === 'awaiting_character_approval') openCharacterApproval()
  if (status === 'awaiting_storyboard_approval') openStoryboardApproval()
})

onMounted(() => {
  restoreLayout()
  checkViewport()
  window.addEventListener('resize', checkViewport)
  window.addEventListener('keydown', handleCanvasKeydown)
  window.addEventListener('keyup', handleCanvasKeyup)
  window.addEventListener('pointermove', trackSpacePan)
  window.addEventListener('beforeunload', beforeUnload)
  void bootstrap()
})

onBeforeUnmount(() => {
  componentUnmounted = true
  invalidateImageTransformContext()
  workspaceGeneration += 1
  stopPolling()
  persistLocalDraft()
  if (saveTimer) window.clearTimeout(saveTimer)
  if (retryTimer) window.clearTimeout(retryTimer)
  if (deletionTimer) window.clearTimeout(deletionTimer)
  if (trimHistoryTimer) window.clearTimeout(trimHistoryTimer)
  window.removeEventListener('resize', checkViewport)
  window.removeEventListener('keydown', handleCanvasKeydown)
  window.removeEventListener('keyup', handleCanvasKeyup)
  window.removeEventListener('pointermove', trackSpacePan)
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('pointermove', resizePanel)
})
</script>

<template>
  <div class="video-workflow-page" :style="workspaceStyle" v-loading="loading">
    <div v-if="!desktopReady" class="desktop-required" role="alert">
      <div class="desktop-icon"><Grid /></div>
      <h1>视频画布请在桌面端使用</h1>
      <p>当前窗口宽度 {{ viewportWidth }}px，至少需要 1280px。</p>
      <el-button @click="router.push('/personal/dashboard')">返回个人中心</el-button>
    </div>

    <template v-else>
      <header class="workspace-header" @pointerdown="interactionArea = 'other'">
        <div class="brand-block"><span class="brand-mark"><Aim /></span><strong>灵境智创</strong></div>
        <button class="back-button" title="返回个人中心" @click="router.push('/personal/dashboard')"><ArrowLeft /></button>
        <div class="workflow-name">
          <el-select v-if="workflows.length" :model-value="activeWorkflow?.id || ''" filterable @change="selectWorkflow">
            <el-option v-for="item in workflows" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
          <strong v-else>{{ activeWorkflow?.name || '雨夜重逢 · 60秒短剧' }}</strong>
          <EditPen />
        </div>
        <div class="save-state" :class="{ dirty, offline: !backendAvailable }" aria-live="polite"><i /><span>{{ saveState }}</span></div>
        <div class="header-spacer" />
        <div v-if="runStatus" class="run-state" aria-live="polite"><i :class="activeRun?.status" />{{ runStatus }}<b v-if="activeRun?.progress !== undefined">{{ activeRun.progress }}%</b></div>
        <button class="icon-button" title="撤销（⌘/Ctrl+Z）" :disabled="!undoStack.length" @click="undo"><RefreshLeft /></button>
        <button class="icon-button" title="重做（⇧⌘/Ctrl+Z）" :disabled="!redoStack.length" @click="redo"><RefreshRight /></button>
        <select v-model="graph.settings.aspect_ratio" class="header-select" aria-label="画幅" @change="markDirty"><option>9:16</option><option>16:9</option><option>1:1</option></select>
        <select v-model="graph.settings.resolution" class="header-select resolution" aria-label="分辨率" @change="markDirty"><option>720p</option><option>1080p</option></select>
        <button class="history-button" :aria-expanded="runHistoryVisible" @click="openRunHistory"><Clock />生成历史<span v-if="runHistoryTotal">{{ runHistoryTotal }}</span></button>
        <button class="preview-button" @click="previewOutput"><VideoPlay />预览</button>
        <el-dropdown
          v-if="!isRunActive"
          class="generate-button"
          split-button
          type="primary"
          trigger="click"
          :disabled="runningAction"
          :button-props="{ loading: runningAction }"
          @click="startRun('full')"
          @command="(command: VideoWorkflowRunMode) => startRun(command)"
        >
          生成成片
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="node_only">运行当前节点</el-dropdown-item><el-dropdown-item command="downstream">运行当前及下游</el-dropdown-item><el-dropdown-item divided command="full">完整运行</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
        <button v-else class="stop-button" :disabled="runningAction" @click="stopRun"><VideoPause />停止</button>
        <el-dropdown
          class="workspace-actions"
          trigger="click"
          :disabled="jsonTransferBusy"
          @command="(command: WorkspaceMenuCommand) => handleWorkspaceMenu(command)"
        >
          <button class="icon-button" title="更多操作" aria-label="更多操作" :aria-busy="jsonTransferBusy"><MoreFilled /></button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="outline"><span class="workspace-menu-item"><Menu />结构大纲</span></el-dropdown-item>
              <el-dropdown-item command="import_json" divided :disabled="!activeWorkflow || isRunActive || runningAction || revisionConflict">
                <span class="workspace-menu-item"><Upload />导入 JSON</span>
              </el-dropdown-item>
              <el-dropdown-item command="export_json" :disabled="!activeWorkflow">
                <span class="workspace-menu-item"><Download />导出 JSON</span>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <input ref="jsonFileInput" class="json-file-input" type="file" accept=".json,application/json" @change="importWorkflowJSON" />
        <button class="icon-button" title="新建工作流" @click="createDialogVisible = true"><Plus /></button>
      </header>

      <div class="workspace-grid">
        <VideoWorkflowLibrary
          :tab="panelTab"
          :assets="assets"
          @update:tab="panelTab = $event"
          @add-node="addNode"
          @upload="replaceImage"
          @select-asset="selectAsset"
        />
        <button class="panel-resizer left-resizer" aria-label="调整左栏宽度" @pointerdown="startResize('left', $event)" />

        <VideoWorkflowCanvas
          v-model:active-tool="activeTool"
          v-model:nodes="flowNodes"
          v-model:edges="flowEdges"
          :backend-available="backendAvailable"
          :conflict-draft-key="conflictDraftKey"
          :mod-key-code="modKeyCode"
          :alignment-guides="alignmentGuides"
          :quick-connect-menu="quickConnectMenu"
          :quick-connect-types="quickConnectTypes"
          :graph-nodes="graph.nodes"
          :selected-node-i-ds="selectedNodeIDs"
          :selected-node-locked="Boolean(selectedNode?.locked)"
          :zoom="canvasViewport.zoom"
          :viewport="canvasViewport"
          @interaction="interactionArea = 'canvas'"
          @drop-node="dropNode"
          @recover-conflict-draft="recoverConflictDraft"
          @group-selected="groupSelected"
          @ungroup-selected="ungroupSelected"
          @auto-layout="autoLayout"
          @align-selected="alignSelected"
          @distribute-selected="distributeSelected"
          @toggle-enabled="toggleEnabled"
          @toggle-lock="toggleLock"
          @toggle-collapse="toggleCollapse"
          @delete-selected="selectedEdgeIDs.length ? removeSelectedEdges() : deleteSelectedNodes()"
          @nodes-change="onNodeChanges"
          @edges-change="onEdgeChanges"
          @node-click="selectNode"
          @node-drag-start="onNodeDragStart"
          @node-drag="onNodeDrag"
          @node-drag-stop="onNodeDragStop"
          @pane-click="clearSelection"
          @connect="onConnect"
          @connect-start="onConnectStart"
          @connect-end="onConnectEnd"
          @edge-update="onEdgeUpdate"
          @edge-click="selectEdge"
          @viewport-change="updateCanvasViewport"
          @minimap-navigate="navigateFromMinimap"
          @create-connected-node="createConnectedNode"
          @cancel-quick-connect="pendingConnection = null; quickConnectMenu = null"
          @zoom-out="zoomOut()"
          @zoom-in="zoomIn()"
          @fit-view="fitView({ padding: .12, duration: 220 })"
        />

        <button v-if="panelLayout.inspectorOpen" class="panel-resizer right-resizer" aria-label="调整检查器宽度" @pointerdown="startResize('right', $event)" />
        <VideoWorkflowInspector
          v-show="panelLayout.inspectorOpen"
          id="node-inspector"
          :node="selectedNode ? displayNode(selectedNode) : null"
          :assets="assets"
          :running="isRunActive"
          @update-title="updateNodeTitle"
          @update-config="updateNodeConfig"
          @transform="applyImageTransform"
          @select-version="selectImageVersion"
          @replace-file="replaceImage"
          @choose-asset="chooseAsset"
          @regenerate="regenerateSelectedImage"
          @run="startRun('node_only', selectedNodeID)"
          @delete="deleteSelectedNodes"
          @add-connection="openConnectionPicker"
          @add-to-timeline="addSelectedVideoToTimeline"
          @preview-output="previewOutput"
          @download-output="downloadOutput"
          @close="setInspectorOpen(false)"
        />

        <button v-if="panelLayout.timelineOpen" class="panel-resizer timeline-resizer" aria-label="调整时间线高度" @pointerdown="startResize('timeline', $event)" />
        <VideoWorkflowTimeline
          v-show="panelLayout.timelineOpen"
          id="workflow-timeline"
          ref="timelineRef"
          :clips="timelineClips"
          :nodes="graph.nodes"
          :selected-clip-i-d="selectedClipID"
          @pointerdown="interactionArea = 'timeline'"
          @select="selectClip"
          @move="moveClip"
          @trim="trimClip"
          @remove="removeClip"
          @close="setTimelineOpen(false)"
        />

        <button
          v-if="!panelLayout.inspectorOpen"
          class="panel-restore inspector-restore"
          title="打开节点详情"
          aria-label="打开节点详情"
          aria-controls="node-inspector"
          @click="setInspectorOpen(true)"
        ><ArrowLeft /><span>节点详情</span></button>
        <button
          v-if="!panelLayout.timelineOpen"
          class="panel-restore timeline-restore"
          title="打开时间线"
          aria-label="打开时间线"
          aria-controls="workflow-timeline"
          @click="setTimelineOpen(true)"
        ><ArrowUp /><span>时间线</span></button>
      </div>

      <transition name="toast"><div v-if="deletionToast" class="undo-toast" role="status">已删除 {{ deletionToast.count }} 个节点<button @click="restoreDeletedNodes">撤销</button><span>5秒</span></div></transition>
      <div class="save-live" aria-live="polite">{{ saveState }}</div>

      <VideoWorkflowOutline v-model="outlineVisible" :nodes="graph.nodes" :edge-count="graph.edges.length" :clip-count="timelineClips.length" @select="selectNode" />

      <VideoWorkflowRunHistory
        v-model="runHistoryVisible"
        :runs="runHistoryItems"
        :total="runHistoryTotal"
        :loading="runHistoryLoading"
        :action-run-i-d="runHistoryActionID"
        :detail="runHistoryDetail"
        @refresh="loadRunHistory(true)"
        @load-more="loadRunHistory(false)"
        @inspect="inspectHistoryRun"
        @back="runHistoryDetail = null"
        @preview="previewHistoryOutput"
        @download="downloadHistoryOutput"
        @generate="generateFromHistory"
      />

      <el-dialog v-model="connectionDialogVisible" title="添加连接" width="460px">
        <el-form label-position="top"><el-form-item label="兼容输出端口"><el-select v-model="connectionSource" style="width: 100%"><el-option v-for="source in selectedCompatibleSources" :key="source.value" :label="source.label" :value="source.value" /></el-select></el-form-item></el-form>
        <template #footer><el-button @click="connectionDialogVisible = false">取消</el-button><el-button type="primary" :disabled="!connectionSource" @click="addKeyboardConnection">添加连接</el-button></template>
      </el-dialog>

      <el-dialog v-model="createDialogVisible" title="创建视频工作流" width="520px">
        <el-form label-position="top"><el-form-item label="工作流名称"><el-input v-model="newWorkflowName" maxlength="40" /></el-form-item><el-form-item label="基础模板"><el-radio-group v-model="selectedTemplateID" class="template-list"><el-radio v-for="item in templates" :key="item.id" :value="item.id" border><b>{{ item.name }}</b><span>{{ item.description || '图片生成 · 15秒视频 · 单轨成片' }}</span></el-radio></el-radio-group><div v-if="!templates.length" class="dialog-empty">服务端尚未提供可用模板</div></el-form-item></el-form>
        <template #footer><el-button @click="createDialogVisible = false">取消</el-button><el-button type="primary" :loading="creating" :disabled="!selectedTemplateID" @click="createFromTemplate">创建</el-button></template>
      </el-dialog>

      <el-dialog v-model="characterDialogVisible" title="选定角色定妆" width="780px" :close-on-click-modal="false">
        <div class="candidate-roles"><section v-for="role in characterCandidates" :key="role.nodeID"><header><b>{{ role.title }}</b><span>选择 1 张作为角色版本</span></header><div><button v-for="candidate in role.candidates" :key="candidate.id" :class="{ selected: characterSelections[role.nodeID] === candidate.id }" @click="characterSelections[role.nodeID] = candidate.id"><img v-if="candidate.url" :src="candidate.url" :alt="role.title" /><span v-else>候选 {{ candidate.id.slice(-4) }}</span><Check v-if="characterSelections[role.nodeID] === candidate.id" /></button></div></section></div>
        <template #footer><el-button type="primary" :disabled="!canApproveCharacters" @click="approveCharacters">确认角色并继续</el-button></template>
      </el-dialog>

      <el-dialog v-model="storyboardDialogVisible" title="确认分镜剧本" width="760px" :close-on-click-modal="false"><el-input v-model="storyboardJSON" type="textarea" :rows="20" resize="none" class="storyboard-editor" /><template #footer><span v-if="!canApproveStoryboard" class="json-error">JSON 格式错误</span><el-button type="primary" :disabled="!canApproveStoryboard" @click="approveStoryboard">确认剧本并生成场景</el-button></template></el-dialog>
    </template>
    <VideoWorkflowRunConfirm ref="runConfirmRef" />
  </div>
</template>

<style scoped lang="scss">
.video-workflow-page {
  --shell: #f8fafc;
  --panel: #fff;
  --border: #e2e8f0;
  --canvas: #111318;
  --grid: #252a34;
  --primary: #2563eb;
  width: 100vw;
  height: 100vh;
  min-width: 0;
  overflow: hidden;
  color: #0f172a;
  background: var(--shell);
  font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", sans-serif;
}
.desktop-required { position: fixed; inset: 0; z-index: 2000; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; padding: 24px; background: #f8fafc; text-align: center; }
.desktop-icon { width: 58px; height: 58px; display: grid; place-items: center; color: #2563eb; background: #dbeafe; border-radius: 16px; }.desktop-icon svg { width: 28px; }
.desktop-required h1 { margin: 6px 0 0; font-size: 22px; }.desktop-required p { margin: 0 0 8px; color: #64748b; font-size: 13px; }
.workspace-header { height: 64px; display: grid; grid-template-columns: 138px 40px minmax(180px, 340px) 142px minmax(0, 1fr) auto 32px 32px 78px 86px 78px 92px auto 32px 32px; align-items: center; gap: 6px; box-sizing: border-box; padding: 0 16px; background: #fff; border-bottom: 1px solid var(--border); }
.brand-block { display: flex; align-items: center; gap: 8px; }.brand-block strong { font-size: 19px; letter-spacing: -.5px; white-space: nowrap; }.brand-mark { width: 28px; height: 28px; display: grid; place-items: center; color: #fff; background: #2563eb; clip-path: polygon(50% 0, 100% 100%, 50% 75%, 0 100%); }.brand-mark svg { width: 17px; }
.back-button, .icon-button { width: 32px; height: 32px; display: grid; place-items: center; color: #475569; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; }.back-button svg, .icon-button svg { width: 15px; }.icon-button:disabled { opacity: .35; cursor: default; }.back-button:hover, .icon-button:not(:disabled):hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }
.workspace-actions { width: 32px; height: 32px; }.workspace-actions .icon-button[aria-busy="true"] { color: #2563eb; background: #eff6ff; }.workspace-menu-item { min-width: 104px; display: flex; align-items: center; gap: 8px; }.workspace-menu-item svg { width: 14px; color: #64748b; }.json-file-input { display: none; }
.workflow-name { min-width: 0; display: flex; align-items: center; gap: 6px; }.workflow-name strong { overflow: hidden; font-size: 16px; text-overflow: ellipsis; white-space: nowrap; }.workflow-name > svg { width: 14px; color: #64748b; }.workflow-name :deep(.el-select) { width: 100%; }.workflow-name :deep(.el-select__wrapper) { box-shadow: none; font-size: 16px; font-weight: 650; }
.save-state, .run-state { display: flex; align-items: center; gap: 6px; color: #475569; white-space: nowrap; font-size: 11px; }.save-state i, .run-state i { width: 8px; height: 8px; background: #22c55e; border-radius: 50%; }.save-state.dirty i { background: #f59e0b; }.save-state.offline i { background: #ef4444; }.run-state i { background: #38bdf8; }.run-state i.failed { background: #ef4444; }.run-state i.succeeded { background: #22c55e; }.run-state b { color: #2563eb; }
.header-select { height: 32px; padding: 0 8px; color: #334155; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; outline: none; font-size: 11px; }.header-select.resolution { width: 86px; }
.history-button, .preview-button, .stop-button { height: 34px; display: flex; align-items: center; justify-content: center; gap: 5px; padding: 0 9px; color: #334155; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; white-space: nowrap; font-size: 11px; }.history-button:hover, .preview-button:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.history-button svg, .preview-button svg, .stop-button svg { width: 14px; }.history-button span { min-width: 16px; padding: 1px 4px; color: #1d4ed8; background: #dbeafe; border-radius: 999px; font-size: 8px; }.stop-button { color: #dc2626; }
.generate-button { height: 34px; }.generate-button :deep(.el-button) { height: 34px; border-radius: 5px; }.generate-button :deep(svg) { width: 12px; }
.workspace-grid { position: relative; height: calc(100% - 64px); display: grid; grid-template-columns: var(--left-panel) minmax(0, 1fr) var(--right-panel); grid-template-rows: minmax(0, 1fr) var(--timeline-height); grid-template-areas: "library canvas inspector" "library timeline inspector"; }
.workflow-library { grid-area: library; border-right: 1px solid var(--border); }.workflow-inspector { grid-area: inspector; border-left: 1px solid var(--border); }.workflow-timeline { grid-area: timeline; border-top: 1px solid var(--border); }
.panel-resizer { position: absolute; z-index: 20; padding: 0; background: transparent; border: 0; }.left-resizer { left: calc(var(--left-panel) - 3px); top: 0; bottom: 0; width: 6px; cursor: col-resize; }.right-resizer { right: calc(var(--right-panel) - 3px); top: 0; bottom: 0; width: 6px; cursor: col-resize; }.timeline-resizer { left: var(--left-panel); right: var(--right-panel); bottom: calc(var(--timeline-height) - 3px); height: 6px; cursor: row-resize; }.panel-resizer:hover { background: rgba(37, 99, 235, .45); }
.panel-restore { position: absolute; z-index: 21; height: 32px; display: flex; align-items: center; gap: 6px; padding: 0 10px; color: #334155; background: rgba(255, 255, 255, .94); border: 1px solid #cbd5e1; border-radius: 5px; box-shadow: 0 4px 14px rgba(15, 23, 42, .14); cursor: pointer; font-size: 11px; backdrop-filter: blur(8px); }.panel-restore:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.panel-restore svg { width: 13px; }.inspector-restore { top: 12px; right: 12px; }.timeline-restore { left: calc(var(--left-panel) + (100% - var(--left-panel) - var(--right-panel)) / 2); bottom: 12px; transform: translateX(-50%); }
.undo-toast { position: fixed; z-index: 120; left: 50%; bottom: 28px; transform: translateX(-50%); display: flex; align-items: center; gap: 12px; padding: 10px 14px; color: #f8fafc; background: #1e293b; border-radius: 6px; box-shadow: 0 12px 28px rgba(15, 23, 42, .25); font-size: 12px; }.undo-toast button { color: #93c5fd; background: transparent; border: 0; cursor: pointer; font-weight: 650; }.undo-toast span { color: #94a3b8; font-size: 10px; }.toast-enter-active, .toast-leave-active { transition: opacity .18s ease, transform .18s ease; }.toast-enter-from, .toast-leave-to { opacity: 0; transform: translate(-50%, 8px); }
.save-live { position: fixed; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
.template-list { width: 100%; display: grid; gap: 8px; }.template-list :deep(.el-radio) { width: 100%; height: auto; min-height: 58px; margin: 0; padding: 10px 12px; }.template-list :deep(.el-radio__label) { display: flex; flex-direction: column; gap: 3px; }.template-list span, .dialog-empty { color: #64748b; font-size: 11px; }.dialog-empty { padding: 20px; }
.candidate-roles { display: grid; gap: 16px; max-height: 62vh; overflow: auto; }.candidate-roles section header { display: flex; justify-content: space-between; margin-bottom: 8px; }.candidate-roles section header span { color: #64748b; font-size: 11px; }.candidate-roles section > div { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }.candidate-roles button { position: relative; height: 230px; overflow: hidden; color: #64748b; background: #f8fafc; border: 2px solid transparent; border-radius: 5px; }.candidate-roles button.selected { border-color: #2563eb; }.candidate-roles img { width: 100%; height: 100%; object-fit: contain; }.candidate-roles button > svg { position: absolute; right: 8px; top: 8px; width: 24px; padding: 4px; color: #fff; background: #2563eb; border-radius: 50%; }.storyboard-editor :deep(textarea) { font-family: "SFMono-Regular", Consolas, monospace; font-size: 11px; line-height: 1.6; }.json-error { margin-right: 12px; color: #dc2626; font-size: 11px; }
button, select { font-family: inherit; } button:focus-visible, select:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
</style>
