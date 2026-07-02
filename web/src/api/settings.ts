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

export function testVideoGen(): Promise<{
  ok: boolean
  duration_ms: number
  model_count: number
  model_name: string
  models?: VideoGenProbeModel[]
}> {
  return http.post('/api/admin/settings/test-videogen', {})
}

export interface VideoGenFreeQuota {
  model_id?: string
  model_name?: string
  remaining_count?: number
}

export interface VideoGenBalance {
  credits: number
  recharge_balance: number
  free_quotas: VideoGenFreeQuota[]
  duration_ms: number
}

export function fetchVideoGenBalance(silent = false): Promise<VideoGenBalance> {
  return http.get('/api/admin/settings/videogen-balance', { silent } as any)
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
