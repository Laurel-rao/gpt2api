<script setup lang="ts">
import { WarningFilled } from '@element-plus/icons-vue'
import type { VideoWorkflowValidationIssue } from '@/api/videoWorkflow'

defineProps<{
  modelValue: boolean
  issues: VideoWorkflowValidationIssue[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  select: [issue: VideoWorkflowValidationIssue]
}>()

function issueMeta(issue: VideoWorkflowValidationIssue) {
  const parts: string[] = []
  if (issue.node_id) parts.push(`节点 ${issue.node_id}`)
  if (issue.edge_id) parts.push(`连线 ${issue.edge_id}`)
  if (issue.code) parts.push(issue.code)
  return parts.join(' · ')
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    direction="rtl"
    size="380px"
    class="validation-issues-drawer"
    :append-to-body="true"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="issues-heading">
        <WarningFilled />
        <span>
          <strong>校验问题</strong>
          <small>共 {{ issues.length }} 项，点击可定位节点</small>
        </span>
      </div>
    </template>

    <div v-if="!issues.length" class="issues-empty">暂无校验问题</div>
    <ol v-else class="issues-list">
      <li v-for="(issue, index) in issues" :key="`${issue.code}-${issue.node_id || ''}-${issue.edge_id || ''}-${index}`">
        <button type="button" @click="emit('select', issue)">
          <b>{{ issue.message }}</b>
          <small v-if="issueMeta(issue)">{{ issueMeta(issue) }}</small>
        </button>
      </li>
    </ol>
  </el-drawer>
</template>

<style scoped lang="scss">
.issues-heading {
  display: flex;
  align-items: center;
  gap: 10px;
}
.issues-heading > svg { width: 20px; color: #b45309; }
.issues-heading span { display: grid; gap: 2px; }
.issues-heading strong { color: #0f172a; font-size: 15px; }
.issues-heading small { color: #64748b; font-size: 10px; }
.issues-empty {
  padding: 48px 12px;
  color: #94a3b8;
  text-align: center;
  font-size: 12px;
}
.issues-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 8px;
}
.issues-list button {
  width: 100%;
  display: grid;
  gap: 4px;
  padding: 12px;
  text-align: left;
  color: #0f172a;
  background: #fff;
  border: 1px solid #dbe2ea;
  border-radius: 6px;
  cursor: pointer;
}
.issues-list button:hover {
  border-color: #93c5fd;
  background: #eff6ff;
}
.issues-list b {
  color: #334155;
  font-size: 12px;
  font-weight: 600;
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.issues-list small {
  color: #64748b;
  font-size: 10px;
}
</style>
