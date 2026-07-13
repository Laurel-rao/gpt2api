import { http } from './http'

export type VideoWorkflowAspectRatio = '9:16' | '16:9' | '1:1'
export type VideoWorkflowResolution = '720p' | '1080p'
export type VideoWorkflowCharacterApprovalPolicy = 'manual' | 'auto_first'
export type VideoWorkflowStoryboardApprovalPolicy = 'manual' | 'auto'
export type VideoWorkflowRunMode = 'full' | 'node_only' | 'downstream'
export type VideoWorkflowPortType = 'text' | 'script' | 'scene' | 'image' | 'image_set' | 'video' | 'video_list'
export type VideoWorkflowNodeStatus = 'idle' | 'queued' | 'running' | 'awaiting_approval' | 'cancel_pending' | 'succeeded' | 'failed' | 'canceled' | 'stale'
export type VideoWorkflowRunStatus = 'queued' | 'running' | 'awaiting_character_approval' | 'awaiting_storyboard_approval' | 'cancel_pending' | 'canceled' | 'succeeded' | 'failed'
export type VideoWorkflowPositionMode = 'auto' | 'manual'
export type VideoAssetKind = 'image' | 'video'
export type VideoAssetStatus = 'pending' | 'ready' | 'failed' | 'deleted'
export type VideoAssetVersionSource = 'upload' | 'generated' | 'transform'

export interface VideoWorkflowNormalizedCrop {
  x: number
  y: number
  width: number
  height: number
}

export interface VideoWorkflowImageTransform {
  crop: VideoWorkflowNormalizedCrop
  rotation: 0 | 90 | 180 | 270
  flip_horizontal: boolean
  flip_vertical: boolean
}

export interface VideoWorkflowTimelineClip {
  id: string
  source_node_id: string
  source_port: string
  trim_in_ms: number
  trim_out_ms: number
}

export interface VideoWorkflowTimelineConfig {
  clips: VideoWorkflowTimelineClip[]
  /** Graph v1 compatibility. New writes use `clips`. */
  clip_node_ids?: string[]
}

export interface VideoWorkflowNodeConfig extends Record<string, any> {
  image_transform?: VideoWorkflowImageTransform
  clips?: VideoWorkflowTimelineClip[]
  clip_node_ids?: string[]
}

export interface VideoWorkflowPort {
  id: string
  label?: string
  type: VideoWorkflowPortType
  required?: boolean
}

export interface VideoWorkflowPosition { x: number; y: number }

export interface VideoWorkflowNode {
  id: string
  type: string
  version?: number
  title?: string
  position: VideoWorkflowPosition
  position_mode?: VideoWorkflowPositionMode
  role_id?: string
  scene_id?: string
  duration_seconds?: number
  size?: { width?: number; height?: number }
  collapsed?: boolean
  enabled?: boolean
  locked?: boolean
  asset_id?: string
  asset_version_id?: string
  config: VideoWorkflowNodeConfig
  inputs?: VideoWorkflowPort[]
  outputs?: VideoWorkflowPort[]
  status?: VideoWorkflowNodeStatus
  progress?: number
  output?: Record<string, any>
  stale_reason?: string
}

export interface VideoWorkflowEdge {
  id: string
  source: string
  source_port: string
  target: string
  target_port: string
  curve?: VideoWorkflowPosition
  route?: VideoWorkflowPosition[]
}

export interface VideoWorkflowGroup {
  id: string
  type: 'scene'
  scene_id: string
  enabled: boolean
  duration_seconds: 15
  node_ids: string[]
  position: VideoWorkflowPosition
  size: { width: number; height: number }
  collapsed: boolean
}

export interface VideoWorkflowLayoutAnchor {
  position: VideoWorkflowPosition
  position_mode: VideoWorkflowPositionMode
}

export interface VideoWorkflowGraphLayout {
  shared_character_bus?: VideoWorkflowLayoutAnchor
}

export interface VideoWorkflowGraph {
  schema_version: number
  settings: {
    aspect_ratio: VideoWorkflowAspectRatio
    resolution?: VideoWorkflowResolution
    fps: 30
    scene_duration_ms?: 15000
    /** Graph v1 compatibility. New writes use `scene_duration_ms`. */
    scene_duration_seconds?: 15
    character_approval_policy?: VideoWorkflowCharacterApprovalPolicy
    storyboard_approval_policy?: VideoWorkflowStoryboardApprovalPolicy
    text_model?: string
    image_model?: string
    video_model?: string
  }
  nodes: VideoWorkflowNode[]
  edges: VideoWorkflowEdge[]
  groups: VideoWorkflowGroup[]
  layout?: VideoWorkflowGraphLayout
}

export interface VideoWorkflowTemplate {
  id: string | number
  code?: string
  version?: number
  name: string
  description?: string
  source_url?: string
  graph?: VideoWorkflowGraph
}

export interface VideoWorkflow {
  id: string
  name: string
  template_id?: string | number
  template_version?: number
  revision: number
  graph: VideoWorkflowGraph
  created_at?: string
  updated_at?: string
  latest_run?: VideoWorkflowRun | null
}

export interface VideoWorkflowNodeRun {
  id: string
  run_id?: string
  node_id: string
  node_type?: string
  status: VideoWorkflowNodeStatus
  progress?: number
  input_hash?: string
  model_snapshot?: Record<string, any>
  upstream_task_id?: string
  output?: Record<string, any>
  error?: string
  error_code?: string
  error_message?: string
  output_version_id?: string
  attempt?: number
  attempt_count?: number
  credit_cost?: number
  cache_hit?: boolean
  created_at?: string
  started_at?: string
  finished_at?: string
}

export interface VideoWorkflowRun {
  id: string
  workflow_id: string
  workflow_revision: number
  status: VideoWorkflowRunStatus
  progress?: number
  run_mode?: VideoWorkflowRunMode
  start_node_id?: string
  request_id?: string
  estimated_credits?: number
  actual_credits?: number
  node_runs?: VideoWorkflowNodeRun[]
  graph_snapshot?: VideoWorkflowGraph
  error?: string
  error_code?: string
  error_message?: string
  output_version_id?: string
  output_asset_version_id?: string
  output_url?: string
  output?: VideoAssetVersion
  created_at?: string
  started_at?: string
  finished_at?: string
  updated_at?: string
}

export interface VideoWorkflowRunPage {
  items: VideoWorkflowRun[]
  total: number
  limit: number
  offset: number
}

export interface VideoWorkflowNodeHistoryEntry {
  run_id: string
  workflow_revision: number
  run_status: VideoWorkflowRunStatus
  run_mode?: VideoWorkflowRunMode
  run_created_at?: string
  node_run: VideoWorkflowNodeRun
}

export interface VideoWorkflowValidationIssue {
  code: string
  message: string
  node_id?: string
  edge_id?: string
}

export interface VideoWorkflowEstimate {
  token: string
  expires_at?: string
  total_credits: number
  character_credits?: number
  scene_credits?: number
  production_credits?: number
  cached_credits?: number
  balance?: number
}

export interface VideoAssetVersion {
  id: string
  version: number
  asset_id?: string
  status?: VideoAssetStatus
  mime?: string
  mime_type?: string
  size_bytes: number
  sha256?: string
  width?: number
  height?: number
  duration_ms?: number
  source?: VideoAssetVersionSource
  source_type?: VideoAssetVersionSource
  parent_version_id?: string
  input_hash?: string
  image_transform?: VideoWorkflowImageTransform
  metadata?: { image_transform?: VideoWorkflowImageTransform } & Record<string, any>
  preview_url?: string
  created_at?: string
}

export interface VideoAsset {
  id: string
  name: string
  kind: VideoAssetKind
  status?: VideoAssetStatus
  current_version_id?: string
  current_version?: number
  versions?: VideoAssetVersion[]
  preview_url?: string
  created_at?: string
}

function hydrateVideoAsset(asset: VideoAsset): VideoAsset {
  const current = asset.versions?.find((version) => version.id === asset.current_version_id) || asset.versions?.at(-1)
  return {
    ...asset,
    current_version: asset.current_version || current?.version,
    preview_url: asset.preview_url || current?.preview_url,
  }
}

function unwrapItems<T>(value: T[] | { items: T[] } | undefined): T[] {
  if (Array.isArray(value)) return value
  return value?.items || []
}

export async function listVideoWorkflowTemplates(): Promise<VideoWorkflowTemplate[]> {
  return unwrapItems(await http.get('/api/me/video-workflows/templates'))
}

export async function listVideoWorkflows(): Promise<VideoWorkflow[]> {
  return unwrapItems(await http.get('/api/me/video-workflows'))
}

export function createVideoWorkflow(body: { name: string; template_id: string | number }): Promise<VideoWorkflow> {
  return http.post('/api/me/video-workflows', body)
}

export function getVideoWorkflow(id: string): Promise<VideoWorkflow> {
  return http.get(`/api/me/video-workflows/${id}`)
}

export function updateVideoWorkflow(id: string, body: { name: string; revision: number; graph: VideoWorkflowGraph }): Promise<VideoWorkflow> {
  return http.put(`/api/me/video-workflows/${id}`, body)
}

export function deleteVideoWorkflow(id: string): Promise<void> {
  return http.delete(`/api/me/video-workflows/${id}`)
}

export function validateVideoWorkflow(id: string, graph?: VideoWorkflowGraph): Promise<{ valid: boolean; issues: VideoWorkflowValidationIssue[] }> {
  return http.post(`/api/me/video-workflows/${id}/validate`, graph ? { graph } : {}).then((result: any) => ({
    valid: Boolean(result.valid),
    issues: result.issues || result.errors || [],
  }))
}

export interface VideoWorkflowRunTarget {
  revision: number
  run_mode: VideoWorkflowRunMode
  start_node_id?: string
}

export interface StartVideoWorkflowRunBody extends VideoWorkflowRunTarget {
  estimate_token?: string
  request_id?: string
  /** Legacy field accepted while the server and saved clients migrate to `start_node_id`. */
  node_id?: string
}

export function estimateVideoWorkflowRun(id: string, body?: Partial<VideoWorkflowRunTarget>): Promise<VideoWorkflowEstimate> {
  return http.post(`/api/me/video-workflows/${id}/run-estimate`, body || {})
}

export function runVideoWorkflow(id: string, body: StartVideoWorkflowRunBody | {
  revision: number
  estimate_token?: string
  node_id?: string
}): Promise<VideoWorkflowRun> {
  return http.post(`/api/me/video-workflows/${id}/runs`, body)
}

export async function listVideoWorkflowRuns(id: string, params: { limit?: number; offset?: number } = {}): Promise<VideoWorkflowRunPage> {
  const result = await http.get(`/api/me/video-workflows/${id}/runs`, { params, silent: true } as any) as any
  const items = unwrapItems<VideoWorkflowRun>(result)
  return {
    items,
    total: Number(result?.total ?? items.length),
    limit: Number(result?.limit ?? params.limit ?? 20),
    offset: Number(result?.offset ?? params.offset ?? 0),
  }
}

export function getVideoWorkflowRun(runID: string, silent = false): Promise<VideoWorkflowRun> {
  return http.get(`/api/me/video-workflow-runs/${runID}`, { silent } as any)
}

export function cancelVideoWorkflowRun(runID: string): Promise<{ ok: boolean }> {
  return http.post(`/api/me/video-workflow-runs/${runID}/cancel`, {})
}

export function approveVideoWorkflowCharacters(runID: string, body: {
  selections: Array<{ node_run_id: string; input_hash: string; selected_version_id: string }>
}): Promise<{ ok: boolean }> {
  return http.post(`/api/me/video-workflow-runs/${runID}/approve-characters`, body)
}

export function approveVideoWorkflowStoryboard(runID: string, body: {
  node_run_id: string
  input_hash: string
  script: Record<string, any>
}): Promise<{ ok: boolean }> {
  return http.post(`/api/me/video-workflow-runs/${runID}/approve-storyboard`, body)
}

export async function listVideoAssets(params: { kind?: VideoAssetKind; keyword?: string; limit?: number; offset?: number } = {}): Promise<VideoAsset[]> {
  return unwrapItems<VideoAsset>(await http.get('/api/me/video-assets', { params, silent: true } as any)).map(hydrateVideoAsset)
}

export function videoAssetKindForFile(file: Pick<File, 'type'>): VideoAssetKind {
  if (file.type.startsWith('image/')) return 'image'
  if (file.type.startsWith('video/')) return 'video'
  throw new Error(`不支持的素材类型：${file.type || 'unknown'}`)
}

export function uploadVideoAsset(file: File, name = file.name): Promise<VideoAsset> {
  const data = new FormData()
  data.append('file', file)
  data.append('name', name)
  data.append('kind', videoAssetKindForFile(file))
  return (http.post('/api/me/video-assets', data, { headers: { 'Content-Type': 'multipart/form-data' } }) as unknown as Promise<VideoAsset>)
    .then(hydrateVideoAsset)
}

export function deleteVideoAsset(assetID: string): Promise<{ ok: boolean }> {
  return http.delete(`/api/me/video-assets/${assetID}`)
}

export function transformVideoAssetVersion(
  assetID: string,
  versionID: string,
  imageTransform: VideoWorkflowImageTransform,
): Promise<VideoAssetVersion> {
  return http.post(`/api/me/video-assets/${assetID}/versions/${versionID}/transform`, imageTransform)
}

export function signVideoAssetVersion(assetID: string, versionID: string | number, purpose: 'preview' | 'download' | 'seedance' = 'preview'):
  Promise<{ url: string; expires_at?: string }> {
  return http.post(`/api/me/video-assets/${assetID}/versions/${versionID}/sign`, { purpose })
}
