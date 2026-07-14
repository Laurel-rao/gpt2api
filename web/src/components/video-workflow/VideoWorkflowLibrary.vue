<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  ArrowLeft,
  Clock,
  Document,
  Film,
  FolderOpened,
  Grid,
  Picture,
  Plus,
  Search,
  Upload,
  User,
  VideoCamera,
} from '@element-plus/icons-vue'
import type { VideoAsset } from '@/api/videoWorkflow'
import { VIDEO_WORKFLOW_NODE_CATALOG } from '@/utils/videoWorkflowGraph'

type PanelTab = 'nodes' | 'assets'

const props = defineProps<{
  tab: PanelTab
  assets: VideoAsset[]
}>()
const emit = defineEmits<{
  'update:tab': [tab: PanelTab]
  'add-node': [type: string]
  'upload': [file: File]
  'select-asset': [asset: VideoAsset]
  close: []
}>()

const query = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const groups = ['文本', '角色', '图像', '视频', '合成']
const systemTypes = new Set(['timeline', 'compose'])
const filteredCatalog = computed(() => VIDEO_WORKFLOW_NODE_CATALOG.filter((item) => !query.value.trim() || item.label.includes(query.value.trim())))
const filteredAssets = computed(() => props.assets.filter((asset) => !query.value.trim() || asset.name.includes(query.value.trim())))
const nodeIcon = (type: string) => ({
  story_brief: Document,
  character: User,
  script: Grid,
  scene: Grid,
  background: Picture,
  image: Picture,
  video: VideoCamera,
  timeline: Clock,
  compose: Film,
} as Record<string, any>)[type] || Document

function startDrag(event: DragEvent, type: string) {
  if (systemTypes.has(type)) return
  event.dataTransfer?.setData('application/video-workflow-node', type)
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'copy'
}

function pickFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) emit('upload', file)
  input.value = ''
}
</script>

<template>
  <aside id="node-library" class="workflow-library" aria-label="节点与素材库">
    <div class="library-tabs" role="tablist" aria-label="资源类型">
      <button role="tab" :aria-selected="tab === 'nodes'" :class="{ active: tab === 'nodes' }" @click="emit('update:tab', 'nodes')">
        <Grid />节点
      </button>
      <button role="tab" :aria-selected="tab === 'assets'" :class="{ active: tab === 'assets' }" @click="emit('update:tab', 'assets')">
        <FolderOpened />素材
      </button>
      <button
        class="library-collapse"
        type="button"
        title="隐藏节点侧边栏"
        aria-label="隐藏节点侧边栏"
        @click="emit('close')"
      ><ArrowLeft /></button>
    </div>

    <label class="library-search">
      <Search />
      <input v-model="query" :placeholder="tab === 'nodes' ? '搜索节点' : '搜索素材'" />
    </label>

    <div v-if="tab === 'nodes'" class="library-scroll">
      <section v-for="group in groups" :key="group" class="library-group">
        <h3>{{ group }}</h3>
        <button
          v-for="item in filteredCatalog.filter((node) => node.group === group)"
          :key="item.type"
          class="library-node"
          :class="{ system: systemTypes.has(item.type) }"
          :draggable="!systemTypes.has(item.type)"
          :title="systemTypes.has(item.type) ? '系统节点已固定在画布中' : `添加${item.label}`"
          @dragstart="startDrag($event, item.type)"
          @click="systemTypes.has(item.type) || emit('add-node', item.type)"
        >
          <span class="node-icon"><component :is="nodeIcon(item.type)" /></span>
          <span><b>{{ item.label }}</b><small>{{ systemTypes.has(item.type) ? '系统固定节点' : `${item.group}处理` }}</small></span>
          <Plus v-if="!systemTypes.has(item.type)" />
          <span v-else class="fixed-tag">固定</span>
        </button>
      </section>
    </div>

    <div v-else class="library-scroll asset-list">
      <button class="upload-tile" @click="fileInput?.click()"><Upload /><span>上传图片或视频</span></button>
      <input ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp,video/mp4,video/webm,video/quicktime" hidden @change="pickFile" />
      <button v-for="asset in filteredAssets" :key="asset.id" class="asset-tile" @click="emit('select-asset', asset)">
        <img v-if="asset.preview_url" :src="asset.preview_url" :alt="asset.name" />
        <span v-else class="asset-placeholder"><VideoCamera v-if="asset.kind === 'video'" /><Picture v-else /></span>
        <span><b>{{ asset.name }}</b><small>{{ asset.kind === 'video' ? '视频' : '图片' }} · V{{ asset.current_version || asset.versions?.length || 1 }}</small></span>
      </button>
      <div v-if="!filteredAssets.length" class="library-empty">暂无匹配素材</div>
    </div>

    <footer class="status-legend" aria-label="节点状态图例">
      <span><i class="ready" />已就绪</span><span><i class="stale" />需更新</span><span><i class="failed" />错误</span><span><i />待生成</span>
    </footer>
  </aside>
</template>

<style scoped lang="scss">
.workflow-library { height: 100%; display: grid; grid-template-rows: 44px 48px minmax(0, 1fr) 34px; color: #0f172a; background: #fff; }
.library-tabs { display: grid; grid-template-columns: 1fr 1fr 36px; border-bottom: 1px solid #e2e8f0; }
.library-tabs button { position: relative; display: flex; align-items: center; justify-content: center; gap: 7px; color: #64748b; background: #fff; border: 0; cursor: pointer; font-size: 13px; }
.library-tabs button::after { position: absolute; left: 20px; right: 20px; bottom: -1px; height: 2px; content: ''; background: transparent; }
.library-tabs button.active { color: #2563eb; font-weight: 650; }
.library-tabs button.active::after { background: #2563eb; }
.library-tabs svg { width: 16px; }
.library-collapse { color: #64748b; border-left: 1px solid #e2e8f0; }
.library-collapse:hover { color: #2563eb; background: #f8fbff; }
.library-collapse::after { display: none; }
.library-collapse svg { width: 14px; }
.library-search { height: 32px; display: flex; align-items: center; gap: 7px; margin: 8px 12px; padding: 0 9px; color: #94a3b8; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 6px; }
.library-search:focus-within { border-color: #93c5fd; box-shadow: 0 0 0 2px rgba(37, 99, 235, .1); }
.library-search svg { width: 14px; }
.library-search input { width: 100%; min-width: 0; color: #0f172a; background: transparent; border: 0; outline: 0; font-size: 12px; }
.library-scroll { min-height: 0; padding: 2px 12px 14px; overflow: auto; }
.library-group { margin-top: 10px; }
.library-group h3 { margin: 0 0 6px; color: #64748b; font-size: 11px; font-weight: 650; }
.library-node, .asset-tile { width: 100%; display: grid; align-items: center; text-align: left; background: #fff; border: 1px solid #e2e8f0; border-radius: 6px; cursor: grab; transition: background .18s ease, border-color .18s ease, box-shadow .18s ease; }
.library-node { height: 42px; grid-template-columns: 28px minmax(0, 1fr) 16px; gap: 8px; margin-bottom: 6px; padding: 0 9px; }
.library-node:hover, .asset-tile:hover { background: #f8fbff; border-color: #93c5fd; box-shadow: 0 3px 10px rgba(15, 23, 42, .06); }
.library-node:focus-visible, .asset-tile:focus-visible, .upload-tile:focus-visible, .library-tabs button:focus-visible { outline: 2px solid #2563eb; outline-offset: -2px; }
.library-node.system { cursor: default; opacity: .68; }
.node-icon { width: 28px; height: 28px; display: grid; place-items: center; color: #475569; background: #f1f5f9; border-radius: 5px; }
.node-icon svg { width: 15px; }
.library-node > span:nth-child(2), .asset-tile > span:last-child { min-width: 0; display: grid; gap: 1px; }
.library-node b, .asset-tile b { overflow: hidden; color: #1e293b; font-size: 12px; font-weight: 620; text-overflow: ellipsis; white-space: nowrap; }
.library-node small, .asset-tile small { color: #94a3b8; font-size: 9px; }
.library-node > svg { width: 14px; color: #64748b; }
.fixed-tag { color: #64748b; font-size: 9px; }
.asset-list { padding-top: 8px; }
.upload-tile { width: 100%; height: 58px; display: flex; align-items: center; justify-content: center; gap: 8px; color: #2563eb; background: #f8fbff; border: 1px dashed #93c5fd; border-radius: 6px; cursor: pointer; font-size: 12px; }
.upload-tile:hover { background: #eff6ff; border-color: #2563eb; }
.upload-tile svg { width: 17px; }
.asset-tile { grid-template-columns: 48px minmax(0, 1fr); gap: 9px; min-height: 58px; margin-top: 8px; padding: 5px; cursor: pointer; }
.asset-tile img, .asset-placeholder { width: 48px; height: 46px; object-fit: cover; border-radius: 4px; }
.asset-placeholder { display: grid; place-items: center; color: #64748b; background: #e2e8f0; }
.asset-placeholder svg { width: 18px; }
.library-empty { padding: 28px 8px; color: #64748b; text-align: center; font-size: 12px; }
.status-legend { display: flex; align-items: center; justify-content: space-around; gap: 5px; padding: 0 8px; border-top: 1px solid #e2e8f0; color: #64748b; font-size: 9px; }
.status-legend span { display: inline-flex; align-items: center; gap: 4px; white-space: nowrap; }
.status-legend i { width: 7px; height: 7px; background: #94a3b8; border-radius: 50%; }
.status-legend i.ready { background: #22c55e; }.status-legend i.stale { background: #f59e0b; }.status-legend i.failed { background: #ef4444; }
</style>
