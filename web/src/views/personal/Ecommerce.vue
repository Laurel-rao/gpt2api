<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
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
const POLL_INTERVAL = 2500

const loading = ref(false)
const submitting = ref(false)
const canceling = ref(false)
const retryingAssetID = ref(0)
const exporting = ref(false)
const polling = ref<number | null>(null)
const ticker = ref<number | null>(null)
const nowTs = ref(Date.now())
const previewVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
const brokenAssetIDs = ref<Set<number>>(new Set())
const retryPrompts = ref<Record<number, string>>({})
const previewThumbKB = 20

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
  running: 'warn',
  success: 'ok',
  failed: 'bad',
  canceled: 'muted',
}
const statusType: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
  queued: 'info',
  running: 'warning',
  success: 'success',
  failed: 'danger',
  canceled: 'info',
}
const assetText: Record<string, string> = {
  title_image: '店标题图',
  main_image: '电商大图',
  white_image: '白底图',
  detail_image: '详情图',
  price_image: '价格图',
}
const assetOrder = ['title_image', 'main_image', 'white_image', 'detail_image', 'price_image']

const output = computed<any>(() => activeTask.value?.output_json || {})
const assets = computed(() => activeTask.value?.assets || [])
const running = computed(() => ['queued', 'running'].includes(activeTask.value?.status || ''))
const currentPlatform = computed(() => platforms.value.find((p) => p.id === form.platform_id))
const activePlatform = computed(() => platforms.value.find((p) => p.id === activeTask.value?.platform_id))
const selectedLanguage = computed(() => currentPlatform.value?.language || '自动')
const activeLanguage = computed(() => activePlatform.value?.language || '自动')
const visibleAssets = computed(() => [...assets.value].sort((a, b) => assetRank(a.asset_type) - assetRank(b.asset_type)))
const doneAssetCount = computed(() => assets.value.filter((asset) => asset.status === 'success' && asset.url && !brokenAssetIDs.value.has(asset.id)).length)
const totalAssetCount = computed(() => Math.max(assets.value.length, 5))
const assetMetricText = computed(() => activeTask.value ? `${doneAssetCount.value}/${totalAssetCount.value}` : '--')
const activePercent = computed(() => activeTask.value?.progress || 0)
const taskElapsed = computed(() => activeTask.value ? generationElapsed(activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const taskQueueElapsed = computed(() => activeTask.value ? queueElapsed(activeTask.value.created_at, activeTask.value.started_at, activeTask.value.finished_at, running.value) : '0秒')
const heroTitle = computed(() => output.value?.product_title || '等待生成商品标题')
const heroDescription = computed(() => output.value?.description || '生成完成后，这里会展示适配平台语言的商品描述、卖点、价格文案和整套图片资产。')
const marketingCopy = computed<string[]>(() => Array.isArray(output.value?.marketing_copy) ? output.value.marketing_copy : [])
const detailDoc = computed(() => {
  if (!activeTask.value?.output_html) return ''
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>*{box-sizing:border-box}html,body{margin:0;max-width:100%;overflow-x:hidden;background:#fff;color:#151515;font-family:Georgia,'Songti SC',serif}.stage{width:100%;max-width:860px;margin:0 auto;padding:24px;overflow-x:hidden}.ecommerce-detail-preview{width:100%;max-width:100%;overflow-x:hidden}.ecommerce-detail-preview img{display:block;width:100%;max-width:100%;height:auto;object-fit:contain}.copy-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px}.detail-head,.detail-section{max-width:100%;overflow-wrap:anywhere}</style></head><body><main class="stage">${withThumbImages(activeTask.value.output_html)}</main></body></html>`
})
const flowSteps = computed(() => {
  const status = activeTask.value?.status || ''
  const hasCopy = !!output.value?.product_title
  const hasAsset = assets.value.length > 0
  const failed = status === 'failed'
  const canceled = status === 'canceled'
  const success = status === 'success'
  const active = failed || canceled ? -1 : success ? 4 : hasAsset ? 3 : hasCopy ? 2 : activeTask.value ? 1 : 0
  return [
    { key: 'brief', label: '商品资料', done: active > 0 || success, active: active === 0 },
    { key: 'copy', label: '营销文案', done: active > 2 || success, active: active === 1 || active === 2 },
    { key: 'image', label: `图片资产 ${doneAssetCount.value}/${totalAssetCount.value}`, done: success, active: active === 3 },
    { key: 'page', label: canceled ? '已中断' : failed ? '生成失败' : '详情页', done: success, active: false, failed: failed || canceled },
  ]
})

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

function thumbURL(url: string, kb = previewThumbKB) {
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

function withThumbImages(html: string) {
  return html.replace(/(<img\b[^>]*\bsrc=["'])([^"']+)(["'])/gi, (_match, prefix, url, suffix) => `${prefix}${thumbURL(url)}${suffix}`)
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

async function loadTasks() {
  const data = await listEcommerceTasks({ limit: 12, offset: 0 })
  tasks.value = data.items || []
}

async function initialize() {
  loading.value = true
  try {
    await Promise.all([loadOptions(), loadTasks()])
    if (activeTask.value && isAssetWorking(activeTask.value.status)) startPolling(activeTask.value.task_id)
  } catch (err) {
    console.error('ecommerce initialize failed:', err)
    ElMessage.error('电商工作室初始化失败')
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
  if (file.size > 20 * 1024 * 1024) {
    ElMessage.warning('单张图片不能超过 20MB')
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
    ElMessage.warning('请输入商品文字需求')
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
  try {
    const fresh = await getEcommerceTask(task.task_id)
    activeTask.value = fresh
    brokenAssetIDs.value = new Set()
    if (isAssetWorking(fresh.status)) startPolling(fresh.task_id)
    else stopPolling()
  } catch (err) {
    console.error('open ecommerce task failed:', err)
  }
}

async function cancelTask() {
  if (!activeTask.value || !running.value) return
  const confirmed = await ElMessageBox.confirm('中断后，已完成资产会保留，未完成资产停止继续更新。', '中断生成', {
    type: 'warning',
    confirmButtonText: '中断',
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
  polling.value = window.setInterval(async () => {
    try {
      const fresh = await getEcommerceTask(taskID)
      activeTask.value = fresh
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
}

function openAssetPreview(asset: EcommerceAsset) {
  if (!assetHasImage(asset)) return
  previewAsset.value = asset
  previewVisible.value = true
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
    const objectURL = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectURL
    link.download = assetFileName(asset)
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(objectURL)
  } catch (err) {
    console.error('download ecommerce asset failed:', err)
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

function downloadCanvas(canvas: HTMLCanvasElement, filename: string) {
  canvas.toBlob((blob) => {
    if (!blob) {
      ElMessage.error('导出失败')
      return
    }
    const objectURL = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectURL
    link.download = filename
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(objectURL)
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
    const loaded = await Promise.all(imageAssets.map(async (asset) => ({ asset, img: await fetchImage(asset.url) })))
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas unsupported')

    const width = 1242
    const padding = 72
    const contentWidth = width - padding * 2
    const blockGap = 34
    const title = String(heroTitle.value)
    const description = String(output.value?.description || '')
    const price = String(output.value?.price_copy || '')
    const lineHeight = 42
    const imageHeight = loaded.reduce((sum, item) => sum + Math.round(item.img.height * contentWidth / item.img.width) + 86 + blockGap, 0)
    const height = 360 + imageHeight + marketingCopy.value.length * 42

    canvas.width = width
    canvas.height = height
    ctx.fillStyle = '#f1eadc'
    ctx.fillRect(0, 0, width, height)
    ctx.fillStyle = '#16130f'
    ctx.font = '700 52px Georgia, serif'
    ctx.fillText(title.slice(0, 26), padding, 104)
    ctx.font = '400 28px Georgia, serif'
    wrapCanvasText(ctx, description, padding, 160, contentWidth, lineHeight, 3)
    if (price) {
      ctx.fillStyle = '#c55a26'
      ctx.font = '700 34px Georgia, serif'
      ctx.fillText(price.slice(0, 34), padding, 300)
    }
    let y = 350
    for (const item of loaded) {
      ctx.fillStyle = '#16130f'
      ctx.font = '700 30px Georgia, serif'
      ctx.fillText(assetText[item.asset.asset_type] || item.asset.asset_type, padding, y)
      y += 32
      const imgH = Math.round(item.img.height * contentWidth / item.img.width)
      ctx.drawImage(item.img, padding, y, contentWidth, imgH)
      y += imgH + blockGap
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

onMounted(() => {
  ticker.value = window.setInterval(() => { nowTs.value = Date.now() }, 1000)
  initialize()
})

onBeforeUnmount(() => {
  stopPolling()
  if (ticker.value) window.clearInterval(ticker.value)
})
</script>

<template>
  <div class="ecom-studio">
    <section class="studio-hero panel reveal-a">
      <div class="hero-copy">
        <span class="eyebrow">Commerce Atelier</span>
        <h1>电商板块</h1>
        <p>把商品文字和参考图变成标题、描述、价格图、白底图、详情图和可投放详情页。</p>
      </div>
      <div class="hero-metrics">
        <div>
          <b>{{ tasks.length }}</b>
          <span>历史任务</span>
        </div>
        <div>
          <b>{{ selectedLanguage }}</b>
          <span>平台语言</span>
        </div>
        <div>
          <b>{{ assetMetricText }}</b>
          <span>资产完成</span>
        </div>
      </div>
    </section>

    <section class="studio-grid">
      <aside class="left-rail reveal-b">
        <div class="composer panel">
          <div class="panel-head">
            <div>
              <span class="eyebrow">01 / Brief</span>
              <h2>生成任务</h2>
            </div>
            <span v-if="running" class="pulse-chip">生成中</span>
          </div>

          <el-form label-position="top" class="brief-form">
            <el-form-item label="商品文字">
              <el-input
                v-model="form.requirement"
                type="textarea"
                :rows="7"
                maxlength="1200"
                show-word-limit
                resize="none"
                placeholder="写入商品名、核心卖点、规格、目标客群、价格、活动、适用平台限制..."
              />
            </el-form-item>

            <div class="option-grid">
              <el-form-item label="电商平台">
                <el-select v-model="form.platform_id" placeholder="选择平台" filterable>
                  <el-option v-for="platform in platforms" :key="platform.id" :label="`${platform.name} · ${platform.language || 'auto'}`" :value="platform.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="提示词模板">
                <el-select v-model="form.prompt_template_id" placeholder="选择提示词" filterable>
                  <el-option v-for="prompt in prompts" :key="prompt.id" :label="prompt.name" :value="prompt.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="风格模板">
                <el-select v-model="form.style_template_id" placeholder="选择风格" filterable>
                  <el-option v-for="style in styles" :key="style.id" :label="style.name" :value="style.id" />
                </el-select>
              </el-form-item>
            </div>

            <el-form-item label="商品图片">
              <el-upload drag multiple accept="image/*" :auto-upload="false" :show-file-list="false" :on-change="onImageChange" class="drop-zone">
                <el-icon><UploadFilled /></el-icon>
                <strong>拖入参考图</strong>
                <small>最多 {{ MAX_IMAGES }} 张，每张 20MB 内</small>
              </el-upload>
              <div v-if="form.reference_images.length" class="thumb-strip">
                <button v-for="(img, index) in form.reference_images" :key="index" class="thumb-card" type="button" @click="removeImage(index)">
                  <img :src="img" alt="参考图" />
                  <span>移除</span>
                </button>
              </div>
            </el-form-item>

            <button class="launch-btn" type="button" :disabled="submitting" @click="submit">
              <el-icon v-if="submitting" class="spin"><Loading /></el-icon>
              <span>{{ submitting ? '正在提交' : '生成整套电商内容' }}</span>
            </button>
          </el-form>
        </div>

        <div class="history panel" v-loading="loading">
          <div class="panel-head compact">
            <div>
              <span class="eyebrow">02 / Archive</span>
              <h2>历史记录</h2>
            </div>
            <el-button size="small" text @click="loadTasks">刷新</el-button>
          </div>
          <button
            v-for="task in tasks"
            :key="task.task_id"
            class="history-card"
            :class="{ active: activeTask?.task_id === task.task_id }"
            type="button"
            @click="openTask(task)"
          >
            <span class="history-main">{{ task.platform_name || '未知平台' }}</span>
            <span>{{ formatDateTime(task.created_at) }}</span>
            <small>{{ task.prompt_name || '默认提示词' }} · {{ task.style_name || '默认风格' }}</small>
            <i :class="['state-dot', statusTone[task.status] || 'muted']">{{ statusText[task.status] || task.status }}</i>
          </button>
          <el-empty v-if="!tasks.length" description="暂无生成记录" :image-size="72" />
        </div>
      </aside>

      <main class="result-stage reveal-c">
        <div class="panel result-shell">
          <template v-if="!activeTask">
            <div class="blank-output">
              <span class="eyebrow">03 / Output</span>
              <h2>等待新任务</h2>
              <p>这里默认保持空白。提交新生成任务后会展示实时结果；点击左侧历史记录后才会载入历史生成信息。</p>
            </div>
          </template>
          <template v-else>
            <div class="result-top">
              <div>
                <span class="eyebrow">03 / Output</span>
                <h2>{{ heroTitle }}</h2>
              </div>
              <div class="result-controls">
                <button v-if="running" class="ghost-danger" type="button" :disabled="canceling" @click="cancelTask">
                  {{ canceling ? '中断中' : '中断生成' }}
                </button>
                <span :class="['state-pill', statusTone[activeTask.status] || 'muted']">{{ statusText[activeTask.status] || activeTask.status }}</span>
              </div>
            </div>

            <div class="progress-band">
              <el-progress :percentage="activePercent" :stroke-width="10" :show-text="false" />
              <span>{{ activePercent }}%</span>
              <span>生成 {{ taskElapsed }}</span>
              <span>排队 {{ taskQueueElapsed }}</span>
              <code>{{ activeTask.task_id }}</code>
            </div>

            <div class="flow-line">
              <div v-for="step in flowSteps" :key="step.key" :class="['flow-step', { active: step.active, done: step.done, failed: step.failed }]">
                <span></span>
                {{ step.label }}
              </div>
            </div>

            <section class="task-meta">
              <span>平台：{{ activeTask.platform_name || '未知平台' }} · {{ activeLanguage }}</span>
              <span>提示词模板：{{ activeTask.prompt_name || '默认提示词' }}</span>
              <span>风格模板：{{ activeTask.style_name || '默认风格' }}</span>
            </section>

            <el-alert v-if="activeTask.error" type="error" :closable="false" :title="activeTask.error" class="error-alert" />

            <section class="copy-ledger">
              <div class="ledger-block brief-block">
                <span class="eyebrow">Product Brief</span>
                <h3>商品资料</h3>
                <p>{{ activeTask.requirement || '暂无商品资料' }}</p>
              </div>
              <div class="ledger-block">
                <span class="eyebrow">Marketing Copy</span>
                <h3>{{ heroTitle }}</h3>
                <p>{{ heroDescription }}</p>
                <strong v-if="output?.price_copy">{{ output.price_copy }}</strong>
                <div v-if="marketingCopy.length" class="tag-cloud">
                  <span v-for="(copy, index) in marketingCopy" :key="index">{{ copy }}</span>
                </div>
              </div>
            </section>

            <section class="asset-board">
              <article v-for="asset in visibleAssets" :key="asset.id" class="asset-card">
                <header>
                  <b>{{ assetText[asset.asset_type] || asset.asset_type }}</b>
                  <span>生成 {{ assetGenerateElapsed(asset) }}</span>
                  <em>排队 {{ assetQueueElapsed(asset) }}</em>
                  <i :class="['state-dot', statusTone[asset.status] || 'muted']">{{ statusText[asset.status] || asset.status }}</i>
                </header>

                <div v-if="assetHasImage(asset)" class="asset-image" @dblclick="openAssetPreview(asset)">
                  <img :src="thumbURL(asset.url)" :alt="asset.asset_type" @error="markBrokenAsset(asset)" />
                  <div class="asset-tools">
                    <button type="button" @click.stop="openAssetPreview(asset)"><el-icon><ZoomIn /></el-icon></button>
                    <button type="button" @click.stop="downloadAsset(asset)"><el-icon><Download /></el-icon></button>
                  </div>
                </div>
                <div v-else class="asset-placeholder" :class="{ working: isAssetWorking(asset.status) }">
                  <el-icon v-if="isAssetWorking(asset.status)" class="spin"><Loading /></el-icon>
                  <span>{{ isAssetWorking(asset.status) ? '图片生成中' : (asset.error || '暂无图片') }}</span>
                </div>

                <textarea
                  v-if="!isAssetWorking(asset.status)"
                  v-model="retryPrompts[asset.id]"
                  class="retry-prompt"
                  rows="3"
                  maxlength="500"
                  placeholder="可选：追加描述词，用当前商品参考图进行图生图，例如：加入雷军代言发布会氛围、背景改成科技蓝..."
                />
                <button v-if="!isAssetWorking(asset.status)" class="retry-btn" type="button" :disabled="retryingAssetID === asset.id" @click="retryAsset(asset)">
                  {{ retryingAssetID === asset.id ? '重试中' : (asset.status === 'success' ? '再生成' : '重试生成') }}
                </button>
                <details v-if="asset.prompt" class="asset-prompt">
                  <summary>生成提示词</summary>
                  <pre>{{ asset.prompt }}</pre>
                </details>
              </article>
            </section>

            <section v-if="activeTask.output_json || detailDoc" class="detail-lab">
              <div class="lab-toolbar">
                <span class="eyebrow">Detail Page</span>
                <button type="button" :disabled="exporting || !assets.some(assetHasImage)" @click="exportPoster">
                  <el-icon><Printer /></el-icon>
                  {{ exporting ? '导出中' : '导出长图' }}
                </button>
              </div>
              <el-tabs class="preview-tabs">
                <el-tab-pane label="详情页预览">
                  <iframe v-if="detailDoc" class="detail-frame" :srcdoc="detailDoc" sandbox="" />
                  <el-empty v-else description="暂无详情页预览" />
                </el-tab-pane>
                <el-tab-pane label="结构化 JSON">
                  <pre>{{ JSON.stringify(activeTask.output_json || {}, null, 2) }}</pre>
                </el-tab-pane>
              </el-tabs>
            </section>
          </template>
        </div>
      </main>
    </section>

    <el-dialog v-model="previewVisible" width="920px" append-to-body :title="previewAsset ? (assetText[previewAsset.asset_type] || previewAsset.asset_type) : '图片预览'" class="asset-dialog">
      <div v-if="previewAsset" class="asset-dialog-body">
        <img :src="thumbURL(previewAsset.url, 500)" :alt="previewAsset.asset_type" />
      </div>
      <template #footer>
        <el-button v-if="previewAsset" @click="downloadAsset(previewAsset)"><el-icon><Download /></el-icon> 下载</el-button>
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.ecom-studio {
  --studio-ink: #f4f4f4;
  --studio-paper: #050505;
  --studio-card: rgba(13, 13, 13, 0.92);
  --studio-line: rgba(255, 255, 255, 0.16);
  --studio-soft: rgba(255, 255, 255, 0.62);
  --studio-muted: rgba(255, 255, 255, 0.42);
  --studio-accent: #ffffff;
  --studio-bad: #ffffff;
  min-height: 100%;
  padding: 22px;
  color: var(--studio-ink);
  background:
    radial-gradient(circle at 14% -8%, rgba(255, 255, 255, 0.16), transparent 28%),
    radial-gradient(circle at 100% 8%, rgba(255, 255, 255, 0.08), transparent 34%),
    linear-gradient(135deg, #000 0%, #111 48%, #050505 100%);
  font-family: "DIN Condensed", "Avenir Next Condensed", "PingFang SC", "Microsoft YaHei", sans-serif;
  position: relative;
  overflow: hidden;
}
.ecom-studio::before {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background-image:
    linear-gradient(rgba(255,255,255,.055) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,.055) 1px, transparent 1px);
  background-size: 28px 28px;
  mask-image: linear-gradient(to bottom, #000, transparent 78%);
}
.ecom-studio::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: linear-gradient(120deg, transparent 0 46%, rgba(255,255,255,.06) 46% 47%, transparent 47% 100%);
}
.panel {
  position: relative;
  z-index: 1;
  border: 1px solid var(--studio-line);
  border-radius: 26px;
  background: var(--studio-card);
  box-shadow: 0 24px 80px rgba(0, 0, 0, .48);
  backdrop-filter: blur(18px);
}
.studio-hero {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  padding: 30px 34px;
  margin-bottom: 18px;
  overflow: hidden;
}
.studio-hero::after {
  content: 'BLACK / WHITE';
  position: absolute;
  right: -10px;
  bottom: -22px;
  font-family: Georgia, "Songti SC", serif;
  font-size: 92px;
  line-height: 1;
  color: rgba(255, 255, 255, .055);
  letter-spacing: -5px;
}
.eyebrow {
  display: inline-flex;
  margin-bottom: 8px;
  color: #fff;
  font-size: 12px;
  font-weight: 900;
  letter-spacing: .18em;
  text-transform: uppercase;
}
h1, h2 {
  margin: 0;
  font-family: Georgia, "Songti SC", serif;
  letter-spacing: -.04em;
  color: #fff;
}
h1 { font-size: clamp(42px, 6vw, 82px); }
h2 { font-size: 25px; }
.hero-copy p {
  max-width: 620px;
  margin: 12px 0 0;
  color: var(--studio-soft);
  font-size: 16px;
}
.hero-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(92px, 1fr));
  gap: 12px;
  min-width: 360px;
  align-self: stretch;
}
.hero-metrics div {
  display: grid;
  align-content: center;
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 20px;
  padding: 18px;
  background: rgba(255,255,255,.06);
}
.hero-metrics b { font-size: 26px; color: #fff; }
.hero-metrics span { color: var(--studio-muted); font-size: 12px; }
.studio-grid {
  display: grid;
  grid-template-columns: minmax(360px, 430px) minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}
.left-rail { display: grid; gap: 18px; }
.composer, .history, .result-shell { padding: 24px; }
.panel-head, .result-top, .lab-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}
.panel-head.compact { align-items: center; }
.pulse-chip {
  border-radius: 999px;
  padding: 7px 12px;
  background: #fff;
  color: #000;
  font-size: 12px;
  font-weight: 900;
}
.brief-form :deep(.el-form-item__label) {
  color: rgba(255,255,255,.82);
  font-weight: 900;
}
.brief-form :deep(.el-textarea__inner),
.brief-form :deep(.el-input__wrapper),
.brief-form :deep(.el-select__wrapper) {
  border-radius: 14px;
  color: #fff;
  background: rgba(0,0,0,.46);
  box-shadow: inset 0 0 0 1px rgba(255,255,255,.16);
}
.brief-form :deep(.el-textarea__inner::placeholder),
.brief-form :deep(.el-input__inner::placeholder) { color: rgba(255,255,255,.32); }
.option-grid { display: grid; grid-template-columns: 1fr; gap: 2px; }
.drop-zone :deep(.el-upload-dragger) {
  border-radius: 20px;
  border-color: rgba(255,255,255,.28);
  color: rgba(255,255,255,.78);
  background: repeating-linear-gradient(-45deg, rgba(255,255,255,.08), rgba(255,255,255,.08) 10px, rgba(0,0,0,.34) 10px, rgba(0,0,0,.34) 20px);
}
.drop-zone strong { display: block; margin-top: 4px; color: #fff; }
.drop-zone small { color: rgba(255,255,255,.48); }
.thumb-strip { display: grid; grid-template-columns: repeat(4, 1fr); gap: 9px; margin-top: 12px; }
.thumb-card {
  border: 1px solid rgba(255,255,255,.16);
  padding: 0;
  border-radius: 14px;
  overflow: hidden;
  background: #000;
  color: #fff;
  cursor: pointer;
  position: relative;
}
.thumb-card img { width: 100%; aspect-ratio: 1; object-fit: cover; display: block; opacity: .72; filter: grayscale(1) contrast(1.08); }
.thumb-card span { position: absolute; inset: auto 8px 8px; font-size: 12px; }
.launch-btn {
  width: 100%;
  min-height: 52px;
  border: 1px solid #fff;
  border-radius: 16px;
  color: #000;
  background: #fff;
  font-size: 15px;
  font-weight: 900;
  letter-spacing: .08em;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  box-shadow: 0 16px 34px rgba(255,255,255,.12);
}
.launch-btn:hover { background: #000; color: #fff; }
.launch-btn:disabled { opacity: .65; cursor: wait; }
.history-card {
  width: 100%;
  border: 1px solid rgba(255,255,255,.12);
  border-radius: 16px;
  margin-bottom: 10px;
  padding: 14px;
  text-align: left;
  color: #fff;
  background: rgba(255,255,255,.05);
  cursor: pointer;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 4px 10px;
}
.history-card.active { border-color: #fff; background: rgba(255,255,255,.13); }
.history-main { font-weight: 900; }
.history-card > span:not(.history-main) { color: var(--studio-muted); font-size: 12px; }
.history-card small {
  color: rgba(255,255,255,.48);
  font-size: 12px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.state-dot, .state-pill {
  border: 1px solid rgba(255,255,255,.18);
  border-radius: 999px;
  padding: 4px 9px;
  font-style: normal;
  font-size: 12px;
  font-weight: 900;
  justify-self: end;
}
.state-pill { padding: 8px 12px; }
.ok { color: #000; background: #fff; }
.warn { color: #fff; background: #000; border-color: #fff; }
.bad { color: #fff; background: rgba(255,255,255,.16); border-color: rgba(255,255,255,.52); }
.muted { color: rgba(255,255,255,.58); background: rgba(255,255,255,.07); }
.result-shell { min-height: calc(100vh - 150px); }
.blank-output {
  min-height: calc(100vh - 210px);
  display: grid;
  place-content: center;
  text-align: center;
  color: rgba(255,255,255,.56);
}
.blank-output h2 {
  margin: 0;
  font-size: clamp(30px, 4vw, 56px);
}
.blank-output p {
  max-width: 520px;
  margin: 14px auto 0;
  line-height: 1.8;
}
.result-top h2 { max-width: 720px; font-size: clamp(26px, 3vw, 46px); }
.result-controls { display: flex; align-items: center; gap: 10px; }
.ghost-danger, .lab-toolbar button, .retry-btn {
  border: 1px solid rgba(255,255,255,.28);
  border-radius: 999px;
  padding: 8px 13px;
  background: rgba(255,255,255,.06);
  color: #fff;
  cursor: pointer;
  font-weight: 900;
}
.ghost-danger { color: #fff; border-color: rgba(255,255,255,.66); }
.progress-band {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) auto auto auto auto;
  gap: 12px;
  align-items: center;
  color: var(--studio-muted);
  font-size: 12px;
  margin-bottom: 16px;
}
.progress-band :deep(.el-progress-bar__outer) { background: rgba(255,255,255,.12); }
.progress-band :deep(.el-progress-bar__inner) { background: #fff; }
.progress-band code { color: rgba(255,255,255,.38); }
.flow-line { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; margin-bottom: 16px; }
.flow-step {
  display: flex;
  align-items: center;
  gap: 8px;
  color: rgba(255,255,255,.54);
  font-size: 13px;
  font-weight: 900;
}
.flow-step span { width: 10px; height: 10px; border-radius: 50%; background: rgba(255,255,255,.2); }
.flow-step.active span { background: #fff; box-shadow: 0 0 0 7px rgba(255,255,255,.14); }
.flow-step.done span { background: #fff; }
.flow-step.failed span { background: rgba(255,255,255,.5); }
.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: -4px 0 16px;
}
.task-meta span {
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 999px;
  padding: 7px 10px;
  color: rgba(255,255,255,.72);
  background: rgba(255,255,255,.06);
  font-size: 12px;
  font-weight: 900;
}
.copy-ledger {
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 22px;
  padding: 22px;
  background: rgba(255,255,255,.06);
  margin-bottom: 18px;
}
.copy-ledger p { margin: 0 0 12px; color: rgba(255,255,255,.72); font-size: 15px; line-height: 1.8; }
.copy-ledger strong { color: #fff; font-size: 18px; }
.tag-cloud { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 14px; }
.tag-cloud span { border: 1px solid rgba(255,255,255,.18); border-radius: 999px; padding: 7px 11px; background: rgba(255,255,255,.08); color: #fff; font-weight: 900; font-size: 12px; }
.asset-board { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 14px; }
.asset-card {
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 22px;
  padding: 12px;
  background: rgba(255,255,255,.06);
  min-width: 0;
}
.asset-card header {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 5px 8px;
  align-items: center;
  margin-bottom: 10px;
}
.asset-card header b { min-width: 0; font-size: 15px; color: #fff; }
.asset-card header > span, .asset-card header > em { color: rgba(255,255,255,.46); font-size: 12px; font-style: normal; }
.asset-card header > em { color: rgba(255,255,255,.34); }
.retry-prompt {
  width: 100%;
  margin-top: 10px;
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 14px;
  padding: 10px 11px;
  resize: vertical;
  color: #fff;
  background: rgba(0,0,0,.34);
  outline: none;
  font-size: 12px;
  line-height: 1.5;
}
.retry-prompt::placeholder { color: rgba(255,255,255,.34); }
.retry-prompt:focus { border-color: rgba(255,255,255,.62); }
.asset-prompt {
  margin-top: 10px;
  border: 1px solid rgba(255,255,255,.12);
  border-radius: 14px;
  background: rgba(0,0,0,.28);
  color: rgba(255,255,255,.7);
}
.asset-prompt summary {
  padding: 9px 11px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 900;
}
.asset-prompt pre {
  max-height: 220px;
  margin: 0;
  border: 0;
  border-top: 1px solid rgba(255,255,255,.1);
  border-radius: 0 0 14px 14px;
  background: rgba(0,0,0,.42);
  color: rgba(255,255,255,.76);
  font-size: 12px;
}
.asset-image, .asset-placeholder {
  min-height: 178px;
  border-radius: 16px;
  overflow: hidden;
  background: #000;
  position: relative;
}
.asset-image img { width: 100%; height: 100%; min-height: 178px; object-fit: cover; display: block; filter: grayscale(.08) contrast(1.02); }
.asset-tools {
  position: absolute;
  right: 10px;
  bottom: 10px;
  display: flex;
  gap: 8px;
}
.asset-tools button {
  width: 34px;
  height: 34px;
  border: 1px solid rgba(255,255,255,.45);
  border-radius: 50%;
  background: rgba(0,0,0,.68);
  color: #fff;
  cursor: pointer;
}
.asset-placeholder {
  display: grid;
  place-items: center;
  gap: 8px;
  color: rgba(255,255,255,.58);
  text-align: center;
  padding: 18px;
}
.asset-placeholder.working { color: #fff; }
.retry-btn { width: 100%; margin-top: 10px; }
.detail-lab {
  margin-top: 18px;
  border-top: 1px solid rgba(255,255,255,.14);
  padding-top: 18px;
}
.preview-tabs :deep(.el-tabs__item) { font-weight: 900; color: rgba(255,255,255,.56); }
.preview-tabs :deep(.el-tabs__item.is-active) { color: #fff; }
.preview-tabs :deep(.el-tabs__active-bar) { background: #fff; }
.preview-tabs :deep(.el-tabs__nav-wrap::after) { background: rgba(255,255,255,.14); }
.preview-tabs :deep(.el-tab-pane) { overflow-x: hidden; }
.detail-frame {
  width: 100%;
  height: 620px;
  border: 0;
  border-radius: 18px;
  background: #fff;
  filter: grayscale(1);
}
pre {
  max-height: 620px;
  overflow-x: hidden;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
  border-radius: 18px;
  padding: 16px;
  background: #000;
  color: #fff;
  border: 1px solid rgba(255,255,255,.14);
}
.asset-dialog-body { display: grid; place-items: center; }
.asset-dialog-body img { max-width: 100%; max-height: 72vh; border-radius: 16px; }
.spin { animation: spin 1s linear infinite; }
.reveal-a, .reveal-b, .reveal-c { animation: rise .48s ease both; }
.reveal-b { animation-delay: .06s; }
.reveal-c { animation-delay: .12s; }
@keyframes spin { to { transform: rotate(360deg); } }
@keyframes rise { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: translateY(0); } }
@media (max-width: 1180px) {
  .studio-grid { grid-template-columns: 1fr; }
  .left-rail { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 760px) {
  .ecom-studio { padding: 12px; }
  .studio-hero, .panel-head, .result-top { flex-direction: column; }
  .hero-metrics, .left-rail, .flow-line { grid-template-columns: 1fr; min-width: 0; }
  .progress-band { grid-template-columns: 1fr auto; }
  h1 { font-size: 42px; }
}
</style>
