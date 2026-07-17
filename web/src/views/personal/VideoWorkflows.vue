<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  MarkerType,
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
  ArrowRight,
  Check,
  Clock,
  Download,
  EditPen,
  Menu,
  MoreFilled,
  Plus,
  Setting,
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
import VideoWorkflowMediaPreview from '@/components/video-workflow/VideoWorkflowMediaPreview.vue'
import VideoWorkflowOutline from '@/components/video-workflow/VideoWorkflowOutline.vue'
import VideoWorkflowRunHistory from '@/components/video-workflow/VideoWorkflowRunHistory.vue'
import VideoWorkflowRevisionHistory from '@/components/video-workflow/VideoWorkflowRevisionHistory.vue'
import VideoWorkflowRunConfirm from '@/components/video-workflow/VideoWorkflowRunConfirm.vue'
import {
  approveVideoWorkflowCharacters,
  approveVideoWorkflowStoryboard,
  cancelVideoWorkflowRun,
  createVideoWorkflow,
  deleteVideoWorkflow,
  estimateVideoWorkflowRun,
  getVideoWorkflow,
  getVideoWorkflowRevision,
  getVideoWorkflowRun,
  getVideoWorkflowRuntimeSettings,
  listVideoAssets,
  listVideoWorkflowModels,
  listVideoWorkflowRevisions,
  listVideoWorkflowRuns,
  listVideoWorkflowTemplates,
  listVideoWorkflows,
  runVideoWorkflow,
  seedVideoWorkflowStaleEpochBaseline,
  signVideoAssetVersion,
  transformVideoAssetVersion,
  updateVideoWorkflow,
  updateVideoWorkflowRuntimeSettings,
  uploadVideoAsset,
  validateVideoWorkflow,
  type VideoAsset,
  type VideoAssetVersion,
  type VideoWorkflow,
  type VideoWorkflowEdge,
  type VideoWorkflowGraph,
  type VideoWorkflowNode,
  type VideoWorkflowNodeHistoryEntry,
  type VideoWorkflowNodeRun,
  type VideoWorkflowModelOption,
  type VideoWorkflowRevisionListItem,
  type VideoWorkflowRun,
  type VideoWorkflowRunMode,
  type VideoWorkflowRuntimeSettings,
  type VideoWorkflowTemplate,
  type VideoWorkflowTimelineClip,
  type VideoWorkflowValidationIssue,
} from '@/api/videoWorkflow'
import {
  DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
  VIDEO_WORKFLOW_MAX_EDGES,
  VIDEO_WORKFLOW_MAX_NODES,
  VIDEO_WORKFLOW_NODE_CATALOG,
  clearVideoWorkflowImageAssetBinding,
  cloneWorkflowGraph,
  connectionError,
  createTimelineClip,
  createEmptyVideoWorkflowGraph,
  ensureVideoWorkflowNodePorts,
  migrateVideoWorkflowGraph,
  makeVideoWorkflowNode,
  nextVideoWorkflowImageTransform,
  normalizeImageTransform,
  normalizeVideoWorkflowConnection,
  removeNodeFromGraph,
  resolveVideoWorkflowAssetBinding,
  resolveVideoWorkflowDisplayedStatus,
  resolveVideoWorkflowEdgeStroke,
  resolveVideoWorkflowVideoModel,
  validateVideoWorkflowGraph,
  videoWorkflowImageVersionTransformState,
  videoWorkflowNodeDefaultInputPort,
  videoWorkflowNodePreviewURL,
  videoWorkflowNodeRunOutputVersionID,
} from '@/utils/videoWorkflowGraph'
import { updateVideoWorkflowGroupBounds, videoWorkflowMovableNodeIDs } from '@/utils/videoWorkflowLayout'
import { layoutVideoWorkflowGraph } from '@/utils/videoWorkflowLayoutEngine'
import {
  applyLayoutPreset,
  clampPanelLeft,
  clampPanelRight,
  togglePanelFocusMode,
  type VideoWorkflowLayoutPresetID,
  type VideoWorkflowPanelVisibility,
} from '@/utils/videoWorkflowPanelLayout'
import {
  SHARED_CHARACTER_BUS_ID,
  ensureSharedCharacterBusLayout,
  sharedCharacterBus,
  sharedCharacterBusTargetRoute,
  type VideoWorkflowEdgeDisplayMode,
} from '@/utils/videoWorkflowPresentation'
import { SerialVideoWorkflowOperationQueue, runConfirmedVideoWorkflowMutation } from '@/utils/videoWorkflowAsync'
import {
  VIDEO_WORKFLOW_TRANSFER_MAX_BYTES,
  VideoWorkflowTransferError,
  parseVideoWorkflowTransfer,
  serializeVideoWorkflowTransfer,
  videoWorkflowTransferFilename,
} from '@/utils/videoWorkflowTransfer'
import VideoWorkflowValidationIssues from '@/components/video-workflow/VideoWorkflowValidationIssues.vue'

type PanelTab = 'nodes' | 'assets'
type CanvasTool = 'select' | 'pan' | 'connect'
type FlowData = {
  node?: VideoWorkflowNode
  zone?: { title: string; subtitle: string; enabled?: boolean }
  bus?: { sourceCount: number; targetCount: number }
  collapsedInputPortIDs?: string[]
}
type WorkspaceMenuCommand =
  | 'outline'
  | 'import_json'
  | 'export_json'
  | 'delete_workflow'
  | 'preview'
  | 'history'
  | 'create_workflow'
  | 'runtime_settings'
  | 'aspect_9_16'
  | 'aspect_16_9'
  | 'aspect_1_1'
  | 'resolution_720p'
  | 'resolution_1080p'
  | 'layout_edit'
  | 'layout_compose'
  | 'layout_review'
type OutputSettingCommand = 'aspect_9_16' | 'aspect_16_9' | 'aspect_1_1' | 'resolution_720p' | 'resolution_1080p'

const POLL_INTERVAL = 2500
const AUTO_SAVE_DELAY = 30000
const HISTORY_LIMIT = 50
const RUN_HISTORY_PAGE_SIZE = 20
const NODE_HISTORY_LIMIT = 5
const RUN_STORAGE_KEY = 'gpt2api.video-workflow-runs'
const LAYOUT_STORAGE_KEY = 'gpt2api.video-workflow-layout.v2'
const LOCAL_DRAFT_PREFIX = 'gpt2api.video-workflow-draft.'
const PENDING_RUN_PREFIX = 'gpt2api.video-workflow-pending-run.'
const MEDIA_NODE_TYPES = new Set(['character', 'background', 'image', 'video'])
const DEFAULT_RUNTIME_SETTINGS: VideoWorkflowRuntimeSettings = {
  worker_concurrency: 4,
  text_concurrency: 2,
  image_concurrency: 2,
  video_concurrency: 2,
  compose_concurrency: 1,
}
const RUNTIME_SETTING_FIELDS: Array<{
  key: keyof VideoWorkflowRuntimeSettings
  label: string
  min: number
  max: number
}> = [
  { key: 'worker_concurrency', label: '工作流', min: 1, max: 16 },
  { key: 'text_concurrency', label: '文本', min: 1, max: 16 },
  { key: 'image_concurrency', label: '图片', min: 1, max: 16 },
  { key: 'video_concurrency', label: '视频', min: 1, max: 16 },
  { key: 'compose_concurrency', label: '合成', min: 1, max: 8 },
]

const router = useRouter()
const loading = ref(true)
const saving = ref(false)
const creating = ref(false)
const runningAction = ref(false)
const runtimeSettingsVisible = ref(false)
const runtimeSettingsLoading = ref(false)
const runtimeSettingsSaving = ref(false)
const backendAvailable = ref(true)
const revisionConflict = ref(false)
const templates = ref<VideoWorkflowTemplate[]>([])
const workflows = ref<VideoWorkflow[]>([])
const assets = ref<VideoAsset[]>([])
const workflowVideoModels = ref<VideoWorkflowModelOption[]>([])
const runtimeSettings = reactive<VideoWorkflowRuntimeSettings>({ ...DEFAULT_RUNTIME_SETTINGS })
const runtimeSettingsDraft = reactive<VideoWorkflowRuntimeSettings>({ ...DEFAULT_RUNTIME_SETTINGS })
const activeWorkflow = ref<VideoWorkflow | null>(null)
const graph = ref<VideoWorkflowGraph>(createEmptyVideoWorkflowGraph())
const activeRun = ref<VideoWorkflowRun | null>(null)
const panelTab = ref<PanelTab>('nodes')
const activeTool = ref<CanvasTool>('select')
const selectedNodeID = ref('')
const selectedNodeIDs = ref<string[]>([])
const selectedEdgeIDs = ref<string[]>([])
const selectedSummaryEdgeID = ref('')
const dirty = ref(false)
const autosaveReady = ref(false)
const changeSequence = ref(0)
const undoStack = ref<VideoWorkflowGraph[]>([])
const redoStack = ref<VideoWorkflowGraph[]>([])
const clipboard = ref<{ nodes: VideoWorkflowNode[]; edges: VideoWorkflowEdge[] } | null>(null)
const flowNodes = ref<Node<FlowData>[]>([])
const flowEdges = ref<Edge[]>([])
const layoutBusy = ref(false)
const autoArrange = ref(false)
const edgeDisplayMode = ref<VideoWorkflowEdgeDisplayMode>('smart')
const createDialogVisible = ref(false)
const characterDialogVisible = ref(false)
const storyboardDialogVisible = ref(false)
const approvalBusy = ref(false)
const outlineVisible = ref(false)
const validationIssuesVisible = ref(false)
const validationIssues = ref<VideoWorkflowValidationIssue[]>([])
const connectionDialogVisible = ref(false)
const connectionTarget = ref<{ nodeID: string; portID: string } | null>(null)
const connectionSource = ref('')
const pickingAsset = ref(false)
const pendingConnection = ref<{ nodeID: string; portID: string } | null>(null)
const quickConnectMenu = ref<{ x: number; y: number; position: { x: number; y: number } } | null>(null)
const alignmentGuides = reactive<{ x: number | null; y: number | null }>({ x: null, y: null })
const selectedTemplateID = ref<string | number>('')
const newWorkflowName = ref('雨夜重逢 · 60秒短剧')
const characterSelections = ref<Record<string, string>>({})
const storyboardJSON = ref('{}')
const deletionToast = ref<{ count: number } | null>(null)
const conflictDraftKey = ref('')
const runConfirmRef = ref<InstanceType<typeof VideoWorkflowRunConfirm> | null>(null)
const runHistoryVisible = ref(false)
const runHistoryLoading = ref(false)
const runHistoryItems = ref<VideoWorkflowRun[]>([])
const runHistoryTotal = ref(0)
const runHistoryActionID = ref('')
const runHistoryDetail = ref<VideoWorkflowRun | null>(null)
const revisionHistoryVisible = ref(false)
const revisionHistoryLoading = ref(false)
const revisionHistoryItems = ref<VideoWorkflowRevisionListItem[]>([])
const revisionHistoryTotal = ref(0)
const revisionHistoryAction = ref<number | null>(null)
const viewingRevision = ref<number | null>(null)
const runDetailCache = ref<Record<string, VideoWorkflowRun>>({})
const nodeHistoryLoading = ref(false)
const jsonFileInput = ref<HTMLInputElement | null>(null)
const jsonTransferBusy = ref(false)
const mediaPreviewVisible = ref(false)
const mediaPreviewNode = ref<VideoWorkflowNode | null>(null)
const mediaPreviewURL = ref('')
const mediaPreviewLoading = ref(false)
const mediaPreviewError = ref('')
const selectedComposePlaybackURL = ref('')
const selectedComposePlaybackLoading = ref(false)
const panelLayout = reactive({ left: 248, right: 320, libraryOpen: true, inspectorOpen: true })
const lastNonFocusLayout = ref<VideoWorkflowPanelVisibility | null>(null)
const canvasViewport = reactive({ x: 0, y: 0, zoom: 1 })
const headerCompact = ref(false)
const headerPhone = ref(false)

let saveTimer: number | null = null
let retryTimer: number | null = null
let pollingTimer: number | null = null
let deletionTimer: number | null = null
let autoArrangeTimer: number | null = null
let resizing: { kind: 'left' | 'right'; start: number; value: number } | null = null
let activeSavePromise: Promise<VideoWorkflow | null> | null = null
let restoredLocalDraft = false
const imageTransformQueue = new SerialVideoWorkflowOperationQueue()
let imageTransformContextGeneration = 0
let deletedRecord: { before: VideoWorkflowGraph; nodeIDs: string[] } | null = null
let pollingGeneration = 0
let workspaceGeneration = 0
let nodeHistoryGeneration = 0
let nodeHistoryErrorToastAt = 0
let mediaPreviewGeneration = 0
let selectedComposePlaybackGeneration = 0
let componentUnmounted = false
let edgeCurveHistorySnapshot: VideoWorkflowGraph | null = null
let visualEdgeLogicalIDs = new Map<string, string[]>()
let headerCompactQuery: MediaQueryList | null = null
let headerPhoneQuery: MediaQueryList | null = null

function syncHeaderLayoutMode() {
  headerCompact.value = headerCompactQuery?.matches ?? false
  headerPhone.value = headerPhoneQuery?.matches ?? false
}
/** 本地标记 stale 的递增世代；轮询仅清除「标记世代 ≤ 本次运行启动世代」的节点，避免抹掉运行后编辑产生的合法 stale。 */
let staleEpoch = 0
const nodeStaleEpoch = new Map<string, number>()
let activeRunStaleEpoch = 0
let refreshRunFailCount = 0
let refreshRunFailWarned = false
let assetsRefreshFailed = false
let ensureAssetBindingWarned = false

const {
  fitView,
  zoomIn,
  zoomOut,
  setViewport,
  setCenter,
  screenToFlowCoordinate,
  updateNodeInternals,
} = useVueFlow('video-workflow-flow')

const selectedNode = computed(() => graph.value.nodes.find((node) => node.id === selectedNodeID.value) || null)
/** 结构大纲与画布一致：合并 activeRun.node_runs 后的展示态，避免图节点自身 status 为空时全显示「待生成」。 */
const outlineNodes = computed(() => graph.value.nodes.map((node) => displayNode(node)))
const timelineNode = computed(() => graph.value.nodes.find((node) => node.type === 'timeline') || null)
const timelineClips = computed<VideoWorkflowTimelineClip[]>(() => Array.isArray(timelineNode.value?.config.clips)
  ? timelineNode.value!.config.clips as VideoWorkflowTimelineClip[]
  : [])
const timelineSourceTitles = computed(() => Object.fromEntries(
  graph.value.nodes.map((node) => [node.id, nodeTitle(node)]),
))
/** 严格 revision 对齐：仅用于 stale 清除等需要与当前图一致的逻辑，不用于展示。 */
const activeRunMatchesWorkflow = computed(() => Boolean(
  activeRun.value
  && activeWorkflow.value
  && activeRun.value.workflow_revision === activeWorkflow.value.revision,
))
/** 展示层始终使用最近一次运行的 node_runs，避免保存 revision+1 后输出/进度瞬间消失。 */
const activeRunNodeMap = computed(() => new Map(
  (activeRun.value?.node_runs || []).map((item) => [item.node_id, item]),
))
const previousMediaNodeRunMap = computed(() => {
  const currentRunID = activeRun.value?.id || ''
  const runsByID = new Map<string, VideoWorkflowRun>()
  const mergeRun = (run: VideoWorkflowRun | null | undefined) => {
    if (!run || run.id === currentRunID) return
    const existing = runsByID.get(run.id)
    runsByID.set(run.id, {
      ...(existing || run),
      ...run,
      node_runs: run.node_runs?.length ? run.node_runs : existing?.node_runs,
      graph_snapshot: run.graph_snapshot || existing?.graph_snapshot,
    })
  }
  runHistoryItems.value.forEach(mergeRun)
  Object.values(runDetailCache.value).forEach(mergeRun)
  return [...runsByID.values()]
    .sort((a, b) => new Date(b.created_at || 0).getTime() - new Date(a.created_at || 0).getTime())
    .reduce((result, run) => {
      for (const nodeRun of run.node_runs || []) {
        if (result.has(nodeRun.node_id)) continue
        const nodeType = nodeRun.node_type || graph.value.nodes.find((node) => node.id === nodeRun.node_id)?.type
        if (!isMediaNodeType(nodeType) || !nodeRunHasMediaOutput(nodeRun)) continue
        result.set(nodeRun.node_id, nodeRun)
      }
      return result
    }, new Map<string, VideoWorkflowNodeRun>())
})
const selectedNodeRun = computed(() => activeRunNodeMap.value.get(selectedNodeID.value) || null)
const selectedComposePlaybackSignature = computed(() => {
  if (selectedNode.value?.type !== 'compose') return ''
  const nodeRun = selectedNodeRun.value
  return [
    selectedNodeID.value,
    activeRun.value?.id || '',
    activeRun.value?.output_version_id || '',
    activeRun.value?.output_asset_version_id || '',
    activeRun.value?.output?.id || '',
    activeRun.value?.output?.asset_id || '',
    activeRun.value?.output_url || '',
    nodeRun?.output_version_id || '',
    nodeRun?.output?.version_id || '',
    nodeRun?.output?.output_version_id || '',
    nodeRun?.output?.output_url || '',
    nodeRun?.output?.url || '',
  ].join('|')
})
const selectedNodeUpstreams = computed(() => {
  const target = selectedNode.value
  if (!target) return []
  return (target.inputs || []).map((port) => ({
    id: port.id,
    label: port.label || port.id,
    type: port.type,
    required: Boolean(port.required),
    sources: graph.value.edges.filter((edge) => edge.target === target.id && edge.target_port === port.id).map((edge) => {
      const source = graph.value.nodes.find((node) => node.id === edge.source)
      const displayed = source ? displayNode(source) : null
      const output = source?.outputs?.find((item) => item.id === edge.source_port)
      return {
        id: edge.id,
        node_id: edge.source,
        node_title: source ? nodeTitle(source) : edge.source,
        node_type: source?.type || '',
        port_label: output?.label || edge.source_port,
        status: displayed?.status || 'idle',
      }
    }),
  }))
})
const selectedNodeHistory = computed<VideoWorkflowNodeHistoryEntry[]>(() => {
  if (!selectedNodeID.value) return []
  const runs = [activeRun.value, ...Object.values(runDetailCache.value)].filter((run): run is VideoWorkflowRun => Boolean(run))
  const unique = new Map<string, VideoWorkflowNodeHistoryEntry>()
  for (const run of runs) {
    const nodeRun = run.node_runs?.find((item) => item.node_id === selectedNodeID.value)
    if (!nodeRun || unique.has(run.id)) continue
    unique.set(run.id, {
      run_id: run.id,
      workflow_revision: run.workflow_revision,
      run_status: run.status,
      run_mode: run.run_mode,
      run_created_at: nodeRun.created_at || run.created_at,
      node_run: nodeRun,
    })
  }
  return [...unique.values()]
    .sort((a, b) => new Date(b.run_created_at || 0).getTime() - new Date(a.run_created_at || 0).getTime())
    .slice(0, NODE_HISTORY_LIMIT)
})
const selectedNodeModel = computed(() => {
  const type = selectedNode.value?.type || ''
  if (['story_brief', 'script', 'scene'].includes(type)) return graph.value.settings.text_model || 'default'
  if (['character', 'background', 'image'].includes(type)) return graph.value.settings.image_model || 'gpt-image-2'
  if (type === 'video') return currentVideoModel(selectedNode.value)
  return ''
})
const selectedNodeModelOptions = computed(() => {
  const type = selectedNode.value?.type || ''
  const defaults = ['story_brief', 'script', 'scene'].includes(type)
    ? [{ value: 'default', label: '系统默认文本模型' }]
    : ['character', 'background', 'image'].includes(type)
      // 对外兼容名仍是 gpt-image-2；当前 imagegen 网关指向本机 SD-Turbo
      ? [{ value: 'gpt-image-2', label: 'SD-Turbo (本地)' }]
      : type === 'video'
        ? videoModelOptionsWithCurrent(selectedNodeModel.value)
        : []
  return selectedNodeModel.value && !defaults.some((item) => item.value === selectedNodeModel.value)
    ? [{ value: selectedNodeModel.value, label: selectedNodeModel.value }, ...defaults]
    : defaults
})

function videoModelOptionsWithCurrent(current: string) {
  const items = workflowVideoModels.value.length
    ? workflowVideoModels.value
    : [{ value: DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL, label: '本地 Seedance/Motion', channel_type: 'apiyi_seedance2' }]
  const options = items.map((item) => ({
    value: item.value,
    label: item.label || item.value,
  })).filter((item) => item.value)
  return current && !options.some((item) => item.value === current)
    ? [{ value: current, label: `${current}（已停用）` }, ...options]
    : options
}
const isRunActive = computed(() => ['queued', 'running', 'awaiting_character_approval', 'awaiting_storyboard_approval', 'cancel_pending'].includes(activeRun.value?.status || ''))
const awaitingApproval = computed(() => {
  const status = activeRun.value?.status
  return status === 'awaiting_character_approval' || status === 'awaiting_storyboard_approval'
})
const workspaceLifecycle = computed(() => {
  if (loading.value) return 'loading'
  if (!activeWorkflow.value) return 'unbound'
  if (revisionConflict.value) return 'conflict'
  if (saving.value) return 'saving'
  if (dirty.value) return 'dirty'
  return 'ready'
})
const saveStateLabel = computed(() => {
  if (!activeWorkflow.value) return '未关联工作流'
  if (revisionConflict.value) return '修订冲突'
  if (saving.value) return '保存中…'
  if (dirty.value && !backendAvailable.value) return '离线草稿'
  if (dirty.value) return '待保存'
  return '已保存'
})
const saveState = computed(() => {
  if (!activeWorkflow.value) return saveStateLabel.value
  if (revisionConflict.value) return `${saveStateLabel.value} · 已保留本地副本 · R${activeWorkflow.value.revision}`
  return `${saveStateLabel.value} · R${activeWorkflow.value.revision}`
})
const workspaceLifecycleHint = computed(() => ({
  loading: '正在加载工作区…',
  unbound: '当前为空白预览画布，请从模板创建工作流后再编辑和生成',
  conflict: '服务器版本与本地草稿冲突，请先处理修订',
  saving: '正在保存到服务器…',
  dirty: '本地修改约 30 秒后自动保存，也可立即点保存；点修订号打开版本历史',
  ready: '工作流已关联服务器；点修订号打开版本历史并可切换画布快照',
} as Record<string, string>)[workspaceLifecycle.value] || '')
const canOperateWorkflow = computed(() => Boolean(activeWorkflow.value) && !revisionConflict.value)
const canManualSave = computed(() => canOperateWorkflow.value && !saving.value)
const saveButtonPending = computed(() => canManualSave.value && dirty.value)
const outputSettingsLabel = computed(() => `${graph.value.settings.aspect_ratio} · ${graph.value.settings.resolution}`)
const runtimeSettingsLabel = computed(() => `文${runtimeSettings.text_concurrency} · 图${runtimeSettings.image_concurrency} · 视${runtimeSettings.video_concurrency}`)
const runStatus = computed(() => ({
  queued: '排队中', running: '生成中', awaiting_character_approval: '待选角色', awaiting_storyboard_approval: '待确认分镜',
  cancel_pending: '停止中', canceled: '已停止', succeeded: '已完成', failed: '运行失败',
} as Record<string, string>)[activeRun.value?.status || ''] || '')
type WorkspaceContextBar = {
  kind: 'conflict' | 'approval' | 'running' | 'draft'
  text: string
  progress?: number
  showLoadServer?: boolean
  showRecoverDraft?: boolean
  showContinueApproval?: boolean
}
const workspaceContextBar = computed<WorkspaceContextBar | null>(() => {
  if (revisionConflict.value) {
    return {
      kind: 'conflict',
      text: '修订冲突：请先加载服务器版本，再决定是否恢复本地草稿',
      showLoadServer: true,
      showRecoverDraft: Boolean(conflictDraftKey.value),
    }
  }
  if (awaitingApproval.value) {
    return {
      kind: 'approval',
      text: runStatus.value || '待审批',
      showContinueApproval: true,
    }
  }
  // Header already shows run progress + stop; pure running has no bar actions → omit.
  if (isRunActive.value) return null
  if (conflictDraftKey.value) {
    return {
      kind: 'draft',
      text: '检测到旧修订的本地草稿；恢复前请确认已加载最新服务器版本',
      showRecoverDraft: true,
    }
  }
  return null
})
const showHeaderRunProgress = computed(() => isRunActive.value)
const headerRunStats = computed(() => {
  const nodeRuns = activeRun.value?.node_runs || []
  const total = nodeRuns.length
  let done = 0
  let active = 0
  let anyRunning = false
  for (const nodeRun of nodeRuns) {
    const status = String(nodeRun.status || '')
    if (status === 'succeeded') done += 1
    if (status === 'running' || status === 'queued') active += 1
    if (status === 'running') anyRunning = true
  }
  const progress = Math.max(0, Math.min(100, Number(activeRun.value?.progress ?? 0)))
  const label = runStatus.value || '运行中'
  const indeterminate = activeRun.value?.status === 'queued' && progress === 0 && !anyRunning
  const nodeText = total > 0
    ? (headerPhone.value ? `${done}/${total}` : `进行中 ${active} · ${done}/${total}`)
    : (headerPhone.value ? '—' : '进行中 — · —')
  const ariaLabel = total > 0
    ? `${label}，进度 ${progress}%，进行中 ${active} 个节点，已完成 ${done}/${total}`
    : `${label}，进度 ${progress}%`
  return { progress, active, done, total, label, indeterminate, nodeText, ariaLabel }
})
function runHasPreviewOutput(run: VideoWorkflowRun | null | undefined, allowCanvasFallback: boolean) {
  if (run?.output?.asset_id && (run.output_version_id || run.output_asset_version_id)) return true
  const directURL = run?.output_url
  if (directURL && !String(directURL).includes('purpose=preview')) return true
  if (!allowCanvasFallback) return false
  const compose = graph.value.nodes.find((node) => node.type === 'compose')
  return Boolean(compose?.output?.url || compose?.output?.output_url)
}
const hasPreviewOutput = computed(() => runHasPreviewOutput(activeRun.value, true))
const previewStatusLabel = computed(() => (
  hasPreviewOutput.value ? '成片已就绪，点击预览播放' : '最终成片生成后可预览'
))
const workspaceStyle = computed(() => ({
  '--left-panel': panelLayout.libraryOpen ? `${panelLayout.left}px` : '0px',
  '--right-panel': panelLayout.inspectorOpen ? `${panelLayout.right}px` : '0px',
}))
const modKeyCode = computed(() => typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? 'Meta' : 'Control')
const modKeyLabel = computed(() => modKeyCode.value === 'Meta' ? '⌘' : 'Ctrl')
const saveButtonTitle = computed(() => {
  if (!activeWorkflow.value) return '请先创建工作流'
  if (revisionConflict.value) return '修订冲突，请先处理后再保存'
  if (saving.value) return '正在保存…'
  if (dirty.value) return `立即保存（${modKeyLabel.value}+S）；未保存时约 30 秒后自动保存`
  return `已保存 · R${activeWorkflow.value.revision}`
})
const revisionBadgeTitle = computed(() => {
  if (!activeWorkflow.value) return '查看版本历史'
  if (viewingRevision.value != null && viewingRevision.value !== activeWorkflow.value.revision) {
    return `版本历史：画布正在查看 R${viewingRevision.value}（服务器当前 R${activeWorkflow.value.revision}）`
  }
  return `查看版本历史（当前画布 R${activeWorkflow.value.revision}）`
})
const revisionBadgeLabel = computed(() => {
  if (!activeWorkflow.value) return ''
  if (viewingRevision.value != null && viewingRevision.value !== activeWorkflow.value.revision) {
    return `R${viewingRevision.value}`
  }
  return `R${activeWorkflow.value.revision}`
})
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
    const candidates = (Array.isArray(raw) ? raw : []).map((candidate: any, index: number) => {
      const id = typeof candidate === 'string'
        ? candidate
        : String(candidate.asset_version_id || candidate.version_id || candidate.id || `${item.node_id}_${index}`)
      const directURL = typeof candidate === 'string' ? '' : String(candidate.preview_url || candidate.url || '')
      const fromAsset = assets.value
        .flatMap((asset) => asset.versions || [])
        .find((version) => version.id === id)
      return {
        id,
        url: directURL || String(fromAsset?.preview_url || ''),
      }
    })
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

function isMediaNodeType(type?: string | null) {
  return MEDIA_NODE_TYPES.has(String(type || ''))
}

function nodeRunHasMediaOutput(nodeRun?: VideoWorkflowNodeRun | null) {
  if (!nodeRun) return false
  if (videoWorkflowNodeRunOutputVersionID(nodeRun)) return true
  return Boolean(videoWorkflowNodePreviewURL({
    id: nodeRun.node_id,
    type: nodeRun.node_type || 'image',
    position: { x: 0, y: 0 },
    config: {},
    output: nodeRun.output,
  } as VideoWorkflowNode))
}

function fallbackMediaNodeRunForNode(node: VideoWorkflowNode | null) {
  if (!node || !isMediaNodeType(node.type)) return null
  return previousMediaNodeRunMap.value.get(node.id) || null
}

function assetBindingForNode(node: VideoWorkflowNode | null) {
  if (!node) return null
  return resolveVideoWorkflowAssetBinding(node, activeRunNodeMap.value.get(node.id), assets.value)
    || resolveVideoWorkflowAssetBinding(node, fallbackMediaNodeRunForNode(node), assets.value)
}

function invalidateImageTransformContext() { imageTransformContextGeneration += 1 }

async function refreshAssets(options: { silent?: boolean } = {}) {
  const pageSize = 100
  const maxTotal = 1000
  const items: Awaited<ReturnType<typeof listVideoAssets>> = []
  try {
    for (let offset = 0; offset < maxTotal; offset += pageSize) {
      const page = await listVideoAssets({ limit: pageSize, offset })
      items.push(...page)
      if (page.length < pageSize) break
      if (offset + pageSize >= maxTotal) {
        console.warn(`视频工作流素材超过 ${maxTotal} 个，已停止继续拉取`)
        break
      }
    }
    assets.value = items
    assetsRefreshFailed = false
    return items
  } catch (error) {
    if (!options.silent && !assetsRefreshFailed) {
      ElMessage.warning('素材列表刷新失败，缩略图可能不是最新')
    }
    assetsRefreshFailed = true
    throw error
  }
}

async function ensureAssetBinding(nodeID: string, options: { warnMissing?: boolean } = {}) {
  let node = graph.value.nodes.find((item) => item.id === nodeID) || null
  let binding = assetBindingForNode(node)
  if (!binding && nodeRunOutputVersionID(nodeID)) {
    try {
      await refreshAssets({ silent: !options.warnMissing })
    } catch { /* 保留旧素材列表；下方按需提示缺绑定。 */ }
    node = graph.value.nodes.find((item) => item.id === nodeID) || null
    binding = assetBindingForNode(node)
  }
  if (!binding && options.warnMissing && !ensureAssetBindingWarned) {
    ensureAssetBindingWarned = true
    ElMessage.warning('当前节点缺少可用素材绑定，请先运行生成或重新选择素材')
  }
  return binding
}

function closeMediaPreview() {
  mediaPreviewGeneration += 1
  mediaPreviewVisible.value = false
  mediaPreviewLoading.value = false
  mediaPreviewURL.value = ''
  mediaPreviewError.value = ''
}

function updateMediaPreviewVisible(visible: boolean) {
  if (!visible) closeMediaPreview()
}

function directMediaBinding(node: VideoWorkflowNode) {
  const nodeRun = activeRunNodeMap.value.get(node.id)
  const assetID = String(
    node.asset_id
    || node.config.asset_id
    || node.output?.asset_id
    || node.output?.output_asset_id
    || nodeRun?.output?.asset_id
    || '',
  )
  const versionID = String(
    node.asset_version_id
    || node.config.asset_version_id
    || node.output?.asset_version_id
    || node.output?.version_id
    || node.output?.output_version_id
    || node.output?.output_asset_version_id
    || videoWorkflowNodeRunOutputVersionID(nodeRun)
    || '',
  )
  return assetID && versionID ? { assetID, versionID, previewURL: '' } : null
}

function assetIDForVersion(versionID: string) {
  if (!versionID) return ''
  if (activeRun.value?.output?.id === versionID && activeRun.value.output.asset_id) return activeRun.value.output.asset_id
  const asset = assets.value.find((item) => item.versions?.some((version) => version.id === versionID))
  return asset?.id || ''
}

function firstPlayableOutputURL(...values: any[]) {
  for (const value of values) {
    const url = typeof value === 'string' ? value.trim() : ''
    if (url && !/[?&]purpose=preview(?:&|$)/.test(url)) return url
  }
  return ''
}

async function refreshSelectedComposePlaybackURL() {
  const generation = ++selectedComposePlaybackGeneration
  selectedComposePlaybackURL.value = ''
  selectedComposePlaybackLoading.value = false
  if (selectedNode.value?.type !== 'compose') return

  const nodeRun = selectedNodeRun.value
  const directURL = firstPlayableOutputURL(
    nodeRun?.output?.playback_url,
    nodeRun?.output?.video_url,
    nodeRun?.output?.url,
    nodeRun?.output?.output_url,
    activeRun.value?.output_url,
  )
  if (directURL) {
    selectedComposePlaybackURL.value = directURL
    return
  }

  const versionID = String(
    activeRun.value?.output_version_id
    || activeRun.value?.output_asset_version_id
    || nodeRun?.output_version_id
    || nodeRun?.output?.version_id
    || nodeRun?.output?.output_version_id
    || '',
  )
  const assetID = String(
    activeRun.value?.output?.asset_id
    || nodeRun?.output?.asset_id
    || nodeRun?.output?.output_asset_id
    || assetIDForVersion(versionID)
    || '',
  )
  if (!assetID || !versionID) return

  selectedComposePlaybackLoading.value = true
  try {
    const signed = await signVideoAssetVersion(assetID, versionID, 'seedance')
    if (!componentUnmounted && generation === selectedComposePlaybackGeneration) {
      selectedComposePlaybackURL.value = signed.url
    }
  } catch {
    // 保留输出页封面和底部“播放成片”入口。
  } finally {
    if (!componentUnmounted && generation === selectedComposePlaybackGeneration) {
      selectedComposePlaybackLoading.value = false
    }
  }
}

function mediaBindingForNode(node: VideoWorkflowNode) {
  const binding = assetBindingForNode(node)
  return binding ? {
    assetID: binding.assetID,
    versionID: binding.versionID,
    previewURL: binding.version?.preview_url || binding.asset.preview_url || '',
  } : directMediaBinding(node)
}

async function openMediaPreview(sourceNode: VideoWorkflowNode) {
  const graphNode = graph.value.nodes.find((node) => node.id === sourceNode.id)
  const node = graphNode ? displayNode(graphNode) : sourceNode
  if (!isMediaNodeType(node.type)) return

  const requestGeneration = ++mediaPreviewGeneration
  const openingWorkspaceGeneration = workspaceGeneration
  const openingWorkflowID = activeWorkflow.value?.id || ''
  const contextIsCurrent = () => (
    !componentUnmounted
    && mediaPreviewVisible.value
    && requestGeneration === mediaPreviewGeneration
    && openingWorkspaceGeneration === workspaceGeneration
    && openingWorkflowID === (activeWorkflow.value?.id || '')
  )

  mediaPreviewNode.value = node
  mediaPreviewVisible.value = true
  mediaPreviewURL.value = ''
  mediaPreviewError.value = ''
  mediaPreviewLoading.value = true

  let fallbackURL = videoWorkflowNodePreviewURL(node)
  let binding = mediaBindingForNode(node)
  const versionID = String(
    binding?.versionID
    || node.asset_version_id
    || node.config.asset_version_id
    || videoWorkflowNodeRunOutputVersionID(activeRunNodeMap.value.get(node.id))
    || '',
  )

  if (!binding && versionID) {
    try {
      await ensureAssetBinding(node.id, { warnMissing: true })
      if (!contextIsCurrent()) return
      binding = mediaBindingForNode(node)
    } catch { /* 仍可回退到节点已有的预览地址。 */ }
  }

  if (!contextIsCurrent()) return
  if (binding) {
    fallbackURL = binding.previewURL || fallbackURL
    try {
      // 视频 preview 指向封面帧；全屏播放需 download/seedance 取原片。
      const purpose = node.type === 'video' ? 'seedance' : 'preview'
      const signed = await signVideoAssetVersion(binding.assetID, binding.versionID, purpose)
      if (!contextIsCurrent()) return
      mediaPreviewURL.value = signed.url
      mediaPreviewLoading.value = false
      return
    } catch { /* 旧节点可能仅保留可直接读取的输出地址。 */ }
  }

  if (!contextIsCurrent()) return
  mediaPreviewURL.value = fallbackURL
  mediaPreviewLoading.value = false
  if (!fallbackURL) {
    mediaPreviewError.value = binding
      ? '预览签名失败，请重试'
      : '当前节点尚未生成可预览的媒体'
  }
}

function retryMediaPreview() {
  if (mediaPreviewNode.value) void openMediaPreview(mediaPreviewNode.value)
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
  const previewRunNode = nodeRunHasMediaOutput(runNode) ? runNode : fallbackMediaNodeRunForNode(node)
  const binding = assetBindingForNode(node)
  const runPreview = videoWorkflowNodePreviewURL({
    ...displayed,
    output: previewRunNode?.output || displayed.output,
  })
  const bakedPreview = binding?.version?.preview_url || runPreview || displayed.config.preview_url
  const enriched = {
    ...displayed,
    ...(binding ? { asset_id: binding.assetID, asset_version_id: binding.versionID } : {}),
    config: {
      ...displayed.config,
      ...(binding ? { asset_id: binding.assetID, asset_version_id: binding.versionID } : {}),
      ...(bakedPreview ? { preview_url: bakedPreview } : {}),
    },
  }
  if (!runNode) return enriched
  const status = resolveVideoWorkflowDisplayedStatus(node.status, runNode.status)
  const runningWithStaleParams = node.status === 'stale' && (status === 'running' || status === 'queued')
  return {
    ...enriched,
    status,
    progress: status === 'stale' ? undefined : runNode.progress,
    output: runNode.output,
    stale_reason: runningWithStaleParams
      ? '参数已修改，本次运行结果将过期'
      : status === 'stale' ? node.stale_reason : undefined,
    run_error: runNode.error_message || runNode.error || undefined,
    run_error_code: runNode.error_code || undefined,
  }
}

function syncFlow() {
  const bus = edgeDisplayMode.value === 'all' ? null : sharedCharacterBus(graph.value)
  const collapsedEdgeIDs = new Set(bus?.logicalEdges.map((edge) => edge.id) || [])
  const selectedLogicalEdgeIDs = new Set([
    ...selectedEdgeIDs.value,
    ...(visualEdgeLogicalIDs.get(selectedSummaryEdgeID.value) || []),
  ])
  const selectedNodes = new Set(selectedNodeIDs.value)
  const hasSelection = selectedNodes.size > 0 || selectedLogicalEdgeIDs.size > 0
  const nextVisualEdgeLogicalIDs = new Map<string, string[]>()

  function edgeOpacity(logicalEdges: VideoWorkflowEdge[], defaultOpacity: number) {
    const related = logicalEdges.some((edge) => selectedLogicalEdgeIDs.has(edge.id)
      || selectedNodes.has(edge.source)
      || selectedNodes.has(edge.target))
    if (!hasSelection) return edgeDisplayMode.value === 'hidden' ? 0 : defaultOpacity
    if (related) return 1
    if (edgeDisplayMode.value === 'hidden') return 0
    return edgeDisplayMode.value === 'all' ? .18 : .08
  }

  const zones: Node<FlowData>[] = graph.value.groups.map((group) => {
    const members = graph.value.nodes.filter((node) => group.node_ids.includes(node.id) && !['timeline', 'compose'].includes(node.type))
    // 与后端 activeNodeIDs 一致：group.enabled=false 或组内全部节点 disabled → 已停用
    const sceneEnabled = group.enabled !== false && (members.length ? members.some((node) => node.enabled !== false) : true)
    const videoMembers = members.filter((node) => node.type === 'video')
    const durationSeconds = videoMembers.length
      ? videoMembers.reduce((sum, node) => {
        const value = node.config?.duration_seconds ?? node.duration_seconds
        return sum + (typeof value === 'number' && Number.isFinite(value) ? value : 0)
      }, 0)
      : 15
    return {
      id: `__${group.scene_id}`,
      type: 'zone',
      position: { ...group.position },
      draggable: false,
      selectable: false,
      data: { zone: { title: `场景 ${group.scene_id.replace(/\D/g, '').padStart(2, '0')}`, subtitle: `${durationSeconds} 秒 · ${sceneEnabled ? '已启用' : '已停用'}`, enabled: sceneEnabled } },
      style: { width: `${group.size.width}px`, height: `${group.size.height}px`, zIndex: -2 },
    }
  })
  flowNodes.value = [
    ...zones,
    ...graph.value.nodes.map((node) => {
      ensureVideoWorkflowNodePorts(node)
      return {
        id: node.id,
        type: 'workflow',
        position: { ...node.position },
        data: {
          node: displayNode(node),
          collapsedInputPortIDs: bus?.targetPortIDs.get(node.id),
        },
        selected: selectedNodeIDs.value.includes(node.id),
        draggable: !node.locked,
        style: { width: `${isMediaNodeType(node.type) ? 208 : 188}px`, zIndex: 2 },
      } as Node<FlowData>
    }),
    ...(bus ? [{
      id: SHARED_CHARACTER_BUS_ID,
      type: 'assetBus',
      position: { ...bus.position },
      data: { bus: { sourceCount: bus.sourceNodeIDs.length, targetCount: bus.targetNodeIDs.length } },
      selected: selectedSummaryEdgeID.value === SHARED_CHARACTER_BUS_ID,
      draggable: true,
      selectable: true,
      style: { width: '132px', height: '44px', zIndex: 3 },
    } as Node<FlowData>] : []),
  ]
  const regularEdges: Edge[] = graph.value.edges
    .filter((edge) => !collapsedEdgeIDs.has(edge.id))
    .map((edge) => {
      const opacity = edgeOpacity([edge], edgeDisplayMode.value === 'all' ? .72 : .3)
      const sourceNode = graph.value.nodes.find((node) => node.id === edge.source)
      const sourcePort = sourceNode?.outputs?.find((port) => port.id === edge.source_port)
      const runStatus = activeRunNodeMap.value.get(edge.target)?.status
      const selected = selectedEdgeIDs.value.includes(edge.id)
      const stroke = resolveVideoWorkflowEdgeStroke({
        selected,
        runStatus,
        portType: sourcePort?.type,
      })
      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        sourceHandle: edge.source_port,
        targetHandle: edge.target_port,
        type: 'adjustable',
        data: { curve: edge.curve, route: edge.route },
        selected,
        selectable: opacity > 0,
        focusable: opacity > 0,
        interactionWidth: opacity > 0 ? 20 : 0,
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: { opacity, stroke, transition: 'opacity 180ms ease' },
        animated: runStatus === 'running',
      }
    })
  const summaryEdges: Edge[] = []
  if (bus) {
    nextVisualEdgeLogicalIDs.set(SHARED_CHARACTER_BUS_ID, bus.logicalEdges.map((edge) => edge.id))
    for (const sourceID of bus.sourceNodeIDs) {
      const logicalEdges = bus.logicalEdges.filter((edge) => edge.source === sourceID)
      const id = `__shared_character_in_${sourceID}`
      const opacity = edgeOpacity(logicalEdges, .62)
      const selected = selectedSummaryEdgeID.value === id
      const stroke = resolveVideoWorkflowEdgeStroke({
        selected,
        portType: 'character',
      })
      nextVisualEdgeLogicalIDs.set(id, logicalEdges.map((edge) => edge.id))
      summaryEdges.push({
        id,
        source: sourceID,
        target: SHARED_CHARACTER_BUS_ID,
        sourceHandle: logicalEdges[0]?.source_port,
        targetHandle: 'characters',
        type: 'smoothstep',
        selected,
        selectable: opacity > 0,
        focusable: opacity > 0,
        updatable: false,
        interactionWidth: opacity > 0 ? 20 : 0,
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: {
          opacity,
          stroke,
          strokeWidth: selected ? 2.5 : 1.8,
          transition: 'opacity 180ms ease',
        },
      } as Edge)
    }
    for (const [targetIndex, targetID] of bus.targetNodeIDs.entries()) {
      const logicalEdges = bus.logicalEdges.filter((edge) => edge.target === targetID)
      const id = `__shared_character_out_${targetID}`
      const opacity = edgeOpacity(logicalEdges, .62)
      const runStatus = activeRunNodeMap.value.get(targetID)?.status
      const selected = selectedSummaryEdgeID.value === id
      const stroke = resolveVideoWorkflowEdgeStroke({
        selected,
        runStatus,
        portType: 'character',
      })
      nextVisualEdgeLogicalIDs.set(id, logicalEdges.map((edge) => edge.id))
      summaryEdges.push({
        id,
        source: SHARED_CHARACTER_BUS_ID,
        target: targetID,
        sourceHandle: 'shared',
        targetHandle: '__shared_characters',
        type: 'adjustable',
        data: { route: sharedCharacterBusTargetRoute(graph.value, bus, targetID, targetIndex), readonly: true },
        selected,
        selectable: opacity > 0,
        focusable: opacity > 0,
        updatable: false,
        interactionWidth: opacity > 0 ? 20 : 0,
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: {
          opacity,
          stroke,
          strokeWidth: selected ? 2.5 : 1.8,
          transition: 'opacity 180ms ease',
        },
        animated: runStatus === 'running',
      } as Edge)
    }
  }
  visualEdgeLogicalIDs = nextVisualEdgeLogicalIDs
  flowEdges.value = [...regularEdges, ...summaryEdges]
}

function selectEdge(edgeID: string) {
  if (visualEdgeLogicalIDs.has(edgeID)) {
    selectedSummaryEdgeID.value = edgeID
    selectedEdgeIDs.value = []
  } else {
    selectedSummaryEdgeID.value = ''
    selectedEdgeIDs.value = [edgeID]
  }
  selectedNodeIDs.value = []
  selectedNodeID.value = ''
  syncFlow()
}

function onEdgeCurveChangeStart() {
  if (!edgeCurveHistorySnapshot) edgeCurveHistorySnapshot = cloneWorkflowGraph(graph.value)
}

function onEdgeCurveChange(edgeID: string, curve: { x: number; y: number }) {
  const edge = graph.value.edges.find((item) => item.id === edgeID)
  if (!edge) return
  edge.curve = curve.x === 0 && curve.y === 0 ? undefined : { ...curve }
  syncFlow()
}

function onEdgeCurveChangeEnd(_edgeID: string, changed: boolean) {
  const snapshot = edgeCurveHistorySnapshot
  edgeCurveHistorySnapshot = null
  if (!snapshot || !changed) return
  pushUndo(snapshot)
  markDirty()
  syncFlow()
}

function updateCanvasViewport(viewport: { x: number; y: number; zoom: number }) {
  Object.assign(canvasViewport, viewport)
}

function navigateFromMinimap(position: { x: number; y: number }) {
  void setCenter(position.x, position.y, { zoom: canvasViewport.zoom, duration: 0 })
}

function draftKey(workflowID = activeWorkflow.value?.id || 'preview') { return `${LOCAL_DRAFT_PREFIX}${workflowID}` }
function persistLocalDraft() {
  if (!activeWorkflow.value) return
  try {
    localStorage.setItem(draftKey(), JSON.stringify({ revision: activeWorkflow.value.revision, graph: graph.value, dirty: dirty.value, saved_at: Date.now() }))
  } catch { /* 浏览器存储不足时仍保留内存草稿。 */ }
}

function restoreLocalDraft(workflow: VideoWorkflow | null, fallback: VideoWorkflowGraph) {
  restoredLocalDraft = false
  conflictDraftKey.value = ''
  if (!workflow) return migrateVideoWorkflowGraph(fallback)
  try {
    const key = draftKey(workflow.id)
    const value = JSON.parse(localStorage.getItem(key) || 'null')
    if (value?.dirty && value.revision === workflow.revision) {
      restoredLocalDraft = true
      return migrateVideoWorkflowGraph(value.graph)
    }
    if (value?.dirty && value.revision !== workflow.revision) {
      conflictDraftKey.value = `${key}.conflict`
      localStorage.setItem(conflictDraftKey.value, JSON.stringify(value))
    }
  } catch { /* 忽略损坏的本地草稿。 */ }
  return migrateVideoWorkflowGraph(fallback)
}

function recoverConflictDraft() {
  if (!conflictDraftKey.value) return
  if (revisionConflict.value) {
    ElMessage.warning('请先点击「加载服务器版本」，再恢复本地草稿到最新修订')
    return
  }
  try {
    const value = JSON.parse(localStorage.getItem(conflictDraftKey.value) || 'null')
    if (!value?.graph) return
    const draftStorageKey = conflictDraftKey.value
    invalidateImageTransformContext()
    pushUndo()
    graph.value = migrateVideoWorkflowGraph(value.graph)
    nodeStaleEpoch.clear()
    seedVideoWorkflowStaleEpochBaseline(graph.value.nodes, nodeStaleEpoch, staleEpoch)
    localStorage.removeItem(draftStorageKey)
    conflictDraftKey.value = ''
    markDirty()
    syncFlow()
    ElMessage.success('已在最新服务器修订上恢复本地草稿，保存后将覆盖服务器版本')
  } catch { ElMessage.error('冲突草稿无法恢复') }
}

async function loadWorkspace(workflow?: VideoWorkflow | null) {
  if (componentUnmounted) return
  invalidateImageTransformContext()
  const generation = ++workspaceGeneration
  closeMediaPreview()
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
  runDetailCache.value = {}
  revisionHistoryItems.value = []
  revisionHistoryTotal.value = 0
  revisionHistoryVisible.value = false
  viewingRevision.value = next?.revision ?? null
  nodeHistoryLoading.value = false
  nodeHistoryGeneration += 1
  activeRun.value = null
  staleEpoch = 0
  nodeStaleEpoch.clear()
  activeRunStaleEpoch = 0
  refreshRunFailCount = 0
  refreshRunFailWarned = false
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
  graph.value = restoreLocalDraft(next, next?.graph || createEmptyVideoWorkflowGraph())
  seedVideoWorkflowStaleEpochBaseline(graph.value.nodes, nodeStaleEpoch, staleEpoch)
  const preferred = graph.value.nodes.find((node) => node.id === 'background_2') || graph.value.nodes[0]
  selectedNodeID.value = preferred?.id || ''
  selectedNodeIDs.value = preferred ? [preferred.id] : []
  selectedEdgeIDs.value = []
  selectedSummaryEdgeID.value = ''
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

function rememberRunDetailForPreview(run: VideoWorkflowRun | null | undefined) {
  if (!run?.node_runs?.some(nodeRunHasMediaOutput)) return
  runDetailCache.value = { ...runDetailCache.value, [run.id]: run }
  upsertRunHistory(run)
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
      .map((run) => {
        const cached = runDetailCache.value[run.id]
        if (run.node_runs?.length) return run
        if (cached?.node_runs?.length) return { ...run, node_runs: cached.node_runs, graph_snapshot: run.graph_snapshot || cached.graph_snapshot }
        if (activeRun.value?.id === run.id && activeRun.value.node_runs?.length) {
          return { ...run, node_runs: activeRun.value.node_runs, graph_snapshot: run.graph_snapshot || activeRun.value.graph_snapshot }
        }
        return run
      })
    runHistoryTotal.value = page.total
    const missing = runHistoryItems.value.filter((run) => !run.node_runs?.length && !runDetailCache.value[run.id]?.node_runs?.length)
    if (missing.length) void hydrateRunHistoryNodeRuns(missing.slice(0, RUN_HISTORY_PAGE_SIZE))
  } catch {
    ElMessage.error('生成历史加载失败，请重试')
  } finally {
    runHistoryLoading.value = false
  }
}

async function hydrateRunHistoryNodeRuns(runs: VideoWorkflowRun[]) {
  const workflowID = activeWorkflow.value?.id
  if (!workflowID || !runs.length) return
  const details = await Promise.all(runs.map((run) => {
    const cached = runDetailCache.value[run.id]
    if (cached?.node_runs) return Promise.resolve(cached)
    return getVideoWorkflowRun(run.id, true).catch(() => null)
  }))
  if (workflowID !== activeWorkflow.value?.id) return
  const cache = { ...runDetailCache.value }
  for (const detail of details) if (detail) cache[detail.id] = detail
  runDetailCache.value = cache
  runHistoryItems.value = runHistoryItems.value.map((run) => {
    const detail = cache[run.id]
    if (!detail?.node_runs || run.node_runs?.length) return run
    return { ...run, node_runs: detail.node_runs, graph_snapshot: run.graph_snapshot || detail.graph_snapshot }
  })
}

async function expandHistoryRun(run: VideoWorkflowRun) {
  if (run.node_runs?.length) return
  runHistoryActionID.value = run.id
  try {
    await hydrateRunHistoryNodeRuns([run])
  } catch {
    ElMessage.error('节点明细加载失败')
  } finally {
    runHistoryActionID.value = ''
  }
}

async function loadSelectedNodeHistory() {
  const workflowID = activeWorkflow.value?.id
  const nodeID = selectedNodeID.value
  if (!workflowID || !nodeID) return
  if (nodeHistoryLoading.value) return
  const generation = ++nodeHistoryGeneration
  nodeHistoryLoading.value = true
  try {
    const page = await listVideoWorkflowRuns(workflowID, { limit: NODE_HISTORY_LIMIT, offset: 0 })
    if (componentUnmounted || generation !== nodeHistoryGeneration || workflowID !== activeWorkflow.value?.id) return
    const merged = [...page.items, ...runHistoryItems.value]
    runHistoryItems.value = [...new Map(merged.map((item) => [item.id, item])).values()]
      .sort((a, b) => new Date(b.created_at || 0).getTime() - new Date(a.created_at || 0).getTime())
    runHistoryTotal.value = Math.max(runHistoryTotal.value, page.total)
    const missing = page.items.filter((run) => (
      run.id !== activeRun.value?.id
      && !run.node_runs
      && !runDetailCache.value[run.id]
    ))
    const details = await Promise.all(missing.map((run) => getVideoWorkflowRun(run.id, true).catch(() => null)))
    if (componentUnmounted || generation !== nodeHistoryGeneration || workflowID !== activeWorkflow.value?.id) return
    const cache = { ...runDetailCache.value }
    for (const detail of details) if (detail) cache[detail.id] = detail
    for (const run of page.items) if (run.node_runs) cache[run.id] = run
    runDetailCache.value = cache
    runHistoryItems.value = runHistoryItems.value.map((run) => {
      const detail = cache[run.id]
      if (!detail?.node_runs || run.node_runs?.length) return run
      return { ...run, node_runs: detail.node_runs, graph_snapshot: run.graph_snapshot || detail.graph_snapshot }
    })
  } catch (error) {
    if (generation !== nodeHistoryGeneration) return
    const now = Date.now()
    if (now - nodeHistoryErrorToastAt > 4000) {
      nodeHistoryErrorToastAt = now
      console.warn('loadSelectedNodeHistory failed', error)
      ElMessage.error('节点历史加载失败，请重试')
    }
  } finally {
    if (generation === nodeHistoryGeneration) nodeHistoryLoading.value = false
  }
}

function openRunHistory() {
  if (!requireActiveWorkflow('查看生成历史')) return
  runHistoryVisible.value = true
  runHistoryDetail.value = null
  void loadRunHistory(true)
}

async function loadRevisionHistory(reset = true) {
  const workflowID = activeWorkflow.value?.id
  if (!workflowID || revisionHistoryLoading.value) return
  revisionHistoryLoading.value = true
  try {
    const offset = reset ? 0 : revisionHistoryItems.value.length
    const page = await listVideoWorkflowRevisions(workflowID, { limit: 50, offset })
    if (activeWorkflow.value?.id !== workflowID) return
    const merged = reset ? page.items : [...revisionHistoryItems.value, ...page.items]
    revisionHistoryItems.value = [...new Map(merged.map((item) => [item.revision, item])).values()]
      .sort((a, b) => b.revision - a.revision)
    revisionHistoryTotal.value = page.total
  } catch {
    ElMessage.error('版本历史加载失败，请重试')
  } finally {
    revisionHistoryLoading.value = false
  }
}

function openRevisionHistory() {
  if (!requireActiveWorkflow('查看版本历史')) return
  revisionHistoryVisible.value = true
  void loadRevisionHistory(true)
}

async function switchToRevision(item: VideoWorkflowRevisionListItem) {
  if (!requireActiveWorkflow('切换版本')) return
  if (isRunActive.value) {
    ElMessage.warning('当前有运行进行中，请先停止后再切换版本')
    return
  }
  if (viewingRevision.value === item.revision) {
    ElMessage.info(`画布已在查看 R${item.revision}`)
    return
  }
  if (dirty.value) {
    try {
      await ElMessageBox.confirm(
        `当前有未保存修改。切换到 R${item.revision} 会用该版本画布覆盖本地草稿，是否继续？`,
        '切换版本',
        { type: 'warning', confirmButtonText: '切换', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  revisionHistoryAction.value = item.revision
  try {
    const detail = await getVideoWorkflowRevision(activeWorkflow.value!.id, item.revision)
    invalidateImageTransformContext()
    pushUndo()
    graph.value = migrateVideoWorkflowGraph(cloneWorkflowGraph(detail.graph))
    viewingRevision.value = detail.revision
    selectedNodeID.value = ''
    selectedNodeIDs.value = []
    selectedEdgeIDs.value = []
    undoStack.value = []
    redoStack.value = []
    if (detail.is_current || detail.revision === activeWorkflow.value?.revision) {
      dirty.value = false
      localStorage.removeItem(draftKey())
      ElMessage.success(`已恢复查看服务器当前版本 R${detail.revision}`)
    } else {
      markDirty()
      ElMessage.success(`已切换到 R${detail.revision}；保存后将作为新修订写入`)
    }
    syncFlow()
  } catch {
    ElMessage.error('切换版本失败')
  } finally {
    revisionHistoryAction.value = null
  }
}

async function inspectHistoryRun(run: VideoWorkflowRun) {
  runHistoryActionID.value = run.id
  try {
    runHistoryDetail.value = await getVideoWorkflowRun(run.id, true)
    runDetailCache.value = { ...runDetailCache.value, [run.id]: runHistoryDetail.value }
    runHistoryItems.value = runHistoryItems.value.map((item) => (
      item.id === run.id
        ? {
            ...item,
            node_runs: runHistoryDetail.value?.node_runs || item.node_runs,
            graph_snapshot: runHistoryDetail.value?.graph_snapshot || item.graph_snapshot,
          }
        : item
    ))
  } catch { ElMessage.error('运行详情加载失败') } finally { runHistoryActionID.value = '' }
}

async function switchToHistoryRun(run: VideoWorkflowRun) {
  if (!requireActiveWorkflow('切换历史运行')) return
  if (isRunActive.value && activeRun.value?.id !== run.id) {
    ElMessage.warning('当前有运行进行中，请先停止后再切换历史结果')
    return
  }
  if (activeRun.value?.id === run.id && (run.node_runs?.length || activeRun.value.node_runs?.length)) {
    ElMessage.info(`当前已在查看 R${run.workflow_revision} 的运行结果`)
    return
  }
  runHistoryActionID.value = run.id
  try {
    const detail = (run.node_runs?.length ? run : null)
      || (runDetailCache.value[run.id]?.node_runs?.length ? runDetailCache.value[run.id] : null)
      || await getVideoWorkflowRun(run.id, true)
    activeRun.value = detail
    runDetailCache.value = { ...runDetailCache.value, [run.id]: detail }
    upsertRunHistory(detail)
    rememberRun(activeWorkflow.value!.id, detail.id)
    if (['queued', 'running', 'awaiting_character_approval', 'awaiting_storyboard_approval', 'cancel_pending'].includes(detail.status || '')) {
      startPolling()
    } else {
      stopPolling()
    }
    syncFlow()
    ElMessage.success(`已切换到 R${detail.workflow_revision} 的运行结果`)
  } catch {
    ElMessage.error('切换历史运行失败')
  } finally {
    runHistoryActionID.value = ''
  }
}

function openCreateWorkflow() {
  if (templates.value.length) selectedTemplateID.value = templates.value[0]?.id || ''
  createDialogVisible.value = true
}

function requireActiveWorkflow(action = '继续操作') {
  if (activeWorkflow.value) return true
  ElMessage.warning(`尚未关联工作流，无法${action}`)
  openCreateWorkflow()
  return false
}

async function ensureDefaultWorkflow(options: { silent?: boolean } = {}) {
  if (workflows.value.length > 0) return true
  if (!templates.value.length) {
    await loadWorkspace(null)
    if (!options.silent) openCreateWorkflow()
    return false
  }
  const template = templates.value[0]
  const name = (newWorkflowName.value.trim() || template.name || '我的视频工作流').slice(0, 40)
  creating.value = true
  try {
    const item = await createVideoWorkflow({ name, template_id: template.id })
    workflows.value = [item]
    if (!options.silent) ElMessage.success(`已从模板「${template.name}」自动创建工作流`)
    await loadWorkspace(await getVideoWorkflow(item.id))
    return true
  } catch {
    ElMessage.error('自动创建工作流失败，请手动选择模板')
    await loadWorkspace(null)
    openCreateWorkflow()
    return false
  } finally {
    creating.value = false
  }
}

function applyRuntimeSettings(target: VideoWorkflowRuntimeSettings, source: Partial<VideoWorkflowRuntimeSettings>) {
  for (const field of RUNTIME_SETTING_FIELDS) {
    const raw = Number(source[field.key])
    const value = Number.isFinite(raw) ? Math.round(raw) : DEFAULT_RUNTIME_SETTINGS[field.key]
    target[field.key] = Math.min(field.max, Math.max(field.min, value))
  }
}

async function loadRuntimeSettings() {
  runtimeSettingsLoading.value = true
  try {
    applyRuntimeSettings(runtimeSettings, await getVideoWorkflowRuntimeSettings())
  } catch {
    backendAvailable.value = false
  } finally {
    runtimeSettingsLoading.value = false
  }
}

function openRuntimeSettings() {
  applyRuntimeSettings(runtimeSettingsDraft, runtimeSettings)
  runtimeSettingsVisible.value = true
  if (!runtimeSettingsLoading.value) void loadRuntimeSettings().then(() => {
    if (runtimeSettingsVisible.value) applyRuntimeSettings(runtimeSettingsDraft, runtimeSettings)
  })
}

async function saveRuntimeSettings() {
  applyRuntimeSettings(runtimeSettingsDraft, runtimeSettingsDraft)
  runtimeSettingsSaving.value = true
  try {
    const next = await updateVideoWorkflowRuntimeSettings({ ...runtimeSettingsDraft })
    applyRuntimeSettings(runtimeSettings, next)
    applyRuntimeSettings(runtimeSettingsDraft, next)
    runtimeSettingsVisible.value = false
    ElMessage.success('并发设置已生效')
  } catch (error: any) {
    ElMessage.error(error?.message || '并发设置保存失败')
  } finally {
    runtimeSettingsSaving.value = false
  }
}

async function bootstrap() {
  loading.value = true
  try {
    const [templateItems, workflowItems, , modelItems] = await Promise.all([
      listVideoWorkflowTemplates(), listVideoWorkflows(), refreshAssets({ silent: true }).catch(() => {}),
      listVideoWorkflowModels().catch(() => []),
      loadRuntimeSettings().catch(() => {}),
    ])
    if (componentUnmounted) return
    templates.value = templateItems
    workflows.value = workflowItems
    workflowVideoModels.value = modelItems.length ? modelItems : workflowVideoModels.value
    selectedTemplateID.value = templates.value[0]?.id || ''
    if (workflowItems.length === 0) await ensureDefaultWorkflow({ silent: true })
    else await loadWorkspace()
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
  if (!autosaveReady.value || !activeWorkflow.value) return
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
      viewingRevision.value = activeWorkflow.value.revision
      localStorage.removeItem(draftKey())
    } else scheduleSave()
    if (!silent) ElMessage.success(`已保存 R${activeWorkflow.value.revision}`)
    return activeWorkflow.value
  } catch (error) {
    persistLocalDraft()
    if (axios.isAxiosError(error) && error.response?.status === 409) {
      revisionConflict.value = true
      const key = `${draftKey()}.conflict`
      localStorage.setItem(key, JSON.stringify({ graph: cloneWorkflowGraph(graph.value), saved_at: Date.now() }))
      conflictDraftKey.value = key
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

async function saveNow() {
  if (!activeWorkflow.value) {
    ElMessage.warning('请先从模板创建工作流')
    return
  }
  if (revisionConflict.value) {
    ElMessage.warning('修订冲突，请先加载服务器版本或恢复本地草稿')
    return
  }
  if (saveTimer) {
    window.clearTimeout(saveTimer)
    saveTimer = null
  }
  if (!dirty.value) {
    ElMessage.info(`已是最新 · R${activeWorkflow.value.revision}`)
    return
  }
  const ok = await flushSave()
  if (ok) {
    ElMessage.success(`已保存 R${activeWorkflow.value.revision}`)
    return
  }
  if (revisionConflict.value) {
    ElMessage.error('修订冲突，保存未完成')
    return
  }
  ElMessage.error(backendAvailable.value ? '保存失败，请稍后重试' : '网络不可用，草稿已保存在本机')
}

async function reloadWorkflow() {
  if (!activeWorkflow.value) return
  const workflowID = activeWorkflow.value.id
  const preservedConflictKey = conflictDraftKey.value || `${draftKey(workflowID)}.conflict`
  const hadConflictDraft = Boolean(localStorage.getItem(preservedConflictKey))
  await loadWorkspace(await getVideoWorkflow(workflowID))
  if (hadConflictDraft && localStorage.getItem(preservedConflictKey)) {
    conflictDraftKey.value = preservedConflictKey
  }
  ElMessage.success(hadConflictDraft
    ? '已载入服务器版本，本地冲突草稿仍可恢复'
    : '已载入服务器版本')
}

function applyOutputSetting(command: OutputSettingCommand) {
  const actions: Record<OutputSettingCommand, () => void> = {
    aspect_9_16: () => { graph.value.settings.aspect_ratio = '9:16' },
    aspect_16_9: () => { graph.value.settings.aspect_ratio = '16:9' },
    aspect_1_1: () => { graph.value.settings.aspect_ratio = '1:1' },
    resolution_720p: () => { graph.value.settings.resolution = '720p' },
    resolution_1080p: () => { graph.value.settings.resolution = '1080p' },
  }
  const action = actions[command]
  if (!action) return
  action()
  markDirty()
}

function handleWorkspaceMenu(command: WorkspaceMenuCommand) {
  if (command === 'preview') {
    previewOutput()
    return
  }
  if (command === 'history') {
    openRunHistory()
    return
  }
  if (command === 'create_workflow') {
    createDialogVisible.value = true
    return
  }
  if (command === 'runtime_settings') {
    openRuntimeSettings()
    return
  }
  if (command === 'aspect_9_16' || command === 'aspect_16_9' || command === 'aspect_1_1' || command === 'resolution_720p' || command === 'resolution_1080p') {
    applyOutputSetting(command)
    return
  }
  if (command === 'layout_edit' || command === 'layout_compose' || command === 'layout_review') {
    applyWorkspaceLayoutPreset(command.replace('layout_', '') as VideoWorkflowLayoutPresetID)
    return
  }
  if (command === 'outline') {
    outlineVisible.value = true
    return
  }
  if (command === 'export_json') {
    exportWorkflowJSON()
    return
  }
  if (command === 'delete_workflow') {
    void deleteActiveWorkflow()
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
    markDirty()
    syncFlow()
    await nextTick()
    fitView({ padding: .14, duration: 280 })

    const saved = await flushSave()
    if (saved) ElMessage.success(`已导入并保存为 R${activeWorkflow.value?.revision || 0}`)
    else if (!revisionConflict.value) ElMessage.warning('已导入本地草稿，尚未同步到服务器')
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      if (error instanceof VideoWorkflowTransferError && error.issues?.length) {
        openValidationIssues(error.issues)
      }
      ElMessage.error(error?.message || '导入 JSON 失败')
    }
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

async function renameActiveWorkflow() {
  if (!activeWorkflow.value) return
  if (revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')
  try {
    const { value } = await ElMessageBox.prompt('请输入工作流名称', '重命名工作流', {
      confirmButtonText: '保存',
      cancelButtonText: '取消',
      inputValue: activeWorkflow.value.name,
    })
    const name = value.trim()
    if (!name || name === activeWorkflow.value.name) return
    if (activeSavePromise) await activeSavePromise
    if (!activeWorkflow.value || revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')
    if (dirty.value && !await flushSave()) return ElMessage.error('当前草稿尚未保存，已取消重命名')
    if (!activeWorkflow.value || revisionConflict.value) return

    const sequence = changeSequence.value
    const workflowID = activeWorkflow.value.id
    const renameTask = (async (): Promise<VideoWorkflow | null> => {
      saving.value = true
      try {
        const updated = await updateVideoWorkflow(workflowID, {
          name,
          revision: activeWorkflow.value!.revision,
          graph: cloneWorkflowGraph(graph.value),
        })
        if (activeWorkflow.value?.id !== workflowID) return null
        activeWorkflow.value = {
          ...activeWorkflow.value,
          ...updated,
          graph: migrateVideoWorkflowGraph(updated.graph || graph.value),
        }
        graph.value = activeWorkflow.value.graph
        if (changeSequence.value === sequence) {
          dirty.value = false
          localStorage.removeItem(draftKey())
        }
        const index = workflows.value.findIndex((item) => item.id === updated.id)
        if (index >= 0) workflows.value[index] = activeWorkflow.value
        backendAvailable.value = true
        ElMessage.success('已重命名')
        return activeWorkflow.value
      } catch (error) {
        persistLocalDraft()
        if (axios.isAxiosError(error) && error.response?.status === 409) {
          revisionConflict.value = true
          const key = `${draftKey()}.conflict`
          localStorage.setItem(key, JSON.stringify({ graph: cloneWorkflowGraph(graph.value), saved_at: Date.now() }))
          conflictDraftKey.value = key
          ElMessage.error('修订冲突，本地改动已另存为浏览器副本')
        } else {
          backendAvailable.value = false
          ElMessage.warning('网络不可用，重命名未成功，草稿已保存在本机')
        }
        return null
      } finally {
        saving.value = false
      }
    })()
    activeSavePromise = renameTask.finally(() => { activeSavePromise = null })
    await activeSavePromise
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error((error as Error)?.message || '重命名失败')
  }
}

async function deleteActiveWorkflow() {
  if (!activeWorkflow.value) return
  if (isRunActive.value || runningAction.value) return ElMessage.warning('请先停止当前运行再删除')
  if (revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')
  const target = activeWorkflow.value
  try {
    if (dirty.value) {
      try {
        await ElMessageBox.confirm(
          `「${target.name}」有未保存草稿。先保存再删除，或放弃草稿直接删除？运行历史与素材不会随工作流删除。`,
          '删除工作流',
          {
            type: 'warning',
            confirmButtonText: '先保存再删除',
            cancelButtonText: '放弃草稿并删除',
            distinguishCancelAndClose: true,
          },
        )
        if (!await flushSave()) return ElMessage.error('保存失败，已取消删除')
        if (revisionConflict.value) return ElMessage.warning('请先处理当前修订冲突')
      } catch (action) {
        if (action === 'close') return
        // cancel = 放弃草稿并删除
      }
    } else {
      await ElMessageBox.confirm(
        `确定删除「${target.name}」？运行历史与素材不会随工作流删除。`,
        '删除工作流',
        { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
      )
    }
    if (activeWorkflow.value?.id !== target.id) return
    await deleteVideoWorkflow(target.id)
    localStorage.removeItem(draftKey(target.id))
    localStorage.removeItem(`${draftKey(target.id)}.conflict`)
    workflows.value = workflows.value.filter((item) => item.id !== target.id)
    const next = workflows.value[0] || null
    if (next) await loadWorkspace(await getVideoWorkflow(next.id))
    else await loadWorkspace(null)
    ElMessage.success('工作流已删除')
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error((error as Error)?.message || '删除工作流失败')
  }
}

function selectNode(id: string, additive = false) {
  if (id === SHARED_CHARACTER_BUS_ID) {
    selectedNodeID.value = ''
    selectedNodeIDs.value = []
    selectedEdgeIDs.value = []
    selectedSummaryEdgeID.value = SHARED_CHARACTER_BUS_ID
    syncFlow()
    return
  }
  if (id.startsWith('__')) return
  if (!panelLayout.inspectorOpen) setInspectorOpen(true)
  selectedNodeID.value = id
  if (additive) {
    selectedNodeIDs.value = selectedNodeIDs.value.includes(id)
      ? selectedNodeIDs.value.filter((item) => item !== id)
      : [...selectedNodeIDs.value, id]
  } else selectedNodeIDs.value = [id]
  selectedEdgeIDs.value = []
  selectedSummaryEdgeID.value = ''
  syncFlow()
}

function clearSelection() {
  selectedNodeID.value = ''
  selectedNodeIDs.value = []
  selectedEdgeIDs.value = []
  selectedSummaryEdgeID.value = ''
  quickConnectMenu.value = null
  pendingConnection.value = null
  syncFlow()
}

function onNodeChanges(changes: NodeChange[]) {
  let touchedSelect = false
  for (const change of changes) {
    if (change.type !== 'select') continue
    touchedSelect = true
    if (change.id === SHARED_CHARACTER_BUS_ID) {
      selectedSummaryEdgeID.value = change.selected ? SHARED_CHARACTER_BUS_ID : ''
      if (change.selected) {
        selectedNodeID.value = ''
        selectedNodeIDs.value = []
        selectedEdgeIDs.value = []
      }
      continue
    }
    if (change.id.startsWith('__')) continue
    if (change.selected && !selectedNodeIDs.value.includes(change.id)) selectedNodeIDs.value.push(change.id)
    if (!change.selected) selectedNodeIDs.value = selectedNodeIDs.value.filter((id) => id !== change.id)
  }
  if (!touchedSelect) return
  // 框选/多选只更新了 selectedNodeIDs；同步 Inspector 绑定的 selectedNodeID，避免参数/运行作用在旧节点上
  if (!selectedNodeIDs.value.length) selectedNodeID.value = ''
  else if (!selectedNodeIDs.value.includes(selectedNodeID.value)) selectedNodeID.value = selectedNodeIDs.value.at(-1) || ''
}

function onEdgeChanges(changes: EdgeChange[]) {
  for (const change of changes) {
    if (change.type !== 'select') continue
    if (visualEdgeLogicalIDs.has(change.id)) {
      selectedSummaryEdgeID.value = change.selected ? change.id : ''
      if (change.selected) {
        selectedNodeID.value = ''
        selectedNodeIDs.value = []
        selectedEdgeIDs.value = []
      }
      continue
    }
    if (change.selected && !selectedEdgeIDs.value.includes(change.id)) selectedEdgeIDs.value.push(change.id)
    if (!change.selected) selectedEdgeIDs.value = selectedEdgeIDs.value.filter((id) => id !== change.id)
  }
}

function addNode(type: string, position?: { x: number; y: number }) {
  if (!requireActiveWorkflow('添加节点')) return
  if (['timeline', 'compose'].includes(type)) return ElMessage.info('时间线和最终成片为固定系统节点')
  if (graph.value.nodes.length >= VIDEO_WORKFLOW_MAX_NODES) return ElMessage.warning(`节点不能超过 ${VIDEO_WORKFLOW_MAX_NODES} 个`)
  const count = graph.value.nodes.filter((node) => node.type === type).length
  if (type === 'character' && count >= 4) return ElMessage.warning('角色节点不能超过 4 个')
  const sceneID = ['background', 'video'].includes(type) ? selectedNode.value?.scene_id : undefined
  const node = applyVideoModelDefault(makeVideoWorkflowNode(type, position || { x: 320 + count * 32, y: 180 + count * 28 }, sceneID))
  if (autoArrange.value || !position) node.position_mode = 'auto'
  commitGraph((target) => { target.nodes.push(node) })
  selectedNodeID.value = node.id
  selectedNodeIDs.value = [node.id]
  syncFlow()
  void nextTick(() => updateNodeInternals([node.id]))
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

function noteNodeStale(nodeID: string) {
  staleEpoch += 1
  nodeStaleEpoch.set(nodeID, staleEpoch)
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
      if (node && !['timeline', 'compose'].includes(node.type)) {
        node.status = 'stale'
        node.stale_reason = '上游输入已修改，请重新运行'
        noteNodeStale(node.id)
      }
      queue.push(edge.target)
    }
  }
}

function markTargetsStaleFromEdges(
  target: VideoWorkflowGraph,
  edges: Array<Pick<VideoWorkflowEdge, 'target'>>,
) {
  const seen = new Set<string>()
  for (const edge of edges) {
    if (seen.has(edge.target)) continue
    seen.add(edge.target)
    const node = target.nodes.find((item) => item.id === edge.target)
    if (!node || ['timeline', 'compose'].includes(node.type)) continue
    node.status = 'stale'
    node.stale_reason = '上游输入已修改，请重新运行'
    noteNodeStale(node.id)
    markDownstreamStale(target, node.id)
  }
}

function updateNodeConfig(key: string, value: any) {
  if (!selectedNode.value) return
  const id = selectedNode.value.id
  commitGraph((target) => {
    const node = target.nodes.find((item) => item.id === id)
    if (node) {
      node.config[key] = value
      node.status = 'stale'
      node.stale_reason = '节点参数已修改，请重新运行'
      noteNodeStale(id)
      markDownstreamStale(target, id)
    }
  })
}

function updateNodeModel(value: string) {
  const type = selectedNode.value?.type || ''
  const setting: 'text_model' | 'image_model' | 'video_model' | '' = ['story_brief', 'script', 'scene'].includes(type)
    ? 'text_model'
    : ['character', 'background', 'image'].includes(type)
      ? 'image_model'
      : type === 'video' ? 'video_model' : ''
  if (!setting) return
  const affectedTypes = setting === 'text_model'
    ? new Set(['story_brief', 'script', 'scene'])
    : setting === 'image_model'
      ? new Set(['character', 'background', 'image'])
      : new Set(['video'])
  commitGraph((target) => {
    target.settings[setting] = value
    for (const node of target.nodes.filter((item) => affectedTypes.has(item.type))) {
      node.config.model = value
      node.status = 'stale'
      node.stale_reason = '执行模型已修改，请重新运行'
      noteNodeStale(node.id)
      markDownstreamStale(target, node.id)
    }
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
  const edges = clipboard.value.edges.map((edge) => ({ ...edge, route: undefined, id: `edge_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`, source: idMap.get(edge.source)!, target: idMap.get(edge.target)! }))
  let skippedEdges = 0
  const acceptedEdges: VideoWorkflowEdge[] = []
  commitGraph((target) => {
    target.nodes.push(...clones)
    for (const edge of edges) {
      const error = connectionError(target, {
        source: edge.source,
        source_port: edge.source_port,
        target: edge.target,
        target_port: edge.target_port,
      })
      if (error) { skippedEdges += 1; continue }
      target.edges.push(edge)
      acceptedEdges.push(edge)
    }
    markTargetsStaleFromEdges(target, acceptedEdges)
  })
  if (skippedEdges) ElMessage.info(`已跳过 ${skippedEdges} 条不合法连线`)
  selectedNodeIDs.value = clones.map((node) => node.id)
  selectedNodeID.value = clones.at(-1)?.id || ''
  syncFlow()
}

function duplicateSelectedNodes() { copySelectedNodes(); pasteNodes() }

async function deleteSelectedNodes() {
  if (!requireActiveWorkflow('删除节点')) return
  if (!selectedNodeIDs.value.length) return ElMessage.info('请先选择要删除的节点')
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

function onNodeDragStop(event: NodeDragEvent) {
  const moved = event.nodes?.length ? event.nodes : [event.node]
  commitGraph((target) => {
    let movedGraphNode = false
    for (const item of moved) {
      if (item.id === SHARED_CHARACTER_BUS_ID) {
        target.layout = {
          ...(target.layout || {}),
          shared_character_bus: { position: { ...item.position }, position_mode: 'manual' },
        }
        continue
      }
      const node = target.nodes.find((entry) => entry.id === item.id)
      if (node && !node.locked) {
        node.position = { ...item.position }
        node.position_mode = 'manual'
        movedGraphNode = true
      }
    }
    if (movedGraphNode) {
      clearAutoEdgeRoutes(target)
      updateVideoWorkflowGroupBounds(target)
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

function normalizeFlowConnection(connection: Connection): Connection | null {
  return normalizeVideoWorkflowConnection(graph.value, connection)
}

async function onConnect(connection: Connection) {
  const normalized = normalizeFlowConnection(connection)
  if (!normalized?.source || !normalized.target || !normalized.sourceHandle || !normalized.targetHandle) {
    return ElMessage.warning('请拖到目标节点的输入端口上完成连线')
  }
  const candidate = { source: normalized.source, source_port: normalized.sourceHandle, target: normalized.target, target_port: normalized.targetHandle }
  let error = connectionError(graph.value, candidate)
  const old = graph.value.edges.find((edge) => edge.target === candidate.target && edge.target_port === candidate.target_port)
  if (error === '单值输入端口只能连接 1 条边' && old) {
    try {
      await ElMessageBox.confirm('该输入端口已有连接，是否替换？', '替换连接', { confirmButtonText: '替换', cancelButtonText: '取消' })
    } catch { return }
    const withoutOld = { ...graph.value, edges: graph.value.edges.filter((edge) => edge.id !== old.id) }
    error = connectionError(withoutOld, candidate)
    if (!error) {
      commitGraph((target) => {
        target.edges = target.edges.filter((edge) => edge.id !== old.id)
        target.edges.push({ id: `edge_${Date.now().toString(36)}`, ...candidate })
        markTargetsStaleFromEdges(target, [candidate])
      })
    }
    return
  }
  if (error) return ElMessage.warning(error)
  commitGraph((target) => {
    target.edges.push({ id: `edge_${Date.now().toString(36)}`, ...candidate })
    markTargetsStaleFromEdges(target, [candidate])
  })
  pendingConnection.value = null
  quickConnectMenu.value = null
}

function onConnectStart(payload: { nodeId?: string; handleId?: string | null; handleType?: string | null }) {
  if (payload.handleType !== 'source' || !payload.nodeId || !payload.handleId) return
  pendingConnection.value = { nodeID: payload.nodeId, portID: payload.handleId }
}

function findCompatibleTargetPort(sourcePortType: string, target: VideoWorkflowNode) {
  const collapsed = new Set(
    edgeDisplayMode.value === 'all' ? [] : (sharedCharacterBus(graph.value)?.targetPortIDs.get(target.id) || []),
  )
  return (target.inputs || []).find((port) => port.type === sourcePortType && !collapsed.has(port.id))
    || (target.inputs || []).find((port) => port.type === sourcePortType)
}

function resolveConnectionDropTarget(event?: MouseEvent | TouchEvent) {
  const target = (event?.target as HTMLElement | null)?.closest?.('.vue-flow__node') as HTMLElement | null
  const nodeID = target?.dataset?.id || target?.getAttribute('data-id') || ''
  if (!nodeID || nodeID.startsWith('__')) return null
  return graph.value.nodes.find((node) => node.id === nodeID) || null
}

async function onConnectEnd(event?: MouseEvent | TouchEvent) {
  const mouse = event as MouseEvent | undefined
  if (!pendingConnection.value || !mouse) {
    pendingConnection.value = null
    return
  }
  if ((mouse.target as HTMLElement | null)?.closest('.vue-flow__handle')) {
    pendingConnection.value = null
    return
  }
  const source = graph.value.nodes.find((node) => node.id === pendingConnection.value?.nodeID)
  const output = source?.outputs?.find((port) => port.id === pendingConnection.value?.portID)
  const dropTarget = resolveConnectionDropTarget(mouse)
  if (source && output && dropTarget && dropTarget.id !== source.id) {
    const input = findCompatibleTargetPort(output.type, dropTarget)
    if (input) {
      const pending = pendingConnection.value
      pendingConnection.value = null
      await onConnect({
        source: pending.nodeID,
        sourceHandle: pending.portID,
        target: dropTarget.id,
        targetHandle: input.id,
      })
      return
    }
    ElMessage.warning(`无法连接到「${nodeTitle(dropTarget)}」：没有兼容的 ${output.type} 输入端口`)
  }
  quickConnectMenu.value = {
    x: mouse.clientX - (panelLayout.libraryOpen ? panelLayout.left : 0),
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
  if (autoArrange.value) node.position_mode = 'auto'
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
  void nextTick(() => updateNodeInternals([node.id]))
}

function onEdgeUpdate(event: EdgeUpdateEvent) {
  const connection = normalizeFlowConnection(event.connection)
  if (!connection?.source || !connection.target || !connection.sourceHandle || !connection.targetHandle) return
  const candidate = { source: connection.source, source_port: connection.sourceHandle, target: connection.target, target_port: connection.targetHandle }
  const draft = { ...graph.value, edges: graph.value.edges.filter((edge) => edge.id !== event.edge.id) }
  const error = connectionError(draft, candidate)
  if (error) return ElMessage.warning(error)
  commitGraph((target) => {
    const edge = target.edges.find((item) => item.id === event.edge.id)
    if (edge) { Object.assign(edge, candidate); edge.route = undefined }
  })
}

function removeSelectedEdges() {
  if (!selectedEdgeIDs.value.length) return
  commitGraph((target) => {
    const removed = target.edges.filter((edge) => selectedEdgeIDs.value.includes(edge.id))
    target.edges = target.edges.filter((edge) => !selectedEdgeIDs.value.includes(edge.id))
    markTargetsStaleFromEdges(target, removed)
  })
  selectedEdgeIDs.value = []
}

function fitViewToCenter(duration = 280) {
  fitView({ padding: .18, maxZoom: .9, duration })
}

async function runGraphLayout(
  working: VideoWorkflowGraph,
  mode: 'auto' | 'all',
  options: { silent?: boolean } = {},
) {
  const sequence = changeSequence.value
  layoutBusy.value = true
  try {
    const result = await layoutVideoWorkflowGraph(working, mode)
    if (sequence !== changeSequence.value) {
      if (options.silent) scheduleAutoArrange()
      else ElMessage.warning('布局期间画布已发生修改，本次整理结果未应用')
      return
    }
    if (sharedCharacterBus(result.graph)) ensureSharedCharacterBusLayout(result.graph, mode === 'all')
    pushUndo()
    graph.value = result.graph
    markDirty()
    syncFlow()
    await nextTick()
    fitViewToCenter()
    if (!options.silent && result.engine === 'dagre') ElMessage.warning('ELK 布局不可用，已使用兼容布局')
  } finally {
    layoutBusy.value = false
  }
}

async function autoLayout(command: 'auto' | 'selected_auto' | 'all') {
  if (layoutBusy.value) return
  if (!requireActiveWorkflow('整理画布')) return
  if (command === 'all') {
    try {
      await ElMessageBox.confirm(
        '将重新排列所有未锁定节点。锁定节点保持原位，手工调整的位置会改为自动布局。',
        '重新整理全部节点？',
        { confirmButtonText: '重新整理', cancelButtonText: '取消', type: 'warning' },
      )
    } catch { return }
  }

  const working = cloneWorkflowGraph(graph.value)
  if (command === 'selected_auto') {
    const selected = new Set(selectedNodeIDs.value)
    const restorable = working.nodes.filter((node) => selected.has(node.id) && !node.locked)
    if (!restorable.length) return ElMessage.info('请先选择至少一个未锁定节点')
    restorable.forEach((node) => { node.position_mode = 'auto' })
  }
  const mode = command === 'all' ? 'all' : 'auto'
  if (!videoWorkflowMovableNodeIDs(working, mode).length) {
    if (command === 'auto') return ElMessage.info('没有自动布局节点。请用「所选恢复自动布局」或「重新整理全部」')
    if (command === 'selected_auto') return ElMessage.info('所选节点均为锁定或已是手工布局')
    return ElMessage.info('没有可整理的节点')
  }
  await runGraphLayout(working, mode)
  ElMessage.success('画布已重新整理')
}

function scheduleAutoArrange() {
  if (!autoArrange.value) return
  if (autoArrangeTimer) window.clearTimeout(autoArrangeTimer)
  autoArrangeTimer = window.setTimeout(() => {
    autoArrangeTimer = null
    void runAutoArrange()
  }, 480)
}

async function runAutoArrange() {
  if (!autoArrange.value || componentUnmounted) return
  if (layoutBusy.value) return scheduleAutoArrange()
  if (!videoWorkflowMovableNodeIDs(graph.value, 'auto').length) return
  await runGraphLayout(cloneWorkflowGraph(graph.value), 'auto', { silent: true })
}

function toggleAutoArrange() {
  autoArrange.value = !autoArrange.value
  persistPanelLayout()
  if (autoArrange.value) {
    ElMessage.success('已开启自动排布：新增节点或连线后将自动整理画布')
    scheduleAutoArrange()
  } else if (autoArrangeTimer) {
    window.clearTimeout(autoArrangeTimer)
    autoArrangeTimer = null
  }
}

function clearAutoEdgeRoutes(target: VideoWorkflowGraph) {
  target.edges.forEach((edge) => { edge.route = undefined })
}

function alignSelected(axis: 'x' | 'y') {
  if (!requireActiveWorkflow('对齐节点')) return
  const nodes = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id))
  if (nodes.length < 2) return ElMessage.info(axis === 'x' ? '请先选中 2 个及以上节点再左对齐' : '请先选中 2 个及以上节点再顶部对齐')
  const value = Math.min(...nodes.map((node) => node.position[axis]))
  commitGraph((target) => {
    target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !node.locked).forEach((node) => {
      node.position[axis] = value
      node.position_mode = 'manual'
    })
    clearAutoEdgeRoutes(target)
    updateVideoWorkflowGroupBounds(target)
  })
}

function distributeSelected(axis: 'x' | 'y') {
  if (!requireActiveWorkflow('分布节点')) return
  const nodes = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).sort((a, b) => a.position[axis] - b.position[axis])
  if (nodes.length < 3) return ElMessage.info(axis === 'x' ? '请先选中 3 个及以上节点再水平等距分布' : '请先选中 3 个及以上节点再垂直等距分布')
  const gap = (nodes.at(-1)!.position[axis] - nodes[0].position[axis]) / (nodes.length - 1)
  commitGraph((target) => {
    nodes.forEach((source, index) => {
      const node = target.nodes.find((item) => item.id === source.id)
      if (node && !node.locked) {
        node.position[axis] = nodes[0].position[axis] + gap * index
        node.position_mode = 'manual'
      }
    })
    clearAutoEdgeRoutes(target)
    updateVideoWorkflowGroupBounds(target)
  })
}

function toggleLock() {
  if (!requireActiveWorkflow('锁定节点')) return
  if (!selectedNodeIDs.value.length) return ElMessage.info('请先选择要锁定或解锁的节点')
  const shouldLock = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).some((node) => !node.locked)
  commitGraph((target) => target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !['timeline', 'compose'].includes(node.type)).forEach((node) => { node.locked = shouldLock }))
}

function toggleCollapse() {
  if (!requireActiveWorkflow('折叠节点')) return
  if (!selectedNodeIDs.value.length) return ElMessage.info('请先选择要折叠或展开的节点')
  commitGraph((target) => {
    target.nodes.filter((node) => selectedNodeIDs.value.includes(node.id)).forEach((node) => { node.collapsed = !node.collapsed })
    clearAutoEdgeRoutes(target)
    updateVideoWorkflowGroupBounds(target)
  })
}

function canSetTimelineClips(target: VideoWorkflowGraph, clips: VideoWorkflowTimelineClip[]) {
  const timeline = target.nodes.find((node) => node.type === 'timeline')
  if (!timeline) return false
  return target.edges.filter((edge) => edge.target !== timeline.id).length + clips.length <= VIDEO_WORKFLOW_MAX_EDGES
}

function toggleEnabled() {
  if (!requireActiveWorkflow('切换节点状态')) return
  const editable = graph.value.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !['timeline', 'compose'].includes(node.type))
  if (!editable.length) return ElMessage.info('请先选择可启用/停用的节点（时间线与成片除外）')
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
    const selectedIDs = new Set(selected.map((node) => node.id))
    for (const group of target.groups) {
      if (group.type !== 'scene' || !group.node_ids.some((id) => selectedIDs.has(id))) continue
      const members = target.nodes.filter((node) => group.node_ids.includes(node.id) && !['timeline', 'compose'].includes(node.type))
      if (!members.length) continue
      // 组内全部可编辑节点停用 → group.enabled=false；任一重新启用 → true（与后端 activeNodeIDs 双源判定对齐）
      group.enabled = members.some((node) => node.enabled !== false)
    }
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

function updateTimelineClips(clips: VideoWorkflowTimelineClip[]) {
  commitGraph((target) => {
    if (!setTimelineClips(target, clips)) {
      ElMessage.warning(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
    }
  })
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
}

async function replaceImage(file: File) {
  if (!selectedNode.value) return
  try {
    const asset = await uploadVideoAsset(file)
    assets.value.unshift(asset)
    if (asset.kind === 'image') {
      applyAssetToSelected(asset)
      ElMessage.success('图片已上传为新素材版本')
    } else {
      ElMessage.success('素材已上传')
    }
  } catch (error: any) {
    ElMessage.error(error?.message || '素材上传失败')
  }
}

/** 素材库上传：不依赖当前选中节点；仅在 chooseAsset 上下文绑定图片。 */
async function uploadLibraryAsset(file: File) {
  try {
    const asset = await uploadVideoAsset(file)
    assets.value.unshift(asset)
    const canBindImage = pickingAsset.value
      && asset.kind === 'image'
      && selectedNode.value
      && ['background', 'image'].includes(selectedNode.value.type)
    if (canBindImage) {
      applyAssetToSelected(asset)
      ElMessage.success('图片已上传并绑定到当前节点')
    } else {
      ElMessage.success('素材已上传')
    }
  } catch (error: any) {
    ElMessage.error(error?.message || '素材上传失败')
  }
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
  if (pickingAsset.value && asset.kind === 'image') applyAssetToSelected(asset)
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
    noteNodeStale(nodeID)
    markDownstreamStale(target, nodeID)
  })
}

function openConnectionPicker(portID?: string) {
  if (!selectedNode.value) return
  let targetPortID = portID || ''
  if (!targetPortID) {
    commitGraph((target) => {
      const node = target.nodes.find((item) => item.id === selectedNodeID.value)
      if (node) ensureVideoWorkflowNodePorts(node)
    }, { history: false })
    targetPortID = videoWorkflowNodeDefaultInputPort(selectedNode.value)?.id || ''
  }
  if (!targetPortID) return ElMessage.info('该节点没有可连接的上游输入端口')
  connectionTarget.value = { nodeID: selectedNode.value.id, portID: targetPortID }
  connectionSource.value = selectedCompatibleSources.value[0]?.value || ''
  if (!selectedCompatibleSources.value.length) {
    return ElMessage.warning('画布上没有兼容的上游输出端口，请先添加可输出同类数据的节点')
  }
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

function openValidationIssues(issues: VideoWorkflowValidationIssue[]) {
  validationIssues.value = issues
  validationIssuesVisible.value = true
  ElMessage.warning(`发现 ${issues.length} 个问题`)
}

function selectValidationIssue(issue: VideoWorkflowValidationIssue) {
  if (issue.node_id) selectNode(issue.node_id)
  validationIssuesVisible.value = false
}

async function startRun(
  mode: VideoWorkflowRunMode = 'full',
  nodeID?: string,
  deferredMutation?: (target: VideoWorkflowGraph) => void,
) {
  if (runningAction.value) return
  if (!requireActiveWorkflow('运行生成')) return
  const workflow = activeWorkflow.value
  if (!workflow) return
  const startNodeID = mode === 'full' ? undefined : (nodeID || selectedNodeID.value)
  if (!startNodeID && mode !== 'full') return ElMessage.warning('请先选择起始节点')
  const previousRunForPreview = activeRun.value
  runningAction.value = true
  try {
    if (dirty.value && !await flushSave()) return ElMessage.error('画布尚未保存，已阻止运行旧修订')
    const localIssues = validateVideoWorkflowGraph(graph.value, { requireComplete: mode === 'full' })
    if (localIssues.length) {
      openValidationIssues(localIssues)
      if (localIssues[0]?.node_id) selectNode(localIssues[0].node_id)
      return
    }
    const validation = await validateVideoWorkflow(workflow.id, graph.value)
    if (!validation.valid) {
      const issues = validation.issues?.length
        ? validation.issues
        : [{ code: 'invalid_graph', message: '画布校验未通过' }]
      openValidationIssues(issues)
      if (issues[0]?.node_id) selectNode(issues[0].node_id)
      return
    }
    let target = { revision: workflow.revision, run_mode: mode, ...(startNodeID ? { start_node_id: startNodeID } : {}) }
    const estimate = await estimateVideoWorkflowRun(workflow.id, target)
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
      rememberRunDetailForPreview(previousRunForPreview)
      activeRun.value = run
    } else {
      await confirmEstimate(estimate)
      rememberRunDetailForPreview(previousRunForPreview)
      activeRun.value = await submit(estimate)
    }
    // 记录运行启动时的 stale 世代：之后由编辑产生的 stale 不会被本轮轮询清掉
    activeRunStaleEpoch = staleEpoch
    rememberRun(workflow.id, activeRun.value.id)
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
    refreshRunFailCount = 0
    refreshRunFailWarned = false
    // 仅在 revision 对齐且节点已成功跑完、且 stale 标记早于本次运行启动时清除，避免轮询抹掉运行后编辑产生的合法 stale
    if (!dirty.value && activeRunMatchesWorkflow.value) {
      for (const nodeRun of detail.node_runs || []) {
        if (nodeRun.status !== 'succeeded') continue
        const node = graph.value.nodes.find((item) => item.id === nodeRun.node_id)
        if (node?.status !== 'stale') continue
        const markedAt = nodeStaleEpoch.get(node.id)
        if (markedAt === undefined || markedAt > activeRunStaleEpoch) continue
        node.status = undefined
        node.stale_reason = undefined
        nodeStaleEpoch.delete(node.id)
      }
    }
    const missingGeneratedImage = (detail.node_runs || []).some((nodeRun) => {
      if (!['background', 'character'].includes(nodeRun.node_type || '')) return false
      const versionID = nodeRun.output_version_id || String(nodeRun.output?.selected_version_id || '')
      return Boolean(versionID) && !assets.value.some((asset) => asset.versions?.some((version) => version.id === versionID))
    })
    if (missingGeneratedImage) {
      try { await refreshAssets({ silent: true }) } catch { /* 运行详情仍可继续刷新。 */ }
      if (componentUnmounted || generation !== pollingGeneration || activeWorkflow.value?.id !== workflowID || activeRun.value?.id !== runID) return
    }
    upsertRunHistory(activeRun.value)
    syncFlow()
    if (activeRun.value.status === 'awaiting_character_approval') openCharacterApproval()
    if (activeRun.value.status === 'awaiting_storyboard_approval') openStoryboardApproval()
    if (!isRunActive.value) stopPolling()
  } catch {
    refreshRunFailCount += 1
    if (refreshRunFailCount >= 3 && !refreshRunFailWarned) {
      refreshRunFailWarned = true
      ElMessage.warning('运行状态刷新失败，正在重试')
    }
  }
}

function openCharacterApproval() {
  for (const role of characterCandidates.value) if (!characterSelections.value[role.nodeID] && role.candidates[0]) characterSelections.value[role.nodeID] = role.candidates[0].id
  characterDialogVisible.value = true
}
function continueApproval() {
  if (activeRun.value?.status === 'awaiting_character_approval') openCharacterApproval()
  else if (activeRun.value?.status === 'awaiting_storyboard_approval') openStoryboardApproval()
}
async function approveCharacters() {
  if (!activeRun.value || !canApproveCharacters.value || approvalBusy.value) return
  approvalBusy.value = true
  try {
    await approveVideoWorkflowCharacters(activeRun.value.id, { selections: characterCandidates.value.map((item) => ({ node_run_id: item.nodeRunID, input_hash: item.hash, selected_version_id: characterSelections.value[item.nodeID] })) })
    characterDialogVisible.value = false
    await refreshRun(); startPolling()
  } catch (error: any) {
    ElMessage.error(error?.message || '角色审批提交失败')
  } finally {
    approvalBusy.value = false
  }
}
function openStoryboardApproval() {
  const nodeRun = activeRun.value?.node_runs?.find((item) => item.node_type === 'script' || graph.value.nodes.find((node) => node.id === item.node_id)?.type === 'script')
  storyboardJSON.value = JSON.stringify(nodeRun?.output?.script || nodeRun?.output?.storyboard || nodeRun?.output || {}, null, 2)
  storyboardDialogVisible.value = true
}
async function approveStoryboard() {
  if (!activeRun.value || !canApproveStoryboard.value || approvalBusy.value) return
  approvalBusy.value = true
  try {
    const nodeRun = activeRun.value.node_runs?.find((item) => item.node_type === 'script' || graph.value.nodes.find((node) => node.id === item.node_id)?.type === 'script')
    await approveVideoWorkflowStoryboard(activeRun.value.id, { node_run_id: nodeRun?.id || '', input_hash: nodeRun?.input_hash || '', script: JSON.parse(storyboardJSON.value) })
    storyboardDialogVisible.value = false
    await refreshRun(); startPolling()
  } catch (error: any) {
    ElMessage.error(error?.message || '分镜审批提交失败')
  } finally {
    approvalBusy.value = false
  }
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
  const assetID = run?.output?.asset_id
  const versionID = run?.output_version_id || run?.output_asset_version_id
  if (assetID && versionID) {
    try {
      // 成片 output_url/preview_url 为封面 JPEG；新窗口播放需 seedance 原片。
      const signed = await signVideoAssetVersion(assetID, versionID, 'seedance')
      navigatePreview(signed.url)
    } catch {
      previewWindow?.close()
      ElMessage.error('预览链接签发失败')
    }
    return
  }
  const directURL = run?.output_url
  if (directURL && !String(directURL).includes('purpose=preview')) {
    navigatePreview(String(directURL))
    return
  }
  const compose = allowCanvasFallback ? graph.value.nodes.find((node) => node.type === 'compose') : null
  const url = compose?.output?.url || compose?.output?.output_url
  if (url) { navigatePreview(String(url)); return }
  previewWindow?.close()
  selectNode(compose?.id || 'compose')
  ElMessage.info('最终成片生成后可在此预览')
}

function previewOutput() {
  if (!requireActiveWorkflow('预览成片')) return
  void previewRunOutput(activeRun.value, createPreviewWindow(), true)
}

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
  if (mediaPreviewVisible.value) return
  const target = event.target as HTMLElement | null
  if (target?.matches('input, textarea, select, [contenteditable="true"]') || target?.closest('.el-dialog, .el-message-box')) return
  const mod = event.metaKey || event.ctrlKey
  if (mod && event.key.toLowerCase() === 's') { event.preventDefault(); void saveNow(); return }
  if (mod && event.key.toLowerCase() === 'z') { event.preventDefault(); event.shiftKey ? redo() : undo(); return }
  if (mod && event.key.toLowerCase() === 'y') { event.preventDefault(); redo(); return }
  if (mod && event.key.toLowerCase() === 'c') { event.preventDefault(); copySelectedNodes(); return }
  if (mod && event.key.toLowerCase() === 'v') { event.preventDefault(); pasteNodes(); return }
  if (mod && event.key.toLowerCase() === 'd') { event.preventDefault(); duplicateSelectedNodes(); return }
  if (mod && event.key.toLowerCase() === 'a') { event.preventDefault(); selectedNodeIDs.value = graph.value.nodes.map((node) => node.id); selectedNodeID.value = selectedNodeIDs.value.at(-1) || ''; syncFlow(); return }
  if (mod && event.key === 'Enter') { event.preventDefault(); void startRun('node_only'); return }
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
  if (event.key === '\\') {
    event.preventDefault()
    toggleWorkspaceFocusMode()
    return
  }
  if (event.key === '[') {
    event.preventDefault()
    setLibraryOpen(!panelLayout.libraryOpen)
    return
  }
  if (event.key === ']') {
    event.preventDefault()
    setInspectorOpen(!panelLayout.inspectorOpen)
    return
  }
  if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key) && selectedNodeIDs.value.length) {
    event.preventDefault()
    const amount = event.shiftKey ? 10 : 1
    commitGraph((targetGraph) => {
      targetGraph.nodes.filter((node) => selectedNodeIDs.value.includes(node.id) && !node.locked).forEach((node) => {
        if (event.key === 'ArrowLeft') node.position.x -= amount
        if (event.key === 'ArrowRight') node.position.x += amount
        if (event.key === 'ArrowUp') node.position.y -= amount
        if (event.key === 'ArrowDown') node.position.y += amount
        node.position_mode = 'manual'
      })
      clearAutoEdgeRoutes(targetGraph)
      updateVideoWorkflowGroupBounds(targetGraph)
    })
  }
}

function startResize(kind: 'left' | 'right', event: PointerEvent) {
  resizing = { kind, start: event.clientX, value: panelLayout[kind] }
  window.addEventListener('pointermove', resizePanel)
  window.addEventListener('pointerup', stopResize, { once: true })
  event.preventDefault()
}
function resizePanel(event: PointerEvent) {
  if (!resizing) return
  const delta = event.clientX - resizing.start
  if (resizing.kind === 'left') panelLayout.left = clampPanelLeft(resizing.value + delta)
  if (resizing.kind === 'right') panelLayout.right = clampPanelRight(resizing.value - delta)
}
function stopResize() {
  resizing = null
  window.removeEventListener('pointermove', resizePanel)
  persistPanelLayout()
}

function persistPanelLayout() {
  localStorage.setItem(LAYOUT_STORAGE_KEY, JSON.stringify({ ...panelLayout, edgeDisplayMode: edgeDisplayMode.value, autoArrange: autoArrange.value }))
}

function setLibraryOpen(open: boolean) {
  panelLayout.libraryOpen = open
  persistPanelLayout()
}

function setInspectorOpen(open: boolean) {
  panelLayout.inspectorOpen = open
  persistPanelLayout()
}

function toggleWorkspaceFocusMode() {
  const result = togglePanelFocusMode(
    { libraryOpen: panelLayout.libraryOpen, inspectorOpen: panelLayout.inspectorOpen },
    lastNonFocusLayout.value,
  )
  panelLayout.libraryOpen = result.next.libraryOpen
  panelLayout.inspectorOpen = result.next.inspectorOpen
  lastNonFocusLayout.value = result.lastNonFocus
  persistPanelLayout()
}

function applyWorkspaceLayoutPreset(presetID: VideoWorkflowLayoutPresetID) {
  const next = applyLayoutPreset({ ...panelLayout }, presetID)
  panelLayout.left = next.left
  panelLayout.right = next.right
  panelLayout.libraryOpen = next.libraryOpen
  panelLayout.inspectorOpen = next.inspectorOpen
  if (next.libraryOpen || next.inspectorOpen) {
    lastNonFocusLayout.value = { libraryOpen: next.libraryOpen, inspectorOpen: next.inspectorOpen }
  }
  persistPanelLayout()
}

function restoreLayout() {
  try {
    const stored = JSON.parse(localStorage.getItem(LAYOUT_STORAGE_KEY) || '{}')
    if (Number.isFinite(stored.left)) panelLayout.left = clampPanelLeft(stored.left)
    if (Number.isFinite(stored.right)) panelLayout.right = clampPanelRight(stored.right)
    if (typeof stored.libraryOpen === 'boolean') panelLayout.libraryOpen = stored.libraryOpen
    if (typeof stored.inspectorOpen === 'boolean') panelLayout.inspectorOpen = stored.inspectorOpen
    if (typeof stored.autoArrange === 'boolean') autoArrange.value = stored.autoArrange
    if (['smart', 'all', 'hidden'].includes(stored.edgeDisplayMode)) edgeDisplayMode.value = stored.edgeDisplayMode
  } catch { /* 使用默认布局 */ }
}
function beforeUnload(event: BeforeUnloadEvent) { persistLocalDraft(); if (dirty.value) { event.preventDefault(); event.returnValue = '' } }

watch(() => activeRun.value?.status, (status) => {
  if (status === 'awaiting_character_approval') openCharacterApproval()
  if (status === 'awaiting_storyboard_approval') openStoryboardApproval()
})

watch(selectedComposePlaybackSignature, () => {
  void refreshSelectedComposePlaybackURL()
}, { immediate: true })

watch(edgeDisplayMode, () => {
  selectedSummaryEdgeID.value = ''
  syncFlow()
  persistPanelLayout()
})

const graphStructureSignature = computed(() => [
  graph.value.nodes.map((node) => `${node.id}:${node.collapsed ? 1 : 0}`).join('|'),
  graph.value.edges.map((edge) => `${edge.source}.${edge.source_port}>${edge.target}.${edge.target_port}`).join('|'),
].join('#'))

watch(graphStructureSignature, () => {
  if (!autoArrange.value || !autosaveReady.value) return
  scheduleAutoArrange()
})

onMounted(() => {
  restoreLayout()
  headerCompactQuery = window.matchMedia('(max-width: 1023px)')
  headerPhoneQuery = window.matchMedia('(max-width: 767px)')
  syncHeaderLayoutMode()
  headerCompactQuery.addEventListener('change', syncHeaderLayoutMode)
  headerPhoneQuery.addEventListener('change', syncHeaderLayoutMode)
  window.addEventListener('keydown', handleCanvasKeydown)
  window.addEventListener('beforeunload', beforeUnload)
  void bootstrap()
})

onBeforeUnmount(() => {
  componentUnmounted = true
  invalidateImageTransformContext()
  workspaceGeneration += 1
  closeMediaPreview()
  stopPolling()
  persistLocalDraft()
  headerCompactQuery?.removeEventListener('change', syncHeaderLayoutMode)
  headerPhoneQuery?.removeEventListener('change', syncHeaderLayoutMode)
  if (saveTimer) window.clearTimeout(saveTimer)
  if (retryTimer) window.clearTimeout(retryTimer)
  if (deletionTimer) window.clearTimeout(deletionTimer)
  if (autoArrangeTimer) window.clearTimeout(autoArrangeTimer)
  window.removeEventListener('keydown', handleCanvasKeydown)
  window.removeEventListener('beforeunload', beforeUnload)
  window.removeEventListener('pointermove', resizePanel)
})
</script>

<template>
  <div class="video-workflow-page" :style="workspaceStyle" v-loading="loading">
      <header class="workspace-header">
        <div class="header-start">
          <div class="brand-block" title="灵境智创"><span class="brand-mark"><Aim /></span><strong>灵境智创</strong></div>
          <button class="back-button" title="返回个人中心" @click="router.push('/personal/dashboard')"><ArrowLeft /></button>
          <div class="workflow-name">
            <el-select v-if="workflows.length" :model-value="activeWorkflow?.id || ''" filterable @change="selectWorkflow">
              <el-option v-for="item in workflows" :key="item.id" :label="item.name" :value="item.id" />
            </el-select>
            <strong v-else-if="activeWorkflow">{{ activeWorkflow.name }}</strong>
            <strong v-else class="workflow-placeholder">未创建工作流</strong>
            <button class="icon-button rename-button" type="button" title="重命名工作流" :disabled="!activeWorkflow" @click="renameActiveWorkflow"><EditPen /></button>
          </div>
          <div class="save-controls" role="group" :aria-label="`保存状态：${saveState}`">
            <div
              class="save-state"
              :class="[workspaceLifecycle, { offline: !backendAvailable }]"
              :title="`${saveState} · ${workspaceLifecycleHint}`"
              aria-live="polite"
            >
              <i />
              <span>{{ saveStateLabel }}</span>
              <button
                v-if="activeWorkflow"
                type="button"
                class="revision-badge"
                :class="{ previewing: viewingRevision != null && viewingRevision !== activeWorkflow.revision }"
                :title="revisionBadgeTitle"
                :aria-label="revisionBadgeTitle"
                @click="openRevisionHistory"
              >{{ revisionBadgeLabel }}</button>
            </div>
            <button
              class="save-button"
              type="button"
              :class="{ pending: saveButtonPending, busy: saving }"
              :disabled="!canManualSave"
              :title="saveButtonTitle"
              :aria-label="saveButtonTitle"
              @click="saveNow"
            >{{ saving ? '保存中' : '保存' }}</button>
          </div>
        </div>

        <div
          v-if="showHeaderRunProgress"
          class="header-center"
          :class="{ compact: headerPhone }"
        >
          <div
            class="header-run-progress"
            :class="{ indeterminate: headerRunStats.indeterminate }"
            role="progressbar"
            :aria-valuenow="headerRunStats.indeterminate ? undefined : headerRunStats.progress"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="headerRunStats.ariaLabel"
            :title="headerRunStats.ariaLabel"
            aria-live="polite"
          >
            <span v-if="!headerPhone" class="header-run-label">{{ headerRunStats.label }}</span>
            <i class="header-run-track" aria-hidden="true">
              <em :style="headerRunStats.indeterminate ? undefined : { width: `${headerRunStats.progress}%` }" />
            </i>
            <b class="header-run-pct">{{ headerRunStats.progress }}%</b>
            <span class="header-run-nodes">{{ headerRunStats.nodeText }}</span>
          </div>
        </div>

        <div class="header-actions">
          <el-dropdown
            v-if="!headerCompact"
            class="output-settings"
            trigger="click"
            @command="(command: OutputSettingCommand) => applyOutputSetting(command)"
          >
            <button class="output-settings-button" type="button" aria-label="输出设置">
              <span>{{ outputSettingsLabel }}</span>
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>画幅</el-dropdown-item>
                <el-dropdown-item command="aspect_9_16" :class="{ 'is-active': graph.settings.aspect_ratio === '9:16' }">9:16 竖屏</el-dropdown-item>
                <el-dropdown-item command="aspect_16_9" :class="{ 'is-active': graph.settings.aspect_ratio === '16:9' }">16:9 横屏</el-dropdown-item>
                <el-dropdown-item command="aspect_1_1" :class="{ 'is-active': graph.settings.aspect_ratio === '1:1' }">1:1 方形</el-dropdown-item>
                <el-dropdown-item divided disabled>分辨率</el-dropdown-item>
                <el-dropdown-item command="resolution_720p" :class="{ 'is-active': graph.settings.resolution === '720p' }">720p</el-dropdown-item>
                <el-dropdown-item command="resolution_1080p" :class="{ 'is-active': graph.settings.resolution === '1080p' }">1080p</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>

          <button
            v-if="!headerCompact"
            class="runtime-settings-button"
            type="button"
            :title="runtimeSettingsLoading ? '并发设置加载中' : '并发控制'"
            aria-label="并发控制"
            :aria-busy="runtimeSettingsLoading"
            @click="openRuntimeSettings"
          ><Setting /><span>{{ runtimeSettingsLabel }}</span></button>

          <button
            v-if="!headerCompact"
            class="preview-button"
            type="button"
            :class="hasPreviewOutput ? 'is-ready' : 'is-empty'"
            :title="previewStatusLabel"
            :aria-label="previewStatusLabel"
            @click="previewOutput"
          ><VideoPlay />预览</button>

          <div class="header-primary-group">
            <el-dropdown
              v-if="!isRunActive"
              class="generate-button"
              split-button
              type="primary"
              trigger="click"
              :disabled="runningAction || !canOperateWorkflow"
              :button-props="{ loading: runningAction }"
              :title="canOperateWorkflow ? undefined : '请先从模板创建工作流'"
              @click="startRun('node_only')"
              @command="(command: VideoWorkflowRunMode) => startRun(command)"
            >
              运行此节点
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="node_only">运行当前节点</el-dropdown-item>
                <el-dropdown-item command="upstream">连带上游重跑</el-dropdown-item>
                <el-dropdown-item command="downstream">运行当前及下游</el-dropdown-item>
                <el-dropdown-item divided command="full">完整运行</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
            <button v-else class="stop-button" type="button" :disabled="runningAction" @click="stopRun"><VideoPause />停止</button>
          </div>

          <div class="header-util-group">
            <el-dropdown
              class="workspace-actions"
              trigger="click"
              :disabled="jsonTransferBusy"
              @command="(command: WorkspaceMenuCommand) => handleWorkspaceMenu(command)"
            >
              <button class="icon-button" title="更多操作" aria-label="更多操作" :aria-busy="jsonTransferBusy"><MoreFilled /></button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="!headerCompact" command="history" class="workspace-menu-item"><Clock />生成历史<span v-if="runHistoryTotal" class="menu-badge">{{ runHistoryTotal }}</span></el-dropdown-item>
                  <template v-if="headerCompact">
                    <el-dropdown-item command="preview" class="workspace-menu-item"><VideoPlay />预览</el-dropdown-item>
                    <el-dropdown-item command="history" class="workspace-menu-item"><Clock />生成历史</el-dropdown-item>
                    <el-dropdown-item command="aspect_9_16" class="workspace-menu-item">画幅 9:16</el-dropdown-item>
                    <el-dropdown-item command="aspect_16_9" class="workspace-menu-item">画幅 16:9</el-dropdown-item>
                    <el-dropdown-item command="aspect_1_1" class="workspace-menu-item">画幅 1:1</el-dropdown-item>
                    <el-dropdown-item command="resolution_720p" class="workspace-menu-item">分辨率 720p</el-dropdown-item>
                    <el-dropdown-item command="resolution_1080p" class="workspace-menu-item">分辨率 1080p</el-dropdown-item>
                    <el-dropdown-item v-if="headerPhone" command="create_workflow" class="workspace-menu-item" divided><Plus />新建工作流</el-dropdown-item>
                  </template>
                  <el-dropdown-item command="runtime_settings" divided><span class="workspace-menu-item"><Setting />并发控制</span></el-dropdown-item>
                  <el-dropdown-item command="outline"><span class="workspace-menu-item"><Menu />结构大纲</span></el-dropdown-item>
                  <el-dropdown-item command="layout_edit" divided><span class="workspace-menu-item">布局 · 编辑</span></el-dropdown-item>
                  <el-dropdown-item command="layout_compose"><span class="workspace-menu-item">布局 · 构图</span></el-dropdown-item>
                  <el-dropdown-item command="layout_review"><span class="workspace-menu-item">布局 · 审片</span></el-dropdown-item>
                  <el-dropdown-item command="import_json" divided :disabled="!activeWorkflow || isRunActive || runningAction || revisionConflict">
                    <span class="workspace-menu-item"><Upload />导入 JSON</span>
                  </el-dropdown-item>
                  <el-dropdown-item command="export_json" :disabled="!activeWorkflow">
                    <span class="workspace-menu-item"><Download />导出 JSON</span>
                  </el-dropdown-item>
                  <el-dropdown-item command="delete_workflow" divided :disabled="!activeWorkflow || isRunActive || runningAction || revisionConflict">
                    <span class="workspace-menu-item danger">删除工作流</span>
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <input ref="jsonFileInput" class="json-file-input" type="file" accept=".json,application/json" @change="importWorkflowJSON" />
            <button v-if="!headerPhone" class="icon-button" title="新建工作流" @click="createDialogVisible = true"><Plus /></button>
          </div>
        </div>
      </header>

      <div v-if="workspaceLifecycle === 'unbound'" class="workspace-lifecycle-banner" role="status">
        <span>空白预览画布：尚未从模板创建工作流，当前内容不会保存，也无法生成成片。</span>
        <button type="button" @click="openCreateWorkflow">从模板创建</button>
      </div>

      <div
        v-if="workspaceContextBar"
        class="workspace-context-bar"
        :class="`is-${workspaceContextBar.kind}`"
        :role="workspaceContextBar.kind === 'conflict' || workspaceContextBar.kind === 'draft' ? 'alert' : 'status'"
      >
        <div class="workspace-context-bar-main">
          <i class="workspace-context-dot" />
          <span>{{ workspaceContextBar.text }}</span>
          <b v-if="workspaceContextBar.progress !== undefined">{{ workspaceContextBar.progress }}%</b>
        </div>
        <div class="workspace-context-bar-actions">
          <button
            v-if="workspaceContextBar.showLoadServer"
            type="button"
            class="context-bar-button"
            @click="reloadWorkflow"
          >加载服务器版本</button>
          <button
            v-if="workspaceContextBar.showRecoverDraft"
            type="button"
            class="context-bar-button context-bar-button--ghost"
            @click="recoverConflictDraft"
          >恢复草稿</button>
          <button
            v-if="workspaceContextBar.showContinueApproval"
            type="button"
            class="context-bar-button"
            :disabled="approvalBusy"
            @click="continueApproval"
          >继续审批</button>
        </div>
      </div>

      <div class="workspace-grid">
        <VideoWorkflowLibrary
          v-show="panelLayout.libraryOpen"
          :tab="panelTab"
          :assets="assets"
          @update:tab="panelTab = $event"
          @add-node="addNode"
          @upload="uploadLibraryAsset"
          @select-asset="selectAsset"
          @close="setLibraryOpen(false)"
        />
        <button v-if="panelLayout.libraryOpen" class="panel-resizer left-resizer" aria-label="调整左栏宽度" @pointerdown="startResize('left', $event)" />

        <VideoWorkflowCanvas
          v-model:active-tool="activeTool"
          v-model:nodes="flowNodes"
          v-model:edges="flowEdges"
          v-model:edge-display-mode="edgeDisplayMode"
          :backend-available="backendAvailable"
          :mod-key-code="modKeyCode"
          :alignment-guides="alignmentGuides"
          :quick-connect-menu="quickConnectMenu"
          :quick-connect-types="quickConnectTypes"
          :graph-nodes="graph.nodes"
          :selected-node-i-ds="selectedNodeIDs"
          :selected-node-locked="Boolean(selectedNode?.locked)"
          :layout-busy="layoutBusy"
          :auto-arrange="autoArrange"
          :can-undo="Boolean(undoStack.length)"
          :can-redo="Boolean(redoStack.length)"
          :can-operate-workflow="canOperateWorkflow"
          :zoom="canvasViewport.zoom"
          :viewport="canvasViewport"
          @drop-node="dropNode"
          @auto-layout="autoLayout"
          @toggle-auto-arrange="toggleAutoArrange"
          @undo="undo"
          @redo="redo"
          @align-selected="alignSelected"
          @distribute-selected="distributeSelected"
          @toggle-enabled="toggleEnabled"
          @toggle-lock="toggleLock"
          @toggle-collapse="toggleCollapse"
          @delete-selected="selectedEdgeIDs.length ? removeSelectedEdges() : deleteSelectedNodes()"
          @nodes-change="onNodeChanges"
          @edges-change="onEdgeChanges"
          @node-click="selectNode"
          @node-drag="onNodeDrag"
          @node-drag-stop="onNodeDragStop"
          @pane-click="clearSelection"
          @connect="onConnect"
          @connect-start="onConnectStart"
          @connect-end="onConnectEnd"
          @edge-update="onEdgeUpdate"
          @edge-click="selectEdge"
          @edge-curve-change-start="onEdgeCurveChangeStart"
          @edge-curve-change="onEdgeCurveChange"
          @edge-curve-change-end="onEdgeCurveChangeEnd"
          @viewport-change="updateCanvasViewport"
          @minimap-navigate="navigateFromMinimap"
          @preview-media="openMediaPreview"
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
          :upstreams="selectedNodeUpstreams"
          :latest-run="selectedNodeRun"
          :history="selectedNodeHistory"
          :history-loading="nodeHistoryLoading"
          :model-value="selectedNodeModel"
          :model-options="selectedNodeModelOptions"
          :revision="activeWorkflow?.revision || 0"
          :running="isRunActive"
          :compose-playback-url="selectedComposePlaybackURL"
          :compose-playback-loading="selectedComposePlaybackLoading"
          :timeline-source-titles="timelineSourceTitles"
          @update-title="updateNodeTitle"
          @update-config="updateNodeConfig"
          @update-model="updateNodeModel"
          @update-timeline-clips="updateTimelineClips"
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
          @preview-media="openMediaPreview"
          @download-output="downloadOutput"
          @request-history="loadSelectedNodeHistory"
          @open-history="openRunHistory"
          @close="setInspectorOpen(false)"
        />

        <button
          v-if="!panelLayout.libraryOpen"
          class="panel-restore library-restore"
          title="打开节点侧边栏"
          aria-label="打开节点侧边栏"
          aria-controls="node-library"
          @click="setLibraryOpen(true)"
        ><ArrowRight /><span>节点库</span></button>
        <button
          v-if="!panelLayout.inspectorOpen"
          class="panel-restore inspector-restore"
          title="打开节点详情"
          aria-label="打开节点详情"
          aria-controls="node-inspector"
          @click="setInspectorOpen(true)"
        ><ArrowLeft /><span>节点详情</span></button>
      </div>

      <transition name="toast"><div v-if="deletionToast" class="undo-toast" role="status">已删除 {{ deletionToast.count }} 个节点<button @click="restoreDeletedNodes">撤销</button><span>5秒</span></div></transition>
      <div class="save-live" aria-live="polite">{{ saveState }}</div>

      <VideoWorkflowOutline v-model="outlineVisible" :nodes="outlineNodes" :edge-count="graph.edges.length" :clip-count="timelineClips.length" @select="selectNode" />
      <VideoWorkflowValidationIssues
        v-model="validationIssuesVisible"
        :issues="validationIssues"
        @select="selectValidationIssue"
      />

      <VideoWorkflowMediaPreview
        :model-value="mediaPreviewVisible"
        :node="mediaPreviewNode"
        :src="mediaPreviewURL"
        :loading="mediaPreviewLoading"
        :error="mediaPreviewError"
        @update:model-value="updateMediaPreviewVisible"
        @retry="retryMediaPreview"
      />

      <VideoWorkflowRunHistory
        v-model="runHistoryVisible"
        :runs="runHistoryItems"
        :total="runHistoryTotal"
        :loading="runHistoryLoading"
        :action-run-i-d="runHistoryActionID"
        :current-run-i-d="activeRun?.id || ''"
        :detail="runHistoryDetail"
        @refresh="loadRunHistory(true)"
        @load-more="loadRunHistory(false)"
        @inspect="inspectHistoryRun"
        @expand="expandHistoryRun"
        @switch="switchToHistoryRun"
        @back="runHistoryDetail = null"
        @preview="previewHistoryOutput"
        @download="downloadHistoryOutput"
        @generate="generateFromHistory"
      />

      <VideoWorkflowRevisionHistory
        v-model="revisionHistoryVisible"
        :items="revisionHistoryItems"
        :total="revisionHistoryTotal"
        :loading="revisionHistoryLoading"
        :action-revision="revisionHistoryAction"
        :viewing-revision="viewingRevision"
        :current-revision="activeWorkflow?.revision || 0"
        @refresh="loadRevisionHistory(true)"
        @load-more="loadRevisionHistory(false)"
        @switch="switchToRevision"
      />

      <el-dialog v-model="connectionDialogVisible" title="添加连接" width="460px">
        <el-form label-position="top"><el-form-item label="兼容输出端口"><el-select v-model="connectionSource" style="width: 100%"><el-option v-for="source in selectedCompatibleSources" :key="source.value" :label="source.label" :value="source.value" /></el-select></el-form-item></el-form>
        <template #footer><el-button @click="connectionDialogVisible = false">取消</el-button><el-button type="primary" :disabled="!connectionSource" @click="addKeyboardConnection">添加连接</el-button></template>
      </el-dialog>

      <el-dialog v-model="runtimeSettingsVisible" title="并发控制" width="min(520px, calc(100vw - 32px))">
        <div class="runtime-settings-grid">
          <label v-for="field in RUNTIME_SETTING_FIELDS" :key="field.key" class="runtime-setting-field">
            <span>{{ field.label }}</span>
            <el-input-number
              v-model="runtimeSettingsDraft[field.key]"
              :min="field.min"
              :max="field.max"
              :step="1"
              :precision="0"
              controls-position="right"
              size="small"
            />
          </label>
        </div>
        <template #footer>
          <el-button @click="runtimeSettingsVisible = false">取消</el-button>
          <el-button type="primary" :loading="runtimeSettingsSaving" @click="saveRuntimeSettings">保存</el-button>
        </template>
      </el-dialog>

      <el-dialog v-model="createDialogVisible" title="创建视频工作流" width="min(520px, calc(100vw - 32px))">
        <el-form label-position="top" class="create-workflow-form">
          <el-form-item label="工作流名称">
            <el-input v-model="newWorkflowName" maxlength="40" />
          </el-form-item>
          <el-form-item label="基础模板" class="template-form-item">
            <el-radio-group v-model="selectedTemplateID" class="template-list">
              <el-radio v-for="item in templates" :key="item.id" :value="item.id" class="template-option" border>
                <b>{{ item.name }}</b>
                <span>{{ item.description || '图片生成 · 15秒视频 · 单轨成片' }}</span>
              </el-radio>
            </el-radio-group>
            <div v-if="!templates.length" class="dialog-empty">服务端尚未提供可用模板</div>
          </el-form-item>
        </el-form>
        <template #footer><el-button @click="createDialogVisible = false">取消</el-button><el-button type="primary" :loading="creating" :disabled="!selectedTemplateID" @click="createFromTemplate">创建</el-button></template>
      </el-dialog>

      <el-dialog v-model="characterDialogVisible" title="选定角色定妆" width="780px" :close-on-click-modal="false">
        <div class="candidate-roles"><section v-for="role in characterCandidates" :key="role.nodeID"><header><b>{{ role.title }}</b><span>选择 1 张作为角色版本</span></header><div><button v-for="candidate in role.candidates" :key="candidate.id" :class="{ selected: characterSelections[role.nodeID] === candidate.id }" @click="characterSelections[role.nodeID] = candidate.id"><img v-if="candidate.url" :src="candidate.url" :alt="role.title" /><span v-else>候选 {{ candidate.id.slice(-4) }}</span><Check v-if="characterSelections[role.nodeID] === candidate.id" /></button></div></section></div>
        <template #footer><el-button type="primary" :loading="approvalBusy" :disabled="!canApproveCharacters || approvalBusy" @click="approveCharacters">确认角色并继续</el-button></template>
      </el-dialog>

      <el-dialog v-model="storyboardDialogVisible" title="确认分镜剧本" width="760px" :close-on-click-modal="false"><el-input v-model="storyboardJSON" type="textarea" :rows="20" resize="none" class="storyboard-editor" /><template #footer><span v-if="!canApproveStoryboard" class="json-error">JSON 格式错误</span><el-button type="primary" :loading="approvalBusy" :disabled="!canApproveStoryboard || approvalBusy" @click="approveStoryboard">确认剧本并生成场景</el-button></template></el-dialog>
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
.workspace-header {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 8px;
  box-sizing: border-box;
  padding: 0 12px;
  background: #fff;
  border-bottom: 1px solid var(--border);
}
.header-start {
  min-width: 0;
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: 6px;
}
.header-center {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 0;
}
.header-center.compact {
  max-width: 42vw;
}
.header-run-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: #475569;
  font-size: 11px;
  white-space: nowrap;
}
.header-run-label {
  flex: 0 0 auto;
  max-width: 64px;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 600;
  color: #334155;
}
.header-run-track {
  position: relative;
  width: 84px;
  height: 4px;
  flex: 0 0 auto;
  display: block;
  overflow: hidden;
  background: #e2e8f0;
  border-radius: 999px;
}
.header-run-track em {
  position: absolute;
  inset: 0 auto 0 0;
  background: #2563eb;
  border-radius: inherit;
  transition: width .2s ease;
}
.header-run-progress.indeterminate .header-run-track em {
  width: 36% !important;
  animation: header-run-pulse 1.1s ease-in-out infinite;
}
.header-run-pct {
  flex: 0 0 auto;
  min-width: 28px;
  color: #2563eb;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.header-run-nodes {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #64748b;
  font-variant-numeric: tabular-nums;
}
@keyframes header-run-pulse {
  0% { transform: translateX(-120%); }
  100% { transform: translateX(280%); }
}
.header-center.compact .header-run-progress { gap: 6px; }
.header-center.compact .header-run-track { width: 56px; }
.header-actions {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
}
.header-primary-group,
.header-util-group {
  display: flex;
  align-items: center;
  gap: 6px;
}
.header-primary-group {
  flex: 0 0 auto;
}
.brand-block { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; }.brand-block strong { color: #64748b; font-size: 13px; font-weight: 600; letter-spacing: -.2px; white-space: nowrap; }.brand-mark { width: 24px; height: 24px; display: grid; place-items: center; color: #fff; background: #2563eb; clip-path: polygon(50% 0, 100% 100%, 50% 75%, 0 100%); }.brand-mark svg { width: 14px; }
.back-button, .icon-button { width: 30px; height: 30px; display: grid; place-items: center; color: #475569; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; flex: 0 0 auto; }.back-button svg, .icon-button svg { width: 14px; }.icon-button:disabled { opacity: .35; cursor: default; }.back-button:hover, .icon-button:not(:disabled):hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }
.workspace-actions { width: 30px; height: 30px; }.workspace-actions .icon-button[aria-busy="true"] { color: #2563eb; background: #eff6ff; }.workspace-menu-item { min-width: 104px; display: flex; align-items: center; gap: 8px; }.workspace-menu-item svg { width: 14px; color: #64748b; }.workspace-menu-item.danger { color: #dc2626; }.menu-badge { min-width: 16px; margin-left: auto; padding: 1px 5px; color: #1d4ed8; background: #dbeafe; border-radius: 999px; font-size: 10px; text-align: center; }.json-file-input { display: none; }
.workflow-name { min-width: 0; flex: 1 1 160px; max-width: 280px; display: flex; align-items: center; gap: 4px; }.workflow-name strong { overflow: hidden; font-size: 15px; text-overflow: ellipsis; white-space: nowrap; }.workflow-name .workflow-placeholder { color: #94a3b8; font-weight: 500; }.workflow-name .rename-button { width: 26px; height: 26px; flex: 0 0 auto; border: none; background: transparent; color: #64748b; }.workflow-name .rename-button:hover:not(:disabled) { color: #2563eb; background: #eff6ff; }.workflow-name .rename-button svg { width: 13px; }.workflow-name :deep(.el-select) { width: 100%; }.workflow-name :deep(.el-select__wrapper) { box-shadow: none; font-size: 15px; font-weight: 650; }
.save-controls { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; min-width: 0; }
.save-state { display: flex; align-items: center; gap: 6px; color: #475569; white-space: nowrap; font-size: 11px; flex: 0 1 auto; min-width: 0; }.save-state span { overflow: hidden; text-overflow: ellipsis; }.save-state i { width: 8px; height: 8px; background: #22c55e; border-radius: 50%; flex: 0 0 auto; }.save-state.unbound i { background: #94a3b8; }.save-state.ready i { background: #22c55e; }.save-state.dirty i, .save-state.saving i { background: #f59e0b; }.save-state.conflict i, .save-state.offline i { background: #ef4444; }
.revision-badge { height: 22px; padding: 0 7px; color: #1d4ed8; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 999px; cursor: pointer; font-size: 11px; font-weight: 700; font-variant-numeric: tabular-nums; line-height: 1; flex: 0 0 auto; }.revision-badge:hover { color: #fff; background: #2563eb; border-color: #2563eb; }.revision-badge.previewing { color: #0f766e; background: #ccfbf1; border-color: #99f6e4; }
.save-button { height: 28px; padding: 0 10px; color: #334155; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; white-space: nowrap; font-size: 11px; font-weight: 600; flex: 0 0 auto; }.save-button:hover:not(:disabled) { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.save-button.pending { color: #fff; background: #2563eb; border-color: #2563eb; }.save-button.pending:hover:not(:disabled) { color: #fff; background: #1d4ed8; border-color: #1d4ed8; }.save-button.busy, .save-button:disabled { opacity: .55; cursor: default; }
.workspace-lifecycle-banner { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 12px; color: #92400e; background: #fffbeb; border-bottom: 1px solid #fde68a; font-size: 12px; }.workspace-lifecycle-banner button { height: 28px; padding: 0 12px; color: #fff; background: #2563eb; border: 0; border-radius: 5px; cursor: pointer; white-space: nowrap; font-size: 11px; }.workspace-lifecycle-banner button:hover { background: #1d4ed8; }
.workspace-context-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 36px; padding: 6px 12px; border-bottom: 1px solid #e2e8f0; font-size: 12px; }.workspace-context-bar-main { min-width: 0; display: flex; align-items: center; gap: 8px; }.workspace-context-bar-main span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.workspace-context-bar-main b { color: #2563eb; flex: 0 0 auto; }.workspace-context-dot { width: 8px; height: 8px; border-radius: 50%; flex: 0 0 auto; background: #38bdf8; }.workspace-context-bar-actions { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; }.context-bar-button { height: 28px; padding: 0 10px; color: #fff; background: #2563eb; border: 0; border-radius: 5px; cursor: pointer; white-space: nowrap; font-size: 11px; }.context-bar-button:hover:not(:disabled) { background: #1d4ed8; }.context-bar-button:disabled { opacity: .55; cursor: default; }.context-bar-button--ghost { color: #1d4ed8; background: transparent; border: 1px solid #93c5fd; }.context-bar-button--ghost:hover:not(:disabled) { color: #1e40af; background: #eff6ff; }.workspace-context-bar.is-conflict, .workspace-context-bar.is-draft { color: #9a3412; background: #fff7ed; border-bottom-color: #fdba74; }.workspace-context-bar.is-conflict .workspace-context-dot, .workspace-context-bar.is-draft .workspace-context-dot { background: #ea580c; }.workspace-context-bar.is-approval { color: #92400e; background: #fffbeb; border-bottom-color: #fde68a; }.workspace-context-bar.is-approval .workspace-context-dot { background: #f59e0b; }.workspace-context-bar.is-running { color: #1e3a5f; background: #eff6ff; border-bottom-color: #bfdbfe; }
.output-settings-button, .runtime-settings-button, .preview-button, .stop-button { height: 30px; display: flex; align-items: center; justify-content: center; gap: 5px; padding: 0 9px; color: #334155; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; white-space: nowrap; font-size: 11px; }.output-settings-button:hover, .runtime-settings-button:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.runtime-settings-button svg { width: 13px; }.runtime-settings-button[aria-busy="true"] { color: #64748b; background: #f8fafc; }.preview-button.is-empty { color: #64748b; background: #f8fafc; border-color: #e2e8f0; }.preview-button.is-empty:hover { color: #475569; background: #f1f5f9; border-color: #cbd5e1; }.preview-button.is-ready { color: #166534; background: #dcfce7; border-color: #86efac; font-weight: 650; }.preview-button.is-ready:hover { color: #14532d; background: #bbf7d0; border-color: #4ade80; }.preview-button svg, .stop-button svg { width: 14px; }.stop-button { color: #dc2626; }
.output-settings :deep(.el-dropdown-menu__item.is-active) { color: #1d4ed8; font-weight: 650; }
.generate-button { height: 30px; flex: 0 0 auto; }.generate-button :deep(.el-button) { height: 30px; border-radius: 5px; }.generate-button :deep(svg) { width: 12px; }
.workspace-grid { position: relative; height: calc(100% - 56px); display: grid; grid-template-columns: var(--left-panel) minmax(0, 1fr) var(--right-panel); grid-template-areas: "library canvas inspector"; }
.video-workflow-page:has(.workspace-lifecycle-banner) .workspace-grid { height: calc(100% - 56px - 37px); }
.video-workflow-page:has(.workspace-context-bar) .workspace-grid { height: calc(100% - 56px - 37px); }
.video-workflow-page:has(.workspace-lifecycle-banner):has(.workspace-context-bar) .workspace-grid { height: calc(100% - 56px - 74px); }
.workflow-library { grid-area: library; border-right: 1px solid var(--border); }.workflow-inspector { grid-area: inspector; border-left: 1px solid var(--border); }
.panel-resizer { position: absolute; z-index: 20; padding: 0; background: transparent; border: 0; }.left-resizer { left: calc(var(--left-panel) - 3px); top: 0; bottom: 0; width: 6px; cursor: col-resize; }.right-resizer { right: calc(var(--right-panel) - 3px); top: 0; bottom: 0; width: 6px; cursor: col-resize; }.panel-resizer:hover { background: rgba(37, 99, 235, .45); }
.panel-restore { position: absolute; z-index: 21; height: 32px; display: flex; align-items: center; gap: 6px; padding: 0 10px; color: #334155; background: rgba(255, 255, 255, .94); border: 1px solid #cbd5e1; border-radius: 5px; box-shadow: 0 4px 14px rgba(15, 23, 42, .14); cursor: pointer; font-size: 11px; backdrop-filter: blur(8px); }.panel-restore:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.panel-restore svg { width: 13px; }.library-restore { top: 12px; left: 12px; }.inspector-restore { top: 12px; right: 12px; }
.undo-toast { position: fixed; z-index: 120; left: 50%; bottom: 28px; transform: translateX(-50%); display: flex; align-items: center; gap: 12px; padding: 10px 14px; color: #f8fafc; background: #1e293b; border-radius: 6px; box-shadow: 0 12px 28px rgba(15, 23, 42, .25); font-size: 12px; }.undo-toast button { color: #93c5fd; background: transparent; border: 0; cursor: pointer; font-weight: 650; }.undo-toast span { color: #94a3b8; font-size: 10px; }.toast-enter-active, .toast-leave-active { transition: opacity .18s ease, transform .18s ease; }.toast-enter-from, .toast-leave-to { opacity: 0; transform: translate(-50%, 8px); }
.save-live { position: fixed; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
.runtime-settings-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }.runtime-setting-field { min-width: 0; display: grid; gap: 6px; color: #475569; font-size: 12px; }.runtime-setting-field span { font-weight: 650; }.runtime-setting-field :deep(.el-input-number) { width: 100%; }
.create-workflow-form, .template-form-item :deep(.el-form-item__content) { min-width: 0; }.template-form-item :deep(.el-form-item__content) { display: block; }
.template-list { width: 100%; min-width: 0; display: grid; gap: 8px; }.template-list :deep(.template-option.el-radio) { width: 100%; max-width: 100%; box-sizing: border-box; height: auto; min-height: 58px; align-items: flex-start; margin: 0; padding: 10px 12px; white-space: normal; }.template-list :deep(.el-radio__input) { flex: 0 0 auto; padding-top: 3px; }.template-list :deep(.el-radio__label) { min-width: 0; display: flex; flex-direction: column; gap: 3px; line-height: 1.45; white-space: normal; }.template-list b, .template-list span { min-width: 0; overflow-wrap: anywhere; }.template-list span, .dialog-empty { color: #64748b; font-size: 11px; }.dialog-empty { padding: 20px; }
.candidate-roles { display: grid; gap: 16px; max-height: 62vh; overflow: auto; }.candidate-roles section header { display: flex; justify-content: space-between; margin-bottom: 8px; }.candidate-roles section header span { color: #64748b; font-size: 11px; }.candidate-roles section > div { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }.candidate-roles button { position: relative; height: 230px; overflow: hidden; color: #64748b; background: #f8fafc; border: 2px solid transparent; border-radius: 5px; }.candidate-roles button.selected { border-color: #2563eb; }.candidate-roles img { width: 100%; height: 100%; object-fit: contain; }.candidate-roles button > svg { position: absolute; right: 8px; top: 8px; width: 24px; padding: 4px; color: #fff; background: #2563eb; border-radius: 50%; }.storyboard-editor :deep(textarea) { font-family: "SFMono-Regular", Consolas, monospace; font-size: 11px; line-height: 1.6; }.json-error { margin-right: 12px; color: #dc2626; font-size: 11px; }
button, select { font-family: inherit; } button:focus-visible, select:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
@media (max-width: 1279px) {
  .brand-block { display: none; }
  .save-state.ready span,
  .save-state.unbound span,
  .save-state.loading span { display: none; }
}
@media (max-width: 767px) {
  .workflow-name { max-width: none; }
  .workflow-name .rename-button { display: none; }
  .header-actions { gap: 4px; }
  .workspace-header { gap: 6px; padding-inline: 8px; }
  .workspace-context-bar { flex-wrap: wrap; gap: 8px; }
  .runtime-settings-grid { grid-template-columns: 1fr; }
  .workspace-grid { grid-template-columns: minmax(0, 1fr); grid-template-areas: "canvas"; }
  .workflow-library, .workflow-inspector, .left-resizer, .right-resizer, .library-restore, .inspector-restore { display: none !important; }
}
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; animation-duration: .01ms !important; animation-iteration-count: 1 !important; } }
</style>
