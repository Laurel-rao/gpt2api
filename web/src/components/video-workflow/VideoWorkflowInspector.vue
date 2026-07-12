<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  Close,
  Crop,
  Delete,
  MoreFilled,
  Picture,
  Refresh,
  RefreshLeft,
  RefreshRight,
  Select,
  Upload,
  VideoCamera,
  VideoPlay,
  ZoomIn,
} from '@element-plus/icons-vue'
import type { VideoAsset, VideoAssetVersion, VideoWorkflowNode } from '@/api/videoWorkflow'
import { nodeStatusLabel, videoWorkflowModelLabel, videoWorkflowNodePreviewURL } from '@/utils/videoWorkflowGraph'

const props = defineProps<{
  node: VideoWorkflowNode | null
  assets: VideoAsset[]
  running?: boolean
}>()
const emit = defineEmits<{
  'update-title': [value: string]
  'update-config': [key: string, value: any]
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
  'close': []
}>()

const fileInput = ref<HTMLInputElement | null>(null)
const cropping = ref(false)
const crop = reactive({ x: 0, y: 0, width: 100, height: 100 })
const imageNode = computed(() => Boolean(props.node && ['background', 'image'].includes(props.node.type)))
const videoNode = computed(() => props.node?.type === 'video')
const previewURL = computed(() => videoWorkflowNodePreviewURL(props.node))
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
const statusText = computed(() => props.node?.status === 'stale' ? '需更新' : nodeStatusLabel(props.node?.status))
const modelLabel = computed(() => props.node?.type === 'video'
  ? videoWorkflowModelLabel(props.node.config?.model)
  : props.node?.config?.model || (imageNode.value ? '灵境-图像生成 XL v2' : '系统默认'))
const systemNode = computed(() => ['timeline', 'compose'].includes(props.node?.type || ''))

watch(() => [props.node?.id, props.node?.config?.selected_version_id, props.node?.config?.asset_version_id], () => {
  const current = props.node?.config?.image_transform?.crop
  crop.x = Math.round(Number(current?.x || 0) * 100)
  crop.y = Math.round(Number(current?.y || 0) * 100)
  crop.width = Math.round(Number(current?.width || 1) * 100)
  crop.height = Math.round(Number(current?.height || 1) * 100)
  cropping.value = false
}, { immediate: true })

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
        <div><strong>{{ imageNode ? '图片生成' : node.title || node.config?.title || '节点设置' }}</strong><span>{{ node.id }}</span></div>
        <button title="关闭检查器" @click="emit('close')"><Close /></button>
      </header>

      <div class="inspector-scroll">
        <section class="status-row">
          <span>节点状态</span>
          <b :class="node.status || 'idle'"><i />{{ statusText }}</b>
        </section>

        <section v-if="imageNode || videoNode" :class="['image-section', { 'video-section': videoNode }]">
          <div class="image-preview">
            <video v-if="previewURL && videoNode" :src="previewURL" muted playsinline preload="metadata" :aria-label="`${node.title || node.id} 视频缩略预览`" />
            <img v-else-if="previewURL" :src="previewURL" :alt="node.title || '生成图片预览'" draggable="false" />
            <span v-else><component :is="videoNode ? VideoCamera : Picture" /><small>尚未生成{{ videoNode ? '视频' : '图片' }}</small></span>
            <button
              :class="['inspector-preview-button', { video: videoNode }]"
              type="button"
              :aria-label="`全屏预览：${node.title || node.id}`"
              @keydown.stop
              @keyup.stop
              @click="emit('preview-media', node)"
            ><component :is="videoNode ? VideoPlay : ZoomIn" /><span>全屏预览</span></button>
            <em>{{ videoNode ? '15.000s' : '288 × 180' }}</em>
          </div>

          <template v-if="imageNode">
            <label class="field-label" for="image-version">版本</label>
            <select id="image-version" :value="selectedVersion" @change="emit('select-version', ($event.target as HTMLSelectElement).value)">
              <option v-if="!versions.length" value="">当前草稿</option>
              <option v-for="version in versions" :key="version.id" :value="version.id">V{{ version.version }} · {{ version.mime || version.mime_type || 'image' }}</option>
            </select>

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
          </template>
        </section>

        <section class="form-section">
          <label class="field-label">节点名称</label>
          <input :value="node.title || node.config?.title" maxlength="80" @input="emit('update-title', ($event.target as HTMLInputElement).value)" />
          <label class="field-label">{{ node.type === 'script' ? '结构化剧本提示词' : '提示词（Prompt）' }}</label>
          <textarea
            :value="node.config?.prompt || ''"
            :rows="imageNode ? 5 : 8"
            maxlength="1000"
            @input="emit('update-config', 'prompt', ($event.target as HTMLTextAreaElement).value)"
          />
          <small>{{ String(node.config?.prompt || '').length }}/1000</small>
        </section>

        <section v-if="node.type === 'character'" class="form-section split-fields">
          <label>角色姓名<input :value="node.config?.name" @input="emit('update-config', 'name', ($event.target as HTMLInputElement).value)" /></label>
          <label>成年年龄<input :value="node.config?.adult_age" type="number" min="18" max="80" @input="emit('update-config', 'adult_age', Number(($event.target as HTMLInputElement).value))" /></label>
        </section>

        <section class="locked-section">
          <div><span>模型</span><b>{{ modelLabel }}</b></div>
          <div v-if="node.type === 'video'"><span>片段时长</span><b>15.000s</b></div>
          <div v-if="['video', 'compose'].includes(node.type)"><span>输出帧率</span><b>30 fps</b></div>
          <button v-if="node.type === 'video'" class="timeline-add" @click="emit('add-to-timeline')">添加到时间线</button>
          <div v-if="node.type === 'compose'" class="output-actions">
            <button @click="emit('preview-output')">播放成片</button>
            <button @click="emit('download-output')">下载 MP4</button>
          </div>
        </section>

        <section class="ports-section">
          <h3>端口与连接</h3>
          <div v-for="port in node.inputs || []" :key="`i-${port.id}`"><i class="input" /><span>{{ port.label || port.id }}</span><code>{{ port.type }}</code><button @click="emit('add-connection', port.id)">添加</button></div>
          <div v-for="port in node.outputs || []" :key="`o-${port.id}`"><i class="output" /><span>{{ port.label || port.id }}</span><code>{{ port.type }}</code></div>
        </section>
      </div>

      <footer class="inspector-footer">
        <button v-if="!systemNode" class="delete-button" title="删除节点" @click="emit('delete')"><Delete /></button>
        <button class="run-button" :disabled="running" @click="emit('run')"><VideoPlay />{{ running ? '运行中' : '运行此节点' }}</button>
        <button class="more-button" title="更多操作"><MoreFilled /></button>
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
.workflow-inspector { height: 100%; display: grid; grid-template-rows: 56px minmax(0, 1fr) 64px; color: #0f172a; background: #fff; }
.inspector-header { display: flex; align-items: center; justify-content: space-between; padding: 0 14px 0 16px; border-bottom: 1px solid #e2e8f0; }
.inspector-header > div { min-width: 0; display: grid; gap: 2px; }
.inspector-header strong { overflow: hidden; font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.inspector-header span { overflow: hidden; color: #94a3b8; font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.inspector-header button, .inspector-footer button { display: grid; place-items: center; border: 0; cursor: pointer; }
.inspector-header button { width: 32px; height: 32px; color: #64748b; background: transparent; border-radius: 5px; }
.inspector-header button:hover { background: #f1f5f9; }
.inspector-header svg { width: 15px; }
.inspector-scroll { min-height: 0; overflow-y: auto; }
.status-row { height: 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 16px; border-bottom: 1px solid #eef2f7; color: #64748b; font-size: 11px; }
.status-row b { display: flex; align-items: center; gap: 5px; color: #64748b; font-weight: 600; }
.status-row i { width: 7px; height: 7px; background: #94a3b8; border-radius: 50%; }
.status-row b.succeeded { color: #15803d; }.status-row b.succeeded i { background: #22c55e; }
.status-row b.stale { color: #b45309; }.status-row b.stale i { background: #f59e0b; }
.status-row b.failed { color: #b91c1c; }.status-row b.failed i { background: #ef4444; }
.image-section, .form-section, .locked-section, .ports-section { padding: 14px 16px; border-bottom: 1px solid #eef2f7; }
.image-preview { position: relative; height: 180px; display: grid; place-items: center; overflow: hidden; background: #111318; border-radius: 6px; }
.image-preview img, .image-preview video { width: 100%; height: 100%; object-fit: cover; pointer-events: none; transition: transform .18s ease; }
.image-preview > span { display: grid; place-items: center; gap: 8px; color: #94a3b8; }
.image-preview > span svg { width: 30px; }.image-preview small { font-size: 10px; }
.image-preview em { position: absolute; right: 7px; bottom: 7px; padding: 2px 5px; color: #e2e8f0; background: rgba(15, 23, 42, .75); border-radius: 3px; font-size: 9px; font-style: normal; }
.inspector-preview-button { position: absolute; z-index: 2; left: 50%; top: 50%; height: 34px; display: flex; align-items: center; gap: 6px; padding: 0 11px; color: #fff; background: rgba(15, 23, 42, .88); border: 1px solid rgba(255, 255, 255, .7); border-radius: 5px; opacity: 0; cursor: pointer; transform: translate(-50%, -50%) scale(.96); transition: opacity .18s ease, background .18s ease, transform .18s ease; font-size: 10px; }
.inspector-preview-button.video, .image-preview:hover .inspector-preview-button, .image-preview:focus-within .inspector-preview-button { opacity: 1; transform: translate(-50%, -50%) scale(1); }.inspector-preview-button:hover { background: #2563eb; }.inspector-preview-button:focus-visible { outline: 2px solid #93c5fd; outline-offset: 2px; }.inspector-preview-button svg { width: 14px; }
.field-label { display: block; margin: 12px 0 6px; color: #475569; font-size: 11px; }
input, textarea, select { width: 100%; box-sizing: border-box; color: #0f172a; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; outline: 0; font: inherit; font-size: 12px; }
input, select { height: 34px; padding: 0 9px; } textarea { padding: 8px 9px; resize: vertical; line-height: 1.55; }
input:focus, textarea:focus, select:focus { border-color: #60a5fa; box-shadow: 0 0 0 2px rgba(37, 99, 235, .1); }
.action-grid { display: grid; gap: 8px; margin-top: 10px; }.action-grid.two { grid-template-columns: 1fr 1fr; }
.action-grid button, .asset-button, .transform-grid button { min-height: 34px; display: flex; align-items: center; justify-content: center; gap: 6px; color: #334155; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; font-size: 11px; }
.action-grid button:hover, .asset-button:hover, .transform-grid button:hover, .transform-grid button.active { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }
.action-grid svg, .asset-button svg, .transform-grid svg { width: 14px; }
.asset-button { width: 100%; margin-top: 8px; }
.transform-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 7px; margin-top: 10px; }
.transform-grid button { min-width: 0; padding: 0 4px; font-size: 10px; }
.flip-icon { width: 14px; font-size: 15px; line-height: 1; }
.crop-editor { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; margin-top: 10px; padding: 9px; background: #f8fafc; border-radius: 5px; }
.crop-editor label { display: grid; gap: 3px; color: #64748b; font-size: 9px; }.crop-editor input { height: 28px; padding: 0 4px; }
.crop-editor button { grid-column: 1 / -1; height: 30px; color: #fff; background: #2563eb; border: 0; border-radius: 4px; cursor: pointer; }
.form-section { position: relative; }.form-section > .field-label:first-child { margin-top: 0; }.form-section > small { position: absolute; right: 19px; bottom: 16px; color: #94a3b8; font-size: 9px; }
.split-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }.split-fields label { display: grid; gap: 5px; color: #475569; font-size: 10px; }
.locked-section > div { display: flex; justify-content: space-between; padding: 5px 0; color: #64748b; font-size: 10px; }.locked-section b { max-width: 180px; overflow: hidden; color: #334155; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.timeline-add { width: 100%; height: 32px; margin-top: 7px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 5px; cursor: pointer; font-size: 10px; }
.output-actions { display: grid !important; grid-template-columns: 1fr 1fr; gap: 7px; margin-top: 7px; }.output-actions button { height: 32px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 5px; cursor: pointer; font-size: 10px; }
.ports-section h3 { margin: 0 0 7px; color: #475569; font-size: 11px; }
.ports-section > div { min-height: 28px; display: grid; grid-template-columns: 8px minmax(0, 1fr) auto auto; align-items: center; gap: 7px; font-size: 10px; }
.ports-section i { width: 7px; height: 7px; border-radius: 50%; }.ports-section i.input { border: 1px solid #2563eb; }.ports-section i.output { background: #16a34a; }
.ports-section code { color: #94a3b8; }.ports-section button { height: 22px; color: #2563eb; background: #eff6ff; border: 0; border-radius: 4px; cursor: pointer; font-size: 9px; }
.inspector-footer { display: grid; grid-template-columns: 34px minmax(0, 1fr) 34px; align-items: center; gap: 8px; padding: 0 14px; border-top: 1px solid #e2e8f0; box-shadow: 0 -6px 16px rgba(15, 23, 42, .04); }
.inspector-footer button { height: 36px; border-radius: 5px; }.inspector-footer svg { width: 15px; }
.delete-button, .more-button { color: #64748b; background: #f8fafc; }.delete-button:hover { color: #dc2626; background: #fef2f2; }
.run-button { display: flex !important; grid-auto-flow: column; gap: 7px; color: #fff; background: #2563eb; font-weight: 620; }.run-button:hover { background: #1d4ed8; }.run-button:disabled { opacity: .55; cursor: not-allowed; }
.empty-header { grid-row: 1; }.empty-inspector { grid-row: 2 / -1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: #94a3b8; }.empty-inspector svg { width: 30px; }.empty-inspector b { color: #475569; font-size: 13px; }.empty-inspector span { font-size: 11px; }
button:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
</style>
