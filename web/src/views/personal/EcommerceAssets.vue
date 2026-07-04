<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import type { UploadFile } from 'element-plus/es/components/upload/index.mjs'
import {
  adminGetEcommerceLibraryAsset,
  adminListEcommerceLibraryAssets,
  adminReviewEcommerceLibraryAsset,
  adminSetEcommerceLibraryAssetEnabled,
  createEcommerceLibraryAsset,
  deleteEcommerceLibraryAsset,
  getEcommerceLibraryAsset,
  listEcommerceLibraryAssets,
  submitEcommerceLibraryAssetReview,
  updateEcommerceLibraryAsset,
  uploadEcommerceLibraryAssetFile,
  type EcommerceLibraryAsset,
  type EcommerceLibraryAssetDetail,
  type EcommerceLibraryKind,
  type EcommerceLibraryReviewStatus,
  type EcommerceLibraryScope,
} from '@/api/ecommerce'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ admin?: boolean }>()

const loading = ref(false)
const rows = ref<EcommerceLibraryAsset[]>([])
const total = ref(0)
const activeKind = ref<EcommerceLibraryKind>('product')
const selectedIDs = ref<string[]>([])
const detailVisible = ref(false)
const formVisible = ref(false)
const detailLoading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const detail = ref<EcommerceLibraryAssetDetail | null>(null)
const editing = ref<EcommerceLibraryAsset | null>(null)
const preservedGalleryItems = ref<any[]>([])

interface PendingImageFile {
  id: string
  file: File
  preview: string
}

const pendingCover = ref<PendingImageFile | null>(null)
const pendingGallery = ref<PendingImageFile[]>([])

const filter = reactive({
  keyword: '',
  scope: '' as EcommerceLibraryScope | '',
  review_status: '' as EcommerceLibraryReviewStatus | '',
  page: 1,
  page_size: 20,
})

const form = reactive({
  kind: 'product' as EcommerceLibraryKind,
  name: '',
  code: '',
  scope: 'private' as EcommerceLibraryScope,
  enabled: true,
  cover_url: '',
  gallery_urls_text: '',
  tags_text: '',
  detail_text: '',
  submit_review: false,
})

const kindText: Record<EcommerceLibraryKind, string> = { product: '商品资产', model: '模特资产' }
const scopeText: Record<string, string> = { private: '私有', public: '公共' }
const reviewText: Record<string, string> = { draft: '草稿', pending: '待审核', approved: '已通过', rejected: '已拒绝' }
const reviewTone: Record<string, 'info' | 'warning' | 'success' | 'danger'> = {
  draft: 'info',
  pending: 'warning',
  approved: 'success',
  rejected: 'danger',
}

const title = computed(() => props.admin ? '电商资产审核' : '我的电商资产库')
const desc = computed(() => props.admin ? '审核用户申请公开的商品与模特资产，必要时下架公共资产。' : '沉淀可复用商品资料与模特资料，创建任务时直接选择调用。')
const canBatchSubmit = computed(() => !props.admin && selectedIDs.value.length > 0)
const galleryURLPreviews = computed(() => parseURLLines(form.gallery_urls_text))
const detailGalleryItems = computed(() => {
  if (!detail.value) return []
  const out: Array<{ url: string; label: string }> = []
  const seen = new Set<string>()
  const add = (url: string, label: string) => {
    const clean = String(url || '').trim()
    if (!clean || seen.has(clean)) return
    seen.add(clean)
    out.push({ url: clean, label })
  }
  normalizeGalleryItems(detail.value.asset.gallery_json).forEach((item) => {
    add(item.url, isUploadedGalleryItem(item) ? '已上传' : 'URL')
  })
  detail.value.files.forEach((file) => add(file.url, file.width && file.height ? `${file.width}x${file.height}` : '已上传'))
  return out
})

function kindLabel(kind: string) {
  return kindText[kind as EcommerceLibraryKind] || kind
}

function defaultDetail(kind: EcommerceLibraryKind) {
  if (kind === 'product') {
    return {
      product: {
        brand: '',
        category: '',
        product_name: '',
        sku: '',
        spu: '',
        specs: [],
        material: '',
        dimensions: '',
        weight: '',
        colors: [],
        package_list: [],
        selling_points: [],
        target_audience: '',
        use_scenarios: [],
        price_range: '',
        forbidden_claims: [],
        compliance_notes: [],
        shooting_requirements: [],
        prompt_notes: '',
      },
    }
  }
  return {
    model: {
      model_type: 'real',
      gender: '',
      age_range: '',
      height_body: '',
      skin_tone: '',
      hair: '',
      face_features: '',
      styling: '',
      pose_expression: '',
      camera_angles: [],
      suitable_categories: [],
      forbidden_scenes: [],
      license: { status: '', expires_at: '', regions: [], channels: [] },
      virtual_identity_prompt: '',
      reference_constraints: [],
    },
  }
}

async function load() {
  loading.value = true
  try {
    const params = {
      keyword: filter.keyword.trim(),
      kind: activeKind.value,
      scope: filter.scope,
      review_status: filter.review_status,
      limit: filter.page_size,
      offset: (filter.page - 1) * filter.page_size,
    }
    const d = props.admin ? await adminListEcommerceLibraryAssets(params) : await listEcommerceLibraryAssets(params)
    rows.value = d.items || []
    total.value = d.total || 0
  } finally {
    loading.value = false
  }
}

function onSearch() {
  filter.page = 1
  load()
}

function onReset() {
  filter.keyword = ''
  filter.scope = ''
  filter.review_status = ''
  filter.page = 1
  load()
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

function parseURLLines(text: string) {
  const seen = new Set<string>()
  return text
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter((item) => {
      if (!item || seen.has(item)) return false
      seen.add(item)
      return true
    })
}

function normalizeGalleryItems(value: any) {
  if (!Array.isArray(value)) return []
  return value
    .map((item) => {
      if (typeof item === 'string') return { url: item.trim(), usage: 'url' }
      if (!item || typeof item !== 'object') return null
      const url = String(item.url || '').trim()
      if (!url) return null
      return { ...item, url }
    })
    .filter(Boolean) as any[]
}

function isUploadedGalleryItem(item: any) {
  return !!(item?.sha256 || item?.origin || item?.mime || item?.url?.startsWith?.('/ecommerce-assets/'))
}

function buildGalleryJSON() {
  const byURL = new Map<string, any>()
  preservedGalleryItems.value.forEach((item) => {
    if (item?.url) byURL.set(item.url, item)
  })
  parseURLLines(form.gallery_urls_text).forEach((url, index) => {
    byURL.set(url, { url, usage: 'url', source: 'manual_url', sort_order: index })
  })
  return Array.from(byURL.values())
}

function revokePending(file: PendingImageFile | null) {
  if (file?.preview) URL.revokeObjectURL(file.preview)
}

function clearPendingFiles() {
  revokePending(pendingCover.value)
  pendingCover.value = null
  pendingGallery.value.forEach(revokePending)
  pendingGallery.value = []
}

function makePendingFile(file: File): PendingImageFile {
  return {
    id: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
    file,
    preview: URL.createObjectURL(file),
  }
}

function setPendingCover(file: UploadFile) {
  if (!file.raw) return
  revokePending(pendingCover.value)
  pendingCover.value = makePendingFile(file.raw)
}

function clearPendingCover() {
  revokePending(pendingCover.value)
  pendingCover.value = null
}

function addPendingGallery(file: UploadFile) {
  if (!file.raw) return
  pendingGallery.value.push(makePendingFile(file.raw))
}

function removePendingGallery(id: string) {
  const index = pendingGallery.value.findIndex((item) => item.id === id)
  if (index < 0) return
  revokePending(pendingGallery.value[index])
  pendingGallery.value.splice(index, 1)
}

async function uploadPendingFiles(assetID: string) {
  if (pendingCover.value) {
    await uploadEcommerceLibraryAssetFile(assetID, pendingCover.value.file, 'cover', 0)
  }
  for (const [index, item] of pendingGallery.value.entries()) {
    await uploadEcommerceLibraryAssetFile(assetID, item.file, 'gallery', index + 1)
  }
}

function resetForm(kind = activeKind.value) {
  clearPendingFiles()
  preservedGalleryItems.value = []
  Object.assign(form, {
    kind,
    name: '',
    code: '',
    scope: 'private',
    enabled: true,
    cover_url: '',
    gallery_urls_text: '',
    tags_text: '[]',
    detail_text: JSON.stringify(defaultDetail(kind), null, 2),
    submit_review: false,
  })
  editing.value = null
}

function openCreate() {
  resetForm(activeKind.value)
  formVisible.value = true
}

function openEdit(row: EcommerceLibraryAsset) {
  clearPendingFiles()
  editing.value = row
  const galleryItems = normalizeGalleryItems(row.gallery_json)
  preservedGalleryItems.value = galleryItems.filter(isUploadedGalleryItem)
  Object.assign(form, {
    kind: row.kind,
    name: row.name,
    code: row.code,
    scope: row.scope,
    enabled: row.enabled,
    cover_url: row.cover_url,
    gallery_urls_text: galleryItems
      .filter((item) => !isUploadedGalleryItem(item))
      .map((item) => item.url)
      .join('\n'),
    tags_text: JSON.stringify(row.tags_json || [], null, 2),
    detail_text: JSON.stringify(row.detail_json || defaultDetail(row.kind), null, 2),
    submit_review: false,
  })
  formVisible.value = true
}

async function save() {
  if (!form.name.trim()) return ElMessage.warning('资产名称必填')
  let tags: any
  let detailJSON: any
  try {
    tags = parseJSON(form.tags_text, '标签')
    detailJSON = parseJSON(form.detail_text, '生产资料')
  } catch (err) {
    return ElMessage.warning(err instanceof Error ? err.message : String(err))
  }
  saving.value = true
  try {
    const payload = {
      kind: form.kind,
      name: form.name.trim(),
      code: form.code.trim(),
      scope: form.scope,
      enabled: form.enabled,
      cover_url: form.cover_url.trim(),
      gallery_json: buildGalleryJSON(),
      tags_json: tags,
      detail_json: detailJSON,
      submit_review: form.submit_review,
    }
    let savedAsset: EcommerceLibraryAsset
    if (editing.value) {
      savedAsset = await updateEcommerceLibraryAsset(editing.value.asset_id, payload)
      await uploadPendingFiles(savedAsset.asset_id)
      ElMessage.success('保存成功')
    } else {
      savedAsset = await createEcommerceLibraryAsset(payload)
      await uploadPendingFiles(savedAsset.asset_id)
      ElMessage.success('新增成功')
    }
    clearPendingFiles()
    formVisible.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function openDetail(row: EcommerceLibraryAsset) {
  detailVisible.value = true
  detailLoading.value = true
  try {
    detail.value = props.admin ? await adminGetEcommerceLibraryAsset(row.asset_id) : await getEcommerceLibraryAsset(row.asset_id)
  } finally {
    detailLoading.value = false
  }
}

async function onDetailUpload(file: UploadFile, usage: 'cover' | 'gallery') {
  const assetID = detail.value?.asset.asset_id
  if (!assetID || !file.raw) {
    ElMessage.warning('请先打开资产详情后再上传图片')
    return
  }
  uploading.value = true
  try {
    await uploadEcommerceLibraryAssetFile(assetID, file.raw, usage)
    ElMessage.success('上传成功')
    if (detailVisible.value && detail.value) await openDetail(detail.value.asset)
    await load()
  } finally {
    uploading.value = false
  }
}

function onDetailCoverUpload(file: UploadFile) {
  return onDetailUpload(file, 'cover')
}

function onDetailGalleryUpload(file: UploadFile) {
  return onDetailUpload(file, 'gallery')
}

async function submitReview(row: EcommerceLibraryAsset) {
  await submitEcommerceLibraryAssetReview(row.asset_id)
  ElMessage.success('已提交公共审核')
  load()
}

async function batchSubmitReview() {
  if (!canBatchSubmit.value) return
  for (const id of selectedIDs.value) {
    await submitEcommerceLibraryAssetReview(id).catch(() => null)
  }
  ElMessage.success('已提交所选资产审核')
  selectedIDs.value = []
  load()
}

async function remove(row: EcommerceLibraryAsset) {
  const ok = await ElMessageBox.confirm(`确定删除「${row.name}」吗？`, '删除确认', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  }).catch(() => false)
  if (!ok) return
  await deleteEcommerceLibraryAsset(row.asset_id)
  ElMessage.success('已删除')
  load()
}

async function review(row: EcommerceLibraryAsset, status: 'approved' | 'rejected') {
  const title = status === 'approved' ? '通过审核' : '拒绝审核'
  const res = await ElMessageBox.prompt('审核备注', title, {
    inputType: 'textarea',
    confirmButtonText: title,
    cancelButtonText: '取消',
  }).catch(() => null)
  if (!res) return
  await adminReviewEcommerceLibraryAsset(row.asset_id, status, res.value || '')
  ElMessage.success('审核已更新')
  load()
}

async function setEnabled(row: EcommerceLibraryAsset, enabled: boolean) {
  await adminSetEcommerceLibraryAssetEnabled(row.asset_id, enabled, enabled ? '管理员启用' : '管理员下架')
  ElMessage.success(enabled ? '已启用' : '已下架')
  load()
}

function summarize(row: EcommerceLibraryAsset) {
  const detailJSON = row.detail_json || {}
  const data = row.kind === 'product' ? detailJSON.product || {} : detailJSON.model || {}
  const parts = row.kind === 'product'
    ? [data.brand, data.category, data.sku || data.spu, asText(data.selling_points)]
    : [data.model_type, data.gender, data.age_range, data.height_body, asText(data.suitable_categories)]
  return parts.filter(Boolean).join(' / ') || '暂无资料摘要'
}

function asText(v: any) {
  if (Array.isArray(v)) return v.slice(0, 3).join('、')
  return v || ''
}

function onSelection(rows: EcommerceLibraryAsset[]) {
  selectedIDs.value = rows.map((row) => row.asset_id)
}

onBeforeUnmount(clearPendingFiles)
onMounted(load)
</script>

<template>
  <div class="page-container ecommerce-assets-page">
    <div class="card-block">
      <div class="flex-between assets-head">
        <div>
          <h2 class="page-title">{{ title }}</h2>
          <div class="desc">{{ desc }}</div>
        </div>
        <div class="head-actions">
          <el-button v-if="!admin" :disabled="!canBatchSubmit" @click="batchSubmitReview">批量提交审核</el-button>
          <el-button v-if="!admin" type="primary" @click="openCreate"><el-icon><Plus /></el-icon>新增资产</el-button>
        </div>
      </div>

      <el-tabs v-model="activeKind" @tab-change="() => { filter.page = 1; load() }">
        <el-tab-pane label="商品资产" name="product" />
        <el-tab-pane label="模特资产" name="model" />
      </el-tabs>

      <el-form inline class="toolbar" @submit.prevent="onSearch">
        <el-input v-model="filter.keyword" clearable placeholder="名称 / 编码 / 标签 / 资料" style="width:260px" />
        <el-select v-model="filter.scope" clearable placeholder="范围" style="width:120px">
          <el-option label="私有" value="private" />
          <el-option label="公共" value="public" />
        </el-select>
        <el-select v-model="filter.review_status" clearable placeholder="审核状态" style="width:140px">
          <el-option label="草稿" value="draft" />
          <el-option label="待审核" value="pending" />
          <el-option label="已通过" value="approved" />
          <el-option label="已拒绝" value="rejected" />
        </el-select>
        <el-button type="primary" @click="onSearch"><el-icon><Search /></el-icon>查询</el-button>
        <el-button @click="onReset">重置</el-button>
      </el-form>

      <div class="table-wrap">
        <el-table
          v-loading="loading"
          :data="rows"
          stripe
          size="small"
          style="min-width:1080px"
          @selection-change="onSelection"
          @row-click="openDetail"
        >
          <el-table-column v-if="!admin" type="selection" width="44" />
          <el-table-column label="封面" width="92">
            <template #default="{ row }">
              <div class="cover">
                <img v-if="row.cover_url" :src="row.cover_url" :alt="row.name" />
                <el-icon v-else><Picture /></el-icon>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="资产" min-width="240">
            <template #default="{ row }">
              <div class="asset-name">{{ row.name }}</div>
              <div class="asset-meta">{{ row.code || row.asset_id }}</div>
              <div class="asset-summary">{{ summarize(row) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="100">
            <template #default="{ row }">{{ kindLabel(row.kind) }}</template>
          </el-table-column>
          <el-table-column label="范围" width="92">
            <template #default="{ row }">
              <el-tag size="small" :type="row.scope === 'public' ? 'primary' : 'info'">{{ scopeText[row.scope] }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="审核" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="reviewTone[row.review_status]">{{ reviewText[row.review_status] }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column v-if="admin" label="归属" width="100">
            <template #default="{ row }">uid {{ row.owner_user_id }}</template>
          </el-table-column>
          <el-table-column label="状态" width="84">
            <template #default="{ row }">
              <el-tag size="small" :type="row.enabled ? 'success' : 'danger'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="更新时间" width="160">
            <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="230" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click.stop="openDetail(row)">详情</el-button>
              <template v-if="admin">
                <el-button v-if="row.review_status === 'pending'" link type="success" size="small" @click.stop="review(row, 'approved')">通过</el-button>
                <el-button v-if="row.review_status === 'pending'" link type="danger" size="small" @click.stop="review(row, 'rejected')">拒绝</el-button>
                <el-button link :type="row.enabled ? 'warning' : 'success'" size="small" @click.stop="setEnabled(row, !row.enabled)">
                  {{ row.enabled ? '下架' : '启用' }}
                </el-button>
              </template>
              <template v-else>
                <el-button link type="primary" size="small" @click.stop="openEdit(row)">编辑</el-button>
                <el-button v-if="row.review_status !== 'approved'" link type="success" size="small" @click.stop="submitReview(row)">申请公共</el-button>
                <el-button link type="danger" size="small" @click.stop="remove(row)">删除</el-button>
              </template>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <el-pagination
        class="assets-pagination"
        layout="total, sizes, prev, pager, next"
        :total="total"
        :current-page="filter.page"
        :page-size="filter.page_size"
        :page-sizes="[10, 20, 50, 100]"
        @current-change="(p: number) => { filter.page = p; load() }"
        @size-change="(s: number) => { filter.page_size = s; filter.page = 1; load() }"
      />
    </div>

    <el-dialog v-model="formVisible" :title="editing ? '编辑资产' : '新增资产'" width="860px" @closed="clearPendingFiles">
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item label="资产类型">
            <el-select v-model="form.kind" :disabled="!!editing" @change="(v: EcommerceLibraryKind) => { form.detail_text = JSON.stringify(defaultDetail(v), null, 2) }">
              <el-option label="商品资产" value="product" />
              <el-option label="模特资产" value="model" />
            </el-select>
          </el-form-item>
          <el-form-item label="资产名称">
            <el-input v-model="form.name" placeholder="例如：夏季防晒衣 SKU-A12" />
          </el-form-item>
          <el-form-item label="资产编码">
            <el-input v-model="form.code" placeholder="SKU / 内部编码" />
          </el-form-item>
          <el-form-item label="可见范围">
            <el-select v-model="form.scope">
              <el-option label="私有" value="private" />
              <el-option label="申请公共" value="public" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="封面图片">
          <div class="media-editor">
            <div class="media-fields">
              <el-input v-model="form.cover_url" placeholder="输入图片 URL，或选择本地图片上传" />
              <div class="media-actions">
                <el-upload :auto-upload="false" :show-file-list="false" accept="image/png,image/jpeg,image/webp" :on-change="setPendingCover">
                  <el-button><el-icon><Upload /></el-icon>选择本地封面</el-button>
                </el-upload>
                <el-button v-if="pendingCover" @click="clearPendingCover">移除本地封面</el-button>
              </div>
            </div>
            <div class="media-preview">
              <img v-if="pendingCover" :src="pendingCover.preview" alt="本地封面预览" />
              <img v-else-if="form.cover_url" :src="form.cover_url" alt="封面 URL 预览" />
              <el-icon v-else><Picture /></el-icon>
              <span>{{ pendingCover ? '待上传' : (form.cover_url ? 'URL' : '未设置') }}</span>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="图库图片">
          <div class="gallery-editor">
            <el-input
              v-model="form.gallery_urls_text"
              type="textarea"
              :rows="4"
              placeholder="输入图库 URL，每行一张；也可同时选择本地多图上传"
            />
            <div class="media-actions">
              <el-upload
                multiple
                :auto-upload="false"
                :show-file-list="false"
                accept="image/png,image/jpeg,image/webp"
                :on-change="addPendingGallery"
              >
                <el-button><el-icon><Upload /></el-icon>选择本地图库</el-button>
              </el-upload>
              <span class="media-hint">保存资产后自动上传本地图片，URL 图片会直接写入图库。</span>
            </div>
            <div v-if="galleryURLPreviews.length || pendingGallery.length || preservedGalleryItems.length" class="preview-grid">
              <div v-for="url in galleryURLPreviews" :key="`url-${url}`" class="preview-item">
                <img :src="url" alt="图库 URL 预览" />
                <span>URL</span>
              </div>
              <div v-for="item in pendingGallery" :key="item.id" class="preview-item">
                <img :src="item.preview" :alt="item.file.name" />
                <button type="button" class="preview-remove" @click="removePendingGallery(item.id)">移除</button>
                <span>待上传</span>
              </div>
              <div v-for="item in preservedGalleryItems" :key="`saved-${item.url}`" class="preview-item">
                <img :src="item.url" alt="已上传图库预览" />
                <span>已上传</span>
              </div>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="标签 JSON">
          <el-input v-model="form.tags_text" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="生产资料 JSON">
          <el-input v-model="form.detail_text" type="textarea" :rows="14" />
        </el-form-item>
        <div class="form-tail">
          <el-checkbox v-model="form.enabled">启用资产</el-checkbox>
          <el-checkbox v-model="form.submit_review">保存后提交公共审核</el-checkbox>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="detailVisible" title="资产详情" size="640px">
      <div v-loading="detailLoading" class="detail-drawer">
        <template v-if="detail">
          <div class="detail-hero">
            <div class="detail-cover">
              <img v-if="detail.asset.cover_url" :src="detail.asset.cover_url" :alt="detail.asset.name" />
              <el-icon v-else><Picture /></el-icon>
            </div>
            <div>
              <h3>{{ detail.asset.name }}</h3>
              <p>{{ detail.asset.code || detail.asset.asset_id }}</p>
              <div class="tag-row">
              <el-tag size="small">{{ kindLabel(detail.asset.kind) }}</el-tag>
                <el-tag size="small" :type="detail.asset.scope === 'public' ? 'primary' : 'info'">{{ scopeText[detail.asset.scope] }}</el-tag>
                <el-tag size="small" :type="reviewTone[detail.asset.review_status]">{{ reviewText[detail.asset.review_status] }}</el-tag>
              </div>
            </div>
          </div>

          <div class="upload-line" v-if="!admin">
            <el-upload :auto-upload="false" :show-file-list="false" accept="image/png,image/jpeg,image/webp" :on-change="onDetailCoverUpload">
              <el-button :loading="uploading"><el-icon><Upload /></el-icon>上传封面</el-button>
            </el-upload>
            <el-upload :auto-upload="false" :show-file-list="false" accept="image/png,image/jpeg,image/webp" :on-change="onDetailGalleryUpload">
              <el-button :loading="uploading"><el-icon><Upload /></el-icon>上传图库</el-button>
            </el-upload>
          </div>

          <h4>图库</h4>
          <div class="gallery">
            <div v-for="item in detailGalleryItems" :key="item.url" class="gallery-item">
              <img :src="item.url" :alt="detail.asset.name" />
              <span>{{ item.label }}</span>
            </div>
            <el-empty v-if="detailGalleryItems.length === 0" description="暂无图片" :image-size="80" />
          </div>

          <h4>生产资料</h4>
          <pre class="json-view">{{ JSON.stringify(detail.asset.detail_json || {}, null, 2) }}</pre>

          <h4 v-if="detail.asset.review_note">审核备注</h4>
          <p v-if="detail.asset.review_note" class="review-note">{{ detail.asset.review_note }}</p>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.ecommerce-assets-page {
  .desc {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
}
.assets-head {
  margin-bottom: 10px;
}
.head-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.cover,
.detail-cover {
  width: 64px;
  height: 64px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
}
.cover img,
.detail-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.asset-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.asset-meta,
.asset-summary {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.assets-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.form-tail {
  display: flex;
  gap: 18px;
  flex-wrap: wrap;
}
.media-editor {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 112px;
  gap: 12px;
  width: 100%;
}
.media-fields,
.gallery-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}
.media-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.media-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}
.media-preview,
.preview-item {
  position: relative;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
}
.media-preview {
  width: 112px;
  height: 112px;
}
.media-preview img,
.preview-item img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.media-preview span,
.preview-item span,
.preview-remove {
  position: absolute;
  left: 6px;
  bottom: 6px;
  max-width: calc(100% - 12px);
  padding: 2px 6px;
  border-radius: 4px;
  border: 0;
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
  font-size: 11px;
  line-height: 1.4;
}
.preview-remove {
  left: auto;
  right: 6px;
  top: 6px;
  bottom: auto;
  cursor: pointer;
}
.preview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 10px;
}
.preview-item {
  aspect-ratio: 1;
}
.detail-hero {
  display: grid;
  grid-template-columns: 88px 1fr;
  gap: 14px;
  align-items: center;
  margin-bottom: 16px;
}
.detail-cover {
  width: 88px;
  height: 88px;
}
.detail-hero h3 {
  margin: 0 0 6px;
  font-size: 18px;
}
.detail-hero p {
  margin: 0 0 10px;
  color: var(--el-text-color-secondary);
}
.tag-row,
.upload-line {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.upload-line {
  margin: 12px 0 18px;
}
.gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(112px, 1fr));
  gap: 10px;
}
.gallery-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-fill-color-light);
}
.gallery-item img {
  display: block;
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
}
.gallery-item span {
  display: block;
  padding: 4px 6px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.json-view {
  max-height: 360px;
  overflow: auto;
  padding: 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  white-space: pre-wrap;
  word-break: break-word;
}
.review-note {
  color: var(--el-color-danger);
}
@media (max-width: 767px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
  .media-editor {
    grid-template-columns: 1fr;
  }
  .media-preview {
    width: 100%;
    height: auto;
    aspect-ratio: 1.8;
  }
  .detail-hero {
    grid-template-columns: 72px 1fr;
  }
  .detail-cover {
    width: 72px;
    height: 72px;
  }
}
</style>
