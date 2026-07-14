import type { VideoWorkflowNode } from '@/api/videoWorkflow'

export type InspectorTab = 'upstream' | 'status' | 'history' | 'output' | 'settings'

/** 按节点类型/失败态选择默认 Tab；换节点时应用，同节点内用户手点后不强制改回。 */
export function resolveDefaultInspectorTab(
  node: Pick<VideoWorkflowNode, 'type' | 'status' | 'run_error'> | null | undefined,
): InspectorTab {
  if (!node) return 'settings'
  if (node.status === 'failed' || node.run_error) return 'status'
  if (['character', 'background', 'image', 'video'].includes(node.type)) return 'output'
  if (node.type === 'timeline') return 'settings'
  return 'settings'
}
