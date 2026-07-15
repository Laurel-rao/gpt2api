<script setup lang="ts">
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import type { VideoWorkflowEstimate, VideoWorkflowRunMode } from '@/api/videoWorkflow'

function runTitle(mode: VideoWorkflowRunMode) {
  if (mode === 'full') return '生成完整成片'
  if (mode === 'node_only') return '运行当前节点'
  if (mode === 'upstream') return '运行当前节点并重跑上游'
  return '运行当前及下游'
}

function open(mode: VideoWorkflowRunMode, estimate: VideoWorkflowEstimate) {
  const cached = estimate.cached_credits ? `，缓存节省 ${estimate.cached_credits} 积分` : ''
  return ElMessageBox.confirm(
    `预计消耗 ${estimate.total_credits} 积分${cached}。确认后开始生成。`,
    runTitle(mode),
    { confirmButtonText: '确认运行', cancelButtonText: '取消', type: 'warning' },
  )
}

defineExpose({ open })
</script>

<template>
  <span class="run-confirm-host" aria-hidden="true" />
</template>

<style scoped>
.run-confirm-host { display: none; }
</style>
