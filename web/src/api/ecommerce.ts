import { http } from './http'

export interface EcommercePlatform {
  id: number
  code: string
  name: string
  language: string
  field_schema?: any
  remark: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export const ECOMMERCE_LANGUAGES = [
  { label: '中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
  { label: '日本语', value: 'ja-JP' },
  { label: '韩语', value: 'ko-KR' },
  { label: 'Español', value: 'es-ES' },
  { label: 'ไทย', value: 'th-TH' },
] as const

export function ecommerceLanguageName(code?: string) {
  return ECOMMERCE_LANGUAGES.find((item) => item.value === code)?.label || code || '自动'
}

export interface EcommercePromptTemplate {
  id: number
  code: string
  name: string
  content_prompt: string
  image_prompt: string
  video_prompt: string
  remark: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface EcommerceStyleTemplate {
  id: number
  code: string
  name: string
  style_prompt: string
  layout_config?: any
  remark: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface EcommerceAsset {
  id: number
  task_id: string
  asset_type: string
  image_task_id: string
  url: string
  file_id: string
  prompt: string
  status: string
  progress: number
  credit_cost: number
  error?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  updated_at: string
}

export const ECOMMERCE_EXTRA_ASSET_OPTIONS = [
  { label: '首屏主视觉', value: 'hero_visual_image', description: '传递核心价值' },
  { label: '核心卖点图', value: 'core_selling_point_image', description: '突出差异优势' },
  { label: '使用场景图', value: 'usage_scene_image', description: '呈现真实使用场景' },
  { label: '多角度图', value: 'multi_angle_image', description: '多角度呈现外观' },
  { label: '场景氛围图', value: 'scene_atmosphere_image', description: '展示使用场景' },
  { label: '商品细节图', value: 'product_detail_image', description: '放大材质与工艺' },
  { label: '品牌故事图', value: 'brand_story_image', description: '传达品牌理念' },
  { label: '尺寸/容量/尺码图', value: 'size_capacity_image', description: '展示规格信息' },
  { label: '效果对比图', value: 'effect_compare_image', description: '使用前后效果对比' },
  { label: '详细规格/参数表', value: 'spec_sheet_image', description: '展示详细商品数据' },
  { label: '工艺制作图', value: 'craft_process_image', description: '展示工艺制作过程' },
  { label: '配件/赠品图', value: 'accessories_image', description: '明确收货的所有物品' },
  { label: '系列展示图', value: 'series_show_image', description: '多色或多 SKU 展示' },
  { label: '商品成分图', value: 'ingredients_image', description: '展示配方/材质/成分' },
  { label: '售后保障图', value: 'after_sales_image', description: '说明质保退换政策' },
  { label: '使用建议图', value: 'usage_tips_image', description: '商品使用的注意事项' },
  { label: '代言图', value: 'spokesperson_image', description: '代言背书与信任感' },
  { label: '模特展示图', value: 'model_product_image', description: '模特穿戴或使用展示' },
] as const

export interface EcommerceTask {
  id: number
  task_id: string
  platform_id: number
  platform_name: string
  prompt_template_id: number
  prompt_name: string
  style_template_id: number
  style_name: string
  language: string
  language_name: string
  requirement: string
  reference_images?: string[]
  product_asset_id?: string
  model_asset_id?: string
  extra_asset_types?: string[]
  status: string
  progress: number
  output_json?: any
  output_html?: string
  assets: EcommerceAsset[]
  error?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
}

export function getEcommerceOptions(): Promise<{
  platforms: EcommercePlatform[]
  prompt_templates: EcommercePromptTemplate[]
  style_templates: EcommerceStyleTemplate[]
}> {
  return http.get('/api/me/ecommerce/options')
}

export function createEcommerceTask(body: {
  platform_id: number
  prompt_template_id: number
  style_template_id: number
  language?: string
  requirement: string
  reference_images: string[]
  product_asset_id?: string
  model_asset_id?: string
  extra_asset_types?: string[]
}): Promise<EcommerceTask> {
  return http.post('/api/me/ecommerce/tasks', body)
}

export function listEcommerceTasks(params: {
  keyword?: string
  status?: string
  limit?: number
  offset?: number
} = {}): Promise<{ items: EcommerceTask[]; total: number; limit: number; offset: number }> {
  return http.get('/api/me/ecommerce/tasks', { params })
}

export function getEcommerceTask(taskID: string): Promise<EcommerceTask> {
  return http.get(`/api/me/ecommerce/tasks/${taskID}`)
}

export function deleteEcommerceTask(taskID: string): Promise<{ deleted: string; deleted_by: number }> {
  return http.delete(`/api/me/ecommerce/tasks/${taskID}`)
}

export function retryEcommerceAsset(taskID: string, assetID: number, prompt = ''):
  Promise<{ task_id: string; asset_id: number; status: string }> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/assets/${assetID}/retry`, { prompt })
}

export function generateEcommerceVideo(taskID: string, prompt = ''):
  Promise<{ task_id: string; status: string }> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/video`, { prompt })
}

export function cancelEcommerceTask(taskID: string): Promise<EcommerceTask> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/cancel`, {})
}

export function retryEcommerceTask(taskID: string): Promise<EcommerceTask> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/retry`, {})
}

export function exportEcommercePoster(taskID: string) {
  return http.get(`/api/me/ecommerce/tasks/${taskID}/export`, {
    responseType: 'blob',
    timeout: 180_000,
  })
}

export type ConfigKind = 'platforms' | 'prompt-templates' | 'style-templates'

export function listEcommerceConfig<T>(kind: ConfigKind, keyword = ''): Promise<{ items: T[]; total: number }> {
  return http.get(`/api/admin/ecommerce/${kind}`, { params: keyword ? { keyword } : {} })
}

export function createEcommerceConfig<T>(kind: ConfigKind, body: any): Promise<T> {
  return http.post(`/api/admin/ecommerce/${kind}`, body)
}

export function updateEcommerceConfig<T>(kind: ConfigKind, id: number, body: any): Promise<T> {
  return http.put(`/api/admin/ecommerce/${kind}/${id}`, body)
}

export function deleteEcommerceConfig(kind: ConfigKind, id: number) {
  return http.delete(`/api/admin/ecommerce/${kind}/${id}`)
}

export type EcommerceLibraryKind = 'product' | 'model'
export type EcommerceLibraryScope = 'private' | 'public'
export type EcommerceLibraryReviewStatus = 'draft' | 'pending' | 'approved' | 'rejected'

export interface EcommerceLibraryAsset {
  id: number
  asset_id: string
  owner_user_id: number
  kind: EcommerceLibraryKind
  scope: EcommerceLibraryScope
  review_status: EcommerceLibraryReviewStatus
  name: string
  code: string
  cover_url: string
  gallery_json?: any
  tags_json?: any
  detail_json?: any
  enabled: boolean
  review_note?: string
  reviewed_by?: number
  reviewed_at?: string | null
  created_at: string
  updated_at: string
}

export interface EcommerceLibraryAssetFile {
  id: number
  asset_id: string
  file_usage: string
  origin_name: string
  mime: string
  size_bytes: number
  width: number
  height: number
  sha256: string
  url: string
  sort_order: number
  created_at: string
}

export interface EcommerceLibraryAssetDetail {
  asset: EcommerceLibraryAsset
  files: EcommerceLibraryAssetFile[]
}

export interface EcommerceLibraryAssetPayload {
  kind: EcommerceLibraryKind
  scope?: EcommerceLibraryScope
  name: string
  code?: string
  cover_url?: string
  gallery_json?: any
  tags_json?: any
  detail_json?: any
  enabled?: boolean
  submit_review?: boolean
}

export function listEcommerceLibraryAssets(params: {
  keyword?: string
  kind?: EcommerceLibraryKind | ''
  scope?: EcommerceLibraryScope | ''
  review_status?: EcommerceLibraryReviewStatus | ''
  limit?: number
  offset?: number
} = {}, silent = false): Promise<{ items: EcommerceLibraryAsset[]; total: number; limit: number; offset: number }> {
  return http.get('/api/me/ecommerce/library/assets', { params, silent } as any)
}

export function getEcommerceLibraryAsset(assetID: string): Promise<EcommerceLibraryAssetDetail> {
  return http.get(`/api/me/ecommerce/library/assets/${assetID}`)
}

export function createEcommerceLibraryAsset(body: EcommerceLibraryAssetPayload): Promise<EcommerceLibraryAsset> {
  return http.post('/api/me/ecommerce/library/assets', body)
}

export function updateEcommerceLibraryAsset(assetID: string, body: EcommerceLibraryAssetPayload): Promise<EcommerceLibraryAsset> {
  return http.put(`/api/me/ecommerce/library/assets/${assetID}`, body)
}

export function deleteEcommerceLibraryAsset(assetID: string): Promise<{ deleted: string }> {
  return http.delete(`/api/me/ecommerce/library/assets/${assetID}`)
}

export function submitEcommerceLibraryAssetReview(assetID: string): Promise<EcommerceLibraryAsset> {
  return http.post(`/api/me/ecommerce/library/assets/${assetID}/submit-review`, {})
}

export function uploadEcommerceLibraryAssetFile(assetID: string, file: File, usage = 'gallery', sortOrder = 0) {
  const form = new FormData()
  form.append('file', file)
  form.append('usage', usage)
  form.append('sort_order', String(sortOrder))
  return http.post(`/api/me/ecommerce/library/assets/${assetID}/files`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  }) as Promise<{ file: EcommerceLibraryAssetFile; cover_url: string; gallery_json: any }>
}

export function adminListEcommerceLibraryAssets(params: {
  keyword?: string
  kind?: EcommerceLibraryKind | ''
  scope?: EcommerceLibraryScope | ''
  review_status?: EcommerceLibraryReviewStatus | ''
  limit?: number
  offset?: number
} = {}): Promise<{ items: EcommerceLibraryAsset[]; total: number; limit: number; offset: number }> {
  return http.get('/api/admin/ecommerce/library/assets', { params })
}

export function adminGetEcommerceLibraryAsset(assetID: string): Promise<EcommerceLibraryAssetDetail> {
  return http.get(`/api/admin/ecommerce/library/assets/${assetID}`)
}

export function adminReviewEcommerceLibraryAsset(assetID: string, status: 'approved' | 'rejected', note = ''):
  Promise<EcommerceLibraryAsset> {
  return http.post(`/api/admin/ecommerce/library/assets/${assetID}/review`, { status, note })
}

export function adminSetEcommerceLibraryAssetEnabled(assetID: string, enabled: boolean, note = ''):
  Promise<EcommerceLibraryAsset> {
  return http.post(`/api/admin/ecommerce/library/assets/${assetID}/enabled`, { enabled, note })
}
