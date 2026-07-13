<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import { UserFilled } from '@element-plus/icons-vue'

defineProps<{
  sourceCount: number
  targetCount: number
  selected?: boolean
}>()
</script>

<template>
  <article :class="['asset-bus', { selected }]" :aria-label="`公共角色资产，${sourceCount} 个角色供 ${targetCount} 个视频引用`">
    <Handle id="characters" type="target" :position="Position.Left" :connectable="false" class="asset-bus-port" />
    <UserFilled />
    <span><b>公共角色资产</b><small>{{ sourceCount }} → {{ targetCount }}</small></span>
    <Handle id="shared" type="source" :position="Position.Right" :connectable="false" class="asset-bus-port" />
  </article>
</template>

<style scoped>
.asset-bus { width: 132px; height: 44px; box-sizing: border-box; display: grid; grid-template-columns: 24px minmax(0, 1fr); align-items: center; gap: 7px; padding: 0 11px; color: #eef2f6; background: #20262d; border: 1px solid #64748b; border-radius: 6px; box-shadow: 0 8px 20px rgba(0, 0, 0, .28); }
.asset-bus.selected { border-color: #3b82f6; box-shadow: 0 0 0 2px rgba(37, 99, 235, .38), 0 8px 20px rgba(0, 0, 0, .32); }
.asset-bus > svg { width: 20px; color: #fbbf24; }
.asset-bus span { min-width: 0; display: grid; gap: 2px; }
.asset-bus b { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.asset-bus small { color: #aeb8c4; font-size: 9px; }
:global(.vue-flow__handle.asset-bus-port) { width: 11px; height: 11px; box-sizing: border-box; background: #222831; border: 2px solid #fbbf24; }
</style>
