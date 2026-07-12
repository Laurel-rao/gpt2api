<script setup lang="ts">
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import { VIDEO_WORKFLOW_NODE_CATALOG } from '@/utils/videoWorkflowGraph'

defineProps<{
  modelValue: boolean
  nodes: VideoWorkflowNode[]
  edgeCount: number
  clipCount: number
}>()

const emit = defineEmits<{
  'update:modelValue': [visible: boolean]
  'select': [nodeID: string]
}>()

function nodeTitle(node: VideoWorkflowNode) {
  return node.title || node.config?.title || node.config?.name
    || VIDEO_WORKFLOW_NODE_CATALOG.find((item) => item.type === node.type)?.label || node.id || '未命名节点'
}

function selectNode(nodeID: string) {
  emit('select', nodeID)
  emit('update:modelValue', false)
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    title="结构大纲"
    direction="rtl"
    size="360px"
    class="outline-drawer"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="outline-summary"><b>{{ nodes.length }}</b> 节点 · <b>{{ edgeCount }}</b> 连线 · <b>{{ clipCount }}</b> 片段</div>
    <button v-for="node in nodes" :key="node.id" class="outline-node" @click="selectNode(node.id)">
      <span>{{ node.type }}</span>
      <b>{{ nodeTitle(node) }}</b>
      <em :class="node.status || 'idle'">{{ node.status === 'stale' ? '需更新' : node.status || '待生成' }}</em>
    </button>
  </el-drawer>
</template>

<style scoped>
.outline-summary { margin-bottom: 12px; padding: 10px; color: #64748b; background: #f8fafc; border-radius: 6px; font-size: 11px; }
.outline-node { width: 100%; min-height: 42px; display: grid; grid-template-columns: 70px minmax(0, 1fr) auto; align-items: center; gap: 7px; margin-bottom: 6px; padding: 0 9px; text-align: left; background: #fff; border: 1px solid #e2e8f0; border-radius: 5px; cursor: pointer; font-family: inherit; }
.outline-node > span { color: #94a3b8; font-size: 9px; }
.outline-node b { overflow: hidden; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.outline-node em { color: #64748b; font-size: 9px; font-style: normal; }
.outline-node em.stale { color: #b45309; }
.outline-node em.succeeded { color: #15803d; }
.outline-node:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
</style>
