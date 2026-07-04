<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Handle, Position, VueFlow, useVueFlow, type Edge, type Node } from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import {
  ArrowDown,
  Close,
  CopyDocument,
  Download,
  EditPen,
  Finished,
  Grid,
  Plus,
  Operation,
  Refresh,
  RefreshRight,
  Search,
  View,
  ZoomIn,
  ZoomOut,
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import type { UploadFile } from 'element-plus/es/components/upload/index.mjs'
import {
  ECOMMERCE_EXTRA_ASSET_OPTIONS,
  ECOMMERCE_LANGUAGES,
  cancelEcommerceTask,
  createEcommerceTask,
  ecommerceLanguageName,
  getEcommerceOptions,
  getEcommerceTask,
  listEcommerceLibraryAssets,
  listEcommerceTasks,
  retryEcommerceAsset,
  retryEcommerceTask,
  type EcommerceAsset,
  type EcommerceLibraryAsset,
  type EcommercePlatform,
  type EcommercePromptTemplate,
  type EcommerceStyleTemplate,
  type EcommerceTask,
} from '@/api/ecommerce'
import { formatDateTime } from '@/utils/format'
import { getCachedImageObjectURL, peekCachedImageObjectURL } from '@/utils/imageCache'
import ImagePreviewDialog from '@/components/ImagePreviewDialog.vue'

type CanvasNodeKind = 'brief' | 'strategy' | 'copy' | 'asset' | 'video_script' | 'video' | 'detail' | 'export' | 'custom_text' | 'custom_image' | 'custom_video' | 'custom_config'
type CanvasNodeStatus = 'idle' | 'working' | 'success' | 'failed'

interface CanvasNode {
  id: string
  kind: CanvasNodeKind
  title: string
  subtitle: string
  x: number
  y: number
  w: number
  h: number
  status: CanvasNodeStatus
  assetType?: string
  asset?: EcommerceAsset
  editable?: boolean
}

interface CanvasEdge {
  from: string
  to: string
}

interface CanvasFlowNodeData {
  node: CanvasNode
}

const MAX_IMAGES = 4
const MAX_IMAGE_MB = 20
const POLL_INTERVAL = 2500
const TASK_PAGE_SIZE = 12

const optionsLoading = ref(false)
const tasksLoading = ref(false)
const detailLoading = ref(false)
const submitting = ref(false)
const canceling = ref(false)
const exporting = ref(false)
const libraryLoading = ref(false)
const libraryLoaded = ref(false)
const retryingAssetID = ref(0)
const polling = ref<number | null>(null)
const pollingTaskID = ref('')
const ticker = ref<number | null>(null)
const nowTs = ref(Date.now())
const taskKeyword = ref('')
const tasksTotal = ref(0)
const selectedNodeID = ref('brief')
const nodeDetailVisible = ref(false)
const previewVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
const previewImageURL = ref('')
const previewImageLoading = ref(false)
const brokenAssetIDs = ref<Set<number>>(new Set())
const retryPrompts = ref<Record<number, string>>({})
const backgroundMode = ref<'grid' | 'dots' | 'blank'>('grid')
const showMiniMap = ref(true)
const initialViewport = { x: 26, y: 72, zoom: 0.86 }
const { fitView, getViewport, setViewport } = useVueFlow({ id: 'ecommerce-canvas-flow' })
const customNodes = ref<CanvasNode[]>([])
const customEdges = ref<CanvasEdge[]>([])
const editDialogVisible = ref(false)
const editingNode = reactive({
  id: '',
  title: '',
  subtitle: '',
  kind: 'custom_text' as CanvasNodeKind,
})
const downstreamDialogVisible = ref(false)
const downstreamParentID = ref('')
const downstreamForm = reactive({
  title: '',
  subtitle: '',
  kind: 'custom_text' as CanvasNodeKind,
})

const platforms = ref<EcommercePlatform[]>([])
const prompts = ref<EcommercePromptTemplate[]>([])
const styles = ref<EcommerceStyleTemplate[]>([])
const productLibraryAssets = ref<EcommerceLibraryAsset[]>([])
const modelLibraryAssets = ref<EcommerceLibraryAsset[]>([])
const tasks = ref<EcommerceTask[]>([])
const activeTask = ref<EcommerceTask | null>(null)
const flowNodeState = ref<Node<CanvasFlowNodeData>[]>([])
const flowEdgeState = ref<Edge[]>([])

const form = reactive({
  platform_id: 0,
  prompt_template_id: 0,
  style_template_id: 0,
  language: 'zh-CN',
  requirement: '',
  reference_images: [] as string[],
  product_asset_id: '',
  model_asset_id: '',
  extra_asset_types: [] as string[],
})

const statusText: Record<string, string> = {
  queued: '排队中',
  running: '生成中',
  success: '已完成',
  failed: '失败',
  canceled: '已中断',
}

const statusTone: Record<string, string> = {
  queued: 'muted',
  running: 'working',
  success: 'success',
  failed: 'failed',
  canceled: 'muted',
}

const assetText: Record<string, string> = {
  title_image: '首屏主视觉',
  main_image: '核心卖点图',
  white_image: '白底图',
  detail_image: '商品细节图',
  price_image: '价格图',
  hero_visual_image: '首屏主视觉',
  core_selling_point_image: '核心卖点图',
  usage_scene_image: '使用场景图',
  multi_angle_image: '多角度图',
  scene_atmosphere_image: '场景氛围图',
  product_detail_image: '商品细节图',
  brand_story_image: '品牌故事图',
  size_capacity_image: '尺寸/容量/尺码图',
  effect_compare_image: '效果对比图',
  spec_sheet_image: '详细规格/参数表',
  craft_process_image: '工艺制作图',
  accessories_image: '配件/赠品图',
  series_show_image: '系列展示图',
  ingredients_image: '商品成分图',
  after_sales_image: '售后保障图',
  usage_tips_image: '使用建议图',
  spokesperson_image: '代言图',
  model_product_image: '模特展示图',
  product_video: '商品视频',
}

const assetRole: Record<string, string> = {
  title_image: '承担首屏点击与品牌记忆',
  main_image: '承担核心差异和利益点解释',
  white_image: '承担商品可信识别与平台基础素材',
  detail_image: '承担材质、工艺和局部细节说明',
  price_image: '承担促销行动与下单推动',
  hero_visual_image: '传递核心价值',
  core_selling_point_image: '突出差异优势',
  usage_scene_image: '呈现真实使用场景',
  multi_angle_image: '多角度呈现外观',
  scene_atmosphere_image: '展示使用场景',
  product_detail_image: '放大材质与工艺',
  brand_story_image: '传达品牌理念',
  size_capacity_image: '展示规格信息',
  effect_compare_image: '使用前后效果对比',
  spec_sheet_image: '展示详细商品数据',
  craft_process_image: '展示工艺制作过程',
  accessories_image: '明确收货的所有物品',
  series_show_image: '多色或多 SKU 展示',
  ingredients_image: '展示配方/材质/成分',
  after_sales_image: '说明质保退换政策',
  usage_tips_image: '商品使用的注意事项',
  spokesperson_image: '承担代言背书与品牌信任建立',
  model_product_image: '承担模特上身/上手/使用效果展示',
}

const assetOrder = ['white_image', 'title_image', 'main_image', 'detail_image', 'price_image', ...ECOMMERCE_EXTRA_ASSET_OPTIONS.map((item) => item.value)]
const productVideoType = 'product_video'
const customNodeSize: Record<string, { w: number; h: number }> = {
  custom_text: { w: 240, h: 150 },
  custom_image: { w: 260, h: 190 },
  custom_video: { w: 270, h: 190 },
  custom_config: { w: 240, h: 150 },
}

const output = computed<Record<string, any>>(() => activeTask.value?.output_json || {})
const productInfo = computed<Record<string, any>>(() => output.value?.product_info || {})
const priceInfo = computed<Record<string, any>>(() => output.value?.price_info || {})
const assets = computed(() => activeTask.value?.assets || [])
const currentAssets = computed(() => latestAssetsByType(assets.value))
const selectedPlatform = computed(() => platforms.value.find((item) => item.id === form.platform_id))
const selectedProductAsset = computed(() => productLibraryAssets.value.find((item) => item.asset_id === form.product_asset_id))
const selectedModelAsset = computed(() => modelLibraryAssets.value.find((item) => item.asset_id === form.model_asset_id))
const activePlatformName = computed(() => activeTask.value?.platform_name || selectedPlatform.value?.name || '未选择平台')
const selectedLanguage = computed(() => ecommerceLanguageName(form.language || selectedPlatform.value?.language))
const activeLanguage = computed(() => activeTask.value?.language_name || ecommerceLanguageName(activeTask.value?.language || form.language))
const heroTitle = computed(() => output.value?.product_title || productInfo.value?.canonical_title || '等待生成商品标题')
const heroDescription = computed(() => output.value?.description || productInfo.value?.core_value || '输入商品资料后，画布会把平台策略、文案、素材和详情页串成一条交付链路。')
const priceCopy = computed(() => output.value?.price_copy || priceInfo.value?.price_text || priceInfo.value?.promotion_text || '')
const marketingCopy = computed<string[]>(() => asStringArray(output.value?.marketing_copy))
const sellingPoints = computed<string[]>(() => uniqueStrings([
  ...asStringArray(productInfo.value?.selling_points),
  ...asStringArray(output.value?.selling_points),
]).slice(0, 8))
const keySpecs = computed<string[]>(() => uniqueStrings([
  ...asStringArray(productInfo.value?.key_specs),
  ...asStringArray(productInfo.value?.specs),
  ...asStringArray(output.value?.key_specs),
  ...asStringArray(output.value?.specs),
]).slice(0, 10))
const videoScriptLines = computed(() => [
  `开场 0-3 秒：${heroTitle.value || activeTask.value?.requirement || form.requirement || '突出商品核心利益点'}`,
  `展示 3-10 秒：${sellingPoints.value.slice(0, 2).join('；') || '用场景证明卖点'}`,
  `转化 10-15 秒：${priceCopy.value || '给出购买理由和行动引导'}`,
])
const detailSections = computed<Array<{ title: string; body: string }>>(() => (
  Array.isArray(output.value?.detail_sections)
    ? output.value.detail_sections.filter((item: any) => item?.title || item?.body)
    : []
))
const detailDoc = computed(() => {
  if (!activeTask.value?.output_html) return ''
  const body = sanitizeDetailHTML(withThumbImages(activeTask.value.output_html, 500))
  const reset = `html,body,.stage,.ecommerce-detail-preview,.ecommerce-detail-preview *{filter:none!important;-webkit-filter:none!important;mix-blend-mode:normal!important;opacity:1!important}.ecommerce-detail-preview img{display:block;width:100%;max-width:100%;height:auto;object-fit:contain}`
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:#fff;color:#111827;color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif}.stage{width:100%;max-width:860px;margin:0 auto;padding:24px;overflow-x:hidden}.ecommerce-detail-preview{width:100%;max-width:100%;overflow-x:hidden}.copy-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px}.detail-head,.detail-section{max-width:100%;overflow-wrap:anywhere}${reset}</style></head><body><main class="stage">${body}</main><style>${reset}</style></body></html>`
})
const isRunning = computed(() => {
  const taskStatus = activeTask.value?.status || ''
  return taskStatus === 'queued' || taskStatus === 'running' || currentAssets.value.some((asset) => isAssetWorking(asset.status))
})
const doneAssetCount = computed(() => currentAssets.value.filter(assetHasImage).length)
const activePercent = computed(() => {
  if (!activeTask.value) return 0
  if (activeTask.value.status === 'success') return 100
  return Math.max(0, Math.min(100, Number(activeTask.value.progress || 0)))
})
const taskElapsed = computed(() => activeTask.value ? generationElapsed(activeTask.value.started_at, activeTask.value.finished_at, isRunning.value) : '0秒')
const taskQueueElapsed = computed(() => activeTask.value ? queueElapsed(activeTask.value.created_at, activeTask.value.started_at, activeTask.value.finished_at, isRunning.value) : '0秒')
const statusLabel = computed(() => activeTask.value ? statusText[activeTask.value.status] || activeTask.value.status : '待创建')
const statusClass = computed(() => activeTask.value ? statusTone[activeTask.value.status] || 'muted' : 'idle')
const selectedNode = computed(() => canvasNodes.value.find((node) => node.id === selectedNodeID.value) || canvasNodes.value[0])
const selectedAsset = computed(() => selectedNode.value?.asset || null)
const selectedNodeIndex = computed(() => Math.max(0, canvasNodes.value.findIndex((node) => node.id === selectedNodeID.value)) + 1)
const workingNodeCount = computed(() => canvasNodes.value.filter((node) => node.status === 'working').length)
const failedNodeCount = computed(() => canvasNodes.value.filter((node) => node.status === 'failed').length)
const readyNodeCount = computed(() => canvasNodes.value.filter((node) => node.status === 'success').length)
const canvasImageAssetCount = computed(() => canvasNodes.value.filter((node) => node.kind === 'asset').length)
const canvasProgressText = computed(() => `${readyNodeCount.value}/${canvasNodes.value.length} 节点就绪`)

const canvasNodes = computed<CanvasNode[]>(() => {
  const hasTask = !!activeTask.value
  const hasCopy = !!output.value?.product_title
  const hasDetail = !!detailDoc.value
  const failed = activeTask.value?.status === 'failed' || activeTask.value?.status === 'canceled'
  const working = isRunning.value
  const assetMap = new Map(currentAssets.value.map((asset) => [asset.asset_type, asset]))
  const videoAsset = assetMap.get(productVideoType)
  const whiteReady = assetStatus(assetMap.get('white_image'), failed) === 'success'
  const videoReady = assetCanPreview(videoAsset)
  const nodes: CanvasNode[] = [
    {
      id: 'brief',
      kind: 'brief',
      title: '商品简报',
      subtitle: activeTask.value?.requirement || form.requirement || '卖什么、卖给谁、凭什么买',
      x: 60,
      y: 260,
      w: 240,
      h: 150,
      status: hasTask || form.requirement ? 'success' : 'idle',
    },
    {
      id: 'strategy',
      kind: 'strategy',
      title: '平台策略',
      subtitle: `${activePlatformName.value} · ${activeLanguage.value}`,
      x: 350,
      y: 260,
      w: 240,
      h: 145,
      status: hasTask ? 'success' : 'idle',
    },
    {
      id: 'copy',
      kind: 'copy',
      title: '营销文案',
      subtitle: hasCopy ? compactText(heroTitle.value, 42) : '标题、描述、卖点、促销文案',
      x: 640,
      y: 260,
      w: 240,
      h: 150,
      status: hasCopy ? 'success' : working ? 'working' : failed ? 'failed' : 'idle',
    },
  ]
  const selectedExtraTypes = new Set(activeTask.value ? activeTask.value.extra_asset_types || [] : form.extra_asset_types)
  const visibleAssetOrder = assetOrder.filter((type) => !isOptionalCanvasAsset(type) || selectedExtraTypes.has(type) || assetMap.has(type))
  visibleAssetOrder.forEach((type, index) => {
    const asset = assetMap.get(type)
    const isWhiteImage = type === 'white_image'
    const otherImageIndex = Math.max(0, index - 1)
    nodes.push({
      id: `asset:${type}`,
      kind: 'asset',
      title: assetText[type] || type,
      subtitle: assetRole[type] || '电商素材资产',
      x: isWhiteImage ? 930 : 1240,
      y: isWhiteImage ? 260 : 16 + otherImageIndex * 224,
      w: 240,
      h: 180,
      status: assetStatus(asset, failed),
      assetType: type,
      asset,
    })
  })
  nodes.push({
    id: 'video_script',
    kind: 'video_script',
    title: '短视频脚本',
    subtitle: hasCopy ? '15 秒种草视频：开场、卖点、转化口播' : '镜头脚本、口播、字幕和素材节奏',
    x: 930,
    y: 516,
    w: 270,
    h: 175,
    status: hasCopy ? 'success' : working ? 'working' : failed ? 'failed' : 'idle',
  })
  nodes.push({
    id: 'video',
    kind: 'video',
    title: '视频成片',
    subtitle: videoReady ? '视频已生成，可预览或下载' : hasCopy && whiteReady ? '由文案、白底图和脚本生成视频' : '等待文案、白底图和短视频脚本',
    x: 1530,
    y: 504,
    w: 270,
    h: 175,
    status: videoAsset ? assetStatus(videoAsset, failed) : hasCopy && whiteReady ? 'success' : working ? 'working' : failed ? 'failed' : 'idle',
    assetType: productVideoType,
    asset: videoAsset,
  })
  nodes.push({
    id: 'detail',
    kind: 'detail',
    title: '详情页',
    subtitle: hasDetail ? '可预览投放页面' : '最后汇总图文视频资产生成详情页',
    x: 1840,
    y: 260,
    w: 255,
    h: 155,
    status: hasDetail ? 'success' : working ? 'working' : failed ? 'failed' : 'idle',
  })
  nodes.push({
    id: 'export',
    kind: 'export',
    title: '交付导出',
    subtitle: doneAssetCount.value > 0 ? '图片、详情页、长图可交付' : '等待资产完成',
    x: 2140,
    y: 260,
    w: 255,
    h: 145,
    status: doneAssetCount.value > 0 ? 'success' : working ? 'working' : 'idle',
  })
  return [...nodes, ...customNodes.value]
})

const systemEdges = computed<CanvasEdge[]>(() => [
  { from: 'brief', to: 'strategy' },
  { from: 'strategy', to: 'copy' },
  { from: 'copy', to: 'asset:white_image' },
  { from: 'asset:white_image', to: 'asset:detail_image' },
  { from: 'asset:white_image', to: 'asset:price_image' },
  { from: 'asset:white_image', to: 'asset:title_image' },
  { from: 'asset:white_image', to: 'asset:main_image' },
  ...optionalCanvasAssetEdges(),
  { from: 'copy', to: 'video_script' },
  { from: 'copy', to: 'video' },
  { from: 'asset:white_image', to: 'video' },
  { from: 'video_script', to: 'video' },
  { from: 'asset:title_image', to: 'detail' },
  { from: 'asset:main_image', to: 'detail' },
  { from: 'asset:detail_image', to: 'detail' },
  { from: 'asset:price_image', to: 'detail' },
  ...optionalCanvasDetailEdges(),
  { from: 'video', to: 'detail' },
  { from: 'detail', to: 'export' },
])

const canvasEdges = computed<CanvasEdge[]>(() => [...systemEdges.value, ...customEdges.value])

function isOptionalCanvasAsset(type: string) {
  return ECOMMERCE_EXTRA_ASSET_OPTIONS.some((item) => item.value === type)
}

function selectedOptionalCanvasAssets() {
  const persisted = activeTask.value ? activeTask.value.extra_asset_types || [] : form.extra_asset_types
  const generated = new Set(currentAssets.value.map((asset) => asset.asset_type))
  return assetOrder.filter((type) => isOptionalCanvasAsset(type) && (persisted.includes(type) || generated.has(type)))
}

function optionalCanvasAssetEdges(): CanvasEdge[] {
  return selectedOptionalCanvasAssets().map((type) => ({ from: 'asset:white_image', to: `asset:${type}` }))
}

function optionalCanvasDetailEdges(): CanvasEdge[] {
  return selectedOptionalCanvasAssets().map((type) => ({ from: `asset:${type}`, to: 'detail' }))
}

const flowNodes = computed<any[]>(() => canvasNodes.value.map((node) => ({
  id: node.id,
  type: 'commerce',
  position: { x: node.x, y: node.y },
  sourcePosition: Position.Right,
  targetPosition: Position.Left,
  data: { node },
  selected: selectedNodeID.value === node.id,
  draggable: true,
  selectable: true,
  style: {
    width: `${node.w}px`,
    minHeight: `${node.h}px`,
  },
})))

const flowEdges = computed<any[]>(() => canvasEdges.value.map((edge) => ({
  id: `${edge.from}-${edge.to}`,
  source: edge.from,
  target: edge.to,
  type: 'default',
  animated: isRunning.value,
  selectable: false,
  markerEnd: 'edge-arrow',
  style: { stroke: 'var(--edge-color)', strokeWidth: 2 },
})))

function syncFlowElements(resetPositions = false) {
  const previous = Object.fromEntries(flowNodeState.value.map((node: any) => [node.id, node]))
  const nextNodes: any[] = []
  for (const node of flowNodes.value as any[]) {
    const old = previous[node.id]
    nextNodes.push(old && !resetPositions
      ? { ...node, position: old.position, selected: selectedNodeID.value === node.id }
      : node)
  }
  flowNodeState.value = nextNodes
  flowEdgeState.value = flowEdges.value
  if (resetPositions) {
    nextTick(() => setViewport(initialViewport, { duration: 180 }))
  }
}

function assetStatus(asset: EcommerceAsset | undefined, failedTask: boolean): CanvasNodeStatus {
  if (!asset) return failedTask ? 'failed' : 'idle'
  if (isAssetWorking(asset.status)) return 'working'
  if (assetHasImage(asset)) return 'success'
  if (asset.status === 'failed' || asset.status === 'canceled') return 'failed'
  return 'idle'
}

function asStringArray(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean)
  if (typeof value === 'string') {
    return value.split(/[\n\r;；、,，]/).map((item) => item.trim()).filter(Boolean)
  }
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap(asStringArray)
  }
  return []
}

function uniqueStrings(items: string[]) {
  return Array.from(new Set(items.map((item) => item.trim()).filter(Boolean)))
}

function compactText(text?: string | null, limit = 84) {
  const value = String(text || '').replace(/\s+/g, ' ').trim()
  if (!value) return ''
  return value.length > limit ? `${value.slice(0, limit)}...` : value
}

function latestAssetsByType(list: EcommerceAsset[]) {
  const order = [...assetOrder, productVideoType]
  const rank = (type: string) => order.indexOf(type) === -1 ? 99 : order.indexOf(type)
  const map = new Map<string, EcommerceAsset>()
  for (const asset of list) {
    const prev = map.get(asset.asset_type)
    if (!prev || asset.id >= prev.id) map.set(asset.asset_type, asset)
  }
  return [...map.values()].sort((a, b) => rank(a.asset_type) - rank(b.asset_type))
}

function isVideoAsset(assetOrType?: EcommerceAsset | string) {
  const type = typeof assetOrType === 'string' ? assetOrType : assetOrType?.asset_type
  return type === productVideoType
}

function isAssetWorking(status: string) {
  return status === 'queued' || status === 'running'
}

function assetIsReady(asset?: EcommerceAsset) {
  return !!asset && asset.status === 'success' && !!asset.url && !brokenAssetIDs.value.has(asset.id)
}

function assetHasImage(asset?: EcommerceAsset) {
  return !!asset && !isVideoAsset(asset) && assetIsReady(asset)
}

function assetCanPreview(asset?: EcommerceAsset) {
  return isVideoAsset(asset) ? assetIsReady(asset) : assetHasImage(asset)
}

function elapsedText(start?: string | null, end?: string | null, live = false) {
  if (!start) return '0秒'
  const startMs = new Date(start).getTime()
  if (!Number.isFinite(startMs)) return '0秒'
  const endMs = end ? new Date(end).getTime() : live ? nowTs.value : Date.now()
  const total = Math.max(0, Math.floor((endMs - startMs) / 1000))
  const min = Math.floor(total / 60)
  const sec = total % 60
  return min > 0 ? `${min}分${sec}秒` : `${sec}秒`
}

function generationElapsed(start?: string | null, end?: string | null, live = false) {
  if (!start) return '0秒'
  return elapsedText(start, end, live)
}

function queueElapsed(created?: string | null, started?: string | null, fallbackEnd?: string | null, live = false) {
  if (!created) return '0秒'
  if (started) return elapsedText(created, started, false)
  if (fallbackEnd) return elapsedText(created, fallbackEnd, false)
  return live ? elapsedText(created, null, true) : '0秒'
}

function assetGenerateElapsed(asset?: EcommerceAsset) {
  if (!asset) return '0秒'
  return generationElapsed(asset.started_at, asset.finished_at, isAssetWorking(asset.status) && !!asset.started_at)
}

function assetQueueElapsed(asset?: EcommerceAsset) {
  if (!asset) return '0秒'
  return queueElapsed(asset.created_at, asset.started_at, asset.finished_at, asset.status === 'queued')
}

function thumbURL(url: string, kb = 100) {
  if (!url) return url
  const hashAt = url.indexOf('#')
  const main = hashAt >= 0 ? url.slice(0, hashAt) : url
  const hash = hashAt >= 0 ? url.slice(hashAt) : ''
  if (/(\?|&|&amp;)thumb_kb=\d+/.test(main)) {
    return main.replace(/((?:\?|&|&amp;)thumb_kb=)\d+/, `$1${kb}`) + hash
  }
  const sep = main.includes('&amp;') ? '&amp;' : '&'
  return `${main}${main.includes('?') ? sep : '?'}thumb_kb=${kb}${hash}`
}

function withThumbImages(html: string, kb = 100) {
  return html.replace(/(<img\b[^>]*\bsrc=["'])([^"']+)(["'])/gi, (_match, prefix, url, suffix) => `${prefix}${thumbURL(url, kb)}${suffix}`)
}

function sanitizeDetailHTML(html: string) {
  return html
    .replace(/-webkit-filter\s*:\s*[^;"'}]+;?/gi, '')
    .replace(/filter\s*:\s*[^;"'}]+;?/gi, '')
}

function errorMessage(err: unknown, fallback: string) {
  const anyErr = err as any
  return anyErr?.response?.data?.message || anyErr?.message || fallback
}

function selectNode(id: string) {
  selectedNodeID.value = id
  syncFlowElements(false)
}

function openNodeDetail(node?: CanvasNode) {
  if (!node) return
  selectNode(node.id)
  nodeDetailVisible.value = true
}

function openEditNode(node = selectedNode.value) {
  if (!node) return
  editingNode.id = node.id
  editingNode.title = node.title
  editingNode.subtitle = node.subtitle
  editingNode.kind = node.kind
  editDialogVisible.value = true
}

function saveEditingNode() {
  const title = editingNode.title.trim()
  const subtitle = editingNode.subtitle.trim()
  if (!title) {
    ElMessage.warning('节点标题不能为空')
    return
  }
  const patchNode = (node: CanvasNode) => ({
    ...node,
    title,
    subtitle: subtitle || node.subtitle,
    editable: true,
  })
  const index = customNodes.value.findIndex((node) => node.id === editingNode.id)
  if (index >= 0) {
    customNodes.value.splice(index, 1, patchNode(customNodes.value[index]))
  } else {
    const current = canvasNodes.value.find((node) => node.id === editingNode.id)
    if (current) {
      customNodes.value.push(patchNode({ ...current, id: `custom:${Date.now()}`, x: current.x + 36, y: current.y + 36 }))
      ElMessage.info('系统节点已复制为可编辑节点')
    }
  }
  editDialogVisible.value = false
  syncFlowElements(false)
}

function openDownstreamDialog(parentID = selectedNodeID.value) {
  const parent = canvasNodes.value.find((node) => node.id === parentID)
  downstreamParentID.value = parent?.id || 'brief'
  downstreamForm.title = parent ? `${parent.title} 下游` : '新增节点'
  downstreamForm.subtitle = '补充运营判断、提示词、素材要求或交付说明'
  downstreamForm.kind = 'custom_text'
  downstreamDialogVisible.value = true
}

function addDownstreamNode() {
  const parent = canvasNodes.value.find((node) => node.id === downstreamParentID.value)
  if (!parent) {
    ElMessage.warning('请选择上游节点')
    return
  }
  const title = downstreamForm.title.trim()
  if (!title) {
    ElMessage.warning('节点标题不能为空')
    return
  }
  const existingChildren = canvasEdges.value.filter((edge) => edge.from === parent.id).length
  const node: CanvasNode = {
    id: `custom:${Date.now()}`,
    kind: downstreamForm.kind,
    title,
    subtitle: downstreamForm.subtitle.trim() || '自定义下游节点',
    x: parent.x + 360,
    y: parent.y + existingChildren * 120,
    w: customNodeSize[downstreamForm.kind]?.w || 280,
    h: customNodeSize[downstreamForm.kind]?.h || 180,
    status: 'idle',
    editable: true,
  }
  customNodes.value.push(node)
  customEdges.value.push({ from: parent.id, to: node.id })
  selectedNodeID.value = node.id
  downstreamDialogVisible.value = false
  syncFlowElements(false)
  nextTick(() => setViewport({ ...getViewport(), x: getViewport().x - 90 }, { duration: 160 }))
}

function retrySelectedNode() {
  retryNode(selectedNode.value)
}

function retryNode(target?: CanvasNode) {
  if (!target) return
  if (target.asset) {
    retryAsset(target.asset)
    return
  }
  if (target.id === 'copy' || target.id === 'video_script' || target.id === 'detail' || target.id === 'video' || target.id === 'export') {
    retryWholeTask()
    return
  }
  const index = customNodes.value.findIndex((item) => item.id === target.id)
  if (index >= 0) {
    customNodes.value.splice(index, 1, {
      ...customNodes.value[index],
      status: 'success',
      subtitle: `${customNodes.value[index].subtitle} · 已标记重试`,
    })
    syncFlowElements(false)
    ElMessage.success('自定义节点已重新整理')
    return
  }
  ElMessage.info('该系统节点会在整套生成时更新')
}

function centerCanvas() {
  fitView({ padding: 0.06, duration: 260 })
}

function autoArrangeCanvas() {
  const nodes = canvasNodes.value
  if (!nodes.length) return
  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const incomingCount = new Map(nodes.map((node) => [node.id, 0]))
  const children = new Map<string, string[]>()
  for (const edge of canvasEdges.value) {
    if (!nodeMap.has(edge.from) || !nodeMap.has(edge.to)) continue
    incomingCount.set(edge.to, (incomingCount.get(edge.to) || 0) + 1)
    children.set(edge.from, [...(children.get(edge.from) || []), edge.to])
  }
  const queue = nodes.filter((node) => (incomingCount.get(node.id) || 0) === 0).map((node) => node.id)
  const layer = new Map<string, number>(queue.map((id) => [id, 0]))
  for (let index = 0; index < queue.length; index += 1) {
    const id = queue[index]
    const nextLayer = (layer.get(id) || 0) + 1
    for (const childID of children.get(id) || []) {
      layer.set(childID, Math.max(layer.get(childID) || 0, nextLayer))
      incomingCount.set(childID, (incomingCount.get(childID) || 0) - 1)
      if ((incomingCount.get(childID) || 0) <= 0) queue.push(childID)
    }
  }
  nodes.forEach((node) => {
    if (!layer.has(node.id)) layer.set(node.id, 0)
  })
  const grouped = new Map<number, CanvasNode[]>()
  for (const node of nodes) {
    const depth = layer.get(node.id) || 0
    grouped.set(depth, [...(grouped.get(depth) || []), node])
  }
  const previous = new Map(flowNodeState.value.map((node: any) => [node.id, node]))
  const arrangedPositions = new Map<string, { x: number; y: number }>()
  const layerGap = 300
  const rowGap = 210
  const originX = 44
  const originY = 54
  Array.from(grouped.entries()).sort(([a], [b]) => a - b).forEach(([depth, layerNodes]) => {
    layerNodes
      .slice()
      .sort((a, b) => (a.y - b.y) || a.id.localeCompare(b.id))
      .forEach((node, index) => {
        arrangedPositions.set(node.id, {
          x: originX + depth * layerGap,
          y: originY + index * rowGap,
        })
      })
  })
  const nextNodes: any[] = []
  for (const node of flowNodes.value as any[]) {
    nextNodes.push({
      ...node,
      position: arrangedPositions.get(node.id) || previous.get(node.id)?.position || node.position,
      selected: selectedNodeID.value === node.id,
    })
  }
  flowNodeState.value = nextNodes
  flowEdgeState.value = flowEdges.value
  nextTick(() => fitView({ padding: 0.08, duration: 260 }))
}

function resetViewport() {
  setViewport(initialViewport, { duration: 180 })
}

function zoom(delta: number) {
  const current = getViewport()
  setViewport({
    ...current,
    zoom: Math.max(0.35, Math.min(1.5, Number((current.zoom + delta).toFixed(2)))),
  }, { duration: 160 })
}

function toggleBackgroundMode() {
  const modes: Array<'grid' | 'dots' | 'blank'> = ['grid', 'dots', 'blank']
  const nextIndex = (modes.indexOf(backgroundMode.value) + 1) % modes.length
  backgroundMode.value = modes[nextIndex]
}

async function loadOptions() {
  const data = await getEcommerceOptions()
  platforms.value = data.platforms || []
  prompts.value = data.prompt_templates || []
  styles.value = data.style_templates || []
  if (!form.platform_id && platforms.value[0]) {
    form.platform_id = platforms.value[0].id
    form.language = platforms.value[0].language || 'zh-CN'
  }
  if (!form.prompt_template_id && prompts.value[0]) form.prompt_template_id = prompts.value[0].id
  if (!form.style_template_id && styles.value[0]) form.style_template_id = styles.value[0].id
}

async function loadTasks() {
  if (tasksLoading.value) return
  tasksLoading.value = true
  try {
    const keyword = taskKeyword.value.trim()
    const data = await listEcommerceTasks({ limit: TASK_PAGE_SIZE, offset: 0, keyword: keyword || undefined })
    tasks.value = data.items || []
    tasksTotal.value = data.total || tasks.value.length
  } finally {
    tasksLoading.value = false
  }
}

async function loadLibraryAssets() {
  if (libraryLoaded.value || libraryLoading.value) return
  libraryLoading.value = true
  try {
    const [products, models] = await Promise.all([
      listEcommerceLibraryAssets({ kind: 'product', limit: 100 }, true),
      listEcommerceLibraryAssets({ kind: 'model', limit: 100 }, true),
    ])
    productLibraryAssets.value = products.items || []
    modelLibraryAssets.value = models.items || []
  } catch (err) {
    console.warn('ecommerce library assets unavailable:', err)
  } finally {
    libraryLoaded.value = true
    libraryLoading.value = false
  }
}

function onLibraryVisible(visible: boolean) {
  if (visible) loadLibraryAssets()
}

async function initialize() {
  optionsLoading.value = true
  try {
    await Promise.all([loadOptions(), loadTasks()])
    syncFlowElements(true)
  } catch (err) {
    console.error('ecommerce canvas initialize failed:', err)
    ElMessage.error('电商画布初始化失败')
  } finally {
    optionsLoading.value = false
  }
}

function readImageFile(file: File) {
  if (form.reference_images.length >= MAX_IMAGES) {
    ElMessage.warning(`最多上传 ${MAX_IMAGES} 张参考图`)
    return
  }
  if (!file.type.startsWith('image/')) {
    ElMessage.warning('请选择图片文件')
    return
  }
  if (file.size > MAX_IMAGE_MB * 1024 * 1024) {
    ElMessage.warning(`单张图片不能超过 ${MAX_IMAGE_MB}MB`)
    return
  }
  const reader = new FileReader()
  reader.onload = () => form.reference_images.push(String(reader.result || ''))
  reader.onerror = () => ElMessage.error('图片读取失败')
  reader.readAsDataURL(file)
}

function onImageChange(file: UploadFile) {
  if (file.raw) readImageFile(file.raw)
}

function removeImage(index: number) {
  form.reference_images.splice(index, 1)
}

function resetDraft() {
  form.requirement = ''
  form.reference_images = []
  form.product_asset_id = ''
  form.model_asset_id = ''
  form.extra_asset_types = []
  activeTask.value = null
  brokenAssetIDs.value = new Set()
  selectedNodeID.value = 'brief'
  syncFlowElements(true)
  stopPolling()
}

async function retryWholeTask() {
  if (!activeTask.value) return
  submitting.value = true
  try {
    const task = await retryEcommerceTask(activeTask.value.task_id)
    activeTask.value = task
    selectedNodeID.value = 'copy'
    brokenAssetIDs.value = new Set()
    syncFlowElements(true)
    await loadTasks()
    startPolling(task.task_id)
    ElMessage.success('已重新提交整套任务')
  } catch (err) {
    console.error('retry ecommerce canvas task failed:', err)
    ElMessage.error(errorMessage(err, '整套重试失败'))
  } finally {
    submitting.value = false
  }
}

async function submit() {
  if (!form.platform_id || !form.prompt_template_id || !form.style_template_id) {
    ElMessage.warning('请选择平台、提示词模板和风格模板')
    return
  }
  if (!form.requirement.trim()) {
    ElMessage.warning('请输入商品资料')
    return
  }
  submitting.value = true
  try {
    const task = await createEcommerceTask({
      platform_id: form.platform_id,
      prompt_template_id: form.prompt_template_id,
      style_template_id: form.style_template_id,
      language: form.language,
      requirement: form.requirement.trim(),
      reference_images: form.reference_images,
      product_asset_id: form.product_asset_id || undefined,
      model_asset_id: form.model_asset_id || undefined,
      extra_asset_types: form.extra_asset_types,
    })
    activeTask.value = task
    selectedNodeID.value = 'copy'
    brokenAssetIDs.value = new Set()
    syncFlowElements(true)
    await loadTasks()
    startPolling(task.task_id)
    ElMessage.success('已提交画布生成任务')
  } catch (err) {
    console.error('create ecommerce canvas task failed:', err)
    ElMessage.error(errorMessage(err, '任务提交失败'))
  } finally {
    submitting.value = false
  }
}

async function openTask(task: EcommerceTask) {
  stopPolling()
  detailLoading.value = true
  try {
    const fresh = await getEcommerceTask(task.task_id)
    activeTask.value = fresh
    brokenAssetIDs.value = new Set()
    selectedNodeID.value = 'brief'
    syncFlowElements(true)
    if (fresh.status === 'queued' || fresh.status === 'running' || latestAssetsByType(fresh.assets || []).some((asset) => isAssetWorking(asset.status))) {
      startPolling(fresh.task_id)
    }
  } catch (err) {
    console.error('open ecommerce canvas task failed:', err)
    ElMessage.error('打开任务失败')
  } finally {
    detailLoading.value = false
  }
}

async function refreshActiveTask() {
  if (!activeTask.value) return
  await openTask(activeTask.value)
}

async function cancelTask() {
  if (!activeTask.value || !isRunning.value) return
  const confirmed = await ElMessageBox.confirm('中断后已完成资产会保留，未完成资产停止继续更新。', '中断生成', {
    type: 'warning',
    confirmButtonText: '中断生成',
    cancelButtonText: '继续生成',
  }).catch(() => false)
  if (!confirmed || !activeTask.value) return
  canceling.value = true
  try {
    activeTask.value = await cancelEcommerceTask(activeTask.value.task_id)
    stopPolling()
    await loadTasks()
    ElMessage.success('已中断生成')
  } catch (err) {
    console.error('cancel ecommerce canvas task failed:', err)
    ElMessage.error(errorMessage(err, '中断失败'))
  } finally {
    canceling.value = false
  }
}

async function retryAsset(asset: EcommerceAsset) {
  if (!activeTask.value) return
  retryingAssetID.value = asset.id
  try {
    await retryEcommerceAsset(activeTask.value.task_id, asset.id, retryPrompts.value[asset.id] || '')
    const fresh = await getEcommerceTask(activeTask.value.task_id)
    activeTask.value = fresh
    syncFlowElements()
    retryPrompts.value = { ...retryPrompts.value, [asset.id]: '' }
    const next = new Set(brokenAssetIDs.value)
    next.delete(asset.id)
    brokenAssetIDs.value = next
    startPolling(fresh.task_id)
    ElMessage.success('已重新提交该节点')
  } catch (err) {
    console.error('retry ecommerce canvas asset failed:', err)
    ElMessage.error(errorMessage(err, '重新生成失败'))
  } finally {
    retryingAssetID.value = 0
  }
}

function startPolling(taskID: string) {
  stopPolling()
  pollingTaskID.value = taskID
  polling.value = window.setInterval(async () => {
    try {
      const fresh = await getEcommerceTask(taskID)
      if (pollingTaskID.value !== taskID) return
      if (activeTask.value?.task_id === taskID) {
        activeTask.value = fresh
        syncFlowElements()
      }
      if (fresh.status !== 'queued' && fresh.status !== 'running' && !latestAssetsByType(fresh.assets || []).some((asset) => isAssetWorking(asset.status))) {
        stopPolling()
        await loadTasks()
      }
    } catch (err) {
      console.error('poll ecommerce canvas task failed:', err)
    }
  }, POLL_INTERVAL)
}

function stopPolling() {
  if (polling.value) window.clearInterval(polling.value)
  polling.value = null
  pollingTaskID.value = ''
}

function markBrokenAsset(asset: EcommerceAsset) {
  const next = new Set(brokenAssetIDs.value)
  next.add(asset.id)
  brokenAssetIDs.value = next
}

async function openAssetPreview(asset?: EcommerceAsset) {
  if (!assetCanPreview(asset)) return
  previewAsset.value = asset!
  previewVisible.value = true
  if (isVideoAsset(asset)) {
    previewImageURL.value = asset!.url
    previewImageLoading.value = false
    return
  }
  const sourceURL = thumbURL(asset!.url, 500)
  previewImageURL.value = peekCachedImageObjectURL(sourceURL)
  if (previewImageURL.value) {
    previewImageLoading.value = false
    return
  }
  previewImageLoading.value = true
  try {
    const objectURL = await getCachedImageObjectURL(sourceURL)
    if (previewAsset.value?.id === asset!.id) previewImageURL.value = objectURL
  } catch (err) {
    console.error('load ecommerce canvas preview image failed:', err)
    if (previewAsset.value?.id === asset!.id) previewImageURL.value = sourceURL
  } finally {
    if (previewAsset.value?.id === asset!.id) previewImageLoading.value = false
  }
}

function downloadBlob(blob: Blob, filename: string) {
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(objectURL)
}

async function downloadAsset(asset?: EcommerceAsset) {
  if (!asset || !asset.url) return
  try {
    const res = await fetch(asset.url)
    if (!res.ok) throw new Error(`download failed: ${res.status}`)
    const blob = await res.blob()
    const ext = isVideoAsset(asset) ? 'mp4' : 'png'
    downloadBlob(blob, `${activeTask.value?.task_id || asset.task_id || 'ecommerce'}-${asset.asset_type}.${ext}`)
  } catch (err) {
    console.error('download ecommerce canvas asset failed:', err)
    window.open(asset.url, '_blank', 'noopener,noreferrer')
  }
}

function fetchImage(url: string): Promise<HTMLImageElement> {
  return fetch(url)
    .then((res) => {
      if (!res.ok) throw new Error(`image fetch failed: ${res.status}`)
      return res.blob()
    })
    .then((blob) => new Promise<HTMLImageElement>((resolve, reject) => {
      const objectURL = URL.createObjectURL(blob)
      const img = new Image()
      img.onload = () => {
        URL.revokeObjectURL(objectURL)
        resolve(img)
      }
      img.onerror = () => {
        URL.revokeObjectURL(objectURL)
        reject(new Error('image load failed'))
      }
      img.src = objectURL
    }))
}

function wrapCanvasText(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, maxWidth: number, lineHeight: number, maxLines: number) {
  const lines: string[] = []
  let line = ''
  for (const char of text) {
    const next = line + char
    if (ctx.measureText(next).width > maxWidth && line) {
      lines.push(line)
      line = char
      if (lines.length >= maxLines) break
    } else {
      line = next
    }
  }
  if (line && lines.length < maxLines) lines.push(line)
  lines.forEach((item, index) => ctx.fillText(item, x, y + index * lineHeight))
}

async function exportPoster() {
  if (!activeTask.value) return
  const imageAssets = assetOrder
    .map((type) => currentAssets.value.find((asset) => asset.asset_type === type && assetHasImage(asset)))
    .filter(Boolean) as EcommerceAsset[]
  if (!imageAssets.length) {
    ElMessage.warning('暂无可导出的图片')
    return
  }
  exporting.value = true
  try {
    const loaded = await Promise.all(imageAssets.map(async (asset) => ({
      asset,
      img: await fetchImage(thumbURL(asset.url, 500)),
    })))
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas unsupported')
    const width = 1242
    const padding = 72
    const contentWidth = width - padding * 2
    const blockGap = 34
    const imageHeight = loaded.reduce((sum, item) => {
      const imgWidth = item.img.width || contentWidth
      const imgHeight = item.img.height || contentWidth
      return sum + Math.round(imgHeight * contentWidth / imgWidth) + 86 + blockGap
    }, 0)
    const height = 360 + imageHeight + marketingCopy.value.length * 42
    canvas.width = width
    canvas.height = height
    ctx.fillStyle = '#f5f5f2'
    ctx.fillRect(0, 0, width, height)
    ctx.fillStyle = '#111111'
    ctx.font = '700 52px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    ctx.fillText(String(heroTitle.value).slice(0, 26), padding, 104)
    ctx.font = '400 28px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    wrapCanvasText(ctx, heroDescription.value, padding, 160, contentWidth, 42, 3)
    if (priceCopy.value) {
      ctx.font = '700 34px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(String(priceCopy.value).slice(0, 34), padding, 300)
    }
    let y = 350
    for (const item of loaded) {
      ctx.font = '700 30px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(assetText[item.asset.asset_type] || item.asset.asset_type, padding, y)
      y += 32
      const drawHeight = Math.round((item.img.height || contentWidth) * contentWidth / (item.img.width || contentWidth))
      ctx.drawImage(item.img, padding, y, contentWidth, drawHeight)
      y += drawHeight + blockGap
    }
    canvas.toBlob((blob) => {
      if (!blob) {
        ElMessage.error('导出失败')
        return
      }
      downloadBlob(blob, `${activeTask.value?.task_id || 'ecommerce'}-电商画布长图.png`)
    }, 'image/png')
    ElMessage.success('长图已生成')
  } catch (err) {
    console.error('export ecommerce canvas poster failed:', err)
    ElMessage.error('长图导出失败')
  } finally {
    exporting.value = false
  }
}

async function copyNodeText() {
  const node = selectedNode.value
  if (!node) return
  const lines = [
    node.title,
    node.subtitle,
    node.asset?.prompt,
    node.kind === 'copy' ? heroDescription.value : '',
    node.kind === 'copy' ? priceCopy.value : '',
  ].filter(Boolean)
  await navigator.clipboard.writeText(lines.join('\n'))
  ElMessage.success('已复制节点内容')
}

onMounted(() => {
  ticker.value = window.setInterval(() => { nowTs.value = Date.now() }, 1000)
  initialize()
})

watch(
  () => form.platform_id,
  (id) => {
    const platform = platforms.value.find((item) => item.id === id)
    form.language = platform?.language || 'zh-CN'
  },
)

onBeforeUnmount(() => {
  stopPolling()
  if (ticker.value) window.clearInterval(ticker.value)
})
</script>

<template>
  <div class="ecommerce-canvas-page">
    <aside class="left-panel" v-loading="optionsLoading">
      <header>
        <span>Commerce Canvas</span>
        <h1>电商画布</h1>
        <p>按运营决策链路组织商品资料、平台策略、素材生成和交付导出。</p>
      </header>

      <el-form label-position="top" class="canvas-form">
        <el-form-item label="商品资料">
          <el-input
            v-model="form.requirement"
            type="textarea"
            :rows="5"
            maxlength="2000"
            show-word-limit
            resize="none"
            placeholder="商品名、卖点、规格、目标客群、价格、促销、平台要求..."
          />
        </el-form-item>

        <div class="form-grid">
          <el-form-item label="电商平台">
            <el-select v-model="form.platform_id" placeholder="选择平台" filterable popper-class="canvas-select-popper">
              <el-option
                v-for="platform in platforms"
                :key="platform.id"
                :label="`${platform.name} · ${ecommerceLanguageName(platform.language)}`"
                :value="platform.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="文案语言">
            <el-select v-model="form.language" placeholder="选择语言" popper-class="canvas-select-popper">
              <el-option v-for="lang in ECOMMERCE_LANGUAGES" :key="lang.value" :label="lang.label" :value="lang.value" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item label="提示词模板">
          <el-select v-model="form.prompt_template_id" placeholder="选择提示词模板" filterable popper-class="canvas-select-popper">
            <el-option v-for="prompt in prompts" :key="prompt.id" :label="prompt.name" :value="prompt.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="风格模板">
          <el-select v-model="form.style_template_id" placeholder="选择风格模板" filterable popper-class="canvas-select-popper">
            <el-option v-for="style in styles" :key="style.id" :label="style.name" :value="style.id" />
          </el-select>
        </el-form-item>

        <div class="form-grid">
          <el-form-item label="商品资产">
            <el-select
              v-model="form.product_asset_id"
              placeholder="可选"
              filterable
              clearable
              popper-class="canvas-select-popper"
              :loading="libraryLoading"
              @visible-change="onLibraryVisible"
            >
              <el-option v-for="asset in productLibraryAssets" :key="asset.asset_id" :label="asset.name" :value="asset.asset_id" />
            </el-select>
          </el-form-item>
          <el-form-item label="模特资产">
            <el-select
              v-model="form.model_asset_id"
              placeholder="可选"
              filterable
              clearable
              popper-class="canvas-select-popper"
              :loading="libraryLoading"
              @visible-change="onLibraryVisible"
            >
              <el-option v-for="asset in modelLibraryAssets" :key="asset.asset_id" :label="asset.name" :value="asset.asset_id" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item label="可选图片">
          <el-checkbox-group v-model="form.extra_asset_types" class="canvas-extra-asset-options">
            <el-checkbox-button
              v-for="item in ECOMMERCE_EXTRA_ASSET_OPTIONS"
              :key="item.value"
              :label="item.value"
            >
              <span>{{ item.label }}</span>
              <small>{{ item.description }}</small>
            </el-checkbox-button>
          </el-checkbox-group>
        </el-form-item>

        <el-form-item label="参考图片">
          <el-upload
            class="reference-upload"
            drag
            multiple
            accept="image/*"
            :auto-upload="false"
            :show-file-list="false"
            :on-change="onImageChange"
          >
            <div class="upload-copy">
              <b>拖入参考图</b>
              <span>最多 4 张，每张 20MB 内</span>
            </div>
          </el-upload>
          <div v-if="form.reference_images.length" class="reference-strip">
            <div v-for="(img, index) in form.reference_images" :key="`${img.slice(0, 32)}-${index}`" class="reference-thumb">
              <img :src="img" :alt="`参考图 ${index + 1}`" />
              <button type="button" @click="removeImage(index)">移除</button>
            </div>
          </div>
        </el-form-item>

        <div class="primary-actions">
          <el-button @click="resetDraft">新建空白</el-button>
          <el-button type="primary" :loading="submitting" @click="submit">生成画布任务</el-button>
        </div>
      </el-form>

      <section class="history-panel">
        <div class="history-head">
          <div>
            <span>History</span>
            <b>历史任务</b>
          </div>
          <el-button :icon="Refresh" :loading="tasksLoading" @click="loadTasks">刷新</el-button>
        </div>
        <el-input v-model="taskKeyword" placeholder="搜索商品资料" clearable @keyup.enter="loadTasks">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <div class="history-list" v-loading="tasksLoading">
          <button
            v-for="task in tasks"
            :key="task.task_id"
            type="button"
            :class="{ active: activeTask?.task_id === task.task_id }"
            @click="openTask(task)"
          >
            <b>{{ compactText(task.output_json?.product_title || task.requirement || '未命名任务', 30) }}</b>
            <span>{{ task.platform_name || '未知平台' }} · {{ task.language_name || ecommerceLanguageName(task.language) }}</span>
            <small>{{ formatDateTime(task.created_at) }}</small>
            <em :class="statusTone[task.status] || 'muted'">{{ statusText[task.status] || task.status }}</em>
          </button>
          <el-empty v-if="!tasks.length && !tasksLoading" description="暂无历史任务" :image-size="70" />
        </div>
      </section>
    </aside>

    <main class="canvas-shell">
      <section class="canvas-topbar">
        <div>
          <span>Open Canvas</span>
          <h2>{{ activeTask ? heroTitle : '从商品判断开始，而不是从生成按钮开始' }}</h2>
        </div>
        <div class="task-status">
          <span :class="['status-pill', statusClass]">{{ statusLabel }}</span>
          <b>{{ activePercent }}%</b>
          <small>{{ canvasProgressText }} · 生成 {{ taskElapsed }} / 排队 {{ taskQueueElapsed }}</small>
        </div>
        <div class="topbar-actions">
          <el-button @click="resetDraft">新建</el-button>
          <el-button :disabled="!activeTask" :icon="Refresh" @click="refreshActiveTask">刷新</el-button>
          <el-button :disabled="!activeTask || isRunning" :loading="submitting" :icon="RefreshRight" @click="retryWholeTask">整套重试</el-button>
          <el-button v-if="isRunning" type="danger" plain :loading="canceling" :icon="Close" @click="cancelTask">中断</el-button>
          <el-button :disabled="!doneAssetCount" :loading="exporting" :icon="Download" @click="exportPoster">导出长图</el-button>
        </div>
      </section>

      <section
        :class="['canvas-viewport', `bg-${backgroundMode}`]"
        v-loading="detailLoading"
      >
        <div class="canvas-controls">
          <el-button :icon="ZoomOut" @click="zoom(-0.08)" />
          <el-button :icon="ZoomIn" @click="zoom(0.08)" />
          <el-button :icon="Grid" @click="toggleBackgroundMode">{{ backgroundMode === 'grid' ? '网格' : backgroundMode === 'dots' ? '点阵' : '空白' }}</el-button>
          <el-button @click="resetViewport">回到起点</el-button>
          <el-button @click="centerCanvas">全览</el-button>
          <el-button :icon="Operation" @click="autoArrangeCanvas">自动排列</el-button>
          <el-button :icon="Operation" @click="showMiniMap = !showMiniMap">{{ showMiniMap ? '隐藏小图' : '显示小图' }}</el-button>
        </div>

        <div class="canvas-hint">
          <span>滚轮缩放</span>
          <span>拖动画布移动</span>
          <span>拖拽节点编排</span>
          <span>双击进入详情</span>
        </div>

        <VueFlow
          id="ecommerce-canvas-flow"
          class="commerce-flow"
          v-model:nodes="flowNodeState"
          v-model:edges="flowEdgeState"
          :min-zoom="0.35"
          :max-zoom="1.5"
          :default-viewport="initialViewport"
          :fit-view-on-init="false"
          :nodes-draggable="true"
          :nodes-connectable="false"
          :edges-focusable="false"
          :elements-selectable="true"
          :pan-on-drag="true"
          :zoom-on-scroll="true"
          :zoom-on-pinch="true"
          @node-click="({ node }) => selectNode(node.id)"
          @node-double-click="({ node }) => openNodeDetail(node.data?.node)"
        >
          <svg class="edge-defs">
            <defs>
              <marker id="edge-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--edge-color)" />
              </marker>
            </defs>
          </svg>
          <template #node-commerce="{ data, selected }">
            <article
              :class="['canvas-node', data.node.kind, data.node.status, { selected }]"
              @click.stop="selectNode(data.node.id)"
            >
            <Handle
              type="target"
              :position="Position.Left"
              class="node-handle node-handle-left"
              :connectable="false"
            />
            <Handle
              type="source"
              :position="Position.Right"
              class="node-handle node-handle-right"
              :connectable="false"
            />
            <header>
              <span>{{ data.node.kind === 'asset' ? 'Asset' : data.node.kind }}</span>
              <i>{{ data.node.status === 'working' ? '生成中' : data.node.status === 'success' ? '已就绪' : data.node.status === 'failed' ? '需处理' : '等待' }}</i>
            </header>
            <strong>{{ data.node.title }}</strong>
            <p>{{ data.node.subtitle }}</p>
            <div class="node-meta">
              <span v-if="data.node.kind === 'asset' && data.node.asset">生成 {{ assetGenerateElapsed(data.node.asset) }}</span>
              <span v-if="data.node.kind === 'asset' && data.node.asset">排队 {{ assetQueueElapsed(data.node.asset) }}</span>
              <span v-else>{{ data.node.kind === 'brief' ? '输入节点' : data.node.kind === 'export' ? '交付节点' : '编排节点' }}</span>
            </div>
            <div v-if="data.node.kind === 'asset'" class="node-preview">
              <img v-if="assetHasImage(data.node.asset)" :src="thumbURL(data.node.asset!.url)" :alt="data.node.title" @error="markBrokenAsset(data.node.asset!)" />
              <span v-else>{{ data.node.asset?.error || '等待素材' }}</span>
            </div>
            <div v-else-if="data.node.kind === 'video' && data.node.asset" class="node-preview node-video-preview">
              <button v-if="assetCanPreview(data.node.asset)" type="button" @click="openAssetPreview(data.node.asset)">
                <el-icon><VideoPlay /></el-icon>
                <span>预览视频</span>
              </button>
              <span v-else>{{ data.node.asset.error || '等待视频' }}</span>
            </div>
            <div v-if="data.node.kind === 'asset' && data.node.asset" class="node-actions" @pointerdown.stop @click.stop>
              <button type="button" :disabled="!assetCanPreview(data.node.asset)" @click="openAssetPreview(data.node.asset)">预览</button>
              <button type="button" :disabled="!data.node.asset.url" @click="downloadAsset(data.node.asset)">下载</button>
              <button type="button" :disabled="isAssetWorking(data.node.asset.status) || retryingAssetID === data.node.asset.id" @click="retryAsset(data.node.asset)">
                {{ retryingAssetID === data.node.asset.id ? '提交中' : '重试' }}
              </button>
            </div>
            <div v-else-if="data.node.kind === 'video' && data.node.asset" class="node-actions" @pointerdown.stop @click.stop>
              <button type="button" :disabled="!assetCanPreview(data.node.asset)" @click="openAssetPreview(data.node.asset)">预览</button>
              <button type="button" :disabled="!data.node.asset.url" @click="downloadAsset(data.node.asset)">下载</button>
              <button type="button" :disabled="isAssetWorking(data.node.asset.status) || retryingAssetID === data.node.asset.id" @click="retryAsset(data.node.asset)">
                {{ retryingAssetID === data.node.asset.id ? '提交中' : '重试' }}
              </button>
            </div>
            <div v-else-if="data.node.kind === 'copy'" class="node-tags">
              <span v-for="tag in sellingPoints.slice(0, 3)" :key="tag">{{ tag }}</span>
            </div>
            <div v-else-if="data.node.kind === 'strategy'" class="node-tags">
              <span>{{ activePlatformName }}</span>
              <span>{{ activeLanguage }}</span>
            </div>
            <div v-else-if="data.node.kind === 'video_script' || data.node.kind === 'video' || data.node.kind === 'custom_video'" class="node-video-strip">
              <span>Hook</span>
              <span>Scene</span>
              <span>CTA</span>
            </div>
            <div class="node-quick-actions" @pointerdown.stop @click.stop>
              <button type="button" title="编辑节点" @click="openEditNode(data.node)">
                <el-icon><EditPen /></el-icon>
              </button>
              <button type="button" title="重试节点" :disabled="data.node.asset && isAssetWorking(data.node.asset.status)" @click="retryNode(data.node)">
                <el-icon><RefreshRight /></el-icon>
              </button>
              <button type="button" title="新增下游节点" @click="openDownstreamDialog(data.node.id)">
                <el-icon><Plus /></el-icon>
              </button>
            </div>
            </article>
          </template>
        </VueFlow>

        <div v-if="showMiniMap" class="canvas-minimap">
          <div class="minimap-head">
            <b>画布地图</b>
            <span>{{ canvasNodes.length }} 节点</span>
          </div>
          <button
            v-for="node in canvasNodes"
            :key="`mini-${node.id}`"
            type="button"
            :class="['minimap-node', node.status, { active: selectedNodeID === node.id }]"
            :style="{
              left: `${Math.max(8, Math.min(188, 8 + node.x / 11))}px`,
              top: `${Math.max(36, Math.min(126, 36 + node.y / 6))}px`,
              width: `${Math.max(18, node.w / 16)}px`,
              height: `${Math.max(12, node.h / 18)}px`,
            }"
            :title="node.title"
            @click="selectNode(node.id)"
          />
        </div>
      </section>
    </main>

    <aside class="inspector-panel">
      <template v-if="selectedNode">
        <header>
          <span>Inspector</span>
          <h2>{{ selectedNode.title }}</h2>
          <p>{{ selectedNode.subtitle }}</p>
        </header>

        <section class="inspector-card node-command-card">
          <b>节点操作</b>
          <div class="inspector-actions">
            <el-button :icon="EditPen" @click="openEditNode()">编辑</el-button>
            <el-button :icon="RefreshRight" :disabled="!!selectedAsset && isAssetWorking(selectedAsset.status)" @click="retrySelectedNode">重试</el-button>
            <el-button type="primary" :icon="Plus" @click="openDownstreamDialog()">新增下游</el-button>
          </div>
        </section>

        <section class="inspector-card canvas-summary-card">
          <b>画布状态</b>
          <div class="summary-grid">
            <span><strong>{{ selectedNodeIndex }}</strong><small>当前节点</small></span>
            <span><strong>{{ readyNodeCount }}</strong><small>已就绪</small></span>
            <span><strong>{{ workingNodeCount }}</strong><small>生成中</small></span>
            <span><strong>{{ failedNodeCount }}</strong><small>需处理</small></span>
          </div>
        </section>

        <section class="inspector-card">
          <b>运营判断</b>
          <template v-if="selectedNode.kind === 'brief'">
            <p>{{ activeTask?.requirement || form.requirement || '先描述商品、客群、价格、场景、平台限制。' }}</p>
          </template>
          <template v-else-if="selectedNode.kind === 'strategy'">
            <dl>
              <dt>平台</dt>
              <dd>{{ activePlatformName }}</dd>
              <dt>语言</dt>
              <dd>{{ activeLanguage }}</dd>
              <dt>模板</dt>
              <dd>{{ activeTask?.prompt_name || prompts.find((item) => item.id === form.prompt_template_id)?.name || '-' }}</dd>
              <dt>风格</dt>
              <dd>{{ activeTask?.style_name || styles.find((item) => item.id === form.style_template_id)?.name || '-' }}</dd>
            </dl>
          </template>
          <template v-else-if="selectedNode.kind === 'copy'">
            <h3>{{ heroTitle }}</h3>
            <p>{{ heroDescription }}</p>
            <strong v-if="priceCopy">{{ priceCopy }}</strong>
            <div v-if="marketingCopy.length" class="mini-tags">
              <span v-for="copy in marketingCopy.slice(0, 4)" :key="copy">{{ copy }}</span>
            </div>
          </template>
          <template v-else-if="selectedNode.kind === 'video_script' || selectedNode.kind === 'video' || selectedNode.kind === 'custom_video'">
            <h3>{{ selectedNode.title }}</h3>
            <p>{{ selectedNode.subtitle }}</p>
            <dl v-if="selectedNode.kind === 'video' && selectedAsset">
              <dt>状态</dt>
              <dd>{{ statusText[selectedAsset.status] || selectedAsset.status }}</dd>
              <dt>生成</dt>
              <dd>{{ assetGenerateElapsed(selectedAsset) }}</dd>
              <dt>排队</dt>
              <dd>{{ assetQueueElapsed(selectedAsset) }}</dd>
            </dl>
            <div class="video-script-list">
              <span v-for="line in videoScriptLines" :key="line">{{ line }}</span>
            </div>
          </template>
          <template v-else-if="selectedNode.kind === 'asset'">
            <p>{{ assetRole[selectedNode.assetType || ''] || '素材资产' }}</p>
            <dl v-if="selectedAsset">
              <dt>状态</dt>
              <dd>{{ statusText[selectedAsset.status] || selectedAsset.status }}</dd>
              <dt>生成</dt>
              <dd>{{ assetGenerateElapsed(selectedAsset) }}</dd>
              <dt>排队</dt>
              <dd>{{ assetQueueElapsed(selectedAsset) }}</dd>
              <dt>类型</dt>
              <dd>{{ assetText[selectedAsset.asset_type] || selectedAsset.asset_type }}</dd>
            </dl>
            <el-alert v-if="selectedAsset?.error" type="error" :closable="false" :title="selectedAsset.error" />
          </template>
          <template v-else-if="selectedNode.kind === 'detail'">
            <div v-if="detailSections.length" class="detail-list">
              <article v-for="section in detailSections.slice(0, 4)" :key="section.title || section.body">
                <b>{{ section.title || '详情模块' }}</b>
                <p>{{ section.body }}</p>
              </article>
            </div>
            <p v-else>等待详情页模块生成。</p>
          </template>
          <template v-else>
            <p>导出整套电商资产长图，用于复盘和交付。</p>
          </template>
        </section>

        <section class="inspector-card">
          <b>节点关系</b>
          <div class="relation-list">
            <span>上游</span>
            <p>{{ canvasEdges.filter((edge) => edge.to === selectedNode.id).map((edge) => canvasNodes.find((node) => node.id === edge.from)?.title).filter(Boolean).join('、') || '无' }}</p>
            <span>下游</span>
            <p>{{ canvasEdges.filter((edge) => edge.from === selectedNode.id).map((edge) => canvasNodes.find((node) => node.id === edge.to)?.title).filter(Boolean).join('、') || '无' }}</p>
          </div>
        </section>

        <section v-if="selectedNode.kind === 'asset' || selectedNode.kind === 'video'" class="inspector-card">
          <b>节点操作</b>
          <div class="inspector-actions">
            <el-button :disabled="!assetCanPreview(selectedAsset || undefined)" :icon="View" @click="openAssetPreview(selectedAsset || undefined)">预览</el-button>
            <el-button :disabled="!selectedAsset?.url" :icon="Download" @click="downloadAsset(selectedAsset || undefined)">下载</el-button>
          </div>
          <el-input
            v-if="selectedAsset && !isAssetWorking(selectedAsset.status)"
            v-model="retryPrompts[selectedAsset.id]"
            type="textarea"
            :rows="4"
            resize="none"
            maxlength="500"
            placeholder="追加重试要求，例如：更强调户外露营场景、提高高级感、加入促销倒计时。"
          />
          <el-button
            v-if="selectedAsset && !isAssetWorking(selectedAsset.status)"
            type="primary"
            :loading="retryingAssetID === selectedAsset.id"
            :icon="RefreshRight"
            @click="retryAsset(selectedAsset)"
          >
            重新生成该节点
          </el-button>
          <details v-if="selectedAsset?.prompt" class="prompt-box">
            <summary>生成提示词</summary>
            <pre>{{ selectedAsset.prompt }}</pre>
          </details>
        </section>

        <section class="inspector-card">
          <b>交付清单</b>
          <ul class="delivery-list">
            <li :class="{ done: !!activeTask?.requirement }">商品资料</li>
            <li :class="{ done: !!output?.product_title }">营销文案</li>
            <li :class="{ done: doneAssetCount > 0 }">图片资产 {{ doneAssetCount }}/{{ canvasImageAssetCount }}</li>
            <li :class="{ done: !!output?.product_title }">短视频脚本</li>
            <li :class="{ done: !!detailDoc }">详情页</li>
            <li :class="{ done: doneAssetCount > 0 }">长图导出</li>
          </ul>
          <div class="inspector-actions">
            <el-button :icon="CopyDocument" @click="copyNodeText">复制节点</el-button>
            <el-button :disabled="!doneAssetCount" :loading="exporting" :icon="Download" @click="exportPoster">导出长图</el-button>
          </div>
        </section>

        <section v-if="detailDoc" class="inspector-card detail-preview-card">
          <b>详情页预览</b>
          <iframe :srcdoc="detailDoc" sandbox="" />
        </section>
      </template>
    </aside>

    <ImagePreviewDialog
      v-if="previewAsset && !isVideoAsset(previewAsset)"
      v-model="previewVisible"
      :src="previewImageURL"
      :original-src="previewAsset.url"
      :title="assetText[previewAsset.asset_type] || previewAsset.asset_type"
      :alt="assetText[previewAsset.asset_type] || previewAsset.asset_type"
      :download-name="`${activeTask?.task_id || previewAsset.task_id || 'ecommerce'}-${previewAsset.asset_type}.png`"
      :loading="previewImageLoading"
    />

    <el-dialog
      v-if="previewAsset && isVideoAsset(previewAsset)"
      v-model="previewVisible"
      width="920px"
      append-to-body
      :title="assetText[previewAsset.asset_type] || previewAsset.asset_type"
    >
      <div class="preview-dialog" v-loading="previewImageLoading">
        <video v-if="previewImageURL" :src="previewImageURL" controls autoplay playsinline />
      </div>
      <template #footer>
        <el-button v-if="previewAsset" :icon="Download" @click="downloadAsset(previewAsset)">下载原始资产</el-button>
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="nodeDetailVisible"
      width="760px"
      append-to-body
      :title="selectedNode ? `${selectedNode.title} · 节点详情` : '节点详情'"
      class="canvas-node-dialog canvas-detail-dialog"
    >
      <div v-if="selectedNode" class="node-detail-dialog">
        <header>
          <span :class="['status-pill', selectedNode.status]">{{ selectedNode.status === 'working' ? '生成中' : selectedNode.status === 'success' ? '已就绪' : selectedNode.status === 'failed' ? '需处理' : '等待' }}</span>
          <p>{{ selectedNode.subtitle }}</p>
        </header>

        <section class="detail-block">
          <b>节点内容</b>
          <template v-if="selectedNode.kind === 'brief'">
            <p>{{ activeTask?.requirement || form.requirement || '先描述商品、客群、价格、场景、平台限制。' }}</p>
          </template>
          <template v-else-if="selectedNode.kind === 'strategy'">
            <dl>
              <dt>平台</dt>
              <dd>{{ activePlatformName }}</dd>
              <dt>语言</dt>
              <dd>{{ activeLanguage }}</dd>
              <dt>模板</dt>
              <dd>{{ activeTask?.prompt_name || prompts.find((item) => item.id === form.prompt_template_id)?.name || '-' }}</dd>
              <dt>风格</dt>
              <dd>{{ activeTask?.style_name || styles.find((item) => item.id === form.style_template_id)?.name || '-' }}</dd>
            </dl>
          </template>
          <template v-else-if="selectedNode.kind === 'copy'">
            <h3>{{ heroTitle }}</h3>
            <p>{{ heroDescription }}</p>
            <strong v-if="priceCopy">{{ priceCopy }}</strong>
            <div v-if="marketingCopy.length" class="mini-tags">
              <span v-for="copy in marketingCopy.slice(0, 6)" :key="copy">{{ copy }}</span>
            </div>
          </template>
          <template v-else-if="selectedNode.kind === 'video_script' || selectedNode.kind === 'video' || selectedNode.kind === 'custom_video'">
            <div v-if="selectedNode.kind === 'video' && selectedAsset" class="detail-asset-preview">
              <button v-if="assetCanPreview(selectedAsset)" class="asset-video-play" type="button" @click="openAssetPreview(selectedAsset)">
                <el-icon><VideoPlay /></el-icon>
                <span>播放视频</span>
              </button>
              <span v-else>{{ selectedAsset.error || '等待视频' }}</span>
            </div>
            <div class="video-script-list">
              <span v-for="line in videoScriptLines" :key="line">{{ line }}</span>
            </div>
          </template>
          <template v-else-if="selectedNode.kind === 'asset'">
            <p>{{ assetRole[selectedNode.assetType || ''] || '素材资产' }}</p>
            <div v-if="selectedAsset" class="detail-asset-preview">
              <img v-if="assetHasImage(selectedAsset)" :src="thumbURL(selectedAsset.url)" :alt="selectedNode.title" />
              <span v-else>{{ selectedAsset.error || '等待素材' }}</span>
            </div>
            <dl v-if="selectedAsset">
              <dt>状态</dt>
              <dd>{{ statusText[selectedAsset.status] || selectedAsset.status }}</dd>
              <dt>生成</dt>
              <dd>{{ assetGenerateElapsed(selectedAsset) }}</dd>
              <dt>排队</dt>
              <dd>{{ assetQueueElapsed(selectedAsset) }}</dd>
            </dl>
          </template>
          <template v-else-if="selectedNode.kind === 'detail'">
            <div v-if="detailSections.length" class="detail-list">
              <article v-for="section in detailSections.slice(0, 6)" :key="section.title || section.body">
                <b>{{ section.title || '详情模块' }}</b>
                <p>{{ section.body }}</p>
              </article>
            </div>
            <p v-else>等待详情页模块生成。</p>
          </template>
          <template v-else>
            <p>导出整套电商资产长图，用于复盘和交付。</p>
          </template>
        </section>

        <section class="detail-block">
          <b>节点关系</b>
          <div class="relation-list">
            <span>上游</span>
            <p>{{ canvasEdges.filter((edge) => edge.to === selectedNode.id).map((edge) => canvasNodes.find((node) => node.id === edge.from)?.title).filter(Boolean).join('、') || '无' }}</p>
            <span>下游</span>
            <p>{{ canvasEdges.filter((edge) => edge.from === selectedNode.id).map((edge) => canvasNodes.find((node) => node.id === edge.to)?.title).filter(Boolean).join('、') || '无' }}</p>
          </div>
        </section>
      </div>
      <template #footer>
        <el-button @click="openEditNode()">编辑</el-button>
        <el-button :disabled="!!selectedAsset && isAssetWorking(selectedAsset.status)" @click="retrySelectedNode">重试</el-button>
        <el-button v-if="selectedAsset" :disabled="!assetCanPreview(selectedAsset)" @click="openAssetPreview(selectedAsset)">预览资产</el-button>
        <el-button v-if="selectedAsset" :disabled="!selectedAsset.url" @click="downloadAsset(selectedAsset)">下载</el-button>
        <el-button type="primary" @click="nodeDetailVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="editDialogVisible" width="520px" append-to-body title="编辑画布节点" class="canvas-node-dialog">
      <el-form label-position="top">
        <el-form-item label="节点标题">
          <el-input v-model="editingNode.title" maxlength="60" show-word-limit />
        </el-form-item>
        <el-form-item label="节点说明">
          <el-input v-model="editingNode.subtitle" type="textarea" :rows="4" maxlength="300" show-word-limit resize="none" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveEditingNode">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="downstreamDialogVisible" width="540px" append-to-body title="新增下游节点" class="canvas-node-dialog">
      <el-form label-position="top">
        <el-form-item label="节点类型">
          <el-radio-group v-model="downstreamForm.kind">
            <el-radio-button label="custom_text">文本</el-radio-button>
            <el-radio-button label="custom_image">图片</el-radio-button>
            <el-radio-button label="custom_video">视频</el-radio-button>
            <el-radio-button label="custom_config">生成配置</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="节点标题">
          <el-input v-model="downstreamForm.title" maxlength="60" show-word-limit />
        </el-form-item>
        <el-form-item label="节点说明">
          <el-input v-model="downstreamForm.subtitle" type="textarea" :rows="4" maxlength="300" show-word-limit resize="none" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="downstreamDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="addDownstreamNode">添加到画布</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.ecommerce-canvas-page {
  --bg: #f5f7fb;
  --canvas-bg: #f8fafc;
  --panel: #ffffff;
  --panel-2: #f3f6fb;
  --node-bg: #ffffff;
  --node-bg-2: #f7f9fc;
  --line: rgba(15, 23, 42, 0.12);
  --grid-line: rgba(15, 23, 42, 0.06);
  --muted: rgba(51, 65, 85, 0.68);
  --soft: rgba(30, 41, 59, 0.82);
  --ink: #111827;
  --accent: #2563eb;
  --field-bg: #ffffff;
  --field-border: rgba(15, 23, 42, 0.14);
  --field-border-focus: rgba(37, 99, 235, 0.46);
  --field-placeholder: rgba(71, 85, 105, 0.48);
  --field-count-bg: rgba(15, 23, 42, 0.08);
  --edge-color: rgba(51, 65, 85, 0.44);
  --shadow: rgba(15, 23, 42, 0.12);
  --control-bg: rgba(255, 255, 255, 0.86);
  --button-bg: rgba(15, 23, 42, 0.04);
  --button-hover-bg: rgba(15, 23, 42, 0.08);
  --success-bg: #e8f5ee;
  --success-ink: #166534;
  --working-bg: #fff3df;
  --working-ink: #9a5a00;
  --failed-bg: #fee2e2;
  --failed-ink: #991b1b;
  --panel-pad: 16px;
  --control-radius: 10px;
  min-height: calc(100vh - 60px);
  display: grid;
  grid-template-columns: minmax(280px, 320px) minmax(0, 1fr);
  grid-template-rows: 1fr;
  background: var(--bg);
  color: var(--ink);
  overflow: hidden;
  position: relative;
}

:global(html.dark .ecommerce-canvas-page) {
  --bg: #0f172a;
  --canvas-bg: #101827;
  --panel: #111827;
  --panel-2: #162033;
  --node-bg: #162033;
  --node-bg-2: #111827;
  --line: rgba(148, 163, 184, 0.22);
  --grid-line: rgba(148, 163, 184, 0.1);
  --muted: #94a3b8;
  --soft: #cbd5e1;
  --ink: #f8fafc;
  --accent: #2563eb;
  --field-bg: #162033;
  --field-border: rgba(148, 163, 184, 0.24);
  --field-border-focus: rgba(37, 99, 235, 0.54);
  --field-placeholder: rgba(148, 163, 184, 0.68);
  --field-count-bg: rgba(148, 163, 184, 0.14);
  --edge-color: rgba(148, 163, 184, 0.58);
  --shadow: rgba(0, 0, 0, 0.45);
  --control-bg: rgba(17, 24, 39, 0.9);
  --button-bg: rgba(148, 163, 184, 0.12);
  --button-hover-bg: rgba(37, 99, 235, 0.18);
  --success-bg: rgba(22, 163, 74, 0.18);
  --success-ink: #bbf7d0;
  --working-bg: rgba(245, 158, 11, 0.18);
  --working-ink: #f8d28c;
  --failed-bg: rgba(239, 68, 68, 0.18);
  --failed-ink: #fecaca;
}

.left-panel,
.inspector-panel {
  min-height: calc(100vh - 60px);
  background: var(--panel);
  border-right: 1px solid var(--line);
  overflow-y: auto;
  padding: var(--panel-pad);
}

.inspector-panel {
  position: absolute;
  z-index: 12;
  top: 16px;
  right: 16px;
  bottom: 16px;
  width: min(320px, calc(100vw - 380px));
  min-width: 280px;
  min-height: 0;
  border-right: 0;
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: 0 24px 70px var(--shadow);
}

.left-panel header,
.inspector-panel header,
.canvas-topbar > div:first-child {
  span {
    display: block;
    color: var(--muted);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  h1,
  h2 {
    margin: 8px 0;
    color: var(--ink);
    font-size: 22px;
    line-height: 1.1;
  }

  p {
    margin: 0;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.55;
  }
}

.canvas-form {
  margin-top: 14px;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

:deep(.el-form-item__label) {
  color: var(--soft);
  margin-bottom: 5px;
  font-size: 13px;
  line-height: 1.2;
}

:deep(.el-form-item) {
  margin-bottom: 12px;
}

:deep(.el-textarea__inner),
:deep(.el-input__wrapper),
:deep(.el-select__wrapper) {
  border: 1px solid var(--field-border);
  border-radius: var(--control-radius);
  background: var(--field-bg);
  box-shadow: none;
}

:deep(.el-textarea__inner) {
  min-height: 92px !important;
  padding: 9px 10px;
  color: var(--ink);
  font-size: 13px;
  line-height: 1.45;
}

:deep(.el-input__wrapper),
:deep(.el-select__wrapper) {
  min-height: 34px;
  padding: 0 10px;
}

:deep(.el-input__inner),
:deep(.el-select__selected-item) {
  color: var(--ink);
  font-size: 13px;
}

:deep(.el-input__inner::placeholder),
:deep(.el-textarea__inner::placeholder) {
  color: var(--field-placeholder);
}

:deep(.el-input__wrapper.is-focus),
:deep(.el-select__wrapper.is-focused),
:deep(.el-textarea__inner:focus) {
  border-color: var(--field-border-focus);
  box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.08);
}

:deep(.el-input__count),
:deep(.el-textarea .el-input__count) {
  right: 10px;
  bottom: 6px;
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--field-count-bg);
  color: var(--soft);
  font-size: 10px;
}

.canvas-extra-asset-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.canvas-extra-asset-options :deep(.el-checkbox-button) {
  width: 100%;
}

.canvas-extra-asset-options :deep(.el-checkbox-button__inner) {
  display: grid;
  gap: 4px;
  width: 100%;
  min-height: 64px;
  padding: 9px 10px;
  border-radius: 8px;
  border-left: 1px solid var(--el-border-color);
  text-align: left;
  white-space: normal;
}

.canvas-extra-asset-options :deep(.el-checkbox-button__inner span) {
  color: var(--ink);
  font-size: 13px;
  font-weight: 800;
  line-height: 1.2;
}

.canvas-extra-asset-options :deep(.el-checkbox-button__inner small) {
  color: var(--muted);
  font-size: 11px;
  font-weight: 600;
  line-height: 1.2;
}

:deep(.el-select .el-select__caret),
:deep(.el-input__suffix),
:deep(.el-input__prefix) {
  color: var(--muted);
}

.reference-upload {
  width: 100%;

  :deep(.el-upload),
  :deep(.el-upload-dragger) {
    width: 100%;
  }

  :deep(.el-upload-dragger) {
    background: var(--field-bg);
    border-color: var(--field-border);
    border-radius: 12px;
    padding: 14px;
  }

  :deep(.el-upload-dragger:hover) {
    border-color: var(--field-border-focus);
  }
}

.upload-copy {
  display: grid;
  gap: 4px;
  color: var(--ink);
  font-size: 13px;

  span {
    color: var(--muted);
    font-size: 11px;
  }
}

.reference-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 10px;
}

.reference-thumb {
  width: 58px;
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
  background: var(--panel-2);

  img {
    width: 100%;
    height: 48px;
    display: block;
    object-fit: cover;
  }

  button {
    width: 100%;
    height: 22px;
    border: 0;
    color: var(--ink);
    background: var(--button-bg);
    cursor: pointer;
  }
}

.primary-actions,
.inspector-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.primary-actions .el-button {
  flex: 1;
}

.history-panel {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
}

.history-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;

  span {
    display: block;
    color: var(--muted);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  b {
    display: block;
    margin-top: 3px;
    font-size: 16px;
  }
}

.history-list {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}

.history-list button {
  position: relative;
  min-height: 68px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--panel-2);
  color: var(--ink);
  text-align: left;
  cursor: pointer;

  b,
  span,
  small {
    display: block;
  }

  span,
  small {
    margin-top: 4px;
    color: var(--muted);
  }

  em {
    position: absolute;
    right: 12px;
    top: 12px;
    padding: 3px 8px;
    border-radius: 999px;
    background: var(--button-bg);
    color: var(--ink);
    font-style: normal;
    font-size: 11px;
  }

  &.active {
    border-color: var(--accent);
    background: var(--button-hover-bg);
  }
}

.canvas-shell {
  min-width: 0;
  min-height: calc(100vh - 60px);
  display: grid;
  grid-template-rows: auto 1fr;
  background:
    linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px),
    var(--canvas-bg);
  background-size: 32px 32px;
}

.canvas-topbar {
  min-height: 104px;
  display: grid;
  grid-template-columns: minmax(340px, 1fr) auto;
  gap: 10px 18px;
  align-items: center;
  padding: 14px 360px 14px 22px;
  border-bottom: 1px solid var(--line);
  background: var(--panel);
}

.canvas-topbar > div:first-child {
  min-width: 0;

  h2 {
    max-width: 880px;
    font-size: 26px;
    overflow-wrap: anywhere;
  }
}

.task-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  color: var(--muted);
  justify-self: end;
  min-width: 0;

  b {
    color: var(--ink);
  }
}

.status-pill {
  padding: 4px 9px;
  border-radius: 999px;
  background: var(--button-bg);
  color: var(--ink);

  &.success { background: var(--success-bg); color: var(--success-ink); }
  &.working { background: var(--working-bg); color: var(--working-ink); }
  &.failed { background: var(--failed-bg); color: var(--failed-ink); }
}

.topbar-actions {
  display: flex;
  flex-wrap: wrap;
  grid-column: 1 / -1;
  gap: 8px;
  min-width: 0;

  :deep(.el-button) {
    margin-left: 0;
  }
}

.canvas-viewport {
  position: relative;
  overflow: hidden;
  overscroll-behavior: contain;
  background: var(--canvas-bg);

  &.bg-grid {
    background:
      linear-gradient(var(--grid-line) 1px, transparent 1px),
      linear-gradient(90deg, var(--grid-line) 1px, transparent 1px),
      radial-gradient(circle at 72% 18%, color-mix(in srgb, var(--accent) 8%, transparent), transparent 26%),
      var(--canvas-bg);
    background-size: 32px 32px, 32px 32px, 100% 100%;
  }

  &.bg-dots {
    background:
      radial-gradient(circle, var(--grid-line) 1px, transparent 1px),
      radial-gradient(circle at 72% 18%, color-mix(in srgb, var(--accent) 8%, transparent), transparent 26%),
      var(--canvas-bg);
    background-size: 22px 22px, 100% 100%;
  }

  &.bg-blank {
    background:
      radial-gradient(circle at 72% 18%, color-mix(in srgb, var(--accent) 8%, transparent), transparent 26%),
      var(--canvas-bg);
  }
}

.canvas-controls {
  position: absolute;
  z-index: 5;
  left: 20px;
  top: 20px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  max-width: calc(100% - 380px);
  padding: 6px;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--control-bg);
  backdrop-filter: blur(14px);
  box-shadow: 0 18px 50px var(--shadow);

  :deep(.el-button) {
    border-color: var(--line);
    background: var(--button-bg);
    color: var(--ink);
  }
}

.canvas-hint {
  position: absolute;
  z-index: 5;
  left: 18px;
  bottom: 18px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-width: calc(100% - 380px);

  span {
    padding: 5px 8px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--control-bg);
    color: var(--muted);
    font-size: 11px;
  }
}

.canvas-node {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  height: 100%;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: linear-gradient(145deg, var(--node-bg), var(--node-bg-2));
  color: var(--ink);
  text-align: left;
  cursor: grab;
  box-shadow: 0 24px 60px var(--shadow);
  user-select: none;
  box-sizing: border-box;
  transition: border-color 0.16s ease, box-shadow 0.16s ease, transform 0.16s ease;

  &:hover {
    border-color: var(--field-border-focus);
    transform: translateY(-1px);
  }

  header {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    color: var(--muted);
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  strong {
    font-size: 19px;
    line-height: 1.1;
  }

  p {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
    line-height: 1.45;
  }

  &.selected {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 18%, transparent), 0 24px 60px var(--shadow);
  }

  &.success {
    background: linear-gradient(145deg, var(--success-bg), color-mix(in srgb, var(--success-bg) 82%, var(--panel-2)));
    color: var(--success-ink);

    p,
    header {
      color: color-mix(in srgb, var(--success-ink) 64%, transparent);
    }
  }

  &.working {
    border-color: var(--field-border-focus);
  }

  &.failed {
    border-color: color-mix(in srgb, var(--failed-ink) 32%, transparent);
  }

  &.video_script,
  &.video,
  &.custom_video {
    border-color: rgba(120, 190, 255, 0.48);
    background:
      linear-gradient(135deg, color-mix(in srgb, var(--accent) 10%, transparent), transparent 36%),
      linear-gradient(145deg, var(--node-bg), var(--node-bg-2));
  }
}

.node-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  span {
    padding: 3px 7px;
    border-radius: 999px;
    background: var(--button-bg);
    color: currentColor;
    font-size: 11px;
    opacity: 0.72;
  }
}

.commerce-flow {
  width: 100%;
  height: 100%;
  background: transparent;
}

.commerce-flow :deep(.vue-flow__renderer) {
  cursor: grab;
}

.commerce-flow :deep(.vue-flow__renderer:active) {
  cursor: grabbing;
}

.commerce-flow :deep(.vue-flow__node) {
  border: 0;
  background: transparent;
  outline: 0;
}

.commerce-flow :deep(.node-handle) {
  width: 10px;
  height: 10px;
  border: 1px solid var(--field-border-focus);
  background: var(--panel);
  opacity: 0;
  pointer-events: none;
}

.commerce-flow :deep(.vue-flow__node:hover .node-handle),
.commerce-flow :deep(.vue-flow__node.selected .node-handle) {
  opacity: 0.9;
}

.commerce-flow :deep(.node-handle-left) {
  left: -5px;
}

.commerce-flow :deep(.node-handle-right) {
  right: -5px;
}

.commerce-flow :deep(.vue-flow__node.dragging .canvas-node) {
  cursor: grabbing;
  border-color: var(--accent);
  box-shadow: 0 28px 78px var(--shadow);
}

.commerce-flow :deep(.vue-flow__edge-path) {
  stroke: color-mix(in srgb, var(--ink) 44%, transparent);
  stroke-linecap: round;
  stroke-linejoin: round;
  filter: drop-shadow(0 0 8px color-mix(in srgb, var(--ink) 12%, transparent));
}

.commerce-flow :deep(.vue-flow__edge.animated path) {
  stroke-dasharray: 8 10;
}

.edge-defs {
  position: absolute;
  width: 0;
  height: 0;
  pointer-events: none;
}

.canvas-minimap {
  position: absolute;
  right: 352px;
  bottom: 20px;
  z-index: 6;
  width: 196px;
  height: 136px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background:
    linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px),
    var(--control-bg);
  background-size: 18px 18px;
  backdrop-filter: blur(16px);
  box-shadow: 0 24px 70px var(--shadow);
}

.minimap-head {
  display: flex;
  justify-content: space-between;
  padding: 10px 12px;
  color: var(--muted);
  font-size: 11px;

  b {
    color: var(--ink);
  }
}

.minimap-node {
  position: absolute;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--button-bg);
  cursor: pointer;

  &.success {
    background: var(--success-bg);
  }

  &.working {
    background: var(--working-bg);
  }

  &.failed {
    background: var(--failed-bg);
  }

  &.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 20%, transparent);
  }
}

.node-actions {
  display: flex;
  gap: 6px;
  margin-top: auto;

  button {
    flex: 1;
    height: 26px;
    border: 1px solid currentColor;
    border-radius: 999px;
    background: var(--button-bg);
    color: inherit;
    cursor: pointer;
    font-size: 11px;

    &:disabled {
      cursor: not-allowed;
      opacity: 0.38;
    }
  }
}

.node-quick-actions {
  display: flex;
  gap: 6px;
  margin-top: 2px;

  button {
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    border: 1px solid currentColor;
    border-radius: 999px;
    background: var(--button-bg);
    color: inherit;
    cursor: pointer;
    opacity: 0.76;

    &:hover {
      opacity: 1;
      background: var(--button-hover-bg);
    }

    &:disabled {
      cursor: not-allowed;
      opacity: 0.35;
    }
  }
}

.node-preview {
  flex: 1;
  min-height: 72px;
  display: grid;
  place-items: center;
  border-radius: 12px;
  background: var(--canvas-bg);
  overflow: hidden;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  span {
    color: var(--muted);
  }
}

.node-tags,
.mini-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  span {
    padding: 4px 7px;
    border: 1px solid currentColor;
    border-radius: 999px;
    font-size: 11px;
    opacity: 0.78;
  }
}

.node-video-strip,
.video-script-list {
  display: grid;
  gap: 6px;
}

.node-video-strip {
  grid-template-columns: repeat(3, 1fr);

  span {
    min-height: 28px;
    display: grid;
    place-items: center;
    border: 1px solid rgba(120, 190, 255, 0.38);
    border-radius: 10px;
    background: rgba(120, 190, 255, 0.08);
    color: #d8ecff;
    font-size: 11px;
  }
}

.video-script-list span {
  padding: 8px 10px;
  border: 1px solid rgba(120, 190, 255, 0.24);
  border-radius: 12px;
  background: rgba(120, 190, 255, 0.06);
  color: var(--muted);
  line-height: 1.55;
}

.inspector-card {
  margin-top: 12px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--panel-2);

  > b {
    display: block;
    margin-bottom: 9px;
    font-size: 14px;
  }

  p {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.55;
    overflow-wrap: anywhere;
  }

  h3 {
    margin: 0 0 10px;
    font-size: 17px;
  }

  strong {
    display: block;
    margin-top: 8px;
  }

  dl {
    display: grid;
    grid-template-columns: 60px 1fr;
    gap: 6px 10px;
    margin: 0;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    color: var(--ink);
    min-width: 0;
    overflow-wrap: anywhere;
  }
}

.canvas-summary-card {
  background: linear-gradient(145deg, var(--panel-2), var(--panel));
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;

  span {
    display: grid;
    gap: 4px;
    padding: 8px 6px;
    border-radius: 10px;
    background: var(--button-bg);
    text-align: center;
  }

  strong {
    margin: 0;
    color: var(--ink);
    font-size: 17px;
  }

  small {
    color: var(--muted);
  }
}

.relation-list {
  display: grid;
  gap: 6px;

  span {
    color: var(--soft);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  p {
    margin: 0 0 4px;
    padding: 8px 10px;
    border-radius: 10px;
    background: var(--button-bg);
    overflow-wrap: anywhere;
  }
}

.detail-list {
  display: grid;
  gap: 8px;

  article {
    padding: 10px;
    border-radius: 12px;
    background: var(--button-bg);
  }
}

.delivery-list {
  display: grid;
  gap: 6px;
  padding: 0;
  margin: 0;
  list-style: none;

  li {
    padding: 7px 9px;
    border-radius: 10px;
    background: var(--button-bg);
    color: var(--muted);

    &.done {
      background: var(--success-bg);
      color: var(--success-ink);
    }
  }
}

.prompt-box {
  margin-top: 12px;

  summary {
    cursor: pointer;
    color: var(--soft);
  }

  pre {
    max-height: 220px;
    overflow: auto;
    white-space: pre-wrap;
    color: var(--muted);
  }
}

.detail-preview-card iframe {
  width: 100%;
  height: 320px;
  border: 0;
  border-radius: 14px;
  background: #fff;
}

.preview-dialog {
  min-height: 360px;
  display: grid;
  place-items: center;
  background: var(--canvas-bg);

  img {
    max-width: 100%;
    max-height: 72vh;
    display: block;
  }

  video {
    width: min(100%, 860px);
    max-height: 72vh;
    display: block;
    border-radius: 14px;
    background: #000;
  }
}

.node-detail-dialog {
  display: grid;
  gap: 10px;

  > header {
    display: grid;
    gap: 8px;
    padding: 12px;
    border: 1px solid var(--line);
    border-radius: 16px;
    background: linear-gradient(135deg, var(--button-bg), transparent);

    p {
      margin: 0;
      color: var(--muted);
      line-height: 1.65;
    }
  }
}

.detail-block {
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--panel-2);

  > b {
    display: block;
    margin-bottom: 8px;
  }

  p {
    margin: 0;
    color: var(--muted);
    font-size: 12px;
    line-height: 1.55;
  }

  h3 {
    margin: 0 0 10px;
    font-size: 18px;
  }

  dl {
    display: grid;
    grid-template-columns: 60px 1fr;
    gap: 6px 10px;
    margin: 10px 0 0;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    color: var(--ink);
  }
}

.detail-asset-preview {
  min-height: 180px;
  display: grid;
  place-items: center;
  margin-top: 12px;
  border-radius: 16px;
  background: var(--canvas-bg);
  overflow: hidden;

  img {
    max-width: 100%;
    max-height: 360px;
    display: block;
    object-fit: contain;
  }

  span {
    color: var(--muted);
  }
}

.asset-video-play,
.node-video-preview button {
  display: grid;
  place-items: center;
  gap: 8px;
  width: 100%;
  min-height: 96px;
  border: 0;
  background: transparent;
  color: var(--accent);
  cursor: pointer;

  .el-icon {
    width: 36px;
    height: 36px;
    padding: 9px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--button-bg);
    font-size: 18px;
  }

  span {
    color: inherit;
    font-weight: 700;
  }
}

:deep(.canvas-node-dialog) {
  border: 1px solid var(--line);
  border-radius: 18px;
  background: var(--panel);
  box-shadow: 0 28px 90px var(--shadow);
  overflow: hidden;

  .el-dialog__header {
    padding: 22px 24px 8px;
    margin: 0;
  }

  .el-dialog__title {
    color: var(--ink);
    font-size: 18px;
    font-weight: 800;
  }

  .el-dialog__headerbtn {
    top: 14px;
    right: 14px;
    width: 34px;
    height: 34px;
    border-radius: 999px;
  }

  .el-dialog__close {
    color: var(--muted);
  }

  .el-dialog__body {
    padding: 12px 24px 18px;
  }

  .el-dialog__footer {
    padding: 0 24px 22px;
  }

  .el-form-item {
    margin-bottom: 18px;
  }

  .el-form-item__label {
    margin-bottom: 8px;
    color: var(--soft);
    font-weight: 700;
  }

  .el-input__wrapper,
  .el-textarea__inner {
    border: 1px solid var(--field-border);
    border-radius: 12px;
    background: var(--field-bg);
    box-shadow: none;
  }

  .el-input__wrapper {
    min-height: 42px;
    padding: 0 12px;
  }

  .el-textarea__inner {
    min-height: 108px !important;
    padding: 12px;
    color: var(--ink);
    line-height: 1.55;
  }

  .el-input__inner {
    color: var(--ink);
  }

  .el-input__wrapper.is-focus,
  .el-textarea__inner:focus {
    border-color: var(--field-border-focus);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
  }

  .el-input__count,
  .el-textarea .el-input__count {
    right: 10px;
    bottom: 8px;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--field-count-bg);
    color: var(--soft);
    font-size: 11px;
  }

  .el-radio-group {
    gap: 8px;
  }

  .el-radio-button__inner {
    border: 1px solid var(--field-border) !important;
    border-radius: 999px !important;
    background: var(--field-bg);
    color: var(--soft);
    box-shadow: none !important;
  }

  .el-radio-button.is-active .el-radio-button__inner {
    background: var(--accent);
    color: var(--panel);
  }

  .el-button {
    border-radius: 999px;
  }

  .el-button:not(.el-button--primary) {
    border-color: var(--line);
    background: transparent;
    color: var(--ink);
  }

  .el-button--primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--panel);
  }
}

@media (max-width: 1500px) {
  .ecommerce-canvas-page {
    grid-template-columns: 286px minmax(0, 1fr);
  }

  .canvas-topbar {
    padding-right: 22px;
  }

  .inspector-panel {
    position: static;
    grid-column: 1 / -1;
    width: auto;
    min-width: 0;
    max-height: 420px;
    display: block;
    border-radius: 0;
    border-left: 0;
    border-right: 0;
    border-top: 1px solid var(--line);
    box-shadow: none;
  }

  .canvas-controls,
  .canvas-hint {
    max-width: calc(100% - 40px);
  }

  .canvas-minimap {
    right: 20px;
  }
}

@media (max-width: 1180px) {
  .canvas-topbar {
    grid-template-columns: 1fr;
    align-items: start;
  }

  .task-status {
    justify-self: start;
  }
}

@media (max-width: 900px) {
  .ecommerce-canvas-page {
    height: auto;
    min-height: calc(100vh - 60px);
    display: block;
    overflow: visible;
  }

  .left-panel,
  .inspector-panel {
    min-height: auto;
    max-height: none;
  }

  .canvas-shell {
    min-height: 640px;
  }

  .canvas-topbar {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .form-grid,
  .summary-grid {
    grid-template-columns: 1fr;
  }

  .primary-actions,
  .inspector-actions,
  .topbar-actions {
    flex-direction: column;

    :deep(.el-button) {
      width: 100%;
    }
  }

  .canvas-controls {
    left: 12px;
    right: 12px;
    top: 12px;
    max-width: none;
  }

  .canvas-hint,
  .canvas-minimap {
    display: none;
  }
}
</style>

<style lang="scss">
.canvas-node-dialog {
  --canvas-dialog-bg: #ffffff;
  --canvas-dialog-panel: #f3f6fb;
  --canvas-dialog-ink: #111827;
  --canvas-dialog-muted: rgba(51, 65, 85, 0.68);
  --canvas-dialog-soft: rgba(30, 41, 59, 0.82);
  --canvas-dialog-line: rgba(15, 23, 42, 0.12);
  --canvas-dialog-field: #ffffff;
  --canvas-dialog-accent: #2563eb;
  --canvas-dialog-shadow: rgba(15, 23, 42, 0.18);
  border: 1px solid var(--canvas-dialog-line) !important;
  background: var(--canvas-dialog-bg) !important;
  box-shadow: 0 28px 90px var(--canvas-dialog-shadow) !important;

  .el-dialog__title,
  .el-input__inner,
  .el-textarea__inner {
    color: var(--canvas-dialog-ink) !important;
  }

  .el-dialog__close,
  .el-input__count {
    color: var(--canvas-dialog-muted) !important;
  }

  .el-form-item__label {
    color: var(--canvas-dialog-soft) !important;
  }

  .el-input__wrapper,
  .el-textarea__inner,
  .el-radio-button__inner {
    border-color: var(--canvas-dialog-line) !important;
    background: var(--canvas-dialog-field) !important;
  }

  .el-radio-button.is-active .el-radio-button__inner,
  .el-button--primary {
    border-color: var(--canvas-dialog-accent) !important;
    background: var(--canvas-dialog-accent) !important;
    color: #ffffff !important;
  }

  .node-detail-dialog > header,
  .detail-block {
    border-color: var(--canvas-dialog-line) !important;
    background: var(--canvas-dialog-panel) !important;
  }

  .detail-asset-preview {
    background: var(--canvas-dialog-panel) !important;
  }
}

html.dark .canvas-node-dialog {
  --canvas-dialog-bg: #111827;
  --canvas-dialog-panel: #162033;
  --canvas-dialog-ink: #f8fafc;
  --canvas-dialog-muted: #94a3b8;
  --canvas-dialog-soft: #cbd5e1;
  --canvas-dialog-line: rgba(148, 163, 184, 0.22);
  --canvas-dialog-field: #162033;
  --canvas-dialog-accent: #2563eb;
  --canvas-dialog-shadow: rgba(0, 0, 0, 0.68);

  .el-radio-button.is-active .el-radio-button__inner,
  .el-button--primary {
    color: #ffffff !important;
  }
}

.canvas-select-popper {
  border: 1px solid rgba(15, 23, 42, 0.12) !important;
  background: #ffffff !important;
  box-shadow: 0 22px 70px rgba(15, 23, 42, 0.16) !important;

  .el-popper__arrow::before {
    border-color: rgba(15, 23, 42, 0.12) !important;
    background: #ffffff !important;
  }

  .el-select-dropdown {
    background: #ffffff;
  }

  .el-select-dropdown__item {
    color: rgba(15, 23, 42, 0.76);
  }

  .el-select-dropdown__item.is-hovering,
  .el-select-dropdown__item:hover {
    background: rgba(37, 99, 235, 0.08);
    color: #111827;
  }

  .el-select-dropdown__item.is-selected {
    color: #2563eb;
    background: rgba(37, 99, 235, 0.1);
  }

  .el-select-dropdown__empty {
    color: rgba(71, 85, 105, 0.58);
  }
}

html.dark .canvas-select-popper {
  border-color: rgba(148, 163, 184, 0.22) !important;
  background: #111827 !important;
  box-shadow: 0 22px 70px rgba(0, 0, 0, 0.62) !important;

  .el-popper__arrow::before {
    border-color: rgba(148, 163, 184, 0.22) !important;
    background: #111827 !important;
  }

  .el-select-dropdown {
    background: #111827;
  }

  .el-select-dropdown__item {
    color: #cbd5e1;
  }

  .el-select-dropdown__item.is-hovering,
  .el-select-dropdown__item:hover {
    background: rgba(37, 99, 235, 0.14);
    color: #f8fafc;
  }

  .el-select-dropdown__item.is-selected {
    color: #5b8def;
    background: rgba(37, 99, 235, 0.18);
  }

  .el-select-dropdown__empty {
    color: #64748b;
  }
}
</style>
