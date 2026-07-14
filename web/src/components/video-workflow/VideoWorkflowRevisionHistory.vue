<script setup lang="ts">
import { computed } from 'vue'
import { Clock, Refresh, Switch } from '@element-plus/icons-vue'
import type { VideoWorkflowRevisionListItem } from '@/api/videoWorkflow'

const props = defineProps<{
  modelValue: boolean
  items: VideoWorkflowRevisionListItem[]
  total: number
  loading?: boolean
  actionRevision?: number | null
  viewingRevision?: number | null
  currentRevision?: number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  refresh: []
  'load-more': []
  switch: [item: VideoWorkflowRevisionListItem]
}>()

const hasMore = computed(() => props.items.length < props.total)

function formatDate(value?: string) {
  if (!value) return '时间未知'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

function isViewing(item: VideoWorkflowRevisionListItem) {
  return props.viewingRevision != null && props.viewingRevision === item.revision
}

function switchLabel(item: VideoWorkflowRevisionListItem) {
  if (isViewing(item)) return '当前查看'
  if (item.is_current) return '恢复当前版'
  return '切换到此版'
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    size="400px"
    class="revision-history-drawer"
    :append-to-body="true"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="history-heading">
        <div>
          <Clock />
          <span>
            <strong>版本历史</strong>
            <small>共 {{ total }} 个画布修订 · 当前 R{{ currentRevision || '—' }}</small>
          </span>
        </div>
        <button title="刷新版本历史" aria-label="刷新版本历史" :disabled="loading" @click="emit('refresh')">
          <Refresh />
        </button>
      </div>
    </template>

    <div v-if="loading && !items.length" class="history-skeleton" aria-label="正在加载版本历史">
      <i v-for="index in 4" :key="index" />
    </div>
    <div v-else-if="!items.length" class="history-empty">
      <Clock />
      <b>暂无版本记录</b>
      <span>首次保存后，每次自动/手动保存都会留下一条可切换的画布快照。</span>
    </div>
    <div v-else class="history-list">
      <article
        v-for="item in items"
        :key="item.revision"
        :class="{
          busy: actionRevision === item.revision,
          current: item.is_current,
          viewing: isViewing(item),
        }"
      >
        <header>
          <b>R{{ item.revision }}</b>
          <em v-if="item.is_current" class="tag current-tag">服务器当前</em>
          <em v-if="isViewing(item)" class="tag viewing-tag">画布查看中</em>
        </header>
        <p class="revision-name">{{ item.name || '未命名工作流' }}</p>
        <div class="run-meta">
          <span>{{ formatDate(item.created_at) }}</span>
          <span>{{ item.node_count }} 节点</span>
          <span>{{ item.edge_count }} 连线</span>
        </div>
        <footer>
          <button
            type="button"
            :class="{ current: isViewing(item) }"
            :disabled="actionRevision === item.revision || isViewing(item)"
            @click="emit('switch', item)"
          >
            <Switch />{{ switchLabel(item) }}
          </button>
        </footer>
      </article>
      <button v-if="hasMore" class="load-more" :disabled="loading" @click="emit('load-more')">
        {{ loading ? '加载中…' : `加载更多（${items.length}/${total}）` }}
      </button>
    </div>
  </el-drawer>
</template>

<style scoped lang="scss">
.history-heading {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.history-heading > div {
  display: flex;
  align-items: center;
  gap: 10px;
}
.history-heading > div > svg {
  width: 20px;
  color: #2563eb;
}
.history-heading span {
  display: grid;
  gap: 2px;
}
.history-heading strong {
  color: #0f172a;
  font-size: 15px;
}
.history-heading small {
  color: #64748b;
  font-size: 10px;
}
.history-heading button {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  color: #475569;
  background: #f8fafc;
  border: 1px solid #dbe2ea;
  border-radius: 5px;
  cursor: pointer;
}
.history-heading button:hover {
  color: #2563eb;
  border-color: #93c5fd;
}
.history-heading button:disabled {
  opacity: .5;
  cursor: wait;
}
.history-heading button svg {
  width: 14px;
}
.history-list {
  display: grid;
  gap: 10px;
  padding-bottom: 16px;
}
.history-list article {
  padding: 13px;
  background: #fff;
  border: 1px solid #dbe2ea;
  border-radius: 7px;
}
.history-list article.current {
  border-color: #93c5fd;
}
.history-list article.viewing {
  border-color: #2563eb;
  box-shadow: 0 0 0 1px rgba(37, 99, 235, .18);
}
.history-list article.busy {
  opacity: .58;
  pointer-events: none;
}
.history-list article header {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.history-list article header b {
  color: #0f172a;
  font-size: 14px;
  font-variant-numeric: tabular-nums;
}
.tag {
  padding: 2px 6px;
  border-radius: 999px;
  font-size: 9px;
  font-style: normal;
  font-weight: 650;
}
.current-tag {
  color: #1d4ed8;
  background: #dbeafe;
}
.viewing-tag {
  color: #0f766e;
  background: #ccfbf1;
}
.revision-name {
  margin: 8px 0 0;
  color: #334155;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-meta {
  display: flex;
  gap: 10px;
  margin-top: 8px;
  color: #64748b;
  font-size: 9px;
}
.run-meta span + span {
  position: relative;
  padding-left: 10px;
}
.run-meta span + span::before {
  position: absolute;
  left: 0;
  content: '·';
}
.history-list article footer {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.history-list article footer button {
  height: 28px;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 10px;
  color: #fff;
  background: #2563eb;
  border: 1px solid #2563eb;
  border-radius: 4px;
  cursor: pointer;
  font-size: 10px;
}
.history-list article footer button:hover:not(:disabled) {
  background: #1d4ed8;
  border-color: #1d4ed8;
}
.history-list article footer button.current,
.history-list article footer button:disabled {
  color: #1d4ed8;
  background: #dbeafe;
  border-color: #93c5fd;
  cursor: default;
  opacity: 1;
}
.history-list article footer svg {
  width: 12px;
}
.history-empty {
  min-height: 360px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: #64748b;
  text-align: center;
}
.history-empty > svg {
  width: 34px;
  color: #94a3b8;
}
.history-empty b {
  color: #334155;
  font-size: 13px;
}
.history-empty span {
  width: 260px;
  font-size: 10px;
  line-height: 1.6;
}
.history-skeleton {
  display: grid;
  gap: 10px;
}
.history-skeleton i {
  height: 96px;
  background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 40%, #f1f5f9 65%);
  background-size: 400% 100%;
  border-radius: 7px;
  animation: skeleton 1.3s ease infinite;
}
.load-more {
  height: 34px;
  color: #475569;
  background: #fff;
  border: 1px solid #dbe2ea;
  border-radius: 5px;
  cursor: pointer;
  font-size: 10px;
}
@keyframes skeleton {
  from { background-position: 100% 0; }
  to { background-position: 0 0; }
}
@media (prefers-reduced-motion: reduce) {
  .history-skeleton i { animation: none; }
}
</style>
