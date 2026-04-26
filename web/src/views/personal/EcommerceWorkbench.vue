<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Close, CopyDocument, Download, Refresh, RefreshRight, View } from '@element-plus/icons-vue'
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
const nowTs = ref(Date.now())
const previewVisible = ref(false)
const detailVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
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
const tasks = ref<EcommerceTask[]>([])
const activeTask = ref<EcommerceTask | null>(null)

const form = reactive({
  platform_id: 0,
  prompt_template_id: 0,
  style_template_id: 0,
  requirement: '',
  reference_images: [] as string[],
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
  title_image: '主图',
  main_image: '场景图',
  white_image: '白底图',
  detail_image: '详情图',
  price_image: '价格图',
}

const assetOrder = ['title_image', 'main_image', 'white_image', 'detail_image', 'price_image']

const output = computed<Record<string, any>>(() => activeTask.value?.output_json || {})
const productInfo = computed<Record<string, any>>(() => output.value?.product_info || {})
const priceInfo = computed<Record<string, any>>(() => output.value?.price_info || {})
const imageSpecs = computed<Record<string, any>>(() => output.value?.image_specs || {})
const imageTextPlans = computed<Record<string, any>>(() => output.value?.image_text_plans || {})
const assets = computed(() => activeTask.value?.assets || [])
const running = computed(() => ['queued', 'running'].includes(activeTask.value?.status || ''))
const hasMoreTasks = computed(() => tasks.value.length < tasksTotal.value)
const currentPlatform = computed(() => platforms.value.find((p) => p.id === form.platform_id))
const activePlatform = computed(() => platforms.value.find((p) => p.id === activeTask.value?.platform_id))
const selectedLanguage = computed(() => currentPlatform.value?.language || '自动')
const activeLanguage = computed(() => activePlatform.value?.language || selectedLanguage.value)
const activePercent = computed(() => activeTask.value?.progress || 0)
const taskElapsed = computed(() => activeTask.value ? generationElapsed(activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const taskQueueElapsed = computed(() => activeTask.value ? queueElapsed(activeTask.value.created_at, activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const visibleAssets = computed(() => [...assets.value].sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type)))
const doneAssetCount = computed(() => assets.value.filter((asset) => assetHasImage(asset)).length)
const totalAssetCount = computed(() => Math.max(assets.value.length, assetOrder.length))
const assetMetricText = computed(() => activeTask.value ? `${doneAssetCount.value}/${totalAssetCount.value}` : '0/5')
const heroTitle = computed(() => output.value?.product_title || productInfo.value?.canonical_title || '等待生成商品标题')
const heroDescription = computed(() => output.value?.description || productInfo.value?.core_value || '提交任务后，这里会展示平台文案、图片资产和交付状态。')
const priceCopy = computed(() => output.value?.price_copy || priceInfo.value?.price_text || priceInfo.value?.promotion_text || '')
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
  const status = activeTask.value?.status || ''
  const hasCopy = !!output.value?.product_title
  const hasAsset = assets.value.length > 0
  const hasDetail = !!detailDoc.value
  const failed = status === 'failed'
  const canceled = status === 'canceled'
  const success = status === 'success'
  const active = failed || canceled ? -1 : success ? 5 : hasDetail ? 4 : hasAsset ? 3 : hasCopy ? 2 : activeTask.value ? 1 : 0
  return [
    { key: 'brief', label: '商品资料', done: active > 1 || success, active: active === 1 },
    { key: 'copy', label: '文案生成', done: active > 2 || success, active: active === 2 },
    { key: 'image', label: '图片生成', done: active > 3 || success, active: active === 3 },
    { key: 'detail', label: '详情页生成', done: active > 4 || success, active: active === 4 },
    { key: 'deliver', label: '交付完成', done: success, active: active === 5, failed: failed || canceled },
  ]
})

const deliveryItems = computed(() => [
  { key: 'brief', label: '商品资料', value: activeTask.value?.requirement ? '完成' : '-', done: !!activeTask.value?.requirement },
  { key: 'copy', label: `文案输出（${activeLanguage.value}）`, value: output.value?.product_title ? '完成' : '-', done: !!output.value?.product_title },
  { key: 'assets', label: '图片资产', value: assetMetricText.value, done: doneAssetCount.value > 0 && doneAssetCount.value === totalAssetCount.value },
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

function assetHasImage(asset: EcommerceAsset) {
  return !!asset.url && asset.status === 'success' && !brokenAssetIDs.value.has(asset.id)
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
    console.error('ecommerce workbench initialize failed:', err)
    ElMessage.error('电商工作台初始化失败')
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
    if (isAssetWorking(fresh.status)) startPolling(fresh.task_id)
  } catch (err) {
    console.error('open ecommerce task failed:', err)
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
      if (!isAssetWorking(fresh.status)) {
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
  if (!assetHasImage(asset)) return
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
  const imageAssets = visibleAssets.value.filter(assetHasImage)
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
    .map((type) => assets.value.find((asset) => asset.asset_type === type && assetHasImage(asset)))
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
  () => activeTask.value ? `${activeTask.value.task_id}:${activeTask.value.status}` : '',
  () => applySectionDefaults(activeTask.value),
)

onBeforeUnmount(() => {
  stopPolling()
  if (ticker.value) window.clearInterval(ticker.value)
})
</script>

<template>
  <div class="commerce-workbench" v-loading="loading">
    <aside class="composer-card surface">
      <div class="section-head">
        <div>
          <span class="kicker">任务创建</span>
          <h1>电商生成工作台</h1>
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
              :label="`${platform.name} · ${platform.language || '自动'}`"
              :value="platform.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="文案语言">
          <el-input :model-value="selectedLanguage" disabled />
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

    <main class="task-column">
      <section class="current-task surface">
        <template v-if="!activeTask">
          <div class="empty-current">
            <span class="kicker">当前任务</span>
            <h2>创建任务后在这里查看生成进度</h2>
            <p>也可以从右侧近期任务打开历史记录。</p>
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
              <span :class="['status-chip', statusTone[activeTask.status] || 'muted']">
                {{ statusText[activeTask.status] || activeTask.status }}
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
        <div class="section-head compact">
          <div>
            <span class="kicker">图片资产</span>
            <h2>{{ assetMetricText }}</h2>
          </div>
          <p>点击图片预览大图，支持下载或重新生成。</p>
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
              <el-button title="预览" :disabled="!assetHasImage(asset)" :icon="View" @click="openAssetPreview(asset)" />
              <el-button title="下载" :disabled="!assetHasImage(asset)" :icon="Download" @click="downloadAsset(asset)" />
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
            <div v-else class="asset-placeholder" :class="{ working: isAssetWorking(asset.status) }">
              <el-icon v-if="isAssetWorking(asset.status)" class="spin"><Loading /></el-icon>
              <el-icon v-else><Picture /></el-icon>
              <span>{{ isAssetWorking(asset.status) ? '等待生成' : (asset.error || '暂无图片') }}</span>
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

        <el-empty v-else description="任务开始后生成图片资产" :image-size="86" />
      </section>
    </main>

    <aside class="delivery-card">
      <section class="surface side-block">
        <div class="section-head compact">
          <div>
            <span class="kicker">近期任务</span>
            <h2>任务记录</h2>
          </div>
          <span v-if="tasks.length" class="task-count">{{ tasks.length }}/{{ tasksTotal || tasks.length }}</span>
        </div>

        <div
          v-if="tasks.length"
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
              <b>{{ task.output_json?.product_title || task.requirement || '未命名任务' }}</b>
              <small>{{ task.task_id }}</small>
            </span>
            <em :class="['text-state', statusTone[task.status] || 'muted']">
              {{ statusText[task.status] || task.status }}
            </em>
          </button>
          <div v-if="tasksLoading" class="task-list-more">加载中...</div>
          <div v-else-if="hasMoreTasks" class="task-list-more">继续下拉加载</div>
          <div v-else class="task-list-more">已加载全部</div>
        </div>
        <el-empty v-else description="暂无任务" :image-size="64" />
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
            下载图片资产
          </el-button>
          <el-button :disabled="!detailDoc" @click="openDetailPreview">
            <el-icon><View /></el-icon>
            预览详情页
          </el-button>
        </div>
      </section>
    </aside>

    <el-dialog
      v-model="previewVisible"
      width="920px"
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
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailVisible" width="980px" append-to-body title="详情页预览" class="detail-dialog">
      <iframe v-if="detailDoc" class="detail-frame" :srcdoc="detailDoc" sandbox="" />
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.commerce-workbench {
  --ink: #0f1115;
  --muted: #667085;
  --subtle: #98a2b3;
  --line: #e5e7eb;
  --paper: #ffffff;
  --wash: #fafafa;
  --page: #f5f2ec;
  --green: #148b7f;
  --green-strong: #04786d;
  --amber: #f59e0b;
  --red: #ef4444;
  --shadow: 0 12px 32px rgba(16, 24, 40, 0.08);
  box-sizing: border-box;
  width: 100%;
  min-height: calc(100vh - 60px);
  display: grid;
  grid-template-columns: minmax(320px, 360px) minmax(0, 1fr) minmax(286px, 320px);
  gap: 12px;
  padding: 12px;
  overflow-x: hidden;
  color: var(--ink);
  background:
    linear-gradient(90deg, rgba(20, 139, 127, 0.05), transparent 34%),
    linear-gradient(180deg, #fbfaf7, var(--page));
  font-family: "PingFang SC", "Microsoft YaHei", sans-serif;
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
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.92);
  box-shadow: var(--shadow);
}

.surface-inset {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #fcfbf8;
}

.composer-card,
.current-task,
.asset-section,
.side-block {
  padding: 14px;
}

.composer-card {
  align-self: start;
}

.task-column {
  min-width: 0;
  display: grid;
  gap: 12px;
  align-content: start;
}

.delivery-card {
  min-width: 0;
  display: grid;
  gap: 12px;
  align-content: start;
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
  color: var(--green);
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
  font-size: 22px;
  line-height: 28px;
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
  background: var(--green);
}

.time-pill {
  color: #a15c00;
  background: #fff7e6;
}

.status-chip.success {
  color: #04786d;
  background: #e6f8f6;
}

.status-chip.warning {
  color: #c46a00;
  background: #fff7e6;
}

.status-chip.danger {
  color: #d92d20;
  background: #fff1f0;
}

.status-chip.muted {
  color: #596271;
  background: #f2f4f7;
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

.brief-form :deep(.el-input__wrapper),
.brief-form :deep(.el-select__wrapper),
.brief-form :deep(.el-textarea__inner) {
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 0 0 1px var(--line) inset;
}

.brief-form :deep(.el-input__wrapper.is-focus),
.brief-form :deep(.el-select__wrapper.is-focused),
.brief-form :deep(.el-textarea__inner:focus) {
  box-shadow: 0 0 0 1px var(--green) inset, 0 0 0 3px rgba(20, 139, 127, 0.12);
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
  border-radius: 8px;
  padding: 0;
  background: #f3f4f6;
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
  border-radius: 8px;
  border-color: #d0d5dd;
  background: #fff;
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
  min-height: 32px;
  height: 32px;
  border: 0;
  border-radius: 8px;
  font-weight: 800;
  background: linear-gradient(180deg, #14b8a6, var(--green-strong));
  box-shadow: 0 10px 22px rgba(20, 139, 127, 0.22);
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
  background: #f6f7f9;
  font-size: 12px;
  line-height: 18px;
  overflow-wrap: anywhere;
}

.progress-panel {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 12px;
  background: #fcfbf8;
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
  border-color: rgba(20, 139, 127, 0.34);
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
  background: #d0d5dd;
}

.step-item:first-child::before {
  display: none;
}

.step-item.done::before,
.step-item.active::before {
  background: var(--green);
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
  background: #fff;
  font-size: 12px;
  font-weight: 800;
}

.step-item.done .step-dot {
  border-color: var(--green);
  color: #fff;
  background: var(--green);
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
  background: #e4e7ec;
}

.progress-panel :deep(.el-progress-bar__inner) {
  background: linear-gradient(90deg, var(--green), #25c7b8);
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
  border-radius: 8px;
  padding: 12px;
  background: #fff;
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
  color: #344054;
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
  border-radius: 6px;
  padding: 4px 8px;
  color: #344054;
  background: #f5f2ec;
  font-size: 12px;
  line-height: 18px;
}

.spec-list {
  margin: 8px 0 0;
  padding-left: 18px;
  color: #344054;
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
  border-radius: 8px;
  padding: 10px;
  background: #fff;
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
  border-radius: 8px;
  padding: 10px;
  background: #fff;
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
  border-radius: 8px;
  overflow: hidden;
  background: #f8fafc;
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
  background: #fffbeb;
}

.asset-tile footer {
  display: block;
}

.asset-actions {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
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
  background: #fff;
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
}

.task-count {
  min-height: 24px;
  border-radius: 6px;
  padding: 3px 8px;
  color: var(--muted);
  background: #f2f4f7;
  font-size: 12px;
  font-weight: 800;
  line-height: 18px;
  white-space: nowrap;
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
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 8px;
  background: #fff;
  text-align: left;
  cursor: pointer;
}

.task-list-item.active {
  border-color: var(--green);
  box-shadow: 0 0 0 3px rgba(20, 139, 127, 0.12);
}

.task-list-more {
  min-height: 26px;
  display: grid;
  place-items: center;
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
}

.task-thumb {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border-radius: 8px;
  color: var(--muted);
  background: #f2f4f7;
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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-list-item b {
  font-size: 13px;
  line-height: 20px;
}

.task-list-item small {
  color: var(--muted);
  font-size: 12px;
  line-height: 18px;
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
  color: #344054;
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
  border-radius: 8px;
  padding: 12px;
  background: #f7f8fa;
}

.status-card.warning {
  background: #fff7e6;
}

.status-card.success {
  background: #e6f8f6;
}

.status-card.danger {
  background: #fff1f0;
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
  border-color: var(--green);
  background: linear-gradient(180deg, #14b8a6, var(--green-strong));
}

.asset-dialog-body {
  display: grid;
  place-items: center;
}

.asset-dialog-body img {
  max-width: 100%;
  max-height: 72vh;
  border-radius: 8px;
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

  .delivery-card {
    grid-column: 1 / -1;
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

  .task-column {
    order: 1;
  }

  .composer-card {
    order: 2;
    position: static;
  }

  .delivery-card {
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
  .detail-section-grid {
    grid-template-columns: minmax(0, 1fr);
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

  .task-list-item .text-state {
    grid-column: 2;
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
