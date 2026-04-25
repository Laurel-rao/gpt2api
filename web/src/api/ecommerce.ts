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

export interface EcommercePromptTemplate {
  id: number
  code: string
  name: string
  content_prompt: string
  image_prompt: string
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
  error?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  updated_at: string
}

export interface EcommerceTask {
  id: number
  task_id: string
  platform_id: number
  platform_name: string
  prompt_template_id: number
  prompt_name: string
  style_template_id: number
  style_name: string
  requirement: string
  reference_images?: string[]
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
  requirement: string
  reference_images: string[]
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

export function retryEcommerceAsset(taskID: string, assetID: number):
  Promise<{ task_id: string; asset_id: number; status: string }> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/assets/${assetID}/retry`, {})
}

export function cancelEcommerceTask(taskID: string): Promise<EcommerceTask> {
  return http.post(`/api/me/ecommerce/tasks/${taskID}/cancel`, {})
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
