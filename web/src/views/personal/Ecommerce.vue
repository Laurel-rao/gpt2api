<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, type UploadFile } from 'element-plus'
import {
  createEcommerceTask,
  getEcommerceOptions,
  getEcommerceTask,
  listEcommerceTasks,
  retryEcommerceAsset,
  type EcommercePlatform,
  type EcommercePromptTemplate,
  type EcommerceStyleTemplate,
  type EcommerceTask,
} from '@/api/ecommerce'
import { formatDateTime } from '@/utils/format'

const loading = ref(false)
const submitting = ref(false)
const polling = ref<number | null>(null)
const platforms = ref<EcommercePlatform[]>([])
const prompts = ref<EcommercePromptTemplate[]>([])
const styles = ref<EcommerceStyleTemplate[]>([])
const tasks = ref<EcommerceTask[]>([])
const activeTask = ref<EcommerceTask | null>(null)
const retryingAssetID = ref(0)

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
}
const statusType: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
  queued: 'info',
  running: 'warning',
  success: 'success',
  failed: 'danger',
}
const assetText: Record<string, string> = {
  title_image: '店标题图',
  main_image: '电商大图',
  white_image: '白底图',
  detail_image: '详情图',
  price_image: '价格图',
}

const output = computed<any>(() => activeTask.value?.output_json || {})
const assets = computed(() => activeTask.value?.assets || [])
const running = computed(() => ['queued', 'running'].includes(activeTask.value?.status || ''))
const contentLoading = computed(() => running.value && !output.value?.product_title)
const assetLoading = (status: string) => ['queued', 'running'].includes(status)
const runningAssetCount = computed(() => assets.value.filter((a) => assetLoading(a.status) && !a.url).length)

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

onMounted(async () => {
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
onBeforeUnmount(stopPolling)
</script>

<template>
  <div class="page-container ecommerce-page" v-loading="loading">
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

        <div class="card-block history">
          <div class="flex-between">
            <h2 class="page-title">历史记录</h2>
            <el-button size="small" @click="loadTasks">刷新</el-button>
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
            <el-progress v-if="activeTask" :percentage="activeTask.progress || 0" :status="activeTask.status === 'failed' ? 'exception' : undefined" style="width:220px" />
          </div>
          <el-empty v-if="!activeTask" description="提交任务后在这里查看结果" />
          <template v-else>
            <div class="result-meta">
              <el-tag :type="statusType[activeTask.status] || 'info'">{{ statusText[activeTask.status] || activeTask.status }}</el-tag>
              <span>{{ activeTask.task_id }}</span>
              <span>{{ activeTask.prompt_name }} / {{ activeTask.style_name }}</span>
            </div>
            <div v-if="running" class="inline-loading">
              <el-icon class="spin"><Loading /></el-icon>
              <span>{{ runningAssetCount ? `${runningAssetCount} 张图片处理中` : '正在生成电商内容' }}</span>
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
                  <el-tag size="small" :type="statusType[asset.status] || 'info'">{{ statusText[asset.status] || asset.status }}</el-tag>
                </div>
                <img v-if="asset.url" :src="asset.url" :alt="asset.asset_type" />
                <div v-else class="asset-empty" :class="{ pending: assetLoading(asset.status) }">
                  <template v-if="assetLoading(asset.status)">
                    <el-icon class="spin"><Loading /></el-icon>
                    <span>图片生成中</span>
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
.result-meta { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-bottom: 12px; color: var(--el-text-color-secondary); font-size: 13px; }
.inline-loading {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
  padding: 7px 10px;
  border: 1px solid var(--el-color-warning-light-7);
  border-radius: 6px;
  color: var(--el-color-warning-dark-2);
  background: var(--el-color-warning-light-9);
  font-size: 13px;
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
  img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 6px; display: block; }
}
.asset-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 8px; }
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
.preview-tabs { margin-top: 18px; }
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
@media (max-width: 980px) {
  .workspace { grid-template-columns: 1fr; }
  .result-card { min-height: auto; }
}
</style>
