<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ArrowDown, Close, CopyDocument, Document, Download, MoreFilled, Refresh, RefreshRight, VideoPlay, View } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import type { UploadFile } from 'element-plus/es/components/upload/index.mjs'
import {
  ECOMMERCE_EXTRA_ASSET_OPTIONS,
  ECOMMERCE_LANGUAGES,
  cancelEcommerceTask,
  createEcommerceTask,
  deleteEcommerceTask,
  ecommerceLanguageName,
  generateEcommerceVideo,
  getEcommerceOptions,
  getEcommerceTask,
  listEcommerceLibraryAssets,
  listEcommerceTasks,
  retryEcommerceTask,
  retryEcommerceAsset,
  type EcommerceAsset,
  type EcommerceLibraryAsset,
  type EcommercePlatform,
  type EcommercePromptTemplate,
  type EcommerceStyleTemplate,
  type EcommerceTask,
} from '@/api/ecommerce'
import { formatCredit, formatDateTime } from '@/utils/format'
import { getCachedImageObjectURL, peekCachedImageObjectURL } from '@/utils/imageCache'
import ImagePreviewDialog from '@/components/ImagePreviewDialog.vue'

const MAX_IMAGES = 4
const MAX_IMAGE_MB = 20
const POLL_INTERVAL = 2500
const TASK_PAGE_SIZE = 5
const HISTORY_INITIAL_DELAY = 700
const HISTORY_PAGE_DELAY = 360
const LAST_TASK_STORAGE_KEY = 'gpt2api.ecommerce-v2.last-task-id'
const TASK_COMPACT_TAG_SCORE = 27

const optionsLoading = ref(false)
const detailLoading = ref(false)
const submitting = ref(false)
const canceling = ref(false)
const exporting = ref(false)
const downloadingAll = ref(false)
const generatingVideo = ref(false)
const libraryAssetsLoading = ref(false)
const libraryAssetsLoaded = ref(false)
const tasksLoading = ref(false)
const historyDeferred = ref(true)
const taskHistoryExpanded = ref(true)
const taskMoreVisible = ref(false)
const tasksTotal = ref(0)
const retryingTaskID = ref('')
const retryingAssetID = ref(0)
const deletingTaskID = ref('')
const taskKeyword = ref('')
const polling = ref<number | null>(null)
const pollingTaskID = ref('')
const ticker = ref<number | null>(null)
const historyDelayTimer = ref<number | null>(null)
const nowTs = ref(Date.now())
const previewVisible = ref(false)
const detailVisible = ref(false)
const promptVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
const promptAsset = ref<EcommerceAsset | null>(null)
const previewImageURL = ref('')
const previewImageLoading = ref(false)
const brokenAssetIDs = ref<Set<number>>(new Set())
const brokenTaskThumbIDs = ref<Set<number>>(new Set())
const retryPanelOpenIDs = ref<Set<number>>(new Set())
const retryPrompts = ref<Record<number, string>>({})
type WorkbenchSection = 'progress' | 'copy' | 'tags' | 'specs' | 'detail'
const openSections = reactive<Record<WorkbenchSection, boolean>>({
  progress: true,
  copy: true,
  tags: false,
  specs: false,
  detail: false,
})

const platforms = ref<EcommercePlatform[]>([])
const prompts = ref<EcommercePromptTemplate[]>([])
const styles = ref<EcommerceStyleTemplate[]>([])
const productLibraryAssets = ref<EcommerceLibraryAsset[]>([])
const modelLibraryAssets = ref<EcommerceLibraryAsset[]>([])
const tasks = ref<EcommerceTask[]>([])
const activeTask = ref<EcommerceTask | null>(null)

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
  running: 'warning',
  success: 'success',
  failed: 'danger',
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

const assetOrder = [
  'title_image',
  'main_image',
  'white_image',
  'detail_image',
  'price_image',
  ...ECOMMERCE_EXTRA_ASSET_OPTIONS.map((item) => item.value),
  'product_video',
]
const baseImageAssetCount = 5

const output = computed<Record<string, any>>(() => activeTask.value?.output_json || {})
const productInfo = computed<Record<string, any>>(() => output.value?.product_info || {})
const priceInfo = computed<Record<string, any>>(() => output.value?.price_info || {})
const imageSpecs = computed<Record<string, any>>(() => output.value?.image_specs || {})
const imageTextPlans = computed<Record<string, any>>(() => output.value?.image_text_plans || {})
const assets = computed(() => activeTask.value?.assets || [])
const currentAssets = computed(() => latestAssetsByType(assets.value))
const activeExtraAssetTypes = computed(() => activeTask.value ? activeTask.value.extra_asset_types || [] : form.extra_asset_types)
const requiredImageAssetTypes = computed(() => assetOrder.filter((type) => {
  if (isVideoAsset(type)) return false
  if (ECOMMERCE_EXTRA_ASSET_OPTIONS.some((item) => item.value === type)) return activeExtraAssetTypes.value.includes(type)
  return true
}))
const taskLooksComplete = computed(() => currentAssets.value.length > 0 && currentAssetsReady(currentAssets.value))
const displayTaskStatus = computed(() => (taskLooksComplete.value ? 'success' : activeTask.value?.status || ''))
const running = computed(() => ['queued', 'running'].includes(displayTaskStatus.value))
const hasMoreTasks = computed(() => tasks.value.length < tasksTotal.value)
const currentPlatform = computed(() => platforms.value.find((p) => p.id === form.platform_id))
const activePlatform = computed(() => platforms.value.find((p) => p.id === activeTask.value?.platform_id))
const selectedLanguage = computed(() => ecommerceLanguageName(form.language || currentPlatform.value?.language))
const selectedProductAsset = computed(() => productLibraryAssets.value.find((asset) => asset.asset_id === form.product_asset_id))
const selectedModelAsset = computed(() => modelLibraryAssets.value.find((asset) => asset.asset_id === form.model_asset_id))
const activeLanguage = computed(() => activeTask.value?.language_name || ecommerceLanguageName(activeTask.value?.language || activePlatform.value?.language || form.language))
const activePercent = computed(() => taskLooksComplete.value ? 100 : activeTask.value?.progress || 0)
const taskElapsed = computed(() => activeTask.value ? generationElapsed(activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const taskQueueElapsed = computed(() => activeTask.value ? queueElapsed(activeTask.value.created_at, activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const hasWorkingCurrentAsset = computed(() => currentAssets.value.some((asset) => isAssetWorking(asset.status)))
const visibleAssets = computed(() => currentAssets.value.filter((asset) => !isVideoAsset(asset)).sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type)))
const videoAsset = computed(() => currentAssets.value.find((asset) => isVideoAsset(asset)) || null)
const doneAssetCount = computed(() => currentAssets.value.filter((asset) => assetIsReady(asset)).length)
const expectedAssetCount = computed(() => requiredImageAssetTypes.value.length + (currentAssets.value.some((asset) => isVideoAsset(asset)) ? 1 : 0))
const totalAssetCount = computed(() => Math.max(currentAssets.value.length, expectedAssetCount.value))
const assetMetricText = computed(() => activeTask.value ? `${doneAssetCount.value}/${totalAssetCount.value}` : `0/${baseImageAssetCount + form.extra_asset_types.length}`)
const videoStatusLabel = computed(() => {
  if (!activeTask.value) return '等待任务'
  if (!videoAsset.value) return '未生成'
  return statusText[videoAsset.value.status] || videoAsset.value.status
})
const videoStatusTone = computed(() => videoAsset.value ? statusTone[videoAsset.value.status] || 'muted' : 'muted')
const videoPercent = computed(() => {
  const asset = videoAsset.value
  if (!asset) return 0
  if (asset.status === 'success') return 100
  if (asset.status === 'failed' || asset.status === 'canceled') return 0
  if (asset.status === 'queued') return 0
  return Math.max(1, Math.min(99, Number(asset.progress || 0)))
})
const videoWindowText = computed(() => {
  const asset = videoAsset.value
  if (!asset) return '等待生成视频'
  if (asset.status === 'queued') return '视频排队中'
  if (asset.status === 'running') return `视频生成中 ${videoPercent.value}%`
  if (asset.status === 'failed') return '视频生成失败'
  if (asset.status === 'canceled') return '视频已中断'
  return '等待生成视频'
})
const videoProgressSteps = computed(() => {
  const asset = videoAsset.value
  const status = asset?.status || ''
  const failed = status === 'failed' || status === 'canceled'
  const stepIndex = !asset ? 0 : status === 'queued' ? 1 : status === 'running' ? 2 : status === 'success' ? 3 : failed ? 2 : 0
  return [
    { key: 'submitted', label: '提交', done: !!asset, active: false },
    { key: 'queued', label: '排队中', done: stepIndex > 1, active: stepIndex === 1 && !failed },
    { key: 'running', label: '生成中', done: stepIndex > 2, active: stepIndex === 2 && !failed, value: `${videoPercent.value}%` },
    { key: 'done', label: '完成', done: status === 'success', active: stepIndex === 3, failed },
  ]
})
const heroTitle = computed(() => output.value?.product_title || productInfo.value?.canonical_title || '等待生成商品标题')
const heroDescription = computed(() => output.value?.description || productInfo.value?.core_value || '提交任务后，这里会展示平台文案、图片资产和交付状态。')
const priceCopy = computed(() => output.value?.price_copy || priceInfo.value?.price_text || priceInfo.value?.promotion_text || '')
const taskReferenceImages = computed<string[]>(() => Array.isArray(activeTask.value?.reference_images) ? activeTask.value!.reference_images! : [])
const whiteAnchorAsset = computed(() => currentAssets.value.find((asset) => asset.asset_type === 'white_image' && assetHasImage(asset)))
const promptReferenceGroups = computed(() => {
  const groups: Array<{ title: string; images: Array<{ url: string; href: string; label: string }> }> = []
  if (taskReferenceImages.value.length) {
    groups.push({
      title: '原始参考图',
      images: taskReferenceImages.value.map((img, index) => ({
        url: img,
        href: img,
        label: `原始参考图 ${index + 1}`,
      })),
    })
  }
  const anchor = whiteAnchorAsset.value
  if (promptAsset.value && promptAsset.value.asset_type !== 'white_image' && anchor?.url) {
    groups.push({
      title: '白底锚点图',
      images: [{
        url: thumbURL(anchor.url, 500),
        href: anchor.url,
        label: '非白底图生图参考',
      }],
    })
  }
  return groups
})
const promptReferenceCount = computed(() => promptReferenceGroups.value.reduce((sum, group) => sum + group.images.length, 0))
const promptDialogTitle = computed(() => {
  if (!promptAsset.value) return '参考图与提示词'
  return `${assetText[promptAsset.value.asset_type] || promptAsset.value.asset_type} · 参考图与提示词`
})
const marketingCopy = computed<string[]>(() => asStringArray(output.value?.marketing_copy))
const sellingPoints = computed<string[]>(() => asStringArray(productInfo.value?.selling_points))
const keySpecs = computed<string[]>(() => uniqueStrings([
  ...asStringArray(productInfo.value?.key_specs),
  ...asStringArray(productInfo.value?.specs),
  ...asStringArray(output.value?.key_specs),
  ...asStringArray(output.value?.specs),
  ...Object.values(imageTextPlans.value).flatMap((plan: any) => asStringArray(plan?.specs)),
]).slice(0, 12))
const detailSections = computed<Array<{ title: string; body: string }>>(() => (
  Array.isArray(output.value?.detail_sections)
    ? output.value.detail_sections.filter((it: any) => it?.title || it?.body)
    : []
))
const quickTags = computed(() => uniqueStrings([
  ...sellingPoints.value,
  ...keySpecs.value,
  ...marketingCopy.value,
]).slice(0, 8))
const copyPreview = computed(() => {
  const lines = [
    heroTitle.value,
    sellingPoints.value.slice(0, 2).join(' / ') || heroDescription.value,
    priceCopy.value,
  ].filter(Boolean)
  return lines.join(' · ')
})
const tagPreview = computed(() => quickTags.value.slice(0, 4).join(' / ') || '暂无关键词')
const specsPreview = computed(() => keySpecs.value.slice(0, 3).join(' / ') || '暂无规格')
const detailPreview = computed(() => detailSections.value.slice(0, 2).map((item) => item.title || item.body).filter(Boolean).join(' / ') || '暂无详情结构')

const detailDoc = computed(() => {
  if (!activeTask.value?.output_html) return ''
  const body = sanitizeDetailHTML(withThumbImages(activeTask.value.output_html, 500))
  const reset = `html,body,.stage,.ecommerce-detail-preview,.ecommerce-detail-preview *{filter:none!important;-webkit-filter:none!important;mix-blend-mode:normal!important;opacity:1!important}.ecommerce-detail-preview img{display:block;width:100%;max-width:100%;height:auto;object-fit:contain}`
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:#fff;color:#111827;color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif}.stage{width:100%;max-width:860px;margin:0 auto;padding:24px;overflow-x:hidden}.ecommerce-detail-preview{width:100%;max-width:100%;overflow-x:hidden}.copy-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px}.detail-head,.detail-section{max-width:100%;overflow-wrap:anywhere}${reset}</style></head><body><main class="stage">${body}</main><style>${reset}</style></body></html>`
})

const progressSteps = computed(() => {
  const status = displayTaskStatus.value
  const hasCopy = !!output.value?.product_title
  const hasAllAssets = totalAssetCount.value > 0 && doneAssetCount.value >= totalAssetCount.value
  const hasDetail = !!detailDoc.value
  const failed = status === 'failed'
  const canceled = status === 'canceled'
  const success = status === 'success'
  const active: number = failed || canceled ? -1 : success ? 5 : hasDetail || hasAllAssets ? 4 : hasCopy ? 3 : activeTask.value ? 2 : 0
  return [
    { key: 'brief', label: '商品资料', done: active > 1 || success, active: active === 1 },
    { key: 'copy', label: '文案生成', done: active > 2 || success, active: active === 2 },
  { key: 'image', label: '素材生成', done: hasAllAssets || success, active: active === 3 },
    { key: 'detail', label: '详情页生成', done: hasDetail || success, active: active === 4 },
    { key: 'deliver', label: '交付完成', done: success, active: active === 5, failed: failed || canceled },
  ]
})

const deliveryItems = computed(() => [
  { key: 'brief', label: '商品资料', value: activeTask.value?.requirement ? '完成' : '-', done: !!activeTask.value?.requirement },
  { key: 'copy', label: `文案输出（${activeLanguage.value}）`, value: output.value?.product_title ? '完成' : '-', done: !!output.value?.product_title },
  { key: 'assets', label: '素材资产', value: assetMetricText.value, done: doneAssetCount.value > 0 && doneAssetCount.value === totalAssetCount.value },
  { key: 'detail', label: '详情页预览', value: detailDoc.value ? '可预览' : '-', done: !!detailDoc.value },
  { key: 'poster', label: '长图导出', value: doneAssetCount.value > 0 ? '可导出' : '-', done: doneAssetCount.value > 0 },
])

const statusHeadline = computed(() => {
  const status = displayTaskStatus.value
  if (!activeTask.value) return '等待任务'
  if (status === 'success') return '交付可用'
  if (status === 'failed') return '任务失败'
  if (status === 'canceled') return '已中断'
  if (status === 'queued') return '等待生成'
  return '生成中'
})

function asStringArray(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((it) => String(it || '').trim()).filter(Boolean)
  if (typeof value === 'string') {
    return value
      .split(/[\n\r;；、,，]/)
      .map((it) => it.trim())
      .filter(Boolean)
  }
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap(asStringArray)
  }
  return []
}

function uniqueStrings(items: string[]) {
  return Array.from(new Set(items.map((it) => it.trim()).filter(Boolean)))
}

function toggleSection(section: WorkbenchSection) {
  openSections[section] = !openSections[section]
}

function applySectionDefaults(task: EcommerceTask | null) {
  const status = task?.status || ''
  openSections.progress = status === 'queued' || status === 'running'
  openSections.copy = true
  openSections.tags = false
  openSections.specs = false
  openSections.detail = false
}

function shortTaskID(taskID: string) {
  if (!taskID) return '--'
  if (taskID.length <= 18) return taskID
  return `${taskID.slice(0, 10)}...${taskID.slice(-6)}`
}

function compactText(text?: string | null, limit = 84) {
  const value = String(text || '').replace(/\s+/g, ' ').trim()
  if (!value) return ''
  return value.length > limit ? `${value.slice(0, limit)}...` : value
}

function taskRequirementPreview(task: EcommerceTask) {
  return compactText(task.requirement, 92) || '暂无商品资料'
}

function taskTitle(task: EcommerceTask) {
  return task.output_json?.product_title || task.requirement || '未命名任务'
}

function taskTagItems(task: EcommerceTask) {
  return uniqueStrings([
    task.platform_name || '未知平台',
    task.language_name || ecommerceLanguageName(task.language),
    task.prompt_name || '默认模板',
    task.style_name || '默认风格',
  ])
}

function taskTagScore(tag: string) {
  return Array.from(tag).reduce((sum, char) => sum + (char.charCodeAt(0) <= 255 ? 0.55 : 1), 2)
}

function taskTagView(task: EcommerceTask, limit = TASK_COMPACT_TAG_SCORE) {
  const visible: string[] = []
  const hidden: string[] = []
  let score = 0
  for (const tag of taskTagItems(task)) {
    const next = taskTagScore(tag) + (visible.length ? 1 : 0)
    if (score + next <= limit) {
      visible.push(tag)
      score += next
    } else {
      hidden.push(tag)
    }
  }
  return { visible, hidden }
}

function taskTagsTitle(task: EcommerceTask) {
  return taskTagItems(task).join(' / ')
}

async function showTaskMorePopover() {
  const deferred = historyDeferred.value
  cancelScheduledHistoryLoad()
  historyDeferred.value = false
  if (deferred || (!tasks.value.length && tasksTotal.value !== 0)) {
    await loadTasks(true)
  }
}

async function openTaskFromHistory(task: EcommerceTask) {
  taskMoreVisible.value = false
  await openTask(task)
}

function toggleRetryPanel(assetID: number) {
  const next = new Set(retryPanelOpenIDs.value)
  if (next.has(assetID)) next.delete(assetID)
  else next.add(assetID)
  retryPanelOpenIDs.value = next
}

function assetRank(type: string) {
  const idx = assetOrder.indexOf(type)
  return idx === -1 ? 99 : idx
}

function isAssetWorking(status: string) {
  return status === 'queued' || status === 'running'
}

function latestAssetsByType(list: EcommerceAsset[]) {
  const map = new Map<string, EcommerceAsset>()
  for (const asset of list) {
    const prev = map.get(asset.asset_type)
    if (!prev || asset.id >= prev.id) map.set(asset.asset_type, asset)
  }
  return [...map.values()].sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type))
}

function currentAssetsReady(list: EcommerceAsset[]) {
  if (!list.length) return false
  if (list.some((asset) => isAssetWorking(asset.status) || asset.status === 'failed' || asset.status === 'canceled')) return false
  return requiredImageAssetTypes.value.every((type) => list.some((asset) => asset.asset_type === type && assetIsReady(asset)))
    && !list.some((asset) => isVideoAsset(asset) && !assetIsReady(asset))
}

function canRetryTask(status?: string) {
  return status === 'failed' || status === 'canceled'
}

function isVideoAsset(assetOrType: EcommerceAsset | string) {
  const type = typeof assetOrType === 'string' ? assetOrType : assetOrType.asset_type
  return type === 'product_video'
}

function assetIsReady(asset: EcommerceAsset) {
  return !!asset.url && asset.status === 'success'
}

function assetHasImage(asset: EcommerceAsset) {
  if (isVideoAsset(asset)) return false
  return !!asset.url && asset.status === 'success' && !brokenAssetIDs.value.has(asset.id)
}

function assetCanPreview(asset: EcommerceAsset) {
  return isVideoAsset(asset) ? assetIsReady(asset) : assetHasImage(asset)
}

function canGenerateVideo() {
  return !!activeTask.value && !running.value && !hasWorkingCurrentAsset.value && !generatingVideo.value
}

function errorMessage(err: unknown, fallback: string) {
  const anyErr = err as any
  return anyErr?.response?.data?.message || anyErr?.message || fallback
}

function markBrokenAsset(asset: EcommerceAsset) {
  const next = new Set(brokenAssetIDs.value)
  next.add(asset.id)
  brokenAssetIDs.value = next
}

function taskThumbnailAsset(task: EcommerceTask) {
  return [...(task.assets || [])]
    .sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type))
    .find((asset) => !isVideoAsset(asset) && !!asset.url && asset.status === 'success' && !brokenTaskThumbIDs.value.has(asset.id))
}

function markBrokenTaskThumb(asset: EcommerceAsset) {
  const next = new Set(brokenTaskThumbIDs.value)
  next.add(asset.id)
  brokenTaskThumbIDs.value = next
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

function assetGenerateElapsed(asset: EcommerceAsset) {
  return generationElapsed(asset.started_at, asset.finished_at, isAssetWorking(asset.status) && !!asset.started_at)
}

function assetQueueElapsed(asset: EcommerceAsset) {
  return queueElapsed(asset.created_at, asset.started_at, asset.finished_at, asset.status === 'queued')
}

function videoElapsedText() {
  if (!videoAsset.value) return '0秒'
  return assetGenerateElapsed(videoAsset.value)
}

function videoQueueText() {
  if (!videoAsset.value) return '0秒'
  return assetQueueElapsed(videoAsset.value)
}

function imageSpecText(assetType: string) {
  if (isVideoAsset(assetType)) return '短视频'
  const spec = imageSpecs.value?.[assetType] || {}
  return [spec.size, spec.aspect_ratio].filter(Boolean).join(' · ') || '1024 x 1024'
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

async function loadLibraryAssets() {
  if (libraryAssetsLoaded.value || libraryAssetsLoading.value) return
  libraryAssetsLoading.value = true
  try {
    const [products, models] = await Promise.all([
      listEcommerceLibraryAssets({ kind: 'product', limit: 100 }, true),
      listEcommerceLibraryAssets({ kind: 'model', limit: 100 }, true),
    ])
    productLibraryAssets.value = products.items || []
    modelLibraryAssets.value = models.items || []
  } catch (err) {
    console.warn('ecommerce library assets unavailable:', err)
    productLibraryAssets.value = []
    modelLibraryAssets.value = []
  } finally {
    libraryAssetsLoaded.value = true
    libraryAssetsLoading.value = false
  }
}

function onLibrarySelectVisible(visible: boolean) {
  if (visible) loadLibraryAssets()
}

async function loadTasks(reset = true) {
  if (tasksLoading.value) return
  tasksLoading.value = true
  try {
    const offset = reset ? 0 : tasks.value.length
    const keyword = taskKeyword.value.trim()
    const data = await listEcommerceTasks({ limit: TASK_PAGE_SIZE, offset, keyword: keyword || undefined })
    const items = data.items || []
    tasksTotal.value = data.total || (reset ? items.length : tasks.value.length + items.length)
    if (reset) {
      tasks.value = items
      return
    }
    const exists = new Set(tasks.value.map((task) => task.task_id))
    tasks.value = [...tasks.value, ...items.filter((task) => !exists.has(task.task_id))]
  } finally {
    tasksLoading.value = false
  }
}

async function searchTasks() {
  cancelScheduledHistoryLoad()
  historyDeferred.value = false
  await loadTasks(true)
}

function clearTaskSearch() {
  if (!taskKeyword.value) return
  cancelScheduledHistoryLoad()
  taskKeyword.value = ''
  historyDeferred.value = false
  loadTasks(true)
}

async function loadMoreTasks() {
  if (!hasMoreTasks.value || tasksLoading.value) return
  await wait(HISTORY_PAGE_DELAY)
  await loadTasks(false)
}

function onTaskListScroll(event: Event) {
  const el = event.currentTarget as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 24) {
    loadMoreTasks()
  }
}

function onTaskListWheel(event: WheelEvent) {
  if (event.deltaY <= 0) return
  const el = event.currentTarget as HTMLElement
  if (el.scrollHeight <= el.clientHeight + 1) {
    loadMoreTasks()
  }
}

async function initialize() {
  optionsLoading.value = true
  try {
    await loadOptions()
    optionsLoading.value = false
    await loadInitialTaskDetail()
    scheduleHistoryLoad()
  } catch (err) {
    console.error('ecommerce workbench initialize failed:', err)
    ElMessage.error('电商工作台初始化失败')
  } finally {
    optionsLoading.value = false
  }
}

async function loadInitialTaskDetail() {
  const cachedTaskID = readLastTaskID()
  if (cachedTaskID) {
    if (await openTaskByID(cachedTaskID)) {
      return
    }
    clearLastTaskID()
  }
  detailLoading.value = true
  try {
    const data = await listEcommerceTasks({ limit: 1, offset: 0 })
    const first = data.items?.[0]
    if (!first) return
    tasksTotal.value = data.total || 1
    tasks.value = [first]
    await openTaskByID(first.task_id)
  } catch (err) {
    console.warn('load initial ecommerce task detail failed:', err)
  } finally {
    detailLoading.value = false
  }
}

function scheduleHistoryLoad() {
  cancelScheduledHistoryLoad()
  historyDeferred.value = true
  historyDelayTimer.value = window.setTimeout(async () => {
    historyDeferred.value = false
    await loadTasks(true)
  }, HISTORY_INITIAL_DELAY)
}

function cancelScheduledHistoryLoad() {
  if (historyDelayTimer.value) window.clearTimeout(historyDelayTimer.value)
  historyDelayTimer.value = null
}

function wait(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function readLastTaskID() {
  try {
    return localStorage.getItem(LAST_TASK_STORAGE_KEY) || ''
  } catch {
    return ''
  }
}

function rememberLastTaskID(taskID: string) {
  if (!taskID) return
  try {
    localStorage.setItem(LAST_TASK_STORAGE_KEY, taskID)
  } catch {
    // ignore storage failures
  }
}

function clearLastTaskID() {
  try {
    localStorage.removeItem(LAST_TASK_STORAGE_KEY)
  } catch {
    // ignore storage failures
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

function clearBrief() {
  form.requirement = ''
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
    rememberLastTaskID(task.task_id)
    brokenAssetIDs.value = new Set()
    ElMessage.success('任务已进入生成队列')
    historyDeferred.value = false
    await loadTasks()
    startPolling(task.task_id)
  } catch (err) {
    console.error('create ecommerce task failed:', err)
  } finally {
    submitting.value = false
  }
}

async function openTask(task: EcommerceTask) {
  await openTaskByID(task.task_id)
}

async function openTaskByID(taskID: string): Promise<boolean> {
  stopPolling()
  detailLoading.value = true
  try {
    const fresh = await getEcommerceTask(taskID)
    activeTask.value = fresh
    rememberLastTaskID(fresh.task_id)
    brokenAssetIDs.value = new Set()
    if (isAssetWorking(fresh.status) || latestAssetsByType(fresh.assets || []).some((asset) => isAssetWorking(asset.status))) startPolling(fresh.task_id)
    return true
  } catch (err) {
    console.error('open ecommerce task failed:', err)
    return false
  } finally {
    detailLoading.value = false
  }
}

async function cancelTask() {
  if (!activeTask.value || !running.value) return
  const confirmed = await ElMessageBox.confirm('中断后，已完成资产会保留，未完成资产停止继续更新。', '取消任务', {
    type: 'warning',
    confirmButtonText: '取消任务',
    cancelButtonText: '继续生成',
  }).catch(() => false)
  if (!confirmed || !activeTask.value) return
  canceling.value = true
  try {
    activeTask.value = await cancelEcommerceTask(activeTask.value.task_id)
    stopPolling()
    await loadTasks()
    ElMessage.success('已取消任务')
  } catch (err) {
    console.error('cancel ecommerce task failed:', err)
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
    const next = new Set(brokenAssetIDs.value)
    next.delete(asset.id)
    brokenAssetIDs.value = next
    retryPrompts.value = { ...retryPrompts.value, [asset.id]: '' }
    startPolling(fresh.task_id)
    ElMessage.success(isVideoAsset(asset) ? '已重新提交视频生成' : '已重新提交图片生成')
  } catch (err) {
    console.error('retry ecommerce asset failed:', err)
    ElMessage.error(errorMessage(err, isVideoAsset(asset) ? '视频重新生成失败' : '图片重新生成失败'))
  } finally {
    retryingAssetID.value = 0
  }
}

async function generateVideo() {
  if (!activeTask.value || !canGenerateVideo()) return
  generatingVideo.value = true
  try {
    await generateEcommerceVideo(activeTask.value.task_id)
    const fresh = await getEcommerceTask(activeTask.value.task_id)
    activeTask.value = fresh
    startPolling(fresh.task_id)
    ElMessage.success('视频生成已提交')
  } catch (err) {
    console.error('generate ecommerce video failed:', err)
    ElMessage.error(errorMessage(err, '视频生成提交失败'))
  } finally {
    generatingVideo.value = false
  }
}

async function retryTask(task = activeTask.value, ev?: Event) {
  ev?.stopPropagation()
  if (!task || !canRetryTask(task.status)) return
  retryingTaskID.value = task.task_id
  try {
    const fresh = await retryEcommerceTask(task.task_id)
    activeTask.value = fresh
    brokenAssetIDs.value = new Set()
    brokenTaskThumbIDs.value = new Set()
    retryPanelOpenIDs.value = new Set()
    retryPrompts.value = {}
    await loadTasks()
    startPolling(fresh.task_id)
    ElMessage.success('已重新提交整单生成')
  } catch (err) {
    console.error('retry ecommerce task failed:', err)
  } finally {
    retryingTaskID.value = ''
  }
}

async function deleteTask(task: EcommerceTask, ev?: Event) {
  ev?.stopPropagation()
  if (!task || deletingTaskID.value) return
  const confirmed = await ElMessageBox.confirm(
    `删除后任务会从列表隐藏，后台保留删除时间和删除人信息。${isAssetWorking(task.status) ? '当前任务正在生成，会先中断再删除。' : ''}`,
    '删除任务',
    {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    },
  ).catch(() => false)
  if (!confirmed) return
  deletingTaskID.value = task.task_id
  try {
    await deleteEcommerceTask(task.task_id)
    const wasActive = activeTask.value?.task_id === task.task_id
    tasks.value = tasks.value.filter((item) => item.task_id !== task.task_id)
    tasksTotal.value = Math.max(0, tasksTotal.value - 1)
    if (wasActive) {
      stopPolling()
      activeTask.value = null
      clearLastTaskID()
      if (tasks.value[0]) await openTask(tasks.value[0])
    }
    if (!tasks.value.length && tasksTotal.value > 0) await loadTasks(true)
    ElMessage.success('任务已删除')
  } catch (err) {
    console.error('delete ecommerce task failed:', err)
  } finally {
    deletingTaskID.value = ''
  }
}

function startPolling(taskID: string) {
  stopPolling()
  pollingTaskID.value = taskID
  polling.value = window.setInterval(async () => {
    try {
      const fresh = await getEcommerceTask(taskID)
      if (pollingTaskID.value !== taskID) return
      if (activeTask.value?.task_id === taskID) activeTask.value = fresh
      if (!isAssetWorking(fresh.status) && !latestAssetsByType(fresh.assets || []).some((asset) => isAssetWorking(asset.status))) {
        stopPolling()
        await loadTasks()
      }
    } catch (err) {
      console.error('poll ecommerce task failed:', err)
    }
  }, POLL_INTERVAL)
}

function stopPolling() {
  if (polling.value) window.clearInterval(polling.value)
  polling.value = null
  pollingTaskID.value = ''
}

async function openAssetPreview(asset: EcommerceAsset) {
  if (!assetCanPreview(asset)) return
  if (isVideoAsset(asset)) {
    previewAsset.value = asset
    previewImageURL.value = asset.url
    previewImageLoading.value = false
    previewVisible.value = true
    return
  }
  const sourceURL = thumbURL(asset.url, 500)
  previewAsset.value = asset
  previewImageURL.value = peekCachedImageObjectURL(sourceURL)
  previewVisible.value = true
  if (previewImageURL.value) {
    previewImageLoading.value = false
    return
  }

  previewImageLoading.value = true
  try {
    const objectURL = await getCachedImageObjectURL(sourceURL)
    if (previewAsset.value?.id === asset.id) previewImageURL.value = objectURL
  } catch (err) {
    console.error('load ecommerce preview image failed:', err)
    if (previewAsset.value?.id === asset.id) previewImageURL.value = sourceURL
  } finally {
    if (previewAsset.value?.id === asset.id) previewImageLoading.value = false
  }
}

function openDetailPreview() {
  if (!detailDoc.value) {
    ElMessage.warning('暂无详情页预览')
    return
  }
  detailVisible.value = true
}

function openPromptInspect(asset: EcommerceAsset) {
  promptAsset.value = asset
  promptVisible.value = true
}

async function copyAssetPrompt(asset = promptAsset.value) {
  const prompt = asset?.prompt?.trim()
  if (!prompt) {
    ElMessage.warning('暂无提示词')
    return
  }
  await navigator.clipboard.writeText(prompt)
  ElMessage.success('提示词已复制')
}

function assetFileName(asset: EcommerceAsset) {
  const taskID = activeTask.value?.task_id || asset.task_id || 'ecommerce'
  const ext = isVideoAsset(asset) ? 'mp4' : 'png'
  return `${taskID}-${asset.asset_type || 'asset'}.${ext}`
}

async function downloadAsset(asset: EcommerceAsset) {
  if (!assetIsReady(asset)) return
  try {
    const res = await fetch(asset.url)
    if (!res.ok) throw new Error(`download failed: ${res.status}`)
    const blob = await res.blob()
    downloadBlob(blob, assetFileName(asset))
  } catch (err) {
    console.error('download ecommerce asset failed:', err)
    window.open(asset.url, '_blank', 'noopener,noreferrer')
  }
}

async function downloadAllAssets() {
  const readyAssets = currentAssets.value.filter(assetIsReady).sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type))
  if (!readyAssets.length) {
    ElMessage.warning('暂无可下载素材资产')
    return
  }
  downloadingAll.value = true
  try {
    for (const asset of readyAssets) {
      await downloadAsset(asset)
      await new Promise((resolve) => window.setTimeout(resolve, 120))
    }
  } finally {
    downloadingAll.value = false
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

function downloadCanvas(canvas: HTMLCanvasElement, filename: string) {
  canvas.toBlob((blob) => {
    if (!blob) {
      ElMessage.error('导出失败')
      return
    }
    downloadBlob(blob, filename)
  }, 'image/png')
}

async function exportPoster() {
  if (!activeTask.value) return
  const imageAssets = requiredImageAssetTypes.value
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
    const title = String(heroTitle.value)
    const description = String(heroDescription.value)
    const price = String(priceCopy.value)
    const lineHeight = 42
    const imageHeight = loaded.reduce((sum, item) => {
      const imgWidth = item.img.width || contentWidth
      const imgHeight = item.img.height || contentWidth
      return sum + Math.round(imgHeight * contentWidth / imgWidth) + 86 + blockGap
    }, 0)
    const height = 360 + imageHeight + marketingCopy.value.length * 42

    canvas.width = width
    canvas.height = height
    ctx.fillStyle = '#f3efe8'
    ctx.fillRect(0, 0, width, height)
    ctx.fillStyle = '#101828'
    ctx.font = '700 52px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    ctx.fillText(title.slice(0, 26), padding, 104)
    ctx.font = '400 28px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    wrapCanvasText(ctx, description, padding, 160, contentWidth, lineHeight, 3)
    if (price) {
      ctx.fillStyle = '#d45b2c'
      ctx.font = '700 34px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(price.slice(0, 34), padding, 300)
    }
    let y = 350
    for (const item of loaded) {
      ctx.fillStyle = '#101828'
      ctx.font = '700 30px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(assetText[item.asset.asset_type] || item.asset.asset_type, padding, y)
      y += 32
      const imgWidth = item.img.width || contentWidth
      const imgHeight = item.img.height || contentWidth
      const drawHeight = Math.round(imgHeight * contentWidth / imgWidth)
      ctx.drawImage(item.img, padding, y, contentWidth, drawHeight)
      y += drawHeight + blockGap
    }
    downloadCanvas(canvas, `${activeTask.value.task_id}-电商资产长图.png`)
    ElMessage.success('长图已生成')
  } catch (err) {
    console.error('export ecommerce poster failed:', err)
    ElMessage.error('长图导出失败')
  } finally {
    exporting.value = false
  }
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

async function copyOutput() {
  if (!activeTask.value) return
  const lines = [
    heroTitle.value,
    heroDescription.value,
    priceCopy.value,
    ...marketingCopy.value,
    ...sellingPoints.value,
    ...keySpecs.value,
  ].filter(Boolean)
  if (!lines.length) {
    ElMessage.warning('暂无可复制文案')
    return
  }
  await navigator.clipboard.writeText(lines.join('\n'))
  ElMessage.success('文案已复制')
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

watch(
  () => activeTask.value ? `${activeTask.value.task_id}:${activeTask.value.status}` : '',
  () => applySectionDefaults(activeTask.value),
)

onBeforeUnmount(() => {
  stopPolling()
  cancelScheduledHistoryLoad()
  if (ticker.value) window.clearInterval(ticker.value)
})
</script>

<template>
  <div class="commerce-workbench">
    <main class="task-column">
      <section class="current-task surface" v-loading="detailLoading">
        <template v-if="!activeTask">
          <div class="empty-current">
            <span class="kicker">当前任务</span>
            <h2>{{ detailLoading ? '正在打开最近任务' : '创建任务后在这里查看生成详情' }}</h2>
            <p>{{ detailLoading ? '历史列表会稍后加载，先为你准备最近一次交付。' : '也可以从右侧近期任务打开历史记录。' }}</p>
          </div>
        </template>

        <template v-else>
          <header class="task-header">
            <div>
              <span class="kicker">当前任务</span>
              <h2>{{ heroTitle }}</h2>
            </div>
            <div class="header-actions">
              <el-button :icon="Refresh" @click="openTask(activeTask)">刷新</el-button>
              <el-button
                v-if="canRetryTask(activeTask.status)"
                :loading="retryingTaskID === activeTask.task_id"
                :icon="RefreshRight"
                @click="retryTask(activeTask)"
              >
                整体重试
              </el-button>
              <el-button v-if="running" type="danger" :loading="canceling" @click="cancelTask">取消任务</el-button>
            </div>
          </header>

          <div class="task-meta">
            <el-tooltip :content="activeTask.task_id" placement="top">
              <span>任务 ID：{{ shortTaskID(activeTask.task_id) }}</span>
            </el-tooltip>
            <span>创建时间：{{ formatDateTime(activeTask.created_at) }}</span>
            <span>平台：{{ activeTask.platform_name || '未知平台' }}</span>
            <span>模板：{{ activeTask.prompt_name || '默认模板' }}</span>
            <span>风格：{{ activeTask.style_name || '默认风格' }}</span>
          </div>

          <section class="progress-panel collapsible-card">
            <div class="collapsible-top">
              <button
                class="collapse-head"
                type="button"
                :aria-expanded="openSections.progress"
                @click="toggleSection('progress')"
              >
                <h3>生成进度</h3>
                <el-icon :class="['collapse-icon', { open: openSections.progress }]"><ArrowDown /></el-icon>
              </button>
              <span v-if="running" class="time-pill">预计剩余 --</span>
            </div>
            <div class="progress-summary">
              <span :class="['status-chip', statusTone[displayTaskStatus] || 'muted']">
                {{ statusText[displayTaskStatus] || displayTaskStatus }}
              </span>
              <b>{{ activePercent }}%</b>
              <span>生成 {{ taskElapsed }}</span>
              <span>排队 {{ taskQueueElapsed }}</span>
            </div>
            <el-progress :percentage="activePercent" :stroke-width="5" :show-text="false" />
            <div v-show="openSections.progress" class="collapsible-body">
              <div class="step-line">
                <div
                  v-for="(step, index) in progressSteps"
                  :key="step.key"
                  :class="['step-item', { active: step.active, done: step.done, failed: step.failed }]"
                >
                  <span class="step-dot">
                    <el-icon v-if="step.done"><Check /></el-icon>
                    <template v-else>{{ index + 1 }}</template>
                  </span>
                  <b>{{ step.label }}</b>
                  <small>{{ step.active ? `${activePercent}%` : step.done ? '完成' : '等待中' }}</small>
                </div>
              </div>
            </div>
          </section>

          <el-alert v-if="activeTask.error" class="task-error" type="error" :closable="false" :title="activeTask.error" />

          <section class="copy-grid">
            <article class="copy-card main-copy collapsible-card">
              <div class="card-title collapsible-top">
                <button
                  class="collapse-head"
                  type="button"
                  :aria-expanded="openSections.copy"
                  @click="toggleSection('copy')"
                >
                  <h3>文案输出（{{ activeLanguage }}）</h3>
                  <el-icon :class="['collapse-icon', { open: openSections.copy }]"><ArrowDown /></el-icon>
                </button>
                <el-button text :icon="CopyDocument" @click="copyOutput">复制全部</el-button>
              </div>
              <p v-show="!openSections.copy" class="collapse-preview">{{ copyPreview }}</p>
              <dl v-show="openSections.copy" class="collapsible-body">
                <dt>标题</dt>
                <dd>{{ heroTitle }}</dd>
                <dt>五点描述</dt>
                <dd>
                  <ul v-if="sellingPoints.length">
                    <li v-for="point in sellingPoints" :key="point">{{ point }}</li>
                  </ul>
                  <span v-else>{{ heroDescription }}</span>
                </dd>
                <dt v-if="priceCopy">价格文案</dt>
                <dd v-if="priceCopy">{{ priceCopy }}</dd>
              </dl>
            </article>

            <article class="copy-card collapsible-card">
              <button
                class="collapse-head solo"
                type="button"
                :aria-expanded="openSections.tags"
                @click="toggleSection('tags')"
              >
                <h3>关键词 / 卖点</h3>
                <el-icon :class="['collapse-icon', { open: openSections.tags }]"><ArrowDown /></el-icon>
              </button>
              <p v-show="!openSections.tags" class="collapse-preview">{{ tagPreview }}</p>
              <div v-show="openSections.tags" class="collapsible-body">
                <div v-if="quickTags.length" class="tag-list">
                  <span v-for="tag in quickTags" :key="tag">{{ tag }}</span>
                </div>
                <el-empty v-else description="暂无关键词" :image-size="58" />
              </div>
            </article>

            <article class="copy-card collapsible-card">
              <button
                class="collapse-head solo"
                type="button"
                :aria-expanded="openSections.specs"
                @click="toggleSection('specs')"
              >
                <h3>规格参数</h3>
                <el-icon :class="['collapse-icon', { open: openSections.specs }]"><ArrowDown /></el-icon>
              </button>
              <p v-show="!openSections.specs" class="collapse-preview">{{ specsPreview }}</p>
              <div v-show="openSections.specs" class="collapsible-body">
                <ul v-if="keySpecs.length" class="spec-list">
                  <li v-for="spec in keySpecs" :key="spec">{{ spec }}</li>
                </ul>
                <el-empty v-else description="暂无规格" :image-size="58" />
              </div>
            </article>
          </section>

          <section v-if="detailSections.length" class="detail-summary surface-inset collapsible-card">
            <button
              class="collapse-head solo"
              type="button"
              :aria-expanded="openSections.detail"
              @click="toggleSection('detail')"
            >
              <h3>详情页结构</h3>
              <el-icon :class="['collapse-icon', { open: openSections.detail }]"><ArrowDown /></el-icon>
            </button>
            <p v-show="!openSections.detail" class="collapse-preview">{{ detailPreview }}</p>
            <div v-show="openSections.detail" class="detail-section-grid collapsible-body">
              <article v-for="section in detailSections.slice(0, 4)" :key="section.title || section.body">
                <b>{{ section.title || '详情模块' }}</b>
                <p>{{ section.body }}</p>
              </article>
            </div>
          </section>
        </template>
      </section>

      <section class="asset-section surface">
        <div class="video-panel">
          <div class="video-panel-main">
            <div class="section-head compact">
              <div>
                <span class="kicker">视频生成</span>
                <h2>商品短视频</h2>
              </div>
              <span :class="['status-chip', videoStatusTone]">{{ videoStatusLabel }}</span>
            </div>
            <p class="video-hint">
              使用当前商品文案、卖点、规格、图片方向生成 5 秒商品短视频。
            </p>
            <div v-if="videoAsset" class="video-meta">
              <span>任务：{{ shortTaskID(videoAsset.image_task_id || videoAsset.file_id || '-') }}</span>
              <span>生成 {{ videoElapsedText() }}</span>
              <span>排队 {{ videoQueueText() }}</span>
              <span v-if="videoAsset.credit_cost > 0">扣费 {{ formatCredit(videoAsset.credit_cost) }} 积分</span>
              <span v-else-if="isAssetWorking(videoAsset.status)">完成后按实际消耗结算</span>
            </div>
            <div class="video-stepper" :class="{ failed: videoAsset?.status === 'failed' || videoAsset?.status === 'canceled' }">
              <div
                v-for="step in videoProgressSteps"
                :key="step.key"
                :class="['video-step', { done: step.done, active: step.active, failed: step.failed }]"
              >
                <span class="video-step-dot">
                  <el-icon v-if="step.done"><Check /></el-icon>
                  <el-icon v-else-if="step.failed"><Close /></el-icon>
                </span>
                <b>{{ step.label }}</b>
                <small v-if="step.key === 'running' && videoAsset?.status === 'running'">{{ step.value }}</small>
              </div>
            </div>
            <div v-if="videoAsset && isAssetWorking(videoAsset.status)" class="video-progress-line">
              <span :style="{ width: `${videoAsset.status === 'queued' ? 8 : videoPercent}%` }" />
            </div>
            <el-alert
              v-if="videoAsset?.error"
              class="video-error"
              type="error"
              :closable="false"
              :title="`错误提示：${videoAsset.error}`"
            />
            <div class="video-actions">
              <el-button
                type="primary"
                :loading="generatingVideo"
                :disabled="!canGenerateVideo()"
                @click="generateVideo"
              >
                <el-icon><VideoPlay /></el-icon>
                {{ videoAsset ? '重新生成视频' : '生成视频' }}
              </el-button>
              <el-button
                :disabled="!videoAsset || !assetCanPreview(videoAsset)"
                :icon="View"
                @click="videoAsset && openAssetPreview(videoAsset)"
              >
                预览
              </el-button>
              <el-button
                :disabled="!videoAsset?.prompt"
                :icon="Document"
                @click="videoAsset && openPromptInspect(videoAsset)"
              >
                提示词
              </el-button>
              <el-button
                :disabled="!videoAsset || !assetIsReady(videoAsset)"
                :icon="Download"
                @click="videoAsset && downloadAsset(videoAsset)"
              >
                下载
              </el-button>
            </div>
          </div>
          <button
            v-if="videoAsset && assetIsReady(videoAsset)"
            class="video-window ready"
            type="button"
            @click="openAssetPreview(videoAsset)"
          >
            <video :src="videoAsset.url" muted playsinline preload="metadata" />
            <span><el-icon><VideoPlay /></el-icon></span>
          </button>
          <div v-else class="video-window" :class="{ working: videoAsset && isAssetWorking(videoAsset.status) }">
            <el-icon v-if="videoAsset && isAssetWorking(videoAsset.status)" class="spin"><Loading /></el-icon>
            <el-icon v-else><VideoPlay /></el-icon>
            <span>{{ videoWindowText }}</span>
          </div>
        </div>

        <div class="section-head compact">
          <div>
            <span class="kicker">素材资产</span>
            <h2>{{ assetMetricText }}</h2>
          </div>
          <p>点击素材预览，支持下载或重新生成。</p>
        </div>

        <div v-if="visibleAssets.length" class="asset-grid">
          <article v-for="asset in visibleAssets" :key="asset.id" class="asset-tile">
            <header>
              <div>
                <b>{{ assetText[asset.asset_type] || asset.asset_type }}</b>
                <small>{{ imageSpecText(asset.asset_type) }}</small>
              </div>
              <span :class="['status-chip', statusTone[asset.status] || 'muted']">
                {{ statusText[asset.status] || asset.status }}
              </span>
            </header>

            <div class="asset-actions">
              <el-button title="预览" :disabled="!assetCanPreview(asset)" :icon="View" @click="openAssetPreview(asset)" />
              <el-button title="下载" :disabled="!assetIsReady(asset)" :icon="Download" @click="downloadAsset(asset)" />
              <el-button title="参考图与提示词" :icon="Document" @click="openPromptInspect(asset)" />
              <el-button
                v-if="isAssetWorking(asset.status)"
                title="取消生成"
                type="danger"
                plain
                :loading="canceling"
                :disabled="!running"
                :icon="Close"
                @click="cancelTask"
              />
              <el-button
                v-else
                title="重新生成"
                :loading="retryingAssetID === asset.id"
                :icon="RefreshRight"
                @click="retryAsset(asset)"
              />
            </div>

            <button v-if="assetHasImage(asset)" class="asset-image" type="button" @click="openAssetPreview(asset)">
              <img :src="thumbURL(asset.url)" :alt="assetText[asset.asset_type] || asset.asset_type" @error="markBrokenAsset(asset)" />
            </button>
            <button v-else-if="isVideoAsset(asset) && assetIsReady(asset)" class="asset-image asset-video-thumb" type="button" @click="openAssetPreview(asset)">
              <el-icon><VideoPlay /></el-icon>
              <span>预览视频</span>
            </button>
            <div v-else class="asset-placeholder" :class="{ working: isAssetWorking(asset.status) }">
              <el-icon v-if="isAssetWorking(asset.status)" class="spin"><Loading /></el-icon>
              <el-icon v-else-if="isVideoAsset(asset)"><VideoPlay /></el-icon>
              <el-icon v-else><Picture /></el-icon>
              <span>{{ isAssetWorking(asset.status) ? '等待生成' : (asset.error || (isVideoAsset(asset) ? '暂无视频' : '暂无图片')) }}</span>
            </div>

            <footer>
              <span>生成 {{ assetGenerateElapsed(asset) }} / 排队 {{ assetQueueElapsed(asset) }}</span>
            </footer>

            <div v-if="!isAssetWorking(asset.status)" class="retry-compact">
              <button class="retry-toggle" type="button" @click="toggleRetryPanel(asset.id)">
                重试要求
                <el-icon :class="['collapse-icon', { open: retryPanelOpenIDs.has(asset.id) }]"><ArrowDown /></el-icon>
              </button>
              <el-input
                v-show="retryPanelOpenIDs.has(asset.id)"
                v-model="retryPrompts[asset.id]"
                type="textarea"
                :rows="2"
                maxlength="500"
                resize="none"
                placeholder="可选：补充重试要求"
              />
            </div>
          </article>
        </div>

        <el-empty v-else description="任务开始后生成素材资产" :image-size="86" />
      </section>
    </main>

    <aside class="composer-card surface" v-loading="optionsLoading">
      <div class="section-head">
        <div>
          <span class="kicker">任务创建</span>
          <h1>电商智能体</h1>
        </div>
        <span class="lang-chip">{{ selectedLanguage }}</span>
      </div>

      <el-form label-position="top" class="brief-form">
        <el-form-item>
          <template #label>
            <span class="required-label">商品资料</span>
            <button class="text-action" type="button" @click="clearBrief">清空</button>
          </template>
          <el-input
            v-model="form.requirement"
            type="textarea"
            :rows="4"
            maxlength="2000"
            show-word-limit
            resize="none"
            placeholder="输入商品名、卖点、规格、价格、目标人群、平台要求等信息"
          />
        </el-form-item>

        <el-form-item label="目标平台">
          <el-select v-model="form.platform_id" placeholder="请选择平台" filterable>
            <el-option
              v-for="platform in platforms"
              :key="platform.id"
              :label="`${platform.name} · 默认：${ecommerceLanguageName(platform.language)}`"
              :value="platform.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="文案语言">
          <el-select v-model="form.language" placeholder="请选择文案语言">
            <el-option v-for="lang in ECOMMERCE_LANGUAGES" :key="lang.value" :label="lang.label" :value="lang.value" />
          </el-select>
        </el-form-item>

        <el-form-item label="提示词模板">
          <el-select v-model="form.prompt_template_id" placeholder="请选择提示词模板" filterable>
            <el-option v-for="prompt in prompts" :key="prompt.id" :label="prompt.name" :value="prompt.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="风格模板">
          <el-select v-model="form.style_template_id" placeholder="请选择风格模板" filterable>
            <el-option v-for="style in styles" :key="style.id" :label="style.name" :value="style.id" />
          </el-select>
        </el-form-item>

        <div class="library-picker-grid">
          <el-form-item label="商品资产">
            <el-select
              v-model="form.product_asset_id"
              placeholder="可选：从资产库选择商品"
              filterable
              clearable
              :loading="libraryAssetsLoading"
              @visible-change="onLibrarySelectVisible"
            >
              <el-option v-for="asset in productLibraryAssets" :key="asset.asset_id" :label="asset.name" :value="asset.asset_id">
                <div class="asset-option">
                  <img v-if="asset.cover_url" :src="asset.cover_url" :alt="asset.name" />
                  <span>{{ asset.name }}</span>
                  <small>{{ asset.code || asset.review_status }}</small>
                </div>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item label="模特资产">
            <el-select
              v-model="form.model_asset_id"
              placeholder="可选：从资产库选择模特"
              filterable
              clearable
              :loading="libraryAssetsLoading"
              @visible-change="onLibrarySelectVisible"
            >
              <el-option v-for="asset in modelLibraryAssets" :key="asset.asset_id" :label="asset.name" :value="asset.asset_id">
                <div class="asset-option">
                  <img v-if="asset.cover_url" :src="asset.cover_url" :alt="asset.name" />
                  <span>{{ asset.name }}</span>
                  <small>{{ asset.code || asset.review_status }}</small>
                </div>
              </el-option>
            </el-select>
          </el-form-item>
        </div>
        <div v-if="selectedProductAsset || selectedModelAsset" class="selected-library-assets">
          <div v-if="selectedProductAsset" class="selected-library-card">
            <img v-if="selectedProductAsset.cover_url" :src="selectedProductAsset.cover_url" :alt="selectedProductAsset.name" />
            <div>
              <strong>{{ selectedProductAsset.name }}</strong>
              <span>商品资产将注入资料与参考图</span>
            </div>
          </div>
          <div v-if="selectedModelAsset" class="selected-library-card">
            <img v-if="selectedModelAsset.cover_url" :src="selectedModelAsset.cover_url" :alt="selectedModelAsset.name" />
            <div>
              <strong>{{ selectedModelAsset.name }}</strong>
              <span>模特资产将注入外观与授权约束</span>
            </div>
          </div>
        </div>

        <el-form-item label="可选图片">
          <el-checkbox-group v-model="form.extra_asset_types" class="extra-asset-options">
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

        <el-form-item>
          <template #label>
            <span>参考图片（最多 {{ MAX_IMAGES }} 张，{{ MAX_IMAGE_MB }}MB/张）</span>
          </template>
          <div class="reference-field">
            <div class="reference-grid">
              <button
                v-for="(img, index) in form.reference_images"
                :key="index"
                class="reference-thumb"
                type="button"
                @click="removeImage(index)"
              >
                <img :src="img" alt="参考图" />
                <span><el-icon><Close /></el-icon></span>
              </button>
              <el-upload
                v-if="form.reference_images.length < MAX_IMAGES"
                drag
                multiple
                accept="image/*"
                :auto-upload="false"
                :show-file-list="false"
                :on-change="onImageChange"
                class="reference-upload"
              >
                <el-icon><UploadFilled /></el-icon>
                <strong>上传图片</strong>
              </el-upload>
            </div>
          </div>
        </el-form-item>

        <el-button class="primary-submit" type="primary" size="large" :loading="submitting" @click="submit">
          <el-icon v-if="!submitting"><MagicStick /></el-icon>
          {{ submitting ? '提交中' : '开始生成' }}
        </el-button>
        <p class="cost-hint">预计消耗 30 积分</p>
      </el-form>
    </aside>

    <aside class="delivery-card">
      <section :class="['surface side-block task-history-block', { collapsed: !taskHistoryExpanded }]">
        <div class="section-head compact">
          <div>
            <span class="kicker">近期任务</span>
            <h2>任务记录</h2>
          </div>
          <div class="task-history-actions">
            <span v-if="tasks.length" class="task-count">{{ tasks.length }}/{{ tasksTotal || tasks.length }}</span>
            <el-tooltip :content="taskHistoryExpanded ? '收起任务记录' : '展开任务记录'" placement="top">
              <el-button
                class="task-icon-btn"
                :class="{ open: taskHistoryExpanded }"
                :icon="ArrowDown"
                circle
                :aria-label="taskHistoryExpanded ? '收起任务记录' : '展开任务记录'"
                @click="taskHistoryExpanded = !taskHistoryExpanded"
              />
            </el-tooltip>
            <el-popover
              v-model:visible="taskMoreVisible"
              placement="left-start"
              trigger="click"
              popper-class="task-more-popover"
              :width="540"
              @show="showTaskMorePopover"
            >
              <template #reference>
                <el-button class="task-icon-btn" :icon="MoreFilled" circle aria-label="更多任务" />
              </template>
              <div class="task-more-panel">
                <div class="task-more-head">
                  <div>
                    <span class="kicker">全部记录</span>
                    <h3>任务卡片</h3>
                  </div>
                  <span v-if="tasks.length" class="task-count">{{ tasks.length }}/{{ tasksTotal || tasks.length }}</span>
                </div>
                <div v-if="tasks.length" class="task-more-list" @scroll="onTaskListScroll" @wheel.passive="onTaskListWheel">
                  <button
                    v-for="task in tasks"
                    :key="`more-${task.task_id}`"
                    :class="['task-more-card', { active: activeTask?.task_id === task.task_id }]"
                    type="button"
                    @click="openTaskFromHistory(task)"
                  >
                    <span class="task-more-thumb">
                      <img
                        v-if="taskThumbnailAsset(task)"
                        :src="thumbURL(taskThumbnailAsset(task)!.url)"
                        alt="任务缩略图"
                        @error="markBrokenTaskThumb(taskThumbnailAsset(task)!)"
                      />
                      <el-icon v-else><Picture /></el-icon>
                    </span>
                    <span class="task-more-main">
                      <span class="task-more-title">{{ taskTitle(task) }}</span>
                      <span class="task-more-meta">
                        <span>创建 {{ formatDateTime(task.created_at) }}</span>
                        <span>任务 {{ shortTaskID(task.task_id) }}</span>
                      </span>
                      <span class="task-tags expanded" :title="taskTagsTitle(task)">
                        <i v-for="tag in taskTagItems(task)" :key="`${task.task_id}-more-${tag}`">{{ tag }}</i>
                      </span>
                      <small class="task-brief">{{ taskRequirementPreview(task) }}</small>
                    </span>
                    <em :class="['text-state', statusTone[task.status] || 'muted']">
                      {{ statusText[task.status] || task.status }}
                    </em>
                  </button>
                  <div v-if="tasksLoading" class="task-list-more">加载中...</div>
                  <div v-else-if="hasMoreTasks" class="task-list-more">继续下拉加载</div>
                  <div v-else class="task-list-more">已加载全部</div>
                </div>
                <el-empty v-else :description="tasksLoading ? '任务加载中' : '暂无任务'" :image-size="58" />
              </div>
            </el-popover>
          </div>
        </div>
        <div v-show="taskHistoryExpanded" class="task-search">
          <el-input
            v-model="taskKeyword"
            clearable
            placeholder="搜索标题、商品资料、平台、模板"
            @keyup.enter="searchTasks"
            @clear="clearTaskSearch"
          />
          <el-button :loading="tasksLoading" @click="searchTasks">搜索</el-button>
        </div>

        <div
          v-if="tasks.length && taskHistoryExpanded"
          class="task-list"
          @scroll="onTaskListScroll"
          @wheel.passive="onTaskListWheel"
        >
          <button
            v-for="task in tasks"
            :key="task.task_id"
            :class="['task-list-item', { active: activeTask?.task_id === task.task_id }]"
            type="button"
            @click="openTask(task)"
          >
            <span class="task-thumb">
              <img
                v-if="taskThumbnailAsset(task)"
                :src="thumbURL(taskThumbnailAsset(task)!.url)"
                alt="任务缩略图"
                @error="markBrokenTaskThumb(taskThumbnailAsset(task)!)"
              />
              <el-icon v-else><Picture /></el-icon>
            </span>
            <span>
              <b>{{ taskTitle(task) }}</b>
              <span class="task-tags" :title="taskTagsTitle(task)">
                <i v-for="tag in taskTagView(task).visible" :key="`${task.task_id}-${tag}`">{{ tag }}</i>
                <el-tooltip v-if="taskTagView(task).hidden.length" :content="taskTagView(task).hidden.join(' / ')" placement="top">
                  <i class="more-tag">...</i>
                </el-tooltip>
              </span>
              <small class="task-brief">{{ taskRequirementPreview(task) }}</small>
            </span>
            <span class="task-side">
              <em :class="['text-state', statusTone[task.status] || 'muted']">
                {{ statusText[task.status] || task.status }}
              </em>
              <el-button
                v-if="canRetryTask(task.status)"
                class="task-retry-btn"
                text
                :loading="retryingTaskID === task.task_id"
                :icon="RefreshRight"
                @click="retryTask(task, $event)"
              >
                重试
              </el-button>
              <el-button
                class="task-delete-btn"
                text
                type="danger"
                :loading="deletingTaskID === task.task_id"
                :icon="Close"
                @click="deleteTask(task, $event)"
              >
                删除
              </el-button>
            </span>
          </button>
          <div v-if="tasksLoading" class="task-list-more">加载中...</div>
          <div v-else-if="hasMoreTasks" class="task-list-more">继续下拉加载</div>
          <div v-else class="task-list-more">已加载全部</div>
        </div>
        <div v-else-if="!taskHistoryExpanded && tasks.length" class="history-collapsed">
          <b>任务记录已收起</b>
          <span>{{ tasks.length }}/{{ tasksTotal || tasks.length }} 条，点击上方箭头展开。</span>
        </div>
        <div v-else-if="historyDeferred" class="history-deferred">
          <span class="deferred-dot" />
          <div>
            <b>正在优先打开详情</b>
            <p>历史记录稍后加载，不影响当前任务查看。</p>
          </div>
        </div>
        <el-empty v-else-if="taskHistoryExpanded" :description="taskKeyword.trim() ? '没有匹配任务' : '暂无任务'" :image-size="64" />
      </section>

      <section class="surface side-block">
        <div class="section-head compact">
          <div>
            <span class="kicker">交付清单</span>
            <h2>结果检查</h2>
          </div>
        </div>
        <div class="delivery-list">
          <div v-for="item in deliveryItems" :key="item.key" class="delivery-row">
            <span>{{ item.label }}</span>
            <b v-if="item.done"><el-icon><CircleCheck /></el-icon></b>
            <em v-else>{{ item.value }}</em>
          </div>
        </div>
      </section>

      <section class="surface side-block">
        <div class="status-card" :class="activeTask ? statusTone[activeTask.status] || 'muted' : 'muted'">
          <span :class="['status-dot', activeTask ? statusTone[activeTask.status] || 'muted' : 'muted']" />
          <div>
            <h2>{{ statusHeadline }}</h2>
            <p v-if="activeTask">{{ statusText[activeTask.status] || activeTask.status }} · {{ activePercent }}%</p>
            <p v-else>创建任务或选择历史任务。</p>
          </div>
        </div>
      </section>

      <section class="surface side-block">
        <div class="section-head compact">
          <div>
            <span class="kicker">导出交付</span>
            <h2>下载结果</h2>
          </div>
        </div>
        <div class="export-actions">
          <el-button type="primary" :loading="exporting" :disabled="!doneAssetCount" @click="exportPoster">
            <el-icon><Download /></el-icon>
            导出长图（PNG）
          </el-button>
          <el-button :loading="downloadingAll" :disabled="!doneAssetCount" @click="downloadAllAssets">
            <el-icon><FolderOpened /></el-icon>
            下载素材资产
          </el-button>
          <el-button :disabled="!detailDoc" @click="openDetailPreview">
            <el-icon><View /></el-icon>
            预览详情页
          </el-button>
        </div>
      </section>
    </aside>

    <ImagePreviewDialog
      v-if="previewAsset && !isVideoAsset(previewAsset)"
      v-model="previewVisible"
      :src="previewImageURL"
      :original-src="previewAsset.url"
      :title="assetText[previewAsset.asset_type] || previewAsset.asset_type"
      :alt="assetText[previewAsset.asset_type] || previewAsset.asset_type"
      :download-name="assetFileName(previewAsset)"
      :loading="previewImageLoading"
    />

    <el-dialog
      v-if="previewAsset && isVideoAsset(previewAsset)"
      v-model="previewVisible"
      width="920px"
      append-to-body
      :title="assetText[previewAsset.asset_type] || previewAsset.asset_type"
      class="asset-dialog"
    >
      <div v-if="previewAsset" class="asset-dialog-body" v-loading="previewImageLoading">
        <video
          v-if="previewImageURL"
          :src="previewImageURL"
          controls
          playsinline
          preload="metadata"
        />
      </div>
      <template #footer>
        <el-button v-if="previewAsset" @click="downloadAsset(previewAsset)">
          <el-icon><Download /></el-icon>
          下载
        </el-button>
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="promptVisible"
      width="980px"
      append-to-body
      :title="promptDialogTitle"
      class="prompt-inspect-dialog"
    >
      <div v-if="promptAsset" class="prompt-inspect-body">
        <section class="prompt-reference-pane">
          <div class="inspect-pane-head">
            <b>参考图</b>
            <span>{{ promptReferenceCount }} 张</span>
          </div>
          <div v-if="promptReferenceGroups.length" class="reference-group-list">
            <div v-for="group in promptReferenceGroups" :key="group.title" class="reference-group">
              <h3>{{ group.title }}</h3>
              <div class="reference-grid">
                <a
                  v-for="img in group.images"
                  :key="`${group.title}-${img.label}`"
                  class="reference-thumb"
                  :href="img.href"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <img :src="img.url" :alt="img.label" />
                </a>
              </div>
            </div>
          </div>
          <el-empty v-else description="暂无参考图" :image-size="72" />
        </section>
        <section class="prompt-text-pane">
          <div class="inspect-pane-head">
            <b>生成提示词</b>
            <span>{{ promptAsset.prompt ? `${promptAsset.prompt.length} 字符` : '暂无' }}</span>
          </div>
          <pre v-if="promptAsset.prompt" class="prompt-text">{{ promptAsset.prompt }}</pre>
          <el-empty v-else description="暂无提示词" :image-size="72" />
        </section>
      </div>
      <template #footer>
        <el-button :disabled="!promptAsset?.prompt" @click="copyAssetPrompt()">
          <el-icon><CopyDocument /></el-icon>
          复制提示词
        </el-button>
        <el-button type="primary" @click="promptVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailVisible" width="980px" append-to-body title="详情页预览" class="detail-dialog">
      <iframe v-if="detailDoc" class="detail-frame" :srcdoc="detailDoc" sandbox="" />
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.commerce-workbench {
  --ink: var(--lc-text);
  --muted: var(--lc-muted);
  --subtle: var(--lc-subtle);
  --line: var(--lc-border);
  --paper: var(--lc-surface);
  --wash: var(--lc-surface-soft);
  --page: var(--lc-bg);
  --surface-bg: rgba(255, 255, 255, 0.92);
  --field-bg: #fff;
  --tile-bg: #fff;
  --tile-soft: #f8fafc;
  --chip-bg: #f2f4f7;
  --panel-gradient: linear-gradient(180deg, #ffffff, #f8fcff);
  --green: var(--lc-mint);
  --green-strong: var(--lc-mint-strong);
  --amber: var(--lc-amber);
  --red: var(--lc-danger);
  --shadow: var(--lc-shadow-card);
  box-sizing: border-box;
  width: 100%;
  min-height: calc(100vh - 60px);
  display: grid;
  grid-template-columns: minmax(340px, 390px) minmax(0, 1fr) minmax(286px, 320px);
  gap: 14px;
  padding: 18px;
  overflow-x: hidden;
  color: var(--ink);
  background:
    radial-gradient(900px 420px at 8% -8%, rgba(37, 99, 235, .10), transparent 62%),
    radial-gradient(760px 360px at 96% 0%, rgba(20, 184, 166, .11), transparent 60%),
    var(--page);
  font-family: "PingFang SC", "Microsoft YaHei", sans-serif;
}

:global(html.dark .commerce-workbench) {
  --ink: var(--lc-text);
  --muted: var(--lc-muted);
  --subtle: var(--lc-subtle);
  --line: var(--lc-border);
  --paper: var(--lc-surface);
  --wash: var(--lc-surface-soft);
  --page: var(--lc-bg);
  --surface-bg: rgba(17, 24, 39, 0.94);
  --field-bg: var(--lc-surface-soft);
  --tile-bg: var(--lc-surface);
  --tile-soft: var(--lc-surface-soft);
  --chip-bg: rgba(148, 163, 184, 0.12);
  --panel-gradient: linear-gradient(180deg, var(--lc-surface), var(--lc-surface-soft));
  --green-strong: #5eead4;
  --shadow: 0 18px 46px rgba(0, 0, 0, 0.3);
}

.commerce-workbench :deep(.el-button) {
  min-height: 32px;
  height: 32px;
  padding: 6px 10px;
  border-radius: 7px;
}

.surface {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--surface-bg);
  box-shadow: var(--shadow);
}

.surface-inset {
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--wash);
}

.composer-card,
.current-task,
.asset-section,
.side-block {
  padding: 16px;
}

.composer-card {
  align-self: start;
  position: sticky;
  top: 16px;
  grid-column: 1;
  grid-row: 1;
}

.task-column {
  min-width: 0;
  display: grid;
  gap: 12px;
  align-content: start;
  grid-column: 2;
  grid-row: 1;
}

.delivery-card {
  min-width: 0;
  display: grid;
  gap: 12px;
  align-content: start;
  grid-column: 3;
  grid-row: 1;
}

.section-head,
.task-header,
.card-title,
.collapsible-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.section-head.compact,
.card-title,
.collapsible-top {
  align-items: center;
}

.kicker {
  display: inline-flex;
  margin-bottom: 4px;
  color: var(--lc-primary);
  font-size: 12px;
  font-weight: 700;
}

h1,
h2,
h3,
p {
  margin: 0;
}

h1 {
  font-size: 26px;
  line-height: 34px;
  font-weight: 800;
}

h2 {
  font-size: 20px;
  line-height: 26px;
  font-weight: 800;
}

h3 {
  font-size: 15px;
  line-height: 22px;
  font-weight: 800;
}

.lang-chip,
.time-pill,
.status-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 24px;
  border-radius: 6px;
  padding: 3px 8px;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.lang-chip {
  color: #fff;
  background: linear-gradient(135deg, var(--lc-primary), var(--green));
}

.time-pill {
  color: var(--lc-warning-text);
  background: var(--lc-warning-soft);
}

.status-chip.success {
  color: var(--lc-success-text);
  background: var(--lc-success-soft);
}

.status-chip.warning {
  color: var(--lc-warning-text);
  background: var(--lc-warning-soft);
}

.status-chip.danger {
  color: var(--lc-danger-text);
  background: var(--lc-danger-soft);
}

.status-chip.muted {
  color: var(--muted);
  background: var(--chip-bg);
}

.brief-form {
  margin-top: 12px;
}

.brief-form :deep(.el-form-item) {
  margin-bottom: 10px;
}

.brief-form :deep(.el-form-item__label) {
  min-height: 24px;
  line-height: 24px;
}

.brief-form :deep(.el-form-item__label) {
  width: 100%;
  display: flex;
  justify-content: space-between;
  color: var(--ink);
  font-weight: 700;
}

.brief-form :deep(.el-select) {
  width: 100%;
}

.library-picker-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0;
}

.asset-option {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.asset-option img {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  object-fit: cover;
  flex: 0 0 auto;
}

.asset-option span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.asset-option small {
  margin-left: auto;
  color: var(--el-text-color-secondary);
}

.selected-library-assets {
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
  margin: -4px 0 12px;
}

.selected-library-card {
  display: grid;
  grid-template-columns: 44px 1fr;
  gap: 10px;
  align-items: center;
  min-height: 52px;
  padding: 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--field-bg);
}

.selected-library-card img {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  object-fit: cover;
}

.selected-library-card strong,
.selected-library-card span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-library-card strong {
  font-size: 13px;
  color: var(--ink);
}

.selected-library-card span {
  margin-top: 2px;
  font-size: 11px;
  color: var(--muted);
}

.extra-asset-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.extra-asset-options :deep(.el-checkbox-button) {
  width: 100%;
}

.extra-asset-options :deep(.el-checkbox-button__inner) {
  display: grid;
  gap: 4px;
  width: 100%;
  min-height: 66px;
  padding: 10px 12px;
  border-radius: 8px;
  border-left: 1px solid var(--el-border-color);
  text-align: left;
  white-space: normal;
}

.extra-asset-options :deep(.el-checkbox-button__inner span) {
  color: var(--ink);
  font-size: 14px;
  font-weight: 800;
  line-height: 1.2;
}

.extra-asset-options :deep(.el-checkbox-button__inner small) {
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.2;
}

.brief-form :deep(.el-input__wrapper),
.brief-form :deep(.el-select__wrapper),
.brief-form :deep(.el-textarea__inner) {
  border-radius: 12px;
  background: var(--field-bg);
  box-shadow: 0 0 0 1px var(--line) inset;
}

.brief-form :deep(.el-input__wrapper.is-focus),
.brief-form :deep(.el-select__wrapper.is-focused),
.brief-form :deep(.el-textarea__inner:focus) {
  box-shadow: 0 0 0 1px var(--lc-primary) inset, 0 0 0 3px rgba(37, 99, 235, 0.12);
}

.required-label::after {
  content: '*';
  margin-left: 3px;
  color: var(--red);
}

.text-action {
  border: 0;
  padding: 0;
  color: var(--green);
  background: transparent;
  cursor: pointer;
  font-size: 12px;
}

.reference-field {
  width: 100%;
  display: grid;
  gap: 6px;
}

.reference-grid {
  width: 100%;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.reference-thumb,
.reference-upload {
  min-width: 0;
  height: 64px;
}

.reference-thumb {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 0;
  background: var(--tile-soft);
  cursor: pointer;
}

.reference-thumb img {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}

.reference-thumb span {
  position: absolute;
  right: 5px;
  top: 5px;
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  color: #fff;
  background: rgba(15, 17, 21, 0.6);
}

.reference-upload :deep(.el-upload),
.reference-upload :deep(.el-upload-dragger) {
  width: 100%;
  height: 64px;
}

.reference-upload :deep(.el-upload-dragger) {
  box-sizing: border-box;
  display: grid;
  place-content: center;
  gap: 2px;
  padding: 0;
  border-radius: 12px;
  border-color: var(--line);
  background: var(--field-bg);
  color: var(--muted);
}

.reference-upload :deep(.el-icon) {
  margin: 0;
  font-size: 16px;
}

.reference-upload strong {
  display: block;
  margin-top: 0;
  font-size: 12px;
  font-weight: 700;
  line-height: 18px;
}

.cost-hint {
  margin-top: 8px;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.primary-submit {
  width: 100%;
  min-height: 42px;
  height: 42px;
  border: 0;
  border-radius: 12px;
  font-weight: 800;
  background: linear-gradient(135deg, var(--lc-primary), var(--green));
  box-shadow: 0 12px 24px rgba(37, 99, 235, 0.22);
}

.cost-hint {
  text-align: center;
}

.empty-current {
  min-height: 210px;
  display: grid;
  place-content: center;
  text-align: center;
  color: var(--muted);
}

.empty-current h2 {
  margin-bottom: 8px;
}

.task-header {
  align-items: center;
  flex-wrap: nowrap;
  margin-bottom: 10px;
}

.task-header > div:first-child {
  min-width: 0;
}

.task-header h2 {
  max-width: 760px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.header-actions {
  display: inline-flex;
  flex-wrap: nowrap;
  justify-content: flex-end;
  gap: 8px;
}

.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}

.task-meta span {
  max-width: 100%;
  border-radius: 6px;
  min-height: 24px;
  padding: 3px 8px;
  color: var(--muted);
  background: var(--chip-bg);
  font-size: 12px;
  line-height: 18px;
  overflow-wrap: anywhere;
}

.progress-panel {
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 12px;
  background: var(--wash);
}

.progress-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin: 8px 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.progress-summary b {
  color: var(--ink);
  font-size: 13px;
}

.collapsible-card {
  transition: border-color .18s ease, box-shadow .18s ease;
}

.collapsible-card:hover {
  border-color: rgba(37, 99, 235, 0.28);
}

.collapse-head {
  min-width: 0;
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  border: 0;
  padding: 0;
  color: var(--ink);
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.collapse-head.solo {
  width: 100%;
  justify-content: space-between;
}

.collapse-head h3 {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.collapse-icon {
  flex: 0 0 auto;
  color: var(--muted);
  transition: transform .18s ease, color .18s ease;
}

.collapse-icon.open {
  color: var(--green);
  transform: rotate(180deg);
}

.collapsible-body {
  margin-top: 10px;
}

.collapse-preview {
  display: -webkit-box;
  margin-top: 8px;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.progress-panel .collapsible-body .step-line,
.copy-card .collapsible-body .tag-list,
.copy-card .collapsible-body .spec-list,
.detail-summary .collapsible-body {
  margin-top: 0;
}

.step-line {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 8px;
  margin: 10px 0 8px;
}

.step-item {
  min-width: 0;
  display: grid;
  justify-items: center;
  gap: 4px;
  position: relative;
  color: var(--muted);
  text-align: center;
}

.step-item::before {
  content: '';
  position: absolute;
  top: 11px;
  left: calc(-50% + 14px);
  width: calc(100% - 28px);
  height: 2px;
  background: var(--line);
}

.step-item:first-child::before {
  display: none;
}

.step-item.done::before,
.step-item.active::before {
  background: var(--lc-primary);
}

.step-dot {
  position: relative;
  z-index: 1;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: 1px solid #d0d5dd;
  border-radius: 50%;
  color: var(--muted);
  background: var(--field-bg);
  font-size: 12px;
  font-weight: 800;
}

.step-item.done .step-dot {
  border-color: var(--lc-primary);
  color: #fff;
  background: var(--lc-primary);
}

.step-item.active .step-dot {
  border-color: var(--amber);
  color: #fff;
  background: var(--amber);
  box-shadow: 0 0 0 4px rgba(245, 158, 11, 0.16);
}

.step-item.failed .step-dot {
  border-color: var(--red);
  color: #fff;
  background: var(--red);
}

.step-item b,
.step-item small {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.step-item b {
  color: var(--ink);
  font-size: 12px;
}

.step-item small {
  font-size: 12px;
}

.progress-panel :deep(.el-progress-bar__outer) {
  background: var(--line);
}

.progress-panel :deep(.el-progress-bar__inner) {
  background: linear-gradient(90deg, var(--lc-primary), var(--green));
}

.task-error {
  margin-top: 12px;
}

.copy-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(220px, 0.65fr);
  gap: 10px;
  margin-top: 10px;
}

.copy-card {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 12px;
  background: var(--tile-bg);
}

.main-copy {
  grid-row: span 2;
}

.copy-card dl {
  margin: 10px 0 0;
}

.copy-card dt {
  margin: 8px 0 4px;
  color: var(--ink);
  font-size: 12px;
  font-weight: 800;
}

.copy-card dd {
  margin: 0;
  color: var(--muted);
  font-size: 14px;
  line-height: 20px;
}

.copy-card ul {
  margin: 0;
  padding-left: 18px;
}

.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.tag-list span {
  border-radius: 999px;
  padding: 4px 8px;
  color: var(--ink);
  background: var(--lc-primary-soft);
  font-size: 12px;
  line-height: 18px;
}

.spec-list {
  margin: 8px 0 0;
  padding-left: 18px;
  color: var(--muted);
  font-size: 13px;
  line-height: 22px;
}

.detail-summary {
  margin-top: 10px;
  padding: 12px;
}

.detail-section-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin-top: 8px;
}

.detail-section-grid article {
  border-radius: 12px;
  padding: 10px;
  background: var(--tile-bg);
}

.detail-section-grid b {
  font-size: 13px;
}

.detail-section-grid p {
  margin-top: 6px;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.asset-section .section-head {
  margin-bottom: 10px;
}

.video-panel {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 320px);
  gap: 12px;
  border: 1px solid rgba(20, 139, 127, 0.22);
  border-radius: 16px;
  padding: 12px;
  margin-bottom: 12px;
  background: var(--panel-gradient);
}

.video-panel-main {
  min-width: 0;
  display: grid;
  align-content: start;
  gap: 8px;
}

.video-hint,
.video-meta {
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.video-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.video-meta span {
  border-radius: 6px;
  padding: 3px 8px;
  background: var(--lc-mint-soft);
}

.video-stepper {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

.video-step {
  position: relative;
  min-width: 0;
  display: grid;
  justify-items: center;
  gap: 4px;
  color: var(--muted);
  text-align: center;
}

.video-step::before {
  content: '';
  position: absolute;
  top: 11px;
  left: calc(-50% + 14px);
  width: calc(100% - 28px);
  height: 2px;
  background: var(--line);
}

.video-step:first-child::before {
  display: none;
}

.video-step.done::before,
.video-step.active::before {
  background: var(--green);
}

.video-step.failed::before,
.video-stepper.failed .video-step.active::before {
  background: var(--red);
}

.video-step-dot {
  position: relative;
  z-index: 1;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: 1px solid #d0d5dd;
  border-radius: 50%;
  color: var(--muted);
  background: var(--field-bg);
  font-size: 12px;
  font-weight: 800;
}

.video-step.done .video-step-dot {
  border-color: var(--green);
  color: #fff;
  background: var(--green);
}

.video-step.active .video-step-dot {
  border-color: var(--amber);
  color: #fff;
  background: var(--amber);
  box-shadow: 0 0 0 4px rgba(245, 158, 11, 0.14);
}

.video-step.failed .video-step-dot {
  border-color: var(--red);
  color: #fff;
  background: var(--red);
}

.video-step b,
.video-step small {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.video-step b {
  color: var(--ink);
  font-size: 12px;
  line-height: 18px;
}

.video-step small {
  min-height: 18px;
  border-radius: 6px;
  padding: 1px 6px;
  color: var(--lc-warning-text);
  background: var(--lc-warning-soft);
  font-size: 12px;
  font-weight: 800;
  line-height: 16px;
}

.video-progress-line {
  width: 100%;
  height: 4px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--line);
}

.video-progress-line span {
  display: block;
  height: 100%;
  min-width: 8px;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--green), #3b82f6);
  transition: width .24s ease;
}

.video-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.video-actions :deep(.el-button) {
  margin: 0;
}

.video-actions :deep(.el-button--primary) {
  border-color: var(--lc-primary);
  background: linear-gradient(135deg, var(--lc-primary), var(--green));
}

.video-error {
  margin: 0;
}

.video-window {
  min-width: 0;
  height: 180px;
  box-sizing: border-box;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 14px;
  overflow: hidden;
  color: var(--green);
  background: var(--tile-bg);
  font-size: 13px;
  font-weight: 800;
  text-align: center;
}

.video-window.ready {
  position: relative;
  border: 0;
  padding: 0;
  cursor: pointer;
  background: #111827;
}

.video-window.working {
  color: var(--amber);
  background: var(--lc-warning-soft);
}

.video-window video {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}

.video-window > span {
  pointer-events: none;
}

.video-window.ready > span {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: #fff;
  background: rgba(15, 23, 42, 0.24);
  font-size: 34px;
}

.asset-section .section-head p {
  max-width: 360px;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
  text-align: right;
}

.asset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(156px, 1fr));
  gap: 10px;
  align-items: start;
}

.asset-tile {
  min-width: 0;
  display: grid;
  grid-template-rows: auto auto 140px auto auto;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 10px;
  background: var(--tile-bg);
}

.asset-tile header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  min-height: 42px;
}

.asset-tile b,
.asset-tile small {
  display: block;
}

.asset-tile b {
  font-size: 14px;
  line-height: 20px;
}

.asset-tile small,
.asset-tile footer > span {
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.asset-image,
.asset-placeholder {
  width: 100%;
  height: 140px;
  box-sizing: border-box;
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
  background: var(--tile-soft);
}

.asset-image {
  display: block;
  padding: 0;
  cursor: pointer;
}

.asset-image img {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}

.asset-video-thumb {
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 8px;
  color: var(--green);
  background: linear-gradient(180deg, var(--lc-mint-soft), color-mix(in srgb, var(--lc-mint-soft) 45%, var(--tile-soft)));
  font-size: 13px;
  font-weight: 800;
}

.asset-video-thumb :deep(.el-icon) {
  font-size: 28px;
}

.asset-placeholder {
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 6px;
  color: var(--subtle);
  text-align: center;
  padding: 12px;
  overflow-wrap: anywhere;
}

.asset-placeholder.working {
  color: var(--amber);
  background: var(--lc-warning-soft);
}

.asset-tile footer {
  display: block;
}

.asset-actions {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 5px;
}

.asset-actions :deep(.el-button) {
  width: 100%;
  margin: 0;
}

.retry-compact {
  margin-top: 8px;
}

.retry-toggle {
  width: 100%;
  min-height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  border: 1px solid var(--line);
  border-radius: 7px;
  padding: 4px 8px;
  color: var(--muted);
  background: var(--field-bg);
  cursor: pointer;
  font-size: 12px;
  font-weight: 700;
}

.asset-tile :deep(.el-textarea) {
  margin-top: 8px;
}

.asset-tile :deep(.el-textarea__inner) {
  border-radius: 8px;
}

.side-block {
  min-width: 0;
  align-self: start;
}

.task-history-block {
  transition: padding .18s ease, border-color .18s ease, box-shadow .18s ease;
}

.task-history-block.collapsed {
  padding-bottom: 12px;
}

.task-history-actions {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}

.task-icon-btn {
  width: 28px;
  min-width: 28px;
  height: 28px;
  padding: 0;
}

.task-icon-btn :deep(.el-icon) {
  transition: transform .18s ease;
}

.task-icon-btn.open :deep(.el-icon) {
  transform: rotate(180deg);
}

.history-collapsed {
  min-height: 58px;
  display: grid;
  place-content: center;
  gap: 3px;
  border: 1px dashed rgba(37, 99, 235, 0.22);
  border-radius: 12px;
  margin-top: 10px;
  padding: 10px;
  color: var(--muted);
  background: rgba(37, 99, 235, 0.035);
  text-align: center;
}

.history-collapsed b {
  color: var(--ink);
  font-size: 13px;
  line-height: 18px;
}

.history-collapsed span {
  font-size: 12px;
  line-height: 18px;
}

.task-count {
  min-height: 24px;
  border-radius: 6px;
  padding: 3px 8px;
  color: var(--muted);
  background: var(--chip-bg);
  font-size: 12px;
  font-weight: 800;
  line-height: 18px;
  white-space: nowrap;
}

.task-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  margin-bottom: 10px;
}

.task-search :deep(.el-input__wrapper) {
  border-radius: 8px;
}

.task-list {
  display: grid;
  gap: 10px;
  max-height: 386px;
  overflow-y: auto;
  overscroll-behavior: contain;
  margin: 0 -4px;
  padding: 2px 4px 4px;
  scrollbar-width: thin;
}

.task-list-item {
  width: 100%;
  min-width: 0;
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr) auto;
  align-items: start;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 14px;
  padding: 7px;
  background: var(--tile-bg);
  text-align: left;
  cursor: pointer;
}

.task-side {
  min-width: 0;
  align-self: start;
  display: grid;
  justify-items: end;
  gap: 4px;
}

.task-retry-btn,
.task-delete-btn {
  min-height: 20px;
  padding: 0;
}

.task-list-item.active {
  border-color: var(--lc-primary);
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
}

.task-list-item:hover {
  border-color: rgba(37, 99, 235, 0.34);
  box-shadow: 0 8px 20px rgba(15, 23, 42, 0.06);
}

.task-list-more {
  min-height: 26px;
  display: grid;
  place-items: center;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.history-deferred {
  min-height: 112px;
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr);
  align-items: center;
  gap: 10px;
  border: 1px dashed rgba(37, 99, 235, 0.26);
  border-radius: 14px;
  padding: 14px;
  color: var(--muted);
  background: rgba(37, 99, 235, 0.04);
}

.history-deferred b {
  display: block;
  color: var(--ink);
  font-size: 13px;
  line-height: 20px;
}

.history-deferred p {
  margin-top: 3px;
  font-size: 12px;
  line-height: 18px;
}

.deferred-dot {
  width: 18px;
  height: 18px;
  border: 3px solid rgba(37, 99, 235, 0.18);
  border-top-color: var(--lc-primary);
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

.task-thumb {
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border-radius: 8px;
  color: var(--muted);
  background: var(--tile-soft);
}

.task-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.task-list-item b,
.task-list-item small {
  display: block;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-list-item > span:not(.task-thumb):not(.task-side) {
  min-width: 0;
  overflow: hidden;
}

.task-list-item b {
  font-size: 13px;
  line-height: 18px;
}

.task-list-item small {
  color: var(--muted);
  font-size: 11px;
  line-height: 16px;
}

.task-tags {
  display: flex;
  flex-wrap: nowrap;
  gap: 3px;
  margin-top: 3px;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}

.task-tags i {
  flex: 0 0 auto;
  max-width: 100%;
  min-height: 15px;
  border: 1px solid rgba(20, 139, 127, 0.16);
  border-radius: 999px;
  padding: 1px 4px;
  color: var(--green);
  background: rgba(20, 139, 127, 0.06);
  font-size: 10px;
  font-style: normal;
  font-weight: 800;
  white-space: nowrap;
}

.task-tags.expanded {
  flex-wrap: wrap;
  gap: 4px;
}

.task-tags .more-tag {
  min-width: 20px;
  display: inline-flex;
  justify-content: center;
  color: #2f6f69;
  background: var(--lc-mint-soft);
}

:global(.task-more-popover.el-popper) {
  padding: 12px;
  border-radius: 14px;
  border-color: rgba(148, 163, 184, 0.28);
  box-shadow: 0 18px 50px rgba(15, 23, 42, 0.16);
}

:global(.task-more-panel) {
  min-width: 0;
}

:global(.task-more-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

:global(.task-more-head h3) {
  margin: 0;
  color: var(--lc-text);
  font-size: 16px;
  line-height: 22px;
}

:global(.task-more-list) {
  display: grid;
  gap: 8px;
  max-height: min(560px, calc(100vh - 180px));
  overflow-y: auto;
  overscroll-behavior: contain;
  padding-right: 2px;
  scrollbar-width: thin;
}

:global(.task-more-card) {
  width: 100%;
  min-width: 0;
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  border: 1px solid rgba(148, 163, 184, 0.32);
  border-radius: 12px;
  padding: 10px;
  background: var(--lc-surface);
  text-align: left;
  cursor: pointer;
  transition: border-color .18s ease, box-shadow .18s ease, transform .18s ease;
}

:global(.task-more-card:hover) {
  border-color: rgba(37, 99, 235, 0.42);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.08);
  transform: translateY(-1px);
}

:global(.task-more-card.active) {
  border-color: #2563eb;
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.12);
}

:global(.task-more-thumb) {
  width: 72px;
  height: 72px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border-radius: 10px;
  color: var(--lc-muted);
  background: var(--lc-surface-soft);
}

:global(.task-more-thumb img) {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}

:global(.task-more-main) {
  min-width: 0;
  display: grid;
  gap: 5px;
}

:global(.task-more-title) {
  min-width: 0;
  display: block;
  overflow: hidden;
  color: var(--lc-text);
  font-size: 14px;
  font-weight: 800;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:global(.task-more-meta) {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  color: var(--lc-muted);
  font-size: 11px;
  line-height: 16px;
}

:global(.task-more-meta span) {
  border-radius: 6px;
  padding: 2px 6px;
  background: var(--lc-surface-soft);
}

.task-brief {
  display: block;
  width: 100%;
  max-width: 100%;
  margin-top: 3px;
  color: var(--muted) !important;
  font-size: 11px;
  line-height: 16px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.text-state {
  font-style: normal;
  font-size: 12px;
  font-weight: 800;
  white-space: nowrap;
}

.text-state.success {
  color: var(--green);
}

.text-state.warning {
  color: var(--amber);
}

.text-state.danger {
  color: var(--red);
}

.text-state.muted {
  color: var(--muted);
}

.delivery-list {
  display: grid;
  gap: 4px;
}

.delivery-row {
  min-height: 36px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--muted);
  font-size: 13px;
}

.delivery-row b,
.delivery-row em {
  display: inline-flex;
  align-items: center;
  font-style: normal;
  font-weight: 800;
}

.delivery-row b {
  color: var(--green);
}

.delivery-row em {
  color: var(--muted);
}

.status-card {
  display: flex;
  align-items: center;
  gap: 14px;
  border-radius: 14px;
  padding: 12px;
  background: var(--chip-bg);
}

.status-card.warning {
  background: var(--lc-warning-soft);
}

.status-card.success {
  background: var(--lc-success-soft);
}

.status-card.danger {
  background: var(--lc-danger-soft);
}

.status-card p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 13px;
}

.status-dot {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--subtle);
}

.status-dot.warning {
  background: var(--amber);
}

.status-dot.success {
  background: var(--green);
}

.status-dot.danger {
  background: var(--red);
}

.export-actions {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.export-actions :deep(.el-button) {
  width: 100%;
  margin: 0;
}

.export-actions :deep(.el-button--primary) {
  border-color: var(--lc-primary);
  background: linear-gradient(135deg, var(--lc-primary), var(--green));
}

.asset-dialog-body {
  display: grid;
  place-items: center;
}

.asset-dialog-body img,
.asset-dialog-body video {
  max-width: 100%;
  max-height: 72vh;
  border-radius: 8px;
}

.prompt-inspect-body {
  display: grid;
  grid-template-columns: minmax(220px, 0.75fr) minmax(0, 1.25fr);
  gap: 14px;
  min-height: 420px;
}

.prompt-reference-pane,
.prompt-text-pane {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--tile-bg);
  overflow: hidden;
}

.inspect-pane-head {
  min-height: 42px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  border-bottom: 1px solid var(--line);
  padding: 10px 12px;
  background: var(--tile-soft);
}

.inspect-pane-head b {
  font-size: 13px;
  line-height: 18px;
}

.inspect-pane-head span {
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.reference-group-list {
  display: grid;
  gap: 12px;
  max-height: 520px;
  overflow-y: auto;
  padding: 10px;
}

.reference-group {
  display: grid;
  gap: 8px;
}

.reference-group h3 {
  margin: 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.prompt-reference-pane .reference-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 8px;
}

.reference-thumb {
  display: block;
  aspect-ratio: 1;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
  background: var(--tile-soft);
}

.reference-thumb img {
  width: 100%;
  height: 100%;
  display: block;
  object-fit: cover;
}

.prompt-text {
  max-height: 520px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  color: var(--ink);
  background: var(--tile-soft);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 12px;
  line-height: 1.65;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.detail-frame {
  width: 100%;
  height: 72vh;
  border: 0;
  border-radius: 8px;
  background: #fff;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1320px) {
  .commerce-workbench {
    grid-template-columns: minmax(310px, 360px) minmax(0, 1fr);
  }

  .task-column {
    grid-column: 2;
    grid-row: 1;
  }

  .composer-card {
    grid-column: 1;
    grid-row: 1;
  }

  .delivery-card {
    grid-column: 1 / -1;
    grid-row: 2;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    position: static;
  }

  .delivery-card .side-block:first-child {
    grid-row: span 2;
  }
}

@media (max-width: 960px) {
  .commerce-workbench {
    grid-template-columns: minmax(0, 1fr);
    padding: 12px;
  }

  .composer-card {
    grid-column: 1;
    grid-row: auto;
    order: 1;
    position: static;
  }

  .task-column {
    grid-column: 1;
    grid-row: auto;
    order: 2;
  }

  .delivery-card {
    grid-column: 1;
    grid-row: auto;
    order: 3;
    grid-template-columns: minmax(0, 1fr);
  }

  .delivery-card .side-block:first-child {
    grid-row: auto;
  }

  .task-header,
  .section-head {
    flex-direction: column;
    align-items: stretch;
  }

  .header-actions {
    justify-content: stretch;
  }

  .header-actions :deep(.el-button) {
    flex: 1;
  }

  .step-line,
  .copy-grid,
  .detail-section-grid,
  .video-panel,
  .prompt-inspect-body {
    grid-template-columns: minmax(0, 1fr);
  }

  .prompt-inspect-body {
    min-height: 0;
  }

  .step-item {
    grid-template-columns: 28px minmax(0, 1fr) auto;
    justify-items: start;
    text-align: left;
  }

  .step-item::before {
    display: none;
  }

  .main-copy {
    grid-row: auto;
  }

  .asset-section .section-head p {
    text-align: left;
  }
}

@media (max-width: 560px) {
  .commerce-workbench {
    padding: 12px;
    gap: 12px;
  }

  .composer-card,
  .current-task,
  .asset-section,
  .side-block {
    padding: 12px;
  }

  h1 {
    font-size: 21px;
    line-height: 28px;
  }

  h2 {
    font-size: 19px;
    line-height: 26px;
  }

  .reference-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .task-list-item {
    grid-template-columns: 46px minmax(0, 1fr);
  }

  .task-side {
    grid-column: 2;
    justify-items: start;
  }

  .task-list-item .text-state {
    grid-column: auto;
  }

  .header-actions {
    flex-wrap: wrap;
  }

  .task-meta {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
