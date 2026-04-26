<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  ArrowDown,
  Check,
  CircleCheck,
  Close,
  CopyDocument,
  Download,
  FolderOpened,
  Loading,
  Picture,
  Plus,
  Refresh,
  RefreshRight,
  View,
} from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import {
  cancelEcommerceTask,
  createEcommerceTask,
  getEcommerceOptions,
  getEcommerceTask,
  listEcommerceTasks,
  retryEcommerceAsset,
  type EcommerceAsset,
  type EcommercePlatform,
  type EcommercePromptTemplate,
  type EcommerceStyleTemplate,
  type EcommerceTask,
} from '@/api/ecommerce'
import { formatDateTime } from '@/utils/format'

const MAX_IMAGES = 4
const MAX_IMAGE_MB = 20
const POLL_INTERVAL = 2500
const TASK_PAGE_SIZE = 5

const assetOrder = ['title_image', 'main_image', 'white_image', 'detail_image', 'price_image']
const assetText: Record<string, string> = {
  title_image: '主图',
  main_image: '场景图',
  white_image: '白底图',
  detail_image: '详情图',
  price_image: '价格图',
}

const statusText: Record<string, string> = {
  queued: '排队中',
  running: '生成中',
  success: '已完成',
  failed: '失败',
  canceled: '已取消',
}

const statusTone: Record<string, string> = {
  queued: 'queued',
  running: 'running',
  success: 'success',
  failed: 'failed',
  canceled: 'muted',
}

const loading = ref(false)
const submitting = ref(false)
const canceling = ref(false)
const exporting = ref(false)
const downloadingAll = ref(false)
const tasksLoading = ref(false)
const tasksTotal = ref(0)
const retryingAssetID = ref(0)
const polling = ref<number | null>(null)
const pollingTaskID = ref('')
const ticker = ref<number | null>(null)
const previewVisible = ref(false)
const detailVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
const activeTask = ref<EcommerceTask | null>(null)
const platforms = ref<EcommercePlatform[]>([])
const prompts = ref<EcommercePromptTemplate[]>([])
const styles = ref<EcommerceStyleTemplate[]>([])
const tasks = ref<EcommerceTask[]>([])
const brokenAssetIDs = ref<Set<number>>(new Set())
const brokenTaskThumbIDs = ref<Set<number>>(new Set())
const retryPanelOpenIDs = ref<Set<number>>(new Set())
const retryPrompts = ref<Record<number, string>>({})
const nowTs = ref(Date.now())

const form = reactive({
  platform_id: 0,
  prompt_template_id: 0,
  style_template_id: 0,
  requirement: '',
  reference_images: [] as string[],
})

const output = computed<Record<string, any>>(() => activeTask.value?.output_json || {})
const productInfo = computed<Record<string, any>>(() => output.value?.product_info || {})
const priceInfo = computed<Record<string, any>>(() => output.value?.price_info || {})
const imageSpecs = computed<Record<string, any>>(() => output.value?.image_specs || {})
const imageTextPlans = computed<Record<string, any>>(() => output.value?.image_text_plans || {})
const assets = computed(() => activeTask.value?.assets || [])
const visibleAssets = computed(() => [...assets.value].sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type)))
const assetSlots = computed(() => assetOrder.map((type) => ({
  type,
  asset: visibleAssets.value.find((asset) => asset.asset_type === type),
})))
const running = computed(() => isWorkingStatus(activeTask.value?.status || ''))
const hasMoreTasks = computed(() => tasks.value.length < tasksTotal.value)
const currentPlatform = computed(() => platforms.value.find((p) => p.id === form.platform_id))
const activePlatform = computed(() => platforms.value.find((p) => p.id === activeTask.value?.platform_id))
const selectedLanguage = computed(() => currentPlatform.value?.language || '自动')
const activeLanguage = computed(() => activePlatform.value?.language || selectedLanguage.value)
const activePercent = computed(() => activeTask.value?.progress || 0)
const taskElapsed = computed(() => activeTask.value ? generationElapsed(activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const taskQueueElapsed = computed(() => activeTask.value ? queueElapsed(activeTask.value.created_at, activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const doneAssetCount = computed(() => assetSlots.value.filter((slot) => slot.asset && assetIsReady(slot.asset)).length)
const assetMetricText = computed(() => `${doneAssetCount.value}/${assetOrder.length}`)
const heroTitle = computed(() => output.value?.product_title || productInfo.value?.canonical_title || activeTask.value?.requirement || '等待创建任务')
const heroDescription = computed(() => output.value?.description || productInfo.value?.core_value || '提交商品资料后，这里会展示生成进度、图片资产和交付结果。')
const priceCopy = computed(() => output.value?.price_copy || priceInfo.value?.price_text || priceInfo.value?.promotion_text || '')
const marketingCopy = computed<string[]>(() => asStringArray(output.value?.marketing_copy))
const sellingPoints = computed<string[]>(() => asStringArray(productInfo.value?.selling_points))
const keySpecs = computed(() => uniqueStrings([
  ...asStringArray(productInfo.value?.key_specs),
  ...asStringArray(productInfo.value?.specs),
  ...asStringArray(output.value?.key_specs),
  ...asStringArray(output.value?.specs),
  ...Object.values(imageTextPlans.value).flatMap((plan: any) => asStringArray(plan?.specs)),
]).slice(0, 12))
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
const specPreview = computed(() => keySpecs.value.slice(0, 3).join(' / ') || '暂无规格参数')

const detailDoc = computed(() => {
  if (!activeTask.value?.output_html) return ''
  const body = sanitizeDetailHTML(withThumbImages(activeTask.value.output_html, 500))
  const reset = `html,body,.stage,.ecommerce-detail-preview,.ecommerce-detail-preview *{filter:none!important;-webkit-filter:none!important;mix-blend-mode:normal!important;opacity:1!important}.ecommerce-detail-preview img{display:block;width:100%;max-width:100%;height:auto;object-fit:contain}`
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:#fff;color:#111827;color-scheme:light;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif}.stage{width:100%;max-width:860px;margin:0 auto;padding:18px;overflow-x:hidden}.ecommerce-detail-preview{width:100%;max-width:100%;overflow-x:hidden}.copy-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:10px}.detail-head,.detail-section{max-width:100%;overflow-wrap:anywhere}${reset}</style></head><body><main class="stage">${body}</main><style>${reset}</style></body></html>`
})

const deliveryItems = computed(() => [
  { key: 'brief', label: '商品资料', value: activeTask.value?.requirement ? '完成' : '-', done: !!activeTask.value?.requirement },
  { key: 'copy', label: `文案输出（${activeLanguage.value}）`, value: output.value?.product_title ? '完成' : '-', done: !!output.value?.product_title },
  { key: 'assets', label: '图片资产', value: assetMetricText.value, done: doneAssetCount.value > 0 && doneAssetCount.value === assetOrder.length },
  { key: 'detail', label: '详情页预览', value: detailDoc.value ? '可预览' : '-', done: !!detailDoc.value },
  { key: 'poster', label: '长图导出', value: doneAssetCount.value > 0 ? '可导出' : '-', done: doneAssetCount.value > 0 },
])

const statusHeadline = computed(() => {
  const status = activeTask.value?.status || ''
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

function assetRank(type: string) {
  const idx = assetOrder.indexOf(type)
  return idx === -1 ? 99 : idx
}

function isWorkingStatus(status: string) {
  return status === 'queued' || status === 'running'
}

function assetIsReady(asset: EcommerceAsset) {
  return !!asset.url && asset.status === 'success'
}

function assetHasImage(asset: EcommerceAsset) {
  return assetIsReady(asset) && !brokenAssetIDs.value.has(asset.id)
}

function markBrokenAsset(asset: EcommerceAsset) {
  const next = new Set(brokenAssetIDs.value)
  next.add(asset.id)
  brokenAssetIDs.value = next
}

function taskThumbnailAsset(task: EcommerceTask) {
  return [...(task.assets || [])]
    .sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type))
    .find((asset) => !!asset.url && asset.status === 'success' && !brokenTaskThumbIDs.value.has(asset.id))
}

function markBrokenTaskThumb(asset: EcommerceAsset) {
  const next = new Set(brokenTaskThumbIDs.value)
  next.add(asset.id)
  brokenTaskThumbIDs.value = next
}

function elapsedText(start?: string | null, end?: string | null, live = false) {
  if (!start) return '0秒'
  const startTs = new Date(start).getTime()
  const endTs = end ? new Date(end).getTime() : (live ? nowTs.value : Date.now())
  if (!Number.isFinite(startTs) || !Number.isFinite(endTs) || endTs <= startTs) return '0秒'
  const seconds = Math.floor((endTs - startTs) / 1000)
  if (seconds < 60) return `${seconds}秒`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  if (minutes < 60) return `${minutes}分${rest}秒`
  const hours = Math.floor(minutes / 60)
  return `${hours}小时${minutes % 60}分`
}

function generationElapsed(start?: string | null, end?: string | null, live = false) {
  return elapsedText(start, end, live)
}

function queueElapsed(created?: string | null, started?: string | null, fallbackEnd?: string | null, live = false) {
  if (!created) return '0秒'
  if (started) return elapsedText(created, started, false)
  if (fallbackEnd) return elapsedText(created, fallbackEnd, false)
  return live ? elapsedText(created, null, true) : '0秒'
}

function assetGenerateElapsed(asset: EcommerceAsset) {
  return generationElapsed(asset.started_at, asset.finished_at, isWorkingStatus(asset.status) && !!asset.started_at)
}

function assetQueueElapsed(asset: EcommerceAsset) {
  return queueElapsed(asset.created_at, asset.started_at, asset.finished_at, asset.status === 'queued')
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

function imageSpecText(assetType: string) {
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

function toggleRetryPanel(assetID: number) {
  const next = new Set(retryPanelOpenIDs.value)
  if (next.has(assetID)) next.delete(assetID)
  else next.add(assetID)
  retryPanelOpenIDs.value = next
}

async function loadOptions() {
  const data = await getEcommerceOptions()
  platforms.value = data.platforms || []
  prompts.value = data.prompt_templates || []
  styles.value = data.style_templates || []
  if (!form.platform_id && platforms.value[0]) form.platform_id = platforms.value[0].id
  if (!form.prompt_template_id && prompts.value[0]) form.prompt_template_id = prompts.value[0].id
  if (!form.style_template_id && styles.value[0]) form.style_template_id = styles.value[0].id
}

async function loadTasks(reset = true) {
  if (tasksLoading.value) return
  tasksLoading.value = true
  try {
    const offset = reset ? 0 : tasks.value.length
    const data = await listEcommerceTasks({ limit: TASK_PAGE_SIZE, offset })
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

async function loadMoreTasks() {
  if (!hasMoreTasks.value || tasksLoading.value) return
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
  loading.value = true
  try {
    await Promise.all([loadOptions(), loadTasks()])
    if (!activeTask.value && tasks.value[0]) {
      await openTask(tasks.value[0])
    }
  } catch (err) {
    console.error('ecommerce mobile initialize failed:', err)
    ElMessage.error('电商移动工作台初始化失败')
  } finally {
    loading.value = false
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
      requirement: form.requirement.trim(),
      reference_images: form.reference_images,
    })
    activeTask.value = task
    brokenAssetIDs.value = new Set()
    ElMessage.success('任务已进入生成队列')
    await loadTasks()
    startPolling(task.task_id)
  } catch (err) {
    console.error('create ecommerce task failed:', err)
  } finally {
    submitting.value = false
  }
}

async function openTask(task: EcommerceTask) {
  stopPolling()
  try {
    const fresh = await getEcommerceTask(task.task_id)
    activeTask.value = fresh
    brokenAssetIDs.value = new Set()
    if (isWorkingStatus(fresh.status)) startPolling(fresh.task_id)
  } catch (err) {
    console.error('open ecommerce task failed:', err)
  }
}

async function refreshActiveTask() {
  if (!activeTask.value) return
  await openTask(activeTask.value)
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
    ElMessage.success('已重新提交图片生成')
  } catch (err) {
    console.error('retry ecommerce asset failed:', err)
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
      if (activeTask.value?.task_id === taskID) activeTask.value = fresh
      if (!isWorkingStatus(fresh.status)) {
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

function openAssetPreview(asset: EcommerceAsset) {
  if (!assetHasImage(asset)) return
  previewAsset.value = asset
  previewVisible.value = true
}

function openDetailPreview() {
  if (!detailDoc.value) {
    ElMessage.warning('暂无详情页预览')
    return
  }
  detailVisible.value = true
}

function assetFileName(asset: EcommerceAsset) {
  const taskID = activeTask.value?.task_id || asset.task_id || 'ecommerce'
  return `${taskID}-${asset.asset_type || 'image'}.png`
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
  const imageAssets = visibleAssets.value.filter(assetIsReady)
  if (!imageAssets.length) {
    ElMessage.warning('暂无可下载图片资产')
    return
  }
  downloadingAll.value = true
  try {
    for (const asset of imageAssets) {
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
  const imageAssets = assetOrder
    .map((type) => assets.value.find((asset) => asset.asset_type === type && assetIsReady(asset)))
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

    const width = 1080
    const padding = 48
    const contentWidth = width - padding * 2
    const blockGap = 28
    const lineHeight = 36
    const imageHeight = loaded.reduce((sum, item) => {
      const imgWidth = item.img.width || contentWidth
      const imgHeight = item.img.height || contentWidth
      return sum + Math.round(imgHeight * contentWidth / imgWidth) + 76 + blockGap
    }, 0)
    const height = 318 + imageHeight

    canvas.width = width
    canvas.height = height
    ctx.fillStyle = '#f4f1eb'
    ctx.fillRect(0, 0, width, height)
    ctx.fillStyle = '#101828'
    ctx.font = '700 44px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    ctx.fillText(String(heroTitle.value).slice(0, 24), padding, 86)
    ctx.font = '400 26px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    wrapCanvasText(ctx, String(heroDescription.value), padding, 138, contentWidth, lineHeight, 3)
    if (priceCopy.value) {
      ctx.fillStyle = '#d45b2c'
      ctx.font = '700 30px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(String(priceCopy.value).slice(0, 34), padding, 270)
    }
    let y = 318
    for (const item of loaded) {
      ctx.fillStyle = '#101828'
      ctx.font = '700 28px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.fillText(assetText[item.asset.asset_type] || item.asset.asset_type, padding, y)
      y += 30
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
  initialize()
  ticker.value = window.setInterval(() => {
    nowTs.value = Date.now()
  }, 1000)
})

onBeforeUnmount(() => {
  stopPolling()
  if (ticker.value) window.clearInterval(ticker.value)
})
</script>

<template>
  <div class="mobile-workbench" v-loading="loading">
    <div class="phone-shell">
      <section class="task-card panel">
        <div class="task-top">
          <div>
            <span class="eyebrow">当前任务</span>
            <h1>{{ heroTitle }}</h1>
          </div>
          <div class="task-actions">
            <el-button :icon="Refresh" circle @click="refreshActiveTask" />
            <el-button v-if="running" type="danger" :icon="Close" circle :loading="canceling" @click="cancelTask" />
          </div>
        </div>

        <template v-if="activeTask">
          <div class="status-line">
            <span :class="['status-chip', statusTone[activeTask.status] || 'muted']">
              {{ statusText[activeTask.status] || activeTask.status }}
            </span>
            <b>{{ activePercent }}%</b>
            <span>生成 {{ taskElapsed }}</span>
            <span>排队 {{ taskQueueElapsed }}</span>
          </div>
          <el-progress :percentage="activePercent" :stroke-width="6" :show-text="false" />
          <div class="meta-strip">
            <span>平台：{{ activeTask.platform_name || '未知平台' }}</span>
            <span>提示词模板：{{ activeTask.prompt_name || '默认模板' }}</span>
            <span>风格：{{ activeTask.style_name || '默认风格' }}</span>
            <span>{{ shortTaskID(activeTask.task_id) }}</span>
          </div>
          <el-alert v-if="activeTask.error" class="task-error" type="error" :closable="false" :title="activeTask.error" />
        </template>

        <div v-else class="empty-state compact">
          <el-icon><Picture /></el-icon>
          <span>创建任务后开始生成</span>
        </div>
      </section>

      <section class="asset-strip panel">
        <div class="section-title">
          <div>
            <span class="eyebrow">资产预览</span>
            <h2>{{ assetMetricText }}</h2>
          </div>
          <span>{{ statusHeadline }}</span>
        </div>
        <div class="strip-scroll">
          <button
            v-for="slot in assetSlots"
            :key="slot.type"
            class="strip-item"
            type="button"
            :disabled="!slot.asset || !assetHasImage(slot.asset)"
            @click="slot.asset && openAssetPreview(slot.asset)"
          >
            <img
              v-if="slot.asset && assetHasImage(slot.asset)"
              :src="thumbURL(slot.asset.url)"
              :alt="assetText[slot.type]"
              @error="markBrokenAsset(slot.asset)"
            />
            <el-icon v-else-if="slot.asset && isWorkingStatus(slot.asset.status)" class="spin"><Loading /></el-icon>
            <el-icon v-else><Picture /></el-icon>
            <span>{{ assetText[slot.type] }}</span>
          </button>
        </div>
      </section>

      <section class="create-card panel">
        <div class="section-title">
          <div>
            <span class="eyebrow">创建任务</span>
            <h2>商品资料</h2>
          </div>
          <span>{{ selectedLanguage }}</span>
        </div>

        <div class="form-stack">
          <el-select v-model="form.platform_id" placeholder="选择平台" size="large">
            <el-option v-for="item in platforms" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
          <el-select v-model="form.prompt_template_id" placeholder="选择模板" size="large">
            <el-option v-for="item in prompts" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
          <el-select v-model="form.style_template_id" placeholder="选择风格" size="large">
            <el-option v-for="item in styles" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
          <el-input
            v-model="form.requirement"
            type="textarea"
            :rows="5"
            maxlength="3000"
            show-word-limit
            resize="none"
            placeholder="粘贴商品资料、卖点、目标平台要求..."
          />
        </div>

        <div class="upload-grid">
          <div
            v-for="index in MAX_IMAGES"
            :key="index"
            class="upload-cell"
            :class="{ filled: !!form.reference_images[index - 1], locked: index - 1 > form.reference_images.length }"
          >
            <template v-if="form.reference_images[index - 1]">
              <img :src="form.reference_images[index - 1]" alt="参考图" />
              <button type="button" @click="removeImage(index - 1)">移除</button>
            </template>
            <el-upload
              v-else-if="index - 1 === form.reference_images.length"
              accept="image/*"
              :auto-upload="false"
              :show-file-list="false"
              :on-change="onImageChange"
            >
              <div class="upload-add">
                <el-icon><Plus /></el-icon>
                <span>参考图</span>
              </div>
            </el-upload>
            <div v-else class="upload-add muted">
              <el-icon><Picture /></el-icon>
              <span>待添加</span>
            </div>
          </div>
        </div>

        <el-button class="submit-btn" type="primary" size="large" :loading="submitting" @click="submit">
          创建任务
        </el-button>
      </section>

      <section class="assets-card panel">
        <div class="section-title">
          <div>
            <span class="eyebrow">图片资产</span>
            <h2>统一管理</h2>
          </div>
          <span>{{ assetMetricText }}</span>
        </div>

        <div class="asset-list">
          <article v-for="slot in assetSlots" :key="slot.type" class="asset-card">
            <header>
              <div>
                <b>{{ assetText[slot.type] }}</b>
                <small>{{ imageSpecText(slot.type) }}</small>
              </div>
              <span v-if="slot.asset" :class="['status-chip', statusTone[slot.asset.status] || 'muted']">
                {{ statusText[slot.asset.status] || slot.asset.status }}
              </span>
              <span v-else class="status-chip muted">等待</span>
            </header>

            <div class="asset-actions">
              <el-button title="预览" :disabled="!slot.asset || !assetHasImage(slot.asset)" :icon="View" @click="slot.asset && openAssetPreview(slot.asset)" />
              <el-button title="下载" :disabled="!slot.asset || !assetIsReady(slot.asset)" :icon="Download" @click="slot.asset && downloadAsset(slot.asset)" />
              <el-button
                v-if="slot.asset && isWorkingStatus(slot.asset.status)"
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
                :disabled="!slot.asset"
                :loading="slot.asset && retryingAssetID === slot.asset.id"
                :icon="RefreshRight"
                @click="slot.asset && retryAsset(slot.asset)"
              />
            </div>

            <button v-if="slot.asset && assetHasImage(slot.asset)" class="asset-image" type="button" @click="openAssetPreview(slot.asset)">
              <img :src="thumbURL(slot.asset.url)" :alt="assetText[slot.type]" @error="markBrokenAsset(slot.asset)" />
            </button>
            <div v-else class="asset-placeholder" :class="{ working: slot.asset && isWorkingStatus(slot.asset.status) }">
              <el-icon v-if="slot.asset && isWorkingStatus(slot.asset.status)" class="spin"><Loading /></el-icon>
              <el-icon v-else><Picture /></el-icon>
              <span>{{ slot.asset?.error || (slot.asset && isWorkingStatus(slot.asset.status) ? '等待生成' : '暂无图片') }}</span>
            </div>

            <footer v-if="slot.asset">
              <span>生成 {{ assetGenerateElapsed(slot.asset) }}</span>
              <span>排队 {{ assetQueueElapsed(slot.asset) }}</span>
            </footer>

            <div v-if="slot.asset && !isWorkingStatus(slot.asset.status)" class="retry-block">
              <button class="retry-toggle" type="button" @click="toggleRetryPanel(slot.asset.id)">
                重试要求
                <el-icon :class="['collapse-icon', { open: retryPanelOpenIDs.has(slot.asset.id) }]"><ArrowDown /></el-icon>
              </button>
              <el-input
                v-show="retryPanelOpenIDs.has(slot.asset.id)"
                v-model="retryPrompts[slot.asset.id]"
                type="textarea"
                :rows="2"
                maxlength="500"
                resize="none"
                placeholder="可选：补充重试要求"
              />
            </div>
          </article>
        </div>
      </section>

      <section class="delivery-card panel">
        <div class="section-title">
          <div>
            <span class="eyebrow">交付结果</span>
            <h2>下载与检查</h2>
          </div>
          <span>{{ statusHeadline }}</span>
        </div>

        <div class="copy-box">
          <div class="copy-head">
            <b>文案预览（{{ activeLanguage }}）</b>
            <el-button text :icon="CopyDocument" @click="copyOutput">复制</el-button>
          </div>
          <p>{{ copyPreview }}</p>
        </div>

        <div class="tags" v-if="quickTags.length">
          <span v-for="tag in quickTags" :key="tag">{{ tag }}</span>
        </div>

        <div class="spec-box">
          <b>规格参数</b>
          <ul v-if="keySpecs.length">
            <li v-for="spec in keySpecs" :key="spec">{{ spec }}</li>
          </ul>
          <p v-else>{{ specPreview }}</p>
        </div>

        <div class="delivery-list">
          <div v-for="item in deliveryItems" :key="item.key" class="delivery-row">
            <span>{{ item.label }}</span>
            <b v-if="item.done"><el-icon><CircleCheck /></el-icon></b>
            <em v-else>{{ item.value }}</em>
          </div>
        </div>

        <div class="export-actions">
          <el-button type="primary" :loading="exporting" :disabled="!doneAssetCount" @click="exportPoster">
            <el-icon><Download /></el-icon>
            导出长图 PNG
          </el-button>
          <el-button :loading="downloadingAll" :disabled="!doneAssetCount" @click="downloadAllAssets">
            <el-icon><FolderOpened /></el-icon>
            下载图片
          </el-button>
          <el-button :disabled="!detailDoc" @click="openDetailPreview">
            <el-icon><View /></el-icon>
            详情页
          </el-button>
        </div>
      </section>

      <section class="history-card panel">
        <div class="section-title">
          <div>
            <span class="eyebrow">近期任务</span>
            <h2>任务记录</h2>
          </div>
          <span v-if="tasks.length">{{ tasks.length }}/{{ tasksTotal || tasks.length }}</span>
        </div>

        <div v-if="tasks.length" class="task-list" @scroll="onTaskListScroll" @wheel.passive="onTaskListWheel">
          <button
            v-for="task in tasks"
            :key="task.task_id"
            :class="['task-row', { active: activeTask?.task_id === task.task_id }]"
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
            <span class="task-copy">
              <b>{{ task.output_json?.product_title || task.requirement || '未命名任务' }}</b>
              <span class="task-tags">
                <i>{{ task.platform_name || '未知平台' }}</i>
                <i>{{ task.prompt_name || '默认模板' }}</i>
                <i>{{ task.style_name || '默认风格' }}</i>
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
        <div v-else class="empty-state">
          <el-icon><Picture /></el-icon>
          <span>暂无任务</span>
        </div>
      </section>
    </div>

    <el-dialog
      v-model="previewVisible"
      width="92vw"
      append-to-body
      :title="previewAsset ? (assetText[previewAsset.asset_type] || previewAsset.asset_type) : '图片预览'"
      class="asset-dialog"
    >
      <div v-if="previewAsset" class="asset-dialog-body">
        <img :src="thumbURL(previewAsset.url, 500)" :alt="previewAsset.asset_type" />
      </div>
      <template #footer>
        <el-button v-if="previewAsset" @click="downloadAsset(previewAsset)">
          <el-icon><Download /></el-icon>
          下载
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailVisible" width="92vw" append-to-body title="详情页预览" class="detail-dialog">
      <iframe v-if="detailDoc" class="detail-frame" :srcdoc="detailDoc" />
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.mobile-workbench {
  min-height: calc(100vh - 56px);
  padding: 16px;
  background:
    linear-gradient(180deg, rgba(248, 245, 238, 0.86), rgba(235, 239, 236, 0.94)),
    #f4f1eb;
  color: #101828;
  overflow-x: hidden;
  display: flex;
  justify-content: center;
}

.phone-shell {
  width: min(430px, 100%);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.panel {
  background: rgba(255, 255, 255, 0.94);
  border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 14px;
  padding: 14px;
  box-shadow: 0 14px 34px rgba(16, 24, 40, 0.07);
}

.eyebrow {
  display: block;
  color: #0f8f80;
  font-size: 12px;
  font-weight: 800;
  line-height: 1;
  margin-bottom: 6px;
}

h1,
h2 {
  margin: 0;
  color: #09090b;
  letter-spacing: 0;
}

h1 {
  font-size: 22px;
  line-height: 1.18;
  overflow-wrap: anywhere;
}

h2 {
  font-size: 18px;
  line-height: 1.24;
}

.task-top,
.section-title,
.copy-head,
.asset-card header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
}

.task-actions {
  display: inline-flex;
  gap: 8px;
  flex-shrink: 0;
}

.section-title {
  align-items: center;
  margin-bottom: 12px;

  > span {
    color: #64748b;
    font-size: 12px;
    font-weight: 700;
    white-space: nowrap;
  }
}

.status-line {
  margin-top: 12px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  color: #667085;
  font-size: 12px;

  b {
    color: #101828;
    font-size: 18px;
  }
}

.status-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 24px;
  padding: 0 8px;
  border-radius: 7px;
  font-size: 12px;
  font-weight: 800;
  line-height: 1;
  white-space: nowrap;

  &.success {
    color: #0f8f80;
    background: #e2f6f1;
  }

  &.running,
  &.queued {
    color: #d97706;
    background: #fff7df;
  }

  &.failed {
    color: #ef4444;
    background: #feecec;
  }

  &.muted {
    color: #667085;
    background: #f2f4f7;
  }
}

.text-state {
  font-style: normal;
  font-size: 12px;
  font-weight: 800;
  white-space: nowrap;

  &.success { color: #0f8f80; }
  &.running,
  &.queued { color: #d97706; }
  &.failed { color: #ef4444; }
  &.muted { color: #667085; }
}

.meta-strip {
  display: flex;
  gap: 6px;
  margin-top: 10px;
  overflow-x: auto;
  scrollbar-width: none;

  &::-webkit-scrollbar { display: none; }

  span {
    height: 24px;
    padding: 0 8px;
    display: inline-flex;
    align-items: center;
    border-radius: 7px;
    background: #f3f6f8;
    color: #475467;
    font-size: 12px;
    white-space: nowrap;
  }
}

.task-error {
  margin-top: 10px;
}

.strip-scroll {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: 86px;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 2px;
  scrollbar-width: none;

  &::-webkit-scrollbar { display: none; }
}

.strip-item {
  width: 86px;
  height: 92px;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #f8fafc;
  color: #667085;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  overflow: hidden;
  cursor: pointer;

  &:disabled {
    cursor: default;
  }

  img {
    width: 100%;
    height: 66px;
    object-fit: cover;
    display: block;
  }

  span {
    font-size: 12px;
    font-weight: 700;
  }
}

.form-stack {
  display: grid;
  gap: 10px;
}

.upload-grid {
  margin-top: 12px;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}

.upload-cell {
  position: relative;
  min-width: 0;
  aspect-ratio: 1;
  border-radius: 12px;
  border: 1px dashed #cbd5e1;
  background: #f8fafc;
  overflow: hidden;

  &.filled {
    border-style: solid;
  }

  &.locked {
    opacity: 0.56;
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  button {
    position: absolute;
    right: 5px;
    bottom: 5px;
    height: 22px;
    padding: 0 6px;
    border: 0;
    border-radius: 6px;
    background: rgba(15, 23, 42, 0.72);
    color: #fff;
    font-size: 11px;
    cursor: pointer;
  }
}

.upload-add {
  width: 100%;
  aspect-ratio: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: #64748b;
  font-size: 12px;
  font-weight: 700;

  &.muted {
    color: #98a2b3;
  }
}

.submit-btn {
  width: 100%;
  margin-top: 12px;
  min-height: 40px;
  border-radius: 9px;
  background: #0f8f80;
  border-color: #0f8f80;
  font-weight: 800;
}

.asset-list {
  display: grid;
  gap: 10px;
}

.asset-card {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px;
  background: #fff;

  header {
    align-items: center;

    b,
    small {
      display: block;
    }

    b {
      font-size: 15px;
      line-height: 1.25;
    }

    small {
      margin-top: 2px;
      color: #64748b;
      font-size: 12px;
    }
  }
}

.asset-actions {
  margin-top: 8px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;

  :deep(.el-button) {
    margin: 0;
    min-height: 32px;
    border-radius: 8px;
  }
}

.asset-image,
.asset-placeholder {
  width: 100%;
  height: 150px;
  margin-top: 8px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  overflow: hidden;
  background: #f8fafc;
}

.asset-image {
  padding: 0;
  cursor: pointer;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
}

.asset-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #8a97aa;
  font-size: 13px;
  text-align: center;

  &.working {
    color: #d97706;
    background: #fff9eb;
  }
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.asset-card footer {
  margin-top: 8px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  color: #667085;
  font-size: 12px;
}

.retry-block {
  margin-top: 8px;
}

.retry-toggle {
  width: 100%;
  height: 32px;
  border: 1px solid #d0d5dd;
  border-radius: 8px;
  background: #fff;
  color: #344054;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
}

.collapse-icon {
  transition: transform 0.18s ease;

  &.open {
    transform: rotate(180deg);
  }
}

.copy-box,
.spec-box {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px;
  background: #fbfcfd;
}

.copy-head {
  align-items: center;
  margin-bottom: 6px;
}

.copy-box p,
.spec-box p {
  margin: 0;
  color: #475467;
  font-size: 13px;
  line-height: 1.65;
  display: -webkit-box;
  overflow: hidden;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}

.tags {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;

  span {
    height: 26px;
    display: inline-flex;
    align-items: center;
    padding: 0 9px;
    border-radius: 7px;
    background: #f3efe8;
    color: #475467;
    font-size: 12px;
    font-weight: 700;
  }
}

.spec-box {
  margin-top: 10px;

  b {
    display: block;
    margin-bottom: 6px;
  }

  ul {
    margin: 0;
    padding-left: 18px;
    color: #475467;
    font-size: 13px;
    line-height: 1.7;
  }
}

.delivery-list {
  margin-top: 10px;
  display: grid;
  gap: 8px;
}

.delivery-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 28px;
  color: #475467;
  font-size: 13px;

  b {
    color: #0f8f80;
    display: inline-flex;
  }

  em {
    color: #98a2b3;
    font-style: normal;
  }
}

.export-actions {
  margin-top: 12px;
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;

  :deep(.el-button) {
    margin: 0;
    min-height: 36px;
    border-radius: 8px;
    font-weight: 800;
  }

  :deep(.el-button--primary) {
    background: #0f8f80;
    border-color: #0f8f80;
  }
}

.task-list {
  max-height: 378px;
  overflow-y: auto;
  display: grid;
  gap: 8px;
  padding-right: 2px;
}

.task-row {
  width: 100%;
  min-width: 0;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  background: #fff;
  padding: 7px;
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr) auto;
  gap: 7px;
  align-items: start;
  cursor: pointer;

  &.active {
    border-color: #0f8f80;
    box-shadow: 0 0 0 1px rgba(15, 143, 128, 0.16);
  }
}

.task-row .text-state {
  align-self: start;
  margin-top: 1px;
}

.task-thumb {
  width: 48px;
  height: 48px;
  border-radius: 10px;
  background: #f1f5f9;
  overflow: hidden;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #94a3b8;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
}

.task-copy {
  min-width: 0;
  overflow: hidden;

  b,
  small {
    display: block;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  b {
    color: #101828;
    font-size: 13px;
    line-height: 17px;
  }

  small {
    margin-top: 2px;
    color: #667085;
    font-size: 11px;
  }
}

.task-tags {
  display: flex;
  flex-wrap: nowrap;
  gap: 3px;
  margin-top: 3px;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;

  i {
    max-width: 33%;
    min-width: 0;
    min-height: 15px;
    border: 1px solid #d8eee9;
    border-radius: 999px;
    padding: 1px 4px;
    color: #0f766e;
    background: #f0fdfa;
    font-size: 10px;
    font-style: normal;
    font-weight: 800;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.task-brief {
  display: block;
  width: 100%;
  max-width: 100%;
  margin-top: 3px;
  color: #98a2b3 !important;
  font-size: 11px;
  line-height: 16px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-list-more {
  color: #98a2b3;
  font-size: 12px;
  text-align: center;
  padding: 8px 0 2px;
}

.empty-state {
  min-height: 118px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  color: #98a2b3;
  font-size: 13px;

  &.compact {
    min-height: 72px;
  }
}

.asset-dialog-body {
  display: flex;
  justify-content: center;
  background: #f8fafc;
  border-radius: 10px;
  overflow: hidden;

  img {
    max-width: 100%;
    max-height: 70vh;
    object-fit: contain;
  }
}

.detail-frame {
  width: 100%;
  min-height: 72vh;
  border: 0;
  border-radius: 10px;
  background: #fff;
}

@media (max-width: 767px) {
  .mobile-workbench {
    padding: 10px;
    min-height: calc(100vh - 56px);
  }

  .phone-shell {
    width: 100%;
  }

  .panel {
    border-radius: 12px;
    padding: 12px;
  }
}
</style>
