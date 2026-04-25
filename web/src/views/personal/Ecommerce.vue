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

const loading = ref(false)
const submitting = ref(false)
const polling = ref<number | null>(null)
const clockTimer = ref<number | null>(null)
const nowTs = ref(Date.now())
const platforms = ref<EcommercePlatform[]>([])
const prompts = ref<EcommercePromptTemplate[]>([])
const styles = ref<EcommerceStyleTemplate[]>([])
const tasks = ref<EcommerceTask[]>([])
const activeTask = ref<EcommerceTask | null>(null)
const retryingAssetID = ref(0)
const cancelingTask = ref(false)
const previewVisible = ref(false)
const previewAsset = ref<EcommerceAsset | null>(null)
const exportingPoster = ref(false)

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
const posterAssetOrder = ['title_image', 'main_image', 'price_image', 'white_image', 'detail_image']

const output = computed<any>(() => activeTask.value?.output_json || {})
const assets = computed(() => activeTask.value?.assets || [])
const running = computed(() => ['queued', 'running'].includes(activeTask.value?.status || ''))
const contentLoading = computed(() => running.value && !output.value?.product_title)
const assetLoading = (status: string) => ['queued', 'running'].includes(status)
const runningAssetCount = computed(() => assets.value.filter((a) => assetLoading(a.status) && !a.url).length)
const doneAssetCount = computed(() => assets.value.filter((a) => a.status === 'success' || a.url).length)
const totalAssetCount = computed(() => Math.max(assets.value.length, 5))
const imageProgressText = computed(() => `图片（${Math.min(doneAssetCount.value + runningAssetCount.value, totalAssetCount.value)}/${totalAssetCount.value}）生成中`)
const taskElapsed = computed(() => activeTask.value ? elapsedText(activeTask.value.created_at, activeTask.value.finished_at, running.value) : '0秒')
const generationSteps = computed(() => {
  const status = activeTask.value?.status || ''
  const hasContent = !!output.value?.product_title
  const hasAssets = assets.value.length > 0
  const success = status === 'success'
  const failed = status === 'failed'
  const canceled = status === 'canceled'
  const imageDone = totalAssetCount.value > 0 && doneAssetCount.value >= totalAssetCount.value
  const activeIndex = (failed || canceled) ? -1 : success ? 3 : hasAssets ? 2 : hasContent ? 1 : 0
  return [
    { key: 'prompt', label: '提示词分析', done: activeIndex > 0 || success, active: activeIndex === 0 },
    { key: 'copy', label: '文案生成', done: activeIndex > 1 || success, active: activeIndex === 1 },
    { key: 'image', label: success || imageDone ? `图片（${doneAssetCount.value}/${totalAssetCount.value}）已生成` : imageProgressText.value, done: imageDone || success, active: activeIndex === 2 },
    { key: 'done', label: canceled ? '已中断' : failed ? '生成失败' : '完成', done: success, active: false, failed: failed || canceled },
  ]
})

async function loadOptions() {
  const d = await getEcommerceOptions()
  platforms.value = d.platforms || []
  prompts.value = d.prompt_templates || []
  styles.value = d.style_templates || []
  if (!form.platform_id && platforms.value[0]) form.platform_id = platforms.value[0].id
  if (!form.prompt_template_id && prompts.value[0]) form.prompt_template_id = prompts.value[0].id
  if (!form.style_template_id && styles.value[0]) form.style_template_id = styles.value[0].id
}

async function loadTasks() {
  const d = await listEcommerceTasks({ limit: 10, offset: 0 })
  tasks.value = d.items || []
  if (!activeTask.value && tasks.value[0]) activeTask.value = tasks.value[0]
}

function readImageFile(file: File) {
  if (form.reference_images.length >= 4) {
    ElMessage.warning('最多上传 4 张参考图')
    return
  }
  if (file.size > 20 * 1024 * 1024) {
    ElMessage.warning('单张图片不能超过 20MB')
    return
  }
  if (!file.type.startsWith('image/')) {
    ElMessage.warning('请选择图片文件')
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    form.reference_images.push(String(reader.result || ''))
  }
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
    ElMessage.success('任务已提交')
    await loadTasks()
    startPolling(task.task_id)
  } finally {
    submitting.value = false
  }
}

async function openTask(task: EcommerceTask) {
  const fresh = await getEcommerceTask(task.task_id)
  activeTask.value = fresh
  if (['queued', 'running'].includes(fresh.status)) startPolling(fresh.task_id)
}

async function retryAsset(assetID: number) {
  if (!activeTask.value) return
  retryingAssetID.value = assetID
  try {
    await retryEcommerceAsset(activeTask.value.task_id, assetID)
    ElMessage.success('已重新提交图片生成')
    const fresh = await getEcommerceTask(activeTask.value.task_id)
    activeTask.value = fresh
    startPolling(fresh.task_id)
  } finally {
    retryingAssetID.value = 0
  }
}

async function cancelTask() {
  if (!activeTask.value || !running.value) return
  const ok = await ElMessageBox.confirm('确定中断当前生成任务吗？已完成的图片会保留，未完成的图片会停止更新。', '中断生成', {
    type: 'warning',
    confirmButtonText: '中断',
    cancelButtonText: '继续生成',
  }).catch(() => false)
  if (!ok || !activeTask.value) return
  cancelingTask.value = true
  try {
    const fresh = await cancelEcommerceTask(activeTask.value.task_id)
    activeTask.value = fresh
    stopPolling()
    await loadTasks()
    ElMessage.success('已中断生成')
  } finally {
    cancelingTask.value = false
  }
}

function openAssetPreview(asset: EcommerceAsset) {
  if (!asset.url) return
  previewAsset.value = asset
  previewVisible.value = true
}

function assetFileName(asset: EcommerceAsset) {
  const taskID = activeTask.value?.task_id || asset.task_id || 'ecommerce'
  return `${taskID}-${asset.asset_type || 'image'}.png`
}

async function downloadAsset(asset: EcommerceAsset) {
  if (!asset.url) return
  try {
    const res = await fetch(asset.url)
    if (!res.ok) throw new Error(`download failed: ${res.status}`)
    const blob = await res.blob()
    const objectURL = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objectURL
    a.download = assetFileName(asset)
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectURL)
  } catch (err) {
    window.open(asset.url, '_blank')
    ElMessage.error('下载失败，已打开图片')
  }
}

function drawRoundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  const radius = Math.min(r, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + radius, y)
  ctx.arcTo(x + w, y, x + w, y + h, radius)
  ctx.arcTo(x + w, y + h, x, y + h, radius)
  ctx.arcTo(x, y + h, x, y, radius)
  ctx.arcTo(x, y, x + w, y, radius)
  ctx.closePath()
}

function wrapText(ctx: CanvasRenderingContext2D, text: string, maxWidth: number) {
  const lines: string[] = []
  let line = ''
  for (const char of text || '') {
    const next = line + char
    if (ctx.measureText(next).width > maxWidth && line) {
      lines.push(line)
      line = char
    } else {
      line = next
    }
  }
  if (line) lines.push(line)
  return lines
}

function drawWrappedText(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, maxWidth: number, lineHeight: number) {
  const lines = wrapText(ctx, text, maxWidth)
  lines.forEach((line, index) => ctx.fillText(line, x, y + index * lineHeight))
  return lines.length * lineHeight
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
    const a = document.createElement('a')
    a.href = objectURL
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectURL)
  }, 'image/png')
}

async function downloadDesignPoster() {
  if (!activeTask.value) return
  const imageAssets = posterAssetOrder
    .map((type) => assets.value.find((asset) => asset.asset_type === type && asset.url))
    .filter(Boolean) as EcommerceAsset[]
  if (!imageAssets.length) {
    ElMessage.warning('暂无可导出的图片')
    return
  }
  exportingPoster.value = true
  try {
    const loaded = await Promise.all(imageAssets.map(async (asset) => ({ asset, img: await fetchImage(asset.url) })))
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('canvas not supported')

    const width = 1242
    const padding = 72
    const contentWidth = width - padding * 2
    const blockGap = 34
    const title = output.value.product_title || '电商详情方案'
    const description = output.value.description || ''
    const priceCopy = output.value.price_copy || ''
    const marketingCopy = Array.isArray(output.value.marketing_copy) ? output.value.marketing_copy : []

    ctx.font = '700 50px Arial, sans-serif'
    const titleHeight = wrapText(ctx, title, contentWidth).length * 62
    ctx.font = '400 28px Arial, sans-serif'
    const descHeight = description ? wrapText(ctx, description, contentWidth).length * 42 : 0
    const chipRows = marketingCopy.length ? Math.ceil(marketingCopy.length / 2) : 0
    const headerHeight = 96 + titleHeight + descHeight + (priceCopy ? 54 : 0) + chipRows * 56 + 58
    const imageHeight = loaded.reduce((sum, item) => {
      const h = Math.round(item.img.height * contentWidth / item.img.width)
      return sum + 56 + h + blockGap
    }, 0)
    const height = headerHeight + imageHeight + 72

    canvas.width = width
    canvas.height = height
    ctx.fillStyle = '#f7f3ec'
    ctx.fillRect(0, 0, width, height)

    let y = 64
    ctx.fillStyle = '#16181d'
    ctx.font = '700 28px Arial, sans-serif'
    ctx.fillText('电商详情完整设计方案', padding, y)
    y += 58
    ctx.font = '700 50px Arial, sans-serif'
    y += drawWrappedText(ctx, title, padding, y, contentWidth, 62)
    if (description) {
      y += 22
      ctx.fillStyle = '#4f5663'
      ctx.font = '400 28px Arial, sans-serif'
      y += drawWrappedText(ctx, description, padding, y, contentWidth, 42)
    }
    if (priceCopy) {
      y += 26
      ctx.fillStyle = '#f06f38'
      ctx.font = '700 30px Arial, sans-serif'
      y += drawWrappedText(ctx, priceCopy, padding, y, contentWidth, 42)
    }
    if (marketingCopy.length) {
      y += 18
      ctx.font = '400 24px Arial, sans-serif'
      let chipX = padding
      for (const copy of marketingCopy) {
        const text = String(copy)
        const chipW = Math.min(contentWidth, ctx.measureText(text).width + 42)
        if (chipX + chipW > padding + contentWidth) {
          chipX = padding
          y += 56
        }
        drawRoundRect(ctx, chipX, y - 30, chipW, 42, 21)
        ctx.fillStyle = '#fff8f0'
        ctx.fill()
        ctx.fillStyle = '#b75b2d'
        ctx.fillText(text, chipX + 21, y)
        chipX += chipW + 14
      }
      y += 34
    }
    y += 44

    for (const item of loaded) {
      const label = assetText[item.asset.asset_type] || item.asset.asset_type
      ctx.fillStyle = '#16181d'
      ctx.font = '700 32px Arial, sans-serif'
      ctx.fillText(label, padding, y)
      y += 24
      const imgH = Math.round(item.img.height * contentWidth / item.img.width)
      drawRoundRect(ctx, padding, y, contentWidth, imgH, 18)
      ctx.save()
      ctx.clip()
      ctx.drawImage(item.img, padding, y, contentWidth, imgH)
      ctx.restore()
      y += imgH + blockGap
    }

    downloadCanvas(canvas, `${activeTask.value.task_id}-电商详情长图.png`)
    ElMessage.success('长图已生成')
  } catch (err) {
    ElMessage.error('长图导出失败')
  } finally {
    exportingPoster.value = false
  }
}

function startPolling(taskID: string) {
  stopPolling()
  polling.value = window.setInterval(async () => {
    const fresh = await getEcommerceTask(taskID).catch(() => null)
    if (!fresh) return
    activeTask.value = fresh
    if (!['queued', 'running'].includes(fresh.status)) {
      stopPolling()
      loadTasks().catch(() => {})
    }
  }, 2500)
}

function stopPolling() {
  if (polling.value) window.clearInterval(polling.value)
  polling.value = null
}

function elapsedText(start?: string | null, end?: string | null, live = false) {
  if (!start) return '0秒'
  const startMs = new Date(start).getTime()
  if (!Number.isFinite(startMs)) return '0秒'
  const endMs = end ? new Date(end).getTime() : (live ? nowTs.value : Date.now())
  const total = Math.max(0, Math.floor((endMs - startMs) / 1000))
  const min = Math.floor(total / 60)
  const sec = total % 60
  return min > 0 ? `${min}分${sec}秒` : `${sec}秒`
}

function assetElapsed(asset: { created_at?: string; updated_at?: string; status: string }) {
  const live = assetLoading(asset.status)
  return elapsedText(asset.created_at, live ? null : asset.updated_at, live)
}

onMounted(async () => {
  clockTimer.value = window.setInterval(() => { nowTs.value = Date.now() }, 1000)
  loading.value = true
  try {
    await loadOptions()
    await loadTasks()
    if (activeTask.value && ['queued', 'running'].includes(activeTask.value.status)) {
      startPolling(activeTask.value.task_id)
    }
  } finally {
    loading.value = false
  }
})
onBeforeUnmount(() => {
  stopPolling()
  if (clockTimer.value) window.clearInterval(clockTimer.value)
})
</script>

<template>
  <div class="page-container ecommerce-page">
    <div class="workspace">
      <section class="left-pane">
        <div class="card-block">
          <div class="flex-between">
            <div>
              <h2 class="page-title">电商板块</h2>
              <div class="sub">上传商品资料，生成标题、文案、图片资产和详情页。</div>
            </div>
            <el-tag v-if="running" type="warning">生成中</el-tag>
          </div>

          <el-form label-position="top" class="form">
            <el-form-item label="商品文字">
              <el-input
                v-model="form.requirement"
                type="textarea"
                :rows="6"
                maxlength="1200"
                show-word-limit
                placeholder="输入商品名称、卖点、规格、目标客群、价格区间和活动信息"
              />
            </el-form-item>
            <el-form-item label="商品图片">
              <el-upload
                drag
                multiple
                accept="image/*"
                :auto-upload="false"
                :show-file-list="false"
                :on-change="onImageChange"
              >
                <el-icon><UploadFilled /></el-icon>
                <div>上传商品参考图，最多 4 张</div>
              </el-upload>
              <div v-if="form.reference_images.length" class="thumbs">
                <div v-for="(img, i) in form.reference_images" :key="i" class="thumb">
                  <img :src="img" alt="参考图" />
                  <el-button size="small" text type="danger" @click="removeImage(i)">移除</el-button>
                </div>
              </div>
            </el-form-item>
            <div class="select-grid">
              <el-form-item label="电商平台">
                <el-select v-model="form.platform_id" placeholder="选择平台">
                  <el-option v-for="p in platforms" :key="p.id" :label="p.name" :value="p.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="提示词模板">
                <el-select v-model="form.prompt_template_id" placeholder="选择提示词">
                  <el-option v-for="p in prompts" :key="p.id" :label="p.name" :value="p.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="风格模板">
                <el-select v-model="form.style_template_id" placeholder="选择风格">
                  <el-option v-for="s in styles" :key="s.id" :label="s.name" :value="s.id" />
                </el-select>
              </el-form-item>
            </div>
            <el-button type="primary" :loading="submitting" class="submit" @click="submit">
              生成电商内容
            </el-button>
          </el-form>
        </div>

        <div class="card-block history" v-loading="loading">
          <div class="flex-between">
            <h2 class="page-title">历史记录</h2>
            <el-button size="small" :loading="loading" @click="loadTasks">刷新</el-button>
          </div>
          <div
            v-for="task in tasks"
            :key="task.task_id"
            class="history-row"
            :class="{ active: activeTask?.task_id === task.task_id }"
            @click="openTask(task)"
          >
            <div>
              <b>{{ task.platform_name }}</b>
              <span>{{ formatDateTime(task.created_at) }}</span>
            </div>
            <el-tag size="small" :type="statusType[task.status] || 'info'">{{ statusText[task.status] || task.status }}</el-tag>
          </div>
          <el-empty v-if="tasks.length === 0" description="暂无生成记录" :image-size="80" />
        </div>
      </section>

      <section class="right-pane">
        <div class="card-block result-card">
          <div class="flex-between">
            <h2 class="page-title">生成结果</h2>
            <div v-if="activeTask" class="result-actions">
              <el-button
                v-if="running"
                type="danger"
                plain
                size="small"
                :loading="cancelingTask"
                @click="cancelTask"
              >
                中断生成
              </el-button>
              <el-progress :percentage="activeTask.progress || 0" :status="activeTask.status === 'failed' ? 'exception' : undefined" style="width:220px" />
            </div>
          </div>
          <el-empty v-if="!activeTask" description="提交任务后在这里查看结果" />
          <template v-else>
            <div class="result-meta">
              <el-tag :type="statusType[activeTask.status] || 'info'">{{ statusText[activeTask.status] || activeTask.status }}</el-tag>
              <span>耗时 {{ taskElapsed }}</span>
              <span>{{ activeTask.task_id }}</span>
              <span>{{ activeTask.prompt_name }} / {{ activeTask.style_name }}</span>
            </div>
            <div v-if="running || activeTask.status === 'success' || activeTask.status === 'failed' || activeTask.status === 'canceled'" class="generation-steps">
              <div
                v-for="(step, index) in generationSteps"
                :key="step.key"
                class="generation-step"
                :class="{ active: step.active, done: step.done, failed: step.failed }"
              >
                <span class="step-dot">
                  <el-icon v-if="step.active" class="spin"><Loading /></el-icon>
                  <el-icon v-else-if="step.done"><Check /></el-icon>
                  <el-icon v-else-if="step.failed"><Close /></el-icon>
                  <span v-else>{{ index + 1 }}</span>
                </span>
                <span>{{ step.label }}</span>
              </div>
            </div>
            <el-alert v-if="activeTask.error" type="error" :closable="false" :title="activeTask.error" />

            <div v-if="contentLoading" class="copy-block">
              <el-skeleton animated :rows="4" />
            </div>
            <div v-else class="copy-block">
              <h3>{{ output.product_title || '等待生成标题' }}</h3>
              <p>{{ output.description || '任务完成后展示商品描述。' }}</p>
              <strong>{{ output.price_copy || '' }}</strong>
              <div class="chips">
                <el-tag v-for="(s, i) in output.marketing_copy || []" :key="i" effect="plain">{{ s }}</el-tag>
              </div>
            </div>

            <div class="asset-grid">
              <div
                v-for="asset in assets"
                :key="asset.id"
                class="asset-item"
              >
                <div class="asset-head">
                  <b>{{ assetText[asset.asset_type] || asset.asset_type }}</b>
                  <div class="asset-status">
                    <span>{{ assetElapsed(asset) }}</span>
                    <el-tag size="small" :type="statusType[asset.status] || 'info'">{{ statusText[asset.status] || asset.status }}</el-tag>
                  </div>
                </div>
                <div
                  v-if="asset.url"
                  class="asset-preview"
                  title="双击放大"
                  @dblclick="openAssetPreview(asset)"
                >
                  <img :src="asset.url" :alt="asset.asset_type" />
                  <div class="asset-actions">
                    <el-tooltip content="放大" placement="top">
                      <el-button circle size="small" @click.stop="openAssetPreview(asset)">
                        <el-icon><ZoomIn /></el-icon>
                      </el-button>
                    </el-tooltip>
                    <el-tooltip content="下载" placement="top">
                      <el-button circle size="small" @click.stop="downloadAsset(asset)">
                        <el-icon><Download /></el-icon>
                      </el-button>
                    </el-tooltip>
                  </div>
                </div>
                <div v-else class="asset-empty" :class="{ pending: assetLoading(asset.status) }">
                  <template v-if="assetLoading(asset.status)">
                    <el-icon class="spin"><Loading /></el-icon>
                    <span>图片生成中 · {{ assetElapsed(asset) }}</span>
                  </template>
                  <template v-else>{{ asset.error || '等待图片生成' }}</template>
                </div>
                <el-button
                  v-if="asset.status === 'failed'"
                  size="small"
                  type="primary"
                  plain
                  class="asset-retry"
                  :loading="retryingAssetID === asset.id"
                  @click="retryAsset(asset.id)"
                >
                  重试生成
                </el-button>
              </div>
            </div>

            <div v-if="activeTask.output_html || activeTask.output_json" class="preview-toolbar">
              <el-button
                type="primary"
                plain
                :loading="exportingPoster"
                :disabled="!assets.some((asset) => asset.url)"
                @click="downloadDesignPoster"
              >
                <el-icon><Printer /></el-icon>
                导出长图
              </el-button>
            </div>
            <el-tabs v-if="activeTask.output_html || activeTask.output_json" class="preview-tabs">
              <el-tab-pane label="详情页预览">
                <div class="detail-preview" v-html="activeTask.output_html" />
              </el-tab-pane>
              <el-tab-pane label="结构化 JSON">
                <pre>{{ JSON.stringify(activeTask.output_json, null, 2) }}</pre>
              </el-tab-pane>
            </el-tabs>
          </template>
        </div>
      </section>
    </div>
  </div>

  <el-dialog
    v-model="previewVisible"
    class="asset-dialog"
    width="920px"
    append-to-body
    :title="previewAsset ? (assetText[previewAsset.asset_type] || previewAsset.asset_type) : '图片预览'"
  >
    <div v-if="previewAsset" class="asset-dialog-body">
      <img :src="previewAsset.url" :alt="previewAsset.asset_type" />
    </div>
    <template #footer>
      <div class="asset-dialog-footer">
        <el-button v-if="previewAsset" @click="downloadAsset(previewAsset)">
          <el-icon><Download /></el-icon>
          下载
        </el-button>
        <el-button type="primary" @click="previewVisible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.workspace {
  display: grid;
  grid-template-columns: minmax(360px, 430px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.sub { color: var(--el-text-color-secondary); font-size: 13px; margin-top: 4px; }
.form { margin-top: 14px; }
.select-grid { display: grid; grid-template-columns: 1fr; gap: 2px; }
.submit { width: 100%; }
.thumbs { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.thumb {
  width: 86px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  padding: 6px;
  img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 4px; display: block; }
}
.history-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  cursor: pointer;
  div { min-width: 0; display: grid; gap: 2px; }
  span { color: var(--el-text-color-secondary); font-size: 12px; }
}
.history-row.active { color: var(--el-color-primary); }
.result-card { min-height: calc(100vh - 112px); }
.result-actions { display: inline-flex; align-items: center; gap: 10px; }
.result-meta { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-bottom: 12px; color: var(--el-text-color-secondary); font-size: 13px; }
.generation-steps { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; }
.generation-step {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 10px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 999px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-blank);
  font-size: 13px;
}
.generation-step.active {
  border-color: var(--el-color-warning-light-7);
  color: var(--el-color-warning-dark-2);
  background: var(--el-color-warning-light-9);
}
.generation-step.done {
  border-color: var(--el-color-success-light-7);
  color: var(--el-color-success);
  background: var(--el-color-success-light-9);
}
.generation-step.failed {
  border-color: var(--el-color-danger-light-7);
  color: var(--el-color-danger);
  background: var(--el-color-danger-light-9);
}
.step-dot {
  width: 16px;
  height: 16px;
  display: inline-grid;
  place-items: center;
  border-radius: 50%;
  font-size: 11px;
  line-height: 1;
}
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.copy-block {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 14px;
  margin-bottom: 14px;
  h3 { margin: 0 0 8px; font-size: 20px; }
  p { margin: 0 0 10px; line-height: 1.7; color: var(--el-text-color-regular); }
  strong { color: var(--el-color-danger); }
}
.chips { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.asset-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; }
.asset-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 10px;
}
.asset-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 8px; }
.asset-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;
}
.asset-preview {
  position: relative;
  aspect-ratio: 1;
  overflow: hidden;
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
  cursor: zoom-in;

  img {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: cover;
  }
}
.asset-actions {
  position: absolute;
  right: 8px;
  bottom: 8px;
  display: flex;
  gap: 6px;
  opacity: 1;
}
.asset-empty { height: 160px; display: grid; place-items: center; color: var(--el-text-color-secondary); background: var(--el-fill-color-lighter); border-radius: 6px; text-align: center; padding: 10px; }
.asset-empty.pending {
  gap: 8px;
  color: var(--el-color-primary);
  background: linear-gradient(90deg, var(--el-fill-color-lighter), var(--el-fill-color-light), var(--el-fill-color-lighter));
  background-size: 200% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
}
.asset-retry { width: 100%; margin-top: 8px; }
@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
.preview-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-top: 18px;
}
.preview-tabs { margin-top: 10px; }
.detail-preview {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px;
  overflow: auto;
  :deep(.hero-image), :deep(.wide-image) { width: 100%; border-radius: 6px; display: block; margin: 10px 0; }
  :deep(.detail-head h1) { font-size: 24px; margin: 8px 0; }
  :deep(.copy-grid) { display: flex; gap: 8px; flex-wrap: wrap; margin: 14px 0; }
  :deep(.copy-grid span) { border: 1px solid var(--el-border-color); border-radius: 999px; padding: 6px 10px; }
}
pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-size: 12px; }
:global(.asset-dialog) { max-width: 92vw; }
:global(.asset-dialog .el-dialog__body) { padding-top: 8px; }
.asset-dialog-body {
  max-height: 72vh;
  overflow: auto;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--el-fill-color-lighter);

  img {
    max-width: 100%;
    height: auto;
    display: block;
  }
}
.asset-dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
@media (max-width: 980px) {
  .workspace { grid-template-columns: 1fr; }
  .result-card { min-height: auto; }
}
</style>
