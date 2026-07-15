<script setup lang="ts">
import { computed } from 'vue'
import { ArrowLeft, Clock, Download, Refresh, VideoPlay, View } from '@element-plus/icons-vue'
import type {
  VideoWorkflowNodeRun,
  VideoWorkflowRun,
  VideoWorkflowRunMode,
  VideoWorkflowRunStatus,
} from '@/api/videoWorkflow'
import { formatErrorCode } from '@/utils/format'
import { VIDEO_WORKFLOW_NODE_CATALOG, nodeStatusLabel, videoWorkflowNodeTypeLabel } from '@/utils/videoWorkflowGraph'

const props = defineProps<{
  modelValue: boolean
  runs: VideoWorkflowRun[]
  total: number
  loading?: boolean
  actionRunID?: string
  currentRunID?: string
  detail?: VideoWorkflowRun | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'refresh': []
  'load-more': []
  'inspect': [run: VideoWorkflowRun]
  'expand': [run: VideoWorkflowRun]
  'switch': [run: VideoWorkflowRun]
  'back': []
  'preview': [run: VideoWorkflowRun]
  'download': [run: VideoWorkflowRun]
  'generate': []
}>()

const hasMore = computed(() => props.runs.length < props.total)
const statusLabels: Record<VideoWorkflowRunStatus, string> = {
  queued: '排队中', running: '生成中', awaiting_character_approval: '待选角色', awaiting_storyboard_approval: '待确认分镜',
  cancel_pending: '停止中', canceled: '已停止', succeeded: '已完成', failed: '失败',
}
const modeLabels: Record<VideoWorkflowRunMode, string> = {
  full: '完整成片', node_only: '当前节点', upstream: '当前+上游', downstream: '当前及下游',
}

function statusLabel(status: VideoWorkflowRunStatus) { return statusLabels[status] || status }
function modeLabel(mode?: VideoWorkflowRunMode) { return mode ? modeLabels[mode] || mode : '完整成片' }
function progress(run: VideoWorkflowRun) { return Math.max(0, Math.min(100, Number(run.progress || 0))) }
function nodeProgress(node: VideoWorkflowNodeRun) {
  if (typeof node.progress === 'number') return Math.max(0, Math.min(100, node.progress))
  return node.status === 'succeeded' ? 100 : 0
}
function formatDate(value?: string) {
  if (!value) return '时间未知'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}
function formatDuration(run: VideoWorkflowRun) {
  const start = new Date(run.started_at || run.created_at || '').getTime()
  const end = new Date(run.finished_at || run.updated_at || '').getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return '—'
  const seconds = Math.round((end - start) / 1000)
  if (seconds < 60) return `${seconds}秒`
  return `${Math.floor(seconds / 60)}分${String(seconds % 60).padStart(2, '0')}秒`
}
function credits(run: VideoWorkflowRun) {
  const value = run.actual_credits || run.estimated_credits
  return value ? `${value} 积分` : '未计费'
}
function canOutput(run: VideoWorkflowRun) {
  return run.status === 'succeeded' && Boolean(run.output_version_id || run.output_asset_version_id || run.output_url || run.output)
}
function nodeRuns(run: VideoWorkflowRun) {
  return run.node_runs || []
}
function completedNodes(run: VideoWorkflowRun) {
  const nodes = nodeRuns(run)
  return `${nodes.filter((item) => item.status === 'succeeded').length}/${nodes.length}`
}
function nodeTypeLabel(type?: string) {
  return videoWorkflowNodeTypeLabel(type)
}
function nodeRunTitle(run: VideoWorkflowRun, node: VideoWorkflowNodeRun) {
  const graphNode = run.graph_snapshot?.nodes?.find((item) => item.id === node.node_id)
  const titled = graphNode?.title || graphNode?.config?.title || graphNode?.config?.name
  if (typeof titled === 'string' && titled.trim()) return titled.trim()
  return `${nodeTypeLabel(node.node_type)} · ${node.node_id}`
}
function nodeErrorText(node: VideoWorkflowNodeRun) {
  const label = formatErrorCode(node.error_code)
  const message = node.error_message || node.error || ''
  if (label && message) return `${label}：${message}`
  return message || label || node.error_code || ''
}
function errorLabel(code?: string, fallback = '运行失败') {
  return formatErrorCode(code) || fallback
}
function historyErrorText(run: VideoWorkflowRun) {
  const label = formatErrorCode(run.error_code)
  if (label && run.error_message) return `${label}：${run.error_message}`
  return run.error_message || label || run.error_code || ''
}
function requestNodeRuns(run: VideoWorkflowRun) {
  if (run.node_runs?.length || props.actionRunID === run.id) return
  emit('expand', run)
}
function isCurrentRun(run: VideoWorkflowRun) {
  return Boolean(props.currentRunID) && props.currentRunID === run.id
}
function switchLabel(run: VideoWorkflowRun) {
  return isCurrentRun(run) ? '当前查看' : '切换查看'
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    size="420px"
    class="run-history-drawer"
    :append-to-body="true"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="history-heading">
        <div><Clock /><span><strong>生成历史</strong><small>共 {{ total }} 条运行记录</small></span></div>
        <button title="刷新生成历史" aria-label="刷新生成历史" :disabled="loading" @click="emit('refresh')"><Refresh /></button>
      </div>
    </template>

    <div v-if="detail" class="run-detail">
      <button class="back-history" @click="emit('back')"><ArrowLeft />返回历史列表</button>
      <section class="detail-summary">
        <header><span :class="['status-pill', detail.status]">{{ statusLabel(detail.status) }}</span><b>R{{ detail.workflow_revision }}</b></header>
        <h3>{{ modeLabel(detail.run_mode) }}</h3>
        <p>{{ formatDate(detail.created_at) }} · {{ formatDuration(detail) }} · {{ credits(detail) }}</p>
        <div class="detail-progress"><i :style="{ width: `${progress(detail)}%` }" /><span>{{ progress(detail) }}%</span></div>
        <div class="detail-actions">
          <button
            type="button"
            :class="{ current: isCurrentRun(detail) }"
            :disabled="actionRunID === detail.id"
            @click="emit('switch', detail)"
          >{{ switchLabel(detail) }}</button>
          <button :disabled="!canOutput(detail)" @click="emit('preview', detail)"><VideoPlay />预览成片</button>
          <button :disabled="!canOutput(detail)" @click="emit('download', detail)"><Download />下载</button>
        </div>
      </section>
      <section v-if="detail.error_message || detail.error_code" class="run-error" role="alert">
        <b :title="detail.error_code || undefined">
          {{ errorLabel(detail.error_code) }}
          <small v-if="detail.error_code && formatErrorCode(detail.error_code) !== detail.error_code">（{{ detail.error_code }}）</small>
        </b>
        <span>{{ detail.error_message }}</span>
      </section>
      <section class="node-run-list">
        <header><strong>节点进度</strong><span>{{ completedNodes(detail) }} 完成</span></header>
        <div v-for="node in nodeRuns(detail)" :key="node.id" class="node-run-row">
          <i :class="node.status" />
          <span>
            <b>{{ nodeRunTitle(detail, node) }}</b>
            <small>
              <em :class="['status-pill', 'node-status', node.status]">{{ nodeStatusLabel(node.status) }}</em>
              <template v-if="nodeErrorText(node)"> · {{ nodeErrorText(node) }}</template>
              <template v-else> · {{ nodeTypeLabel(node.node_type) }} · {{ node.node_id }}</template>
            </small>
          </span>
          <em>{{ nodeProgress(node) }}%</em>
        </div>
        <div v-if="!nodeRuns(detail).length" class="detail-empty">该记录没有节点明细</div>
      </section>
    </div>

    <template v-else>
      <div v-if="loading && !runs.length" class="history-skeleton" aria-label="正在加载生成历史"><i v-for="index in 4" :key="index" /></div>
      <div v-else-if="!runs.length" class="history-empty">
        <Clock />
        <b>暂无生成记录</b>
        <span>首次运行后，状态、耗时和成片会保留在这里。</span>
        <button @click="emit('generate')">生成第一条记录</button>
      </div>
      <div v-else class="history-list">
        <article v-for="run in runs" :key="run.id" :class="{ busy: actionRunID === run.id, current: isCurrentRun(run) }">
          <header>
            <span :class="['status-pill', run.status]">{{ statusLabel(run.status) }}</span>
            <b>{{ modeLabel(run.run_mode) }}</b>
            <em>R{{ run.workflow_revision }}</em>
            <small v-if="isCurrentRun(run)" class="current-tag">当前</small>
          </header>
          <div class="run-meta"><span>{{ formatDate(run.created_at) }}</span><span>{{ formatDuration(run) }}</span><span>{{ credits(run) }}</span></div>
          <div class="history-progress"><i :style="{ width: `${progress(run)}%` }" /><span>{{ progress(run) }}%</span></div>
          <p v-if="run.error_message || run.error_code" class="history-error" :title="run.error_code || undefined">{{ historyErrorText(run) }}</p>

          <section class="card-node-runs">
            <header class="node-toggle">
              <strong>节点进度</strong>
              <span v-if="nodeRuns(run).length">{{ completedNodes(run) }} 完成</span>
              <button
                v-else
                type="button"
                class="load-nodes"
                :disabled="actionRunID === run.id"
                @click="requestNodeRuns(run)"
              >{{ actionRunID === run.id ? '加载中…' : '展开全部节点' }}</button>
            </header>
            <div v-if="nodeRuns(run).length" class="card-node-list">
              <div v-for="node in nodeRuns(run)" :key="node.id" class="node-run-row compact">
                <i :class="node.status" />
                <span>
                  <b>{{ nodeRunTitle(run, node) }}</b>
                  <small>
                    <em :class="['status-pill', 'node-status', node.status]">{{ nodeStatusLabel(node.status) }}</em>
                    <template v-if="nodeErrorText(node)"> · {{ nodeErrorText(node) }}</template>
                  </small>
                </span>
                <em>{{ nodeProgress(node) }}%</em>
              </div>
            </div>
          </section>

          <footer>
            <button
              type="button"
              :class="{ current: isCurrentRun(run) }"
              :disabled="actionRunID === run.id"
              @click="emit('switch', run)"
            >{{ switchLabel(run) }}</button>
            <button @click="emit('inspect', run)"><View />详情</button>
            <button :disabled="!canOutput(run)" @click="emit('preview', run)"><VideoPlay />预览</button>
            <button :disabled="!canOutput(run)" @click="emit('download', run)"><Download />下载</button>
          </footer>
        </article>
        <button v-if="hasMore" class="load-more" :disabled="loading" @click="emit('load-more')">{{ loading ? '加载中…' : `加载更多（${runs.length}/${total}）` }}</button>
      </div>
    </template>
  </el-drawer>
</template>

<style scoped lang="scss">
.history-heading { width: 100%; display: flex; align-items: center; justify-content: space-between; }.history-heading > div { display: flex; align-items: center; gap: 10px; }.history-heading > div > svg { width: 20px; color: #2563eb; }.history-heading span { display: grid; gap: 2px; }.history-heading strong { color: #0f172a; font-size: 15px; }.history-heading small { color: #64748b; font-size: 10px; }.history-heading button { width: 32px; height: 32px; display: grid; place-items: center; color: #475569; background: #f8fafc; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; }.history-heading button:hover { color: #2563eb; border-color: #93c5fd; }.history-heading button:disabled { opacity: .5; cursor: wait; }.history-heading button svg { width: 14px; }
.history-list { display: grid; gap: 10px; padding-bottom: 16px; }.history-list article { padding: 13px; background: #fff; border: 1px solid #dbe2ea; border-radius: 7px; transition: border-color .18s ease, box-shadow .18s ease; }.history-list article:hover { border-color: #93c5fd; box-shadow: 0 5px 16px rgba(15, 23, 42, .07); }.history-list article.busy { opacity: .58; pointer-events: none; }.history-list article.current { border-color: #2563eb; box-shadow: 0 0 0 1px rgba(37, 99, 235, .18); }.history-list article header { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; align-items: center; gap: 8px; }.history-list article header b { overflow: hidden; color: #334155; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.history-list article header em { color: #64748b; font-size: 10px; font-style: normal; }.current-tag { color: #1d4ed8; background: #dbeafe; border-radius: 999px; padding: 2px 6px; font-size: 9px; font-weight: 650; }
.status-pill { display: inline-flex; align-items: center; padding: 3px 7px; color: #475569; background: #f1f5f9; border-radius: 999px; font-size: 9px; font-weight: 650; }.status-pill.running, .status-pill.queued, .status-pill.cancel_pending { color: #1d4ed8; background: #dbeafe; }.status-pill.succeeded { color: #15803d; background: #dcfce7; }.status-pill.failed { color: #b91c1c; background: #fee2e2; }.status-pill.canceled { color: #475569; background: #e2e8f0; }.status-pill.awaiting_character_approval, .status-pill.awaiting_storyboard_approval, .status-pill.awaiting_approval { color: #b45309; background: #fef3c7; }.status-pill.node-status { padding: 1px 5px; font-size: 8px; font-style: normal; font-weight: 650; }
.run-meta { display: flex; gap: 10px; margin-top: 9px; color: #64748b; font-size: 9px; }.run-meta span + span { position: relative; padding-left: 10px; }.run-meta span + span::before { position: absolute; left: 0; content: '·'; }.history-progress, .detail-progress { position: relative; height: 5px; margin-top: 10px; overflow: hidden; background: #e2e8f0; border-radius: 999px; }.history-progress i, .detail-progress i { position: absolute; inset: 0 auto 0 0; background: #2563eb; border-radius: inherit; }.history-progress span, .detail-progress span { position: absolute; right: 0; top: -15px; color: #64748b; font-size: 8px; }.history-error { margin: 9px 0 0; overflow: hidden; color: #b91c1c; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.history-list article footer { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 6px; margin-top: 12px; }
.history-list article footer button, .detail-actions button { height: 28px; display: flex; align-items: center; gap: 4px; padding: 0 9px; color: #334155; background: #f8fafc; border: 1px solid #dbe2ea; border-radius: 4px; cursor: pointer; font-size: 9px; }.history-list article footer button:hover, .detail-actions button:hover { color: #2563eb; background: #eff6ff; border-color: #93c5fd; }.history-list article footer button:disabled, .detail-actions button:disabled { opacity: .4; cursor: not-allowed; }.history-list article footer button.current, .detail-actions button.current { color: #1d4ed8; background: #dbeafe; border-color: #93c5fd; cursor: default; }.history-list article footer svg, .detail-actions svg { width: 12px; }
.card-node-runs { margin-top: 10px; padding-top: 8px; border-top: 1px solid #eef2f7; }.node-toggle { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 8px; color: #334155; font-size: 10px; }.node-toggle strong { font-size: 10px; }.node-toggle > span { color: #64748b; font-size: 9px; }.load-nodes { height: 22px; padding: 0 8px; color: #2563eb; background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 4px; cursor: pointer; font-size: 9px; }.load-nodes:disabled { opacity: .5; cursor: wait; }.card-node-list { display: grid; margin-top: 6px; }
.history-empty { min-height: 420px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 9px; color: #64748b; text-align: center; }.history-empty > svg { width: 34px; color: #94a3b8; }.history-empty b { color: #334155; font-size: 13px; }.history-empty span { width: 240px; font-size: 10px; line-height: 1.6; }.history-empty button { height: 32px; margin-top: 4px; padding: 0 14px; color: #fff; background: #2563eb; border: 0; border-radius: 5px; cursor: pointer; font-size: 10px; }.history-skeleton { display: grid; gap: 10px; }.history-skeleton i { height: 126px; background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 40%, #f1f5f9 65%); background-size: 400% 100%; border-radius: 7px; animation: skeleton 1.3s ease infinite; }.load-more { height: 34px; color: #475569; background: #fff; border: 1px solid #dbe2ea; border-radius: 5px; cursor: pointer; font-size: 10px; }
.back-history { display: flex; align-items: center; gap: 5px; padding: 0; color: #475569; background: transparent; border: 0; cursor: pointer; font-size: 10px; }.back-history svg { width: 13px; }.detail-summary { margin-top: 14px; padding: 14px; background: #f8fafc; border: 1px solid #dbe2ea; border-radius: 7px; }.detail-summary header { display: flex; align-items: center; justify-content: space-between; }.detail-summary header b { color: #64748b; font-size: 10px; }.detail-summary h3 { margin: 11px 0 4px; color: #0f172a; font-size: 15px; }.detail-summary p { margin: 0; color: #64748b; font-size: 9px; }.detail-actions { display: flex; gap: 7px; margin-top: 13px; }.run-error { display: grid; gap: 4px; margin-top: 10px; padding: 10px; color: #b91c1c; background: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; font-size: 10px; }.run-error b { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px; }.run-error b small { color: #9f1239; font-size: 8px; font-weight: 500; }.run-error span { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.5; }.node-run-list { margin-top: 16px; }.node-run-list > header { display: flex; justify-content: space-between; margin-bottom: 7px; color: #334155; font-size: 11px; }.node-run-list > header span { color: #64748b; font-size: 9px; }.node-run-row { min-height: 42px; display: grid; grid-template-columns: 8px minmax(0, 1fr) auto; align-items: center; gap: 8px; border-bottom: 1px solid #eef2f7; }.node-run-row.compact { min-height: 36px; }.node-run-row > i { width: 7px; height: 7px; background: #94a3b8; border-radius: 50%; }.node-run-row > i.running, .node-run-row > i.queued { background: #3b82f6; }.node-run-row > i.succeeded { background: #22c55e; }.node-run-row > i.failed { background: #ef4444; }.node-run-row > i.awaiting_approval { background: #f59e0b; }.node-run-row > span { min-width: 0; display: grid; gap: 2px; }.node-run-row b { overflow: hidden; color: #334155; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.node-run-row small { overflow: hidden; color: #64748b; font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }.node-run-row em { color: #64748b; font-size: 9px; font-style: normal; }.detail-empty { padding: 30px 0; color: #94a3b8; text-align: center; font-size: 10px; }
@keyframes skeleton { from { background-position: 100% 0; } to { background-position: 0 0; } }
@media (prefers-reduced-motion: reduce) { .history-skeleton i { animation: none; } }
</style>
