<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  Clock,
  Close,
  Connection,
  Crop,
  Delete,
  Document,
  Picture,
  Refresh,
  RefreshLeft,
  RefreshRight,
  Select,
  Setting,
  Upload,
  VideoCamera,
  VideoPlay,
  ZoomIn,
} from '@element-plus/icons-vue'
import type {
  VideoAsset,
  VideoAssetVersion,
  VideoWorkflowNode,
  VideoWorkflowNodeHistoryEntry,
  VideoWorkflowNodeRun,
  VideoWorkflowNodeStatus,
} from '@/api/videoWorkflow'
import { nodeStatusLabel, videoWorkflowNodePreviewURL } from '@/utils/videoWorkflowGraph'

type InspectorTab = 'upstream' | 'status' | 'history' | 'output' | 'settings'

interface InspectorUpstreamSource {
  id: string
  node_id: string
  node_title: string
  node_type: string
  port_label: string
  status: VideoWorkflowNodeStatus
}

interface InspectorUpstreamGroup {
  id: string
  label: string
  type: string
  required: boolean
  sources: InspectorUpstreamSource[]
}

interface InspectorModelOption {
  value: string
  label: string
}

const props = withDefaults(defineProps<{
  node: VideoWorkflowNode | null
  assets: VideoAsset[]
  upstreams?: InspectorUpstreamGroup[]
  latestRun?: VideoWorkflowNodeRun | null
  history?: VideoWorkflowNodeHistoryEntry[]
  historyLoading?: boolean
  modelValue?: string
  modelOptions?: InspectorModelOption[]
  revision?: number
  running?: boolean
}>(), {
  upstreams: () => [],
  latestRun: null,
  history: () => [],
  historyLoading: false,
  modelValue: '',
  modelOptions: () => [],
  revision: 0,
  running: false,
})

const emit = defineEmits<{
  'update-title': [value: string]
  'update-config': [key: string, value: any]
  'update-model': [value: string]
  'transform': [patch: Record<string, any>]
  'select-version': [versionID: string]
  'replace-file': [file: File]
  'choose-asset': []
  'regenerate': []
  'run': []
  'delete': []
  'add-connection': [portID: string]
  'add-to-timeline': []
  'preview-media': [node: VideoWorkflowNode]
  'preview-output': []
  'download-output': []
  'request-history': []
  'open-history': []
  'close': []
}>()

const tabs: Array<{ id: InspectorTab; label: string; icon: any }> = [
  { id: 'upstream', label: '上游', icon: Connection },
  { id: 'status', label: '状态', icon: Refresh },
  { id: 'history', label: '历史', icon: Clock },
  { id: 'output', label: '输出', icon: Document },
  { id: 'settings', label: '参数', icon: Setting },
]

const activeTab = ref<InspectorTab>('settings')
const selectedHistoryRunID = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const cropping = ref(false)
const crop = reactive({ x: 0, y: 0, width: 100, height: 100 })
const imageNode = computed(() => Boolean(props.node && ['background', 'image'].includes(props.node.type)))
const videoNode = computed(() => props.node?.type === 'video')
const systemNode = computed(() => ['timeline', 'compose'].includes(props.node?.type || ''))
const selectedHistoryEntry = computed(() => props.history.find((entry) => entry.run_id === selectedHistoryRunID.value) || null)
const effectiveOutput = computed(() => selectedHistoryEntry.value?.node_run.output || props.node?.output || {})
const previewURL = computed(() => selectedHistoryEntry.value
  ? String(effectiveOutput.value.preview_url || effectiveOutput.value.url || effectiveOutput.value.output_url || '')
  : videoWorkflowNodePreviewURL(props.node))
const relatedAsset = computed(() => props.assets.find((asset) => asset.id === props.node?.config?.asset_id))
const versions = computed<VideoAssetVersion[]>(() => {
  const remote = relatedAsset.value?.versions || []
  const local = Array.isArray(props.node?.config?.transform_versions) ? props.node!.config.transform_versions : []
  return remote.length ? remote : local.map((version: any, index: number) => ({
    id: String(version.id || `local-${index + 1}`),
    version: Number(version.version || index + 1),
    mime: String(version.mime || 'image/png'),
    size_bytes: Number(version.size_bytes || 0),
    preview_url: String(version.preview_url || previewURL.value),
  }))
})
const selectedVersion = computed(() => String(
  props.node?.config?.asset_version_id
  || props.node?.config?.selected_version_id
  || versions.value.at(-1)?.id
  || '',
))
const statusText = computed(() => statusLabel(props.node?.status))
const progress = computed(() => Math.max(0, Math.min(100, Number(props.node?.progress ?? props.latestRun?.progress ?? 0))))
const outputRows = computed(() => Object.entries(effectiveOutput.value).slice(0, 12).map(([key, value]) => ({
  key,
  label: outputLabel(key),
  value: outputValue(value),
})))
const formattedOutput = computed(() => JSON.stringify(effectiveOutput.value, null, 2))
const hasOutput = computed(() => Boolean(previewURL.value || outputRows.value.length || (!selectedHistoryEntry.value && props.latestRun?.output_version_id)))
const timelineDuration = computed(() => (props.node?.config?.clips || []).reduce((total: number, clip: any) => (
  total + Math.max(0, Number(clip.trim_out_ms || 0) - Number(clip.trim_in_ms || 0))
), 0))

watch(() => [props.node?.id, props.node?.config?.selected_version_id, props.node?.config?.asset_version_id], () => {
  const current = props.node?.config?.image_transform?.crop
  crop.x = Math.round(Number(current?.x || 0) * 100)
  crop.y = Math.round(Number(current?.y || 0) * 100)
  crop.width = Math.round(Number(current?.width || 1) * 100)
  crop.height = Math.round(Number(current?.height || 1) * 100)
  cropping.value = false
  if (activeTab.value === 'history') emit('request-history')
}, { immediate: true })

watch(() => props.node?.id, () => { selectedHistoryRunID.value = '' })

function activateTab(tab: InspectorTab) {
  activeTab.value = tab
  if (tab === 'history') emit('request-history')
}

function showHistoryOutput(entry: VideoWorkflowNodeHistoryEntry) {
  selectedHistoryRunID.value = entry.run_id
  activeTab.value = 'output'
}

function statusLabel(status?: VideoWorkflowNodeStatus) {
  return status === 'stale' ? '需更新' : nodeStatusLabel(status)
}

function outputLabel(key: string) {
  return ({
    summary: '摘要', prompt: '提示词', image_prompt: '图片提示词', video_prompt: '视频提示词',
    location: '地点', time: '时间', duration: '时长', camera: '镜头', action: '动作', dialogue: '台词',
    expression: '表情', lighting: '灯光', audio: '声音', scenes: '分镜', version_id: '输出版本',
    selected_version_id: '选定版本', duration_ms: '输出时长', width: '宽度', height: '高度',
  } as Record<string, string>)[key] || key
}

function outputValue(value: any) {
  if (Array.isArray(value)) return `${value.length} 项 · ${JSON.stringify(value).slice(0, 180)}`
  if (value && typeof value === 'object') return JSON.stringify(value).slice(0, 240)
  if (value === null || value === undefined || value === '') return '--'
  return String(value)
}

function formatDate(value?: string) {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--'
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function pickFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) emit('replace-file', file)
  input.value = ''
}

function applyCrop() {
  emit('transform', {
    crop: {
      x: crop.x / 100,
      y: crop.y / 100,
      width: crop.width / 100,
      height: crop.height / 100,
    },
  })
  cropping.value = false
}
</script>

<template>
  <aside class="workflow-inspector" aria-label="节点检查器">
    <template v-if="node">
      <header class="inspector-header">
        <div><strong>{{ node.title || node.config?.title || '节点设置' }}</strong><span>{{ node.type }} · {{ node.id }}</span></div>
        <button title="关闭检查器" aria-label="关闭检查器" @click="emit('close')"><Close /></button>
      </header>

      <nav class="inspector-tabs" aria-label="节点详情分类">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          :class="{ active: activeTab === tab.id }"
          :aria-selected="activeTab === tab.id"
          role="tab"
          @click="activateTab(tab.id)"
        ><component :is="tab.icon" /><span>{{ tab.label }}</span></button>
      </nav>

      <div class="inspector-scroll">
        <section v-show="activeTab === 'upstream'" class="inspector-panel ports-section" aria-label="上游输入">
          <header class="panel-heading"><div><strong>上游输入</strong><span>{{ upstreams.length }} 个输入端口</span></div></header>
          <div v-if="upstreams.length" class="upstream-list">
            <article v-for="group in upstreams" :key="group.id" class="upstream-port">
              <header><span><i class="input" />{{ group.label }}</span><code>{{ group.type }}</code><em v-if="group.required">必需</em></header>
              <div v-for="source in group.sources" :key="source.id" class="upstream-source">
                <b>{{ source.node_title }}</b><span>{{ source.port_label }}</span><small :class="source.status">{{ statusLabel(source.status) }}</small>
              </div>
              <div v-if="!group.sources.length" class="unconnected"><span>未连接</span><button @click="emit('add-connection', group.id)">添加</button></div>
              <button v-else class="replace-connection" @click="emit('add-connection', group.id)">更换输入</button>
            </article>
          </div>
          <div v-else class="panel-empty"><Connection /><b>无上游输入</b><span>该节点是当前流程的起点</span></div>
        </section>

        <section v-show="activeTab === 'status'" class="inspector-panel status-panel" aria-label="节点状态">
          <header class="panel-heading"><div><strong>运行状态</strong><span>当前工作流 R{{ revision }}</span></div><b :class="['status-badge', node.status || 'idle']"><i />{{ statusText }}</b></header>
          <div class="progress-block"><div><span>进度</span><b>{{ progress }}%</b></div><i><em :style="{ width: `${progress}%` }" /></i></div>
          <p v-if="node.stale_reason" class="status-message" :class="node.status || 'idle'">{{ node.stale_reason }}</p>
          <div class="status-grid">
            <div><span>节点开关</span><b>{{ node.enabled === false ? '已停用' : '已启用' }}</b></div>
            <div><span>位置锁定</span><b>{{ node.locked ? '已锁定' : '可编辑' }}</b></div>
            <div><span>运行次数</span><b>{{ latestRun?.attempt || latestRun?.attempt_count || '--' }}</b></div>
            <div><span>缓存命中</span><b>{{ latestRun ? (latestRun.cache_hit ? '是' : '否') : '--' }}</b></div>
            <div><span>消耗</span><b>{{ latestRun?.credit_cost ?? '--' }}</b></div>
            <div><span>输入哈希</span><code>{{ latestRun?.input_hash?.slice(0, 10) || '--' }}</code></div>
          </div>
          <div v-if="latestRun?.error_message || latestRun?.error" class="run-error"><b>{{ latestRun.error_code || '运行错误' }}</b><span>{{ latestRun.error_message || latestRun.error }}</span></div>
        </section>

        <section v-show="activeTab === 'history'" class="inspector-panel history-panel" aria-label="节点历史">
          <header class="panel-heading"><div><strong>节点历史</strong><span>最近 {{ history.length }} 次</span></div><button class="heading-action" @click="emit('open-history')"><Clock />全部历史</button></header>
          <div v-if="historyLoading" class="history-loading"><i v-for="index in 3" :key="index" /></div>
          <div v-else-if="history.length" class="node-history-list">
            <article v-for="entry in history" :key="entry.run_id">
              <i :class="entry.node_run.status" />
              <div><b>R{{ entry.workflow_revision }} · {{ statusLabel(entry.node_run.status) }}</b><span>{{ formatDate(entry.run_created_at) }} · 尝试 {{ entry.node_run.attempt || entry.node_run.attempt_count || 1 }}</span></div>
              <em>{{ entry.node_run.cache_hit ? '缓存' : `${entry.node_run.credit_cost || 0} 点` }}</em>
              <button class="history-output-button" :disabled="!entry.node_run.output" @click="showHistoryOutput(entry)">输出</button>
            </article>
          </div>
          <div v-else class="panel-empty"><Clock /><b>暂无节点历史</b><span>运行该节点后会记录结果</span></div>
        </section>

        <section v-show="activeTab === 'output'" class="inspector-panel output-panel" aria-label="节点输出">
          <header class="panel-heading"><div><strong>节点输出</strong><span>{{ selectedHistoryEntry ? `历史 R${selectedHistoryEntry.workflow_revision}` : `当前 R${revision}` }} · {{ (node.outputs || []).length }} 个输出端口</span></div></header>
          <div v-if="imageNode || videoNode" class="image-preview">
            <video v-if="previewURL && videoNode" :src="previewURL" muted playsinline preload="metadata" :aria-label="`${node.title || node.id} 视频缩略预览`" />
            <img v-else-if="previewURL" :src="previewURL" :alt="node.title || '生成图片预览'" draggable="false" />
            <span v-else><component :is="videoNode ? VideoCamera : Picture" /><small>尚未生成{{ videoNode ? '视频' : '图片' }}</small></span>
            <button
              v-if="!selectedHistoryEntry"
              :class="['inspector-preview-button', { video: videoNode }]"
              type="button"
              :aria-label="`全屏预览：${node.title || node.id}`"
              @keydown.stop
              @keyup.stop
              @click="emit('preview-media', node)"
            ><component :is="videoNode ? VideoPlay : ZoomIn" /><span>全屏预览</span></button>
            <em>{{ videoNode ? '15.000s' : '媒体输出' }}</em>
          </div>

          <div v-if="imageNode && !selectedHistoryEntry" class="version-field">
            <label class="field-label" for="image-version">输出版本</label>
            <select id="image-version" :value="selectedVersion" @change="emit('select-version', ($event.target as HTMLSelectElement).value)">
              <option v-if="!versions.length" value="">当前草稿</option>
              <option v-for="version in versions" :key="version.id" :value="version.id">V{{ version.version }} · {{ version.mime || version.mime_type || 'image' }}</option>
            </select>
          </div>

          <div v-if="node.outputs?.length" class="output-ports">
            <div v-for="port in node.outputs" :key="port.id"><i /><span>{{ port.label || port.id }}</span><code>{{ port.type }}</code></div>
          </div>

          <div v-if="outputRows.length" class="output-fields">
            <div v-for="row in outputRows" :key="row.key"><span>{{ row.label }}</span><p>{{ row.value }}</p></div>
            <details><summary>原始 JSON</summary><pre>{{ formattedOutput }}</pre></details>
          </div>
          <div v-else-if="!hasOutput" class="panel-empty output-empty"><Document /><b>当前修订暂无输出</b><span>运行节点后在此查看结果</span></div>

          <div v-if="node.type === 'compose' && !selectedHistoryEntry" class="output-actions">
            <button @click="emit('preview-output')"><VideoPlay />播放成片</button>
            <button @click="emit('download-output')"><Document />下载 MP4</button>
          </div>
        </section>

        <section v-show="activeTab === 'settings'" class="inspector-panel settings-panel" aria-label="节点设置">
          <header class="panel-heading"><div><strong>输入与参数</strong><span>{{ systemNode ? '系统节点' : '可编辑' }}</span></div></header>
          <div class="form-section">
            <label class="field-label">节点名称</label>
            <input :value="node.title || node.config?.title" maxlength="80" @input="emit('update-title', ($event.target as HTMLInputElement).value)" />

            <template v-if="!systemNode">
              <label class="field-label">{{ node.type === 'script' ? '结构化剧本提示词' : '提示词（Prompt）' }}</label>
              <textarea
                :value="node.config?.prompt || ''"
                :rows="imageNode ? 5 : 7"
                maxlength="1000"
                @input="emit('update-config', 'prompt', ($event.target as HTMLTextAreaElement).value)"
              />
              <small>{{ String(node.config?.prompt || '').length }}/1000</small>
            </template>
          </div>

          <div class="locked-section model-section">
            <label class="field-label" for="node-model">执行模型</label>
            <select v-if="modelOptions.length" id="node-model" :value="modelValue" @change="emit('update-model', ($event.target as HTMLSelectElement).value)">
              <option v-for="option in modelOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
            <div v-else class="readonly-value"><span>执行器</span><b>{{ node.type === 'compose' ? '系统视频合成器' : '系统流程调度器' }}</b></div>
          </div>

          <div v-if="node.type === 'character'" class="form-section split-fields">
            <label>角色姓名<input :value="node.config?.name" @input="emit('update-config', 'name', ($event.target as HTMLInputElement).value)" /></label>
            <label>成年年龄<input :value="node.config?.adult_age" type="number" min="18" max="80" @input="emit('update-config', 'adult_age', Number(($event.target as HTMLInputElement).value))" /></label>
          </div>

          <div v-if="imageNode" class="media-operations">
            <div class="action-grid two">
              <button @click="emit('regenerate')"><Refresh />重新生成</button>
              <button @click="fileInput?.click()"><Upload />上传替换</button>
              <input ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp" hidden @change="pickFile" />
            </div>
            <button class="asset-button" @click="emit('choose-asset')"><Select />从素材库选择</button>
            <div class="transform-grid">
              <button :class="{ active: cropping }" @click="cropping = !cropping"><Crop />裁剪</button>
              <button title="左转 90°" @click="emit('transform', { rotation_delta: -90 })"><RefreshLeft />左转</button>
              <button title="右转 90°" @click="emit('transform', { rotation_delta: 90 })"><RefreshRight />右转</button>
              <button @click="emit('transform', { flip_horizontal_toggle: true })"><span class="flip-icon">↔</span>水平翻转</button>
              <button @click="emit('transform', { flip_vertical_toggle: true })"><span class="flip-icon">↕</span>垂直翻转</button>
              <button @click="emit('transform', { reset: true })"><Refresh />重置</button>
            </div>
            <div v-if="cropping" class="crop-editor" aria-label="裁剪参数">
              <label>X<input v-model.number="crop.x" type="number" min="0" max="99" step="1" /></label>
              <label>Y<input v-model.number="crop.y" type="number" min="0" max="99" step="1" /></label>
              <label>宽<input v-model.number="crop.width" type="number" min="1" max="100" step="1" /></label>
              <label>高<input v-model.number="crop.height" type="number" min="1" max="100" step="1" /></label>
              <button @click="applyCrop">应用裁剪</button>
            </div>
          </div>

          <div class="parameter-list">
            <div><span>节点类型</span><code>{{ node.type }}</code></div>
            <div v-if="node.duration_seconds || node.config?.duration_seconds"><span>片段时长</span><b>{{ node.duration_seconds || node.config.duration_seconds }} 秒</b></div>
            <div v-if="['video', 'compose'].includes(node.type)"><span>输出帧率</span><b>30 fps</b></div>
            <div v-if="node.type === 'timeline'"><span>片段数量</span><b>{{ node.config?.clips?.length || 0 }}</b></div>
            <div v-if="node.type === 'timeline'"><span>合计时长</span><b>{{ (timelineDuration / 1000).toFixed(1) }} 秒</b></div>
          </div>
          <button v-if="node.type === 'video'" class="timeline-add" @click="emit('add-to-timeline')">添加到顺序时间线</button>
        </section>
      </div>

      <footer class="inspector-footer">
        <button v-if="!systemNode" class="delete-button" title="删除节点" aria-label="删除节点" @click="emit('delete')"><Delete /></button>
        <span v-else />
        <button class="run-button" :disabled="running" @click="emit('run')"><VideoPlay />{{ running ? '运行中' : '运行此节点' }}</button>
        <button class="history-button" title="节点历史" aria-label="节点历史" @click="activateTab('history')"><Clock /></button>
      </footer>
    </template>

    <template v-else>
      <header class="inspector-header empty-header">
        <div><strong>节点详情</strong><span>未选择节点</span></div>
        <button title="关闭检查器" aria-label="关闭检查器" @click="emit('close')"><Close /></button>
      </header>
      <div class="empty-inspector"><Picture /><b>未选择节点</b><span>选择画布中的节点查看配置</span></div>
    </template>
  </aside>
</template>

<style scoped lang="scss">
.workflow-inspector { height: 100%; min-width: 0; display: grid; grid-template-rows: 54px 42px minmax(0, 1fr) 58px; color: #0f172a; background: #fff; }
.inspector-header { display: flex; align-items: center; justify-content: space-between; padding: 0 12px 0 14px; border-bottom: 1px solid #e2e8f0; }
.inspector-header > div { min-width: 0; display: grid; gap: 2px; }.inspector-header strong { overflow: hidden; font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }.inspector-header span { overflow: hidden; color: #64748b; font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.inspector-header button, .inspector-footer button, .inspector-tabs button { border: 0; cursor: pointer; }.inspector-header button { width: 34px; height: 34px; display: grid; place-items: center; color: #64748b; background: transparent; border-radius: 5px; }.inspector-header button:hover { background: #f1f5f9; }.inspector-header svg { width: 15px; }
.inspector-tabs { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); border-bottom: 1px solid #e2e8f0; background: #f8fafc; }
.inspector-tabs button { position: relative; min-width: 0; display: flex; align-items: center; justify-content: center; gap: 4px; padding: 0 3px; color: #64748b; background: transparent; font-size: 9px; }.inspector-tabs button::after { position: absolute; left: 8px; right: 8px; bottom: -1px; height: 2px; content: ''; background: transparent; }.inspector-tabs button.active { color: #1d4ed8; background: #fff; font-weight: 650; }.inspector-tabs button.active::after { background: #2563eb; }.inspector-tabs button:hover { color: #2563eb; }.inspector-tabs button:focus-visible { z-index: 1; outline: 2px solid #2563eb; outline-offset: -2px; }.inspector-tabs svg { width: 13px; flex: 0 0 auto; }
.inspector-scroll { min-height: 0; overflow-y: auto; }.inspector-panel { min-height: 100%; box-sizing: border-box; }.panel-heading { min-height: 48px; display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 0 14px; border-bottom: 1px solid #eef2f7; }.panel-heading > div { min-width: 0; display: grid; gap: 2px; }.panel-heading strong { color: #334155; font-size: 12px; }.panel-heading span { color: #64748b; font-size: 9px; }.heading-action { height: 28px; display: flex; align-items: center; gap: 4px; padding: 0 7px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 4px; cursor: pointer; font-size: 9px; }.heading-action svg { width: 12px; }
.panel-empty { min-height: 250px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 7px; padding: 20px; color: #64748b; text-align: center; }.panel-empty svg { width: 28px; color: #94a3b8; }.panel-empty b { color: #334155; font-size: 12px; }.panel-empty span { font-size: 9px; }
.upstream-list { display: grid; }.upstream-port { padding: 12px 14px; border-bottom: 1px solid #eef2f7; }.upstream-port > header { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; align-items: center; gap: 6px; }.upstream-port > header span { min-width: 0; display: flex; align-items: center; gap: 6px; color: #334155; font-size: 10px; font-weight: 650; }.upstream-port i.input { width: 7px; height: 7px; border: 1px solid #2563eb; border-radius: 50%; }.upstream-port code { color: #64748b; font-size: 8px; }.upstream-port em { color: #b45309; font-size: 8px; font-style: normal; }.upstream-source { min-height: 36px; display: grid; grid-template-columns: minmax(0, 1fr) auto auto; align-items: center; gap: 7px; margin-top: 7px; padding: 0 8px; background: #f8fafc; border-left: 2px solid #60a5fa; }.upstream-source b { overflow: hidden; color: #334155; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.upstream-source span { color: #64748b; font-size: 8px; }.upstream-source small { color: #64748b; font-size: 8px; }.upstream-source small.succeeded { color: #15803d; }.upstream-source small.stale { color: #b45309; }.upstream-source small.failed { color: #b91c1c; }.unconnected { min-height: 34px; display: flex; align-items: center; justify-content: space-between; margin-top: 7px; padding: 0 8px; color: #b45309; background: #fffbeb; font-size: 9px; }.unconnected button, .replace-connection { color: #2563eb; background: transparent; border: 0; cursor: pointer; font-size: 9px; }.replace-connection { margin-top: 7px; padding: 0; }
.status-badge { display: flex; align-items: center; gap: 5px; color: #64748b; font-size: 10px; }.status-badge i { width: 7px; height: 7px; background: #94a3b8; border-radius: 50%; }.status-badge.succeeded { color: #15803d; }.status-badge.succeeded i { background: #22c55e; }.status-badge.stale { color: #b45309; }.status-badge.stale i { background: #f59e0b; }.status-badge.failed { color: #b91c1c; }.status-badge.failed i { background: #ef4444; }.status-badge.running, .status-badge.queued { color: #1d4ed8; }.status-badge.running i, .status-badge.queued i { background: #3b82f6; }
.progress-block { padding: 14px; border-bottom: 1px solid #eef2f7; }.progress-block > div { display: flex; justify-content: space-between; margin-bottom: 7px; color: #64748b; font-size: 9px; }.progress-block > div b { color: #334155; }.progress-block > i { position: relative; height: 6px; display: block; overflow: hidden; background: #e2e8f0; border-radius: 999px; }.progress-block em { position: absolute; inset: 0 auto 0 0; background: #2563eb; border-radius: inherit; }.status-message { margin: 0; padding: 10px 14px; color: #b45309; background: #fffbeb; border-bottom: 1px solid #fde68a; font-size: 9px; line-height: 1.5; }.status-message.failed { color: #b91c1c; background: #fef2f2; border-color: #fecaca; }.status-grid { display: grid; grid-template-columns: 1fr 1fr; border-bottom: 1px solid #eef2f7; }.status-grid > div { min-width: 0; display: grid; gap: 4px; padding: 11px 14px; border-right: 1px solid #eef2f7; border-bottom: 1px solid #eef2f7; }.status-grid > div:nth-child(even) { border-right: 0; }.status-grid span { color: #64748b; font-size: 8px; }.status-grid b, .status-grid code { overflow: hidden; color: #334155; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.run-error { display: grid; gap: 4px; margin: 12px 14px; padding: 10px; color: #b91c1c; background: #fef2f2; border: 1px solid #fecaca; border-radius: 5px; font-size: 9px; }
.history-loading { display: grid; gap: 8px; padding: 14px; }.history-loading i { height: 52px; background: #f1f5f9; border-radius: 5px; }.node-history-list article { min-height: 54px; display: grid; grid-template-columns: 8px minmax(0, 1fr) auto auto; align-items: center; gap: 7px; padding: 0 14px; border-bottom: 1px solid #eef2f7; }.node-history-list article > i { width: 7px; height: 7px; background: #94a3b8; border-radius: 50%; }.node-history-list article > i.succeeded { background: #22c55e; }.node-history-list article > i.failed { background: #ef4444; }.node-history-list article > i.running, .node-history-list article > i.queued { background: #3b82f6; }.node-history-list article > div { min-width: 0; display: grid; gap: 3px; }.node-history-list b { color: #334155; font-size: 10px; }.node-history-list span { color: #64748b; font-size: 8px; }.node-history-list em { color: #64748b; font-size: 8px; font-style: normal; }.history-output-button { height: 24px; padding: 0 6px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 4px; cursor: pointer; font-size: 8px; }.history-output-button:disabled { color: #94a3b8; background: #f8fafc; border-color: #e2e8f0; cursor: not-allowed; }
.output-panel { padding-bottom: 14px; }.image-preview { position: relative; height: 170px; display: grid; place-items: center; overflow: hidden; margin: 14px; background: #111318; border-radius: 6px; }.image-preview img, .image-preview video { width: 100%; height: 100%; object-fit: contain; pointer-events: none; }.image-preview > span { display: grid; place-items: center; gap: 8px; color: #94a3b8; }.image-preview > span svg { width: 30px; }.image-preview small { font-size: 10px; }.image-preview > em { position: absolute; right: 7px; bottom: 7px; padding: 2px 5px; color: #e2e8f0; background: rgba(15, 23, 42, .75); border-radius: 3px; font-size: 9px; font-style: normal; }.inspector-preview-button { position: absolute; z-index: 2; left: 50%; top: 50%; height: 34px; display: flex; align-items: center; gap: 6px; padding: 0 11px; color: #fff; background: rgba(15, 23, 42, .88); border: 1px solid rgba(255, 255, 255, .7); border-radius: 5px; opacity: 0; cursor: pointer; transform: translate(-50%, -50%); transition: opacity .18s ease, background .18s ease; font-size: 10px; }.inspector-preview-button.video, .image-preview:hover .inspector-preview-button, .image-preview:focus-within .inspector-preview-button { opacity: 1; }.inspector-preview-button:hover { background: #2563eb; }.inspector-preview-button svg { width: 14px; }.version-field { padding: 0 14px 12px; border-bottom: 1px solid #eef2f7; }.output-ports { padding: 8px 14px; border-bottom: 1px solid #eef2f7; }.output-ports > div { min-height: 28px; display: grid; grid-template-columns: 8px minmax(0, 1fr) auto; align-items: center; gap: 7px; }.output-ports i { width: 7px; height: 7px; background: #16a34a; border-radius: 50%; }.output-ports span { color: #334155; font-size: 9px; }.output-ports code { color: #64748b; font-size: 8px; }.output-fields > div { display: grid; grid-template-columns: 72px minmax(0, 1fr); gap: 8px; padding: 9px 14px; border-bottom: 1px solid #eef2f7; }.output-fields > div span { color: #64748b; font-size: 8px; }.output-fields p { min-width: 0; margin: 0; overflow-wrap: anywhere; color: #334155; font-size: 9px; line-height: 1.5; }.output-fields details { margin: 10px 14px; }.output-fields summary { color: #2563eb; cursor: pointer; font-size: 9px; }.output-fields pre { max-height: 280px; overflow: auto; padding: 10px; color: #334155; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 5px; white-space: pre-wrap; overflow-wrap: anywhere; font: 9px/1.5 "SFMono-Regular", Consolas, monospace; }.output-empty { min-height: 180px; }.output-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 7px; padding: 12px 14px 0; }.output-actions button { height: 34px; display: flex; align-items: center; justify-content: center; gap: 5px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 5px; cursor: pointer; font-size: 9px; }.output-actions svg { width: 13px; }
.settings-panel { padding-bottom: 14px; }.form-section, .model-section, .media-operations, .parameter-list, .split-fields { padding: 12px 14px; border-bottom: 1px solid #eef2f7; }.form-section { position: relative; }.field-label { display: block; margin: 10px 0 6px; color: #475569; font-size: 10px; }.form-section > .field-label:first-child, .model-section > .field-label:first-child { margin-top: 0; } input, textarea, select { width: 100%; box-sizing: border-box; color: #0f172a; background: #fff; border: 1px solid #cbd5e1; border-radius: 5px; outline: 0; font: inherit; font-size: 11px; } input, select { height: 34px; padding: 0 9px; } textarea { padding: 8px 9px; resize: vertical; line-height: 1.55; } input:focus, textarea:focus, select:focus { border-color: #60a5fa; box-shadow: 0 0 0 2px rgba(37, 99, 235, .1); }.form-section > small { position: absolute; right: 18px; bottom: 15px; color: #64748b; font-size: 8px; }.readonly-value { min-height: 34px; display: flex; align-items: center; justify-content: space-between; color: #64748b; font-size: 9px; }.readonly-value b { color: #334155; }.split-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }.split-fields label { display: grid; gap: 5px; color: #475569; font-size: 9px; }.action-grid { display: grid; gap: 8px; }.action-grid.two { grid-template-columns: 1fr 1fr; }.action-grid button, .asset-button, .transform-grid button { min-height: 34px; display: flex; align-items: center; justify-content: center; gap: 5px; color: #334155; background: #fff; border: 1px solid #cbd5e1; border-radius: 5px; cursor: pointer; font-size: 9px; }.action-grid button:hover, .asset-button:hover, .transform-grid button:hover, .transform-grid button.active { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.action-grid svg, .asset-button svg, .transform-grid svg { width: 13px; }.asset-button { width: 100%; margin-top: 8px; }.transform-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 7px; margin-top: 9px; }.transform-grid button { min-width: 0; padding: 0 3px; }.flip-icon { width: 13px; font-size: 14px; }.crop-editor { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; margin-top: 9px; padding: 8px; background: #f8fafc; }.crop-editor label { display: grid; gap: 3px; color: #64748b; font-size: 8px; }.crop-editor input { height: 27px; padding: 0 3px; }.crop-editor button { grid-column: 1 / -1; height: 30px; color: #fff; background: #2563eb; border: 0; border-radius: 4px; cursor: pointer; }.parameter-list > div { min-height: 30px; display: flex; align-items: center; justify-content: space-between; gap: 8px; color: #64748b; font-size: 9px; }.parameter-list b, .parameter-list code { max-width: 180px; overflow: hidden; color: #334155; text-overflow: ellipsis; white-space: nowrap; }.timeline-add { width: calc(100% - 28px); height: 34px; margin: 12px 14px 0; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 5px; cursor: pointer; font-size: 9px; }
.inspector-footer { display: grid; grid-template-columns: 34px minmax(0, 1fr) 34px; align-items: center; gap: 8px; padding: 0 12px; border-top: 1px solid #e2e8f0; box-shadow: 0 -4px 12px rgba(15, 23, 42, .04); }.inspector-footer button { height: 36px; display: grid; place-items: center; border-radius: 5px; }.inspector-footer svg { width: 15px; }.delete-button, .history-button { color: #64748b; background: #f8fafc; }.delete-button:hover { color: #dc2626; background: #fef2f2; }.history-button:hover { color: #2563eb; background: #eff6ff; }.run-button { display: flex !important; align-items: center; justify-content: center; gap: 6px; color: #fff; background: #2563eb; font-size: 10px; font-weight: 650; }.run-button:disabled { opacity: .5; cursor: wait; }
.empty-header { grid-row: 1; }.empty-inspector { grid-row: 2 / -1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: #64748b; text-align: center; }.empty-inspector svg { width: 34px; color: #cbd5e1; }.empty-inspector b { color: #334155; font-size: 12px; }.empty-inspector span { font-size: 9px; }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { transition-duration: .01ms !important; animation-duration: .01ms !important; } }
</style>
