import { http } from './http'

// 系统设置 KV 条目(管理端用,带 schema)。
export interface SettingItem {
  key: string
  value: string
  type: 'string' | 'password' | 'bool' | 'int' | 'email' | 'url' | string
  category: 'site' | 'auth' | 'limit' | 'mail' | 'imagegen' | 'textgen' | 'videogen' | string
  label: string
  desc: string
}

export function listSettings(): Promise<{ items: SettingItem[] }> {
  return http.get('/api/admin/settings')
}

export function updateSettings(items: Record<string, string>): Promise<{ updated: number }> {
  return http.put('/api/admin/settings', { items })
}

export function reloadSettings(): Promise<{ reloaded: boolean }> {
  return http.post('/api/admin/settings/reload')
}

export function sendTestEmail(to: string): Promise<{ sent: boolean; to: string }> {
  return http.post('/api/admin/settings/test-email', { to })
}

export function testImageGen(): Promise<{ ok: boolean; duration_ms: number; image_count: number }> {
  return http.post('/api/admin/settings/test-imagegen', {})
}

export function testTextGen(): Promise<{ ok: boolean; duration_ms: number; content: string }> {
  return http.post('/api/admin/settings/test-textgen', {})
}

export interface VideoGenProbeModel {
  id: string
  name: string
  type: string
  label: string
  value: string
}

export function testVideoGen(channelType?: string): Promise<{
  ok: boolean
  duration_ms: number
  model_count: number
  model_name: string
  models?: VideoGenProbeModel[]
}> {
  return http.post('/api/admin/settings/test-videogen', channelType ? { channel_type: channelType } : {})
}

export interface VideoGenFreeQuota {
  model_id?: string
  model_name?: string
  remaining_count?: number
}

export interface VideoGenBalance {
  supported?: boolean
  message?: string
  credits: number
  recharge_balance: number
  free_quotas: VideoGenFreeQuota[]
  duration_ms: number
}

export function fetchVideoGenBalance(silent = false, channelType?: string): Promise<VideoGenBalance> {
  return http.get('/api/admin/settings/videogen-balance', {
    silent,
    params: channelType ? { channel_type: channelType } : undefined,
  } as any)
}

export interface VideoGenGenerateTestState {
  id: string
  channel_type: string
  status: string
  progress: number
  progress_known?: boolean
  task_id?: string
  model_id?: string
  image_url?: string
  result_url?: string
  error?: string
  created_at: string
  updated_at: string
  duration_ms?: number
  cost_detail?: {
    model_name?: string
    price?: number
  }
}

export function startVideoGenGenerateTest(payload: {
  channelType: string
  prompt: string
  model?: string
  image?: File | null
  video?: File | null
}): Promise<VideoGenGenerateTestState> {
  const form = new FormData()
  form.append('channel_type', payload.channelType)
  form.append('prompt', payload.prompt)
  if (payload.model) form.append('model', payload.model)
  if (payload.image) form.append('image', payload.image)
  if (payload.video) form.append('video', payload.video)
  return http.post('/api/admin/settings/videogen-generate-test', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

export function getVideoGenGenerateTest(id: string): Promise<VideoGenGenerateTestState> {
  return http.get(`/api/admin/settings/videogen-generate-test/${encodeURIComponent(id)}`, {
    silent: true,
  } as any)
}

export function uploadSiteAsset(key: string, file: File): Promise<{ key: string; url: string }> {
  const form = new FormData()
  form.append('key', key)
  form.append('file', file)
  return http.post('/api/admin/settings/site-asset', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}

// 匿名公开接口:返回登录页需要的站点元信息(site.name 等)。
export function fetchSiteInfo(): Promise<Record<string, string>> {
  return http.get('/api/public/site-info', { silent: true } as any)
}
