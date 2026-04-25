<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import {
  createEcommerceConfig,
  deleteEcommerceConfig,
  listEcommerceConfig,
  updateEcommerceConfig,
  type ConfigKind,
} from '@/api/ecommerce'

const props = defineProps<{
  kind: ConfigKind
  title: string
}>()

const loading = ref(false)
const rows = ref<any[]>([])
const keyword = ref('')
const dlgVisible = ref(false)
const dlgLoading = ref(false)
const formRef = ref<FormInstance>()
const editingID = ref(0)

const endpointTitle = computed(() => props.title)
const isPlatform = computed(() => props.kind === 'platforms')
const isPrompt = computed(() => props.kind === 'prompt-templates')
const isStyle = computed(() => props.kind === 'style-templates')

const form = reactive<any>({
  code: '',
  name: '',
  language: 'zh-CN',
  field_schema_text: '',
  content_prompt: '',
  image_prompt: '',
  style_prompt: '',
  layout_config_text: '',
  remark: '',
  enabled: true,
})

function resetForm() {
  Object.assign(form, {
    code: '',
    name: '',
    language: 'zh-CN',
    field_schema_text: '',
    content_prompt: '',
    image_prompt: '',
    style_prompt: '',
    layout_config_text: '',
    remark: '',
    enabled: true,
  })
  editingID.value = 0
}

async function load() {
  loading.value = true
  try {
    const d = await listEcommerceConfig<any>(props.kind, keyword.value.trim())
    rows.value = d.items || []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  resetForm()
  dlgVisible.value = true
}

function openEdit(row: any) {
  resetForm()
  editingID.value = row.id
  form.code = row.code
  form.name = row.name
  form.language = row.language || 'zh-CN'
  form.remark = row.remark
  form.enabled = row.enabled
  form.field_schema_text = row.field_schema ? JSON.stringify(row.field_schema, null, 2) : ''
  form.content_prompt = row.content_prompt || ''
  form.image_prompt = row.image_prompt || ''
  form.style_prompt = row.style_prompt || ''
  form.layout_config_text = row.layout_config ? JSON.stringify(row.layout_config, null, 2) : ''
  dlgVisible.value = true
}

function parseJSON(text: string, label: string) {
  const s = text.trim()
  if (!s) return undefined
  try {
    return JSON.parse(s)
  } catch {
    throw new Error(`${label} 必须是合法 JSON`)
  }
}

function buildPayload() {
  const base: any = {
    code: form.code.trim(),
    name: form.name.trim(),
    remark: form.remark,
    enabled: form.enabled,
  }
  if (isPlatform.value) {
    base.language = form.language || 'zh-CN'
    base.field_schema = parseJSON(form.field_schema_text, '字段配置')
  } else if (isPrompt.value) {
    base.content_prompt = form.content_prompt.trim()
    base.image_prompt = form.image_prompt.trim()
  } else if (isStyle.value) {
    base.style_prompt = form.style_prompt.trim()
    base.layout_config = parseJSON(form.layout_config_text, '布局配置')
  }
  return base
}

async function submit() {
  if (!form.code.trim() || !form.name.trim()) {
    ElMessage.warning('编码和名称必填')
    return
  }
  let payload: any
  try {
    payload = buildPayload()
  } catch (err: unknown) {
    ElMessage.warning(err instanceof Error ? err.message : String(err))
    return
  }
  dlgLoading.value = true
  try {
    if (editingID.value) {
      await updateEcommerceConfig(props.kind, editingID.value, payload)
      ElMessage.success('保存成功')
    } else {
      await createEcommerceConfig(props.kind, payload)
      ElMessage.success('新增成功')
    }
    dlgVisible.value = false
    await load()
  } finally {
    dlgLoading.value = false
  }
}

async function onDelete(row: any) {
  const ok = await ElMessageBox.confirm(`确定删除「${row.name}」吗？`, '删除确认', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  }).catch(() => false)
  if (!ok) return
  await deleteEcommerceConfig(props.kind, row.id)
  ElMessage.success('已删除')
  load()
}

watch(() => props.kind, () => load(), { immediate: true })
</script>

<template>
  <div class="page-container">
    <div class="card-block">
      <div class="flex-between">
        <div>
          <h2 class="page-title">{{ endpointTitle }}</h2>
          <div class="desc">维护电商生成所需的平台、提示词和风格配置。</div>
        </div>
        <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon>新增</el-button>
      </div>

      <el-form inline class="toolbar" @submit.prevent="load">
        <el-input v-model="keyword" clearable placeholder="编码 / 名称 / 备注" style="width:260px" />
        <el-button type="primary" @click="load"><el-icon><Search /></el-icon>查询</el-button>
        <el-button @click="() => { keyword = ''; load() }">重置</el-button>
      </el-form>

      <div class="table-wrap">
        <el-table v-loading="loading" :data="rows" stripe size="small" style="min-width:900px">
          <el-table-column prop="id" label="ID" width="72" />
          <el-table-column prop="name" label="名称" min-width="150" />
          <el-table-column prop="code" label="编码" min-width="150">
            <template #default="{ row }"><code>{{ row.code }}</code></template>
          </el-table-column>
          <el-table-column v-if="isPlatform" label="生成语言" width="120">
            <template #default="{ row }">{{ row.language === 'en-US' ? '英文' : '中文' }}</template>
          </el-table-column>
          <el-table-column v-if="isPrompt" label="内容提示词" min-width="240" show-overflow-tooltip>
            <template #default="{ row }">{{ row.content_prompt }}</template>
          </el-table-column>
          <el-table-column v-if="isPrompt" label="图片提示词" min-width="240" show-overflow-tooltip>
            <template #default="{ row }">{{ row.image_prompt }}</template>
          </el-table-column>
          <el-table-column v-if="isStyle" label="风格提示词" min-width="260" show-overflow-tooltip>
            <template #default="{ row }">{{ row.style_prompt }}</template>
          </el-table-column>
          <el-table-column prop="remark" label="备注" min-width="180" show-overflow-tooltip />
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="onDelete(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <el-dialog v-model="dlgVisible" :title="editingID ? '编辑' : '新增'" width="680px">
      <el-form ref="formRef" label-position="top">
        <div class="form-grid">
          <el-form-item label="编码">
            <el-input v-model="form.code" placeholder="例如 taobao" />
          </el-form-item>
          <el-form-item label="名称">
            <el-input v-model="form.name" placeholder="显示名称" />
          </el-form-item>
        </div>
        <el-form-item v-if="isPlatform" label="生成语言 / 字段配置 JSON">
          <el-select v-model="form.language" style="margin-bottom:10px" placeholder="选择生成语言">
            <el-option label="中文（适合淘宝/京东/抖音等）" value="zh-CN" />
            <el-option label="英文（适合 Amazon/Shopee/Shopify 等）" value="en-US" />
          </el-select>
          <el-input v-model="form.field_schema_text" type="textarea" :rows="5" placeholder='{"title_max":60}' />
        </el-form-item>
        <template v-if="isPrompt">
          <el-form-item label="内容提示词">
            <el-input v-model="form.content_prompt" type="textarea" :rows="5" />
          </el-form-item>
          <el-form-item label="图片提示词">
            <el-input v-model="form.image_prompt" type="textarea" :rows="5" />
          </el-form-item>
        </template>
        <template v-if="isStyle">
          <el-form-item label="风格提示词">
            <el-input v-model="form.style_prompt" type="textarea" :rows="5" />
          </el-form-item>
          <el-form-item label="布局配置 JSON">
            <el-input v-model="form.layout_config_text" type="textarea" :rows="4" placeholder='{"tone":"clean"}' />
          </el-form-item>
        </template>
        <el-form-item label="备注">
          <el-input v-model="form.remark" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="dlgLoading" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.desc { color: var(--el-text-color-secondary); font-size: 13px; margin-top: 4px; }
.toolbar { margin: 14px 0 12px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
@media (max-width: 767px) {
  .form-grid { grid-template-columns: 1fr; }
}
</style>
