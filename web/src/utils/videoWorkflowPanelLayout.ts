/** 视频工作流左右栏布局：Focus 切换、三档预设与宽度钳制。 */

export type VideoWorkflowPanelLayout = {
  left: number
  right: number
  libraryOpen: boolean
  inspectorOpen: boolean
}

export type VideoWorkflowPanelVisibility = Pick<VideoWorkflowPanelLayout, 'libraryOpen' | 'inspectorOpen'>

export type VideoWorkflowLayoutPresetID = 'edit' | 'compose' | 'review'

export const VIDEO_WORKFLOW_PANEL_LEFT_MIN = 208
export const VIDEO_WORKFLOW_PANEL_LEFT_MAX = 340
export const VIDEO_WORKFLOW_PANEL_RIGHT_MIN = 280
export const VIDEO_WORKFLOW_PANEL_RIGHT_MAX = 420

export const VIDEO_WORKFLOW_LAYOUT_PRESETS: Record<VideoWorkflowLayoutPresetID, Partial<VideoWorkflowPanelLayout> & VideoWorkflowPanelVisibility> = {
  edit: { left: 248, right: 320, libraryOpen: true, inspectorOpen: true },
  compose: { libraryOpen: false, inspectorOpen: true, right: 280 },
  review: { libraryOpen: false, inspectorOpen: true, right: 360 },
}

export function clampPanelLeft(width: number) {
  return Math.max(VIDEO_WORKFLOW_PANEL_LEFT_MIN, Math.min(VIDEO_WORKFLOW_PANEL_LEFT_MAX, width))
}

export function clampPanelRight(width: number) {
  return Math.max(VIDEO_WORKFLOW_PANEL_RIGHT_MIN, Math.min(VIDEO_WORKFLOW_PANEL_RIGHT_MAX, width))
}

export function isPanelFocusMode(layout: VideoWorkflowPanelVisibility) {
  return !layout.libraryOpen && !layout.inspectorOpen
}

/** 任一栏打开时进入 Focus（记下开关）；两栏已关时恢复上次非 Focus 状态。 */
export function togglePanelFocusMode(
  layout: VideoWorkflowPanelVisibility,
  lastNonFocus: VideoWorkflowPanelVisibility | null,
): { next: VideoWorkflowPanelVisibility; lastNonFocus: VideoWorkflowPanelVisibility | null } {
  if (layout.libraryOpen || layout.inspectorOpen) {
    return {
      next: { libraryOpen: false, inspectorOpen: false },
      lastNonFocus: { libraryOpen: layout.libraryOpen, inspectorOpen: layout.inspectorOpen },
    }
  }
  const restore = lastNonFocus || { libraryOpen: true, inspectorOpen: true }
  return { next: { ...restore }, lastNonFocus }
}

export function applyLayoutPreset(
  current: VideoWorkflowPanelLayout,
  presetID: VideoWorkflowLayoutPresetID,
): VideoWorkflowPanelLayout {
  const preset = VIDEO_WORKFLOW_LAYOUT_PRESETS[presetID]
  return {
    left: preset.left !== undefined ? clampPanelLeft(preset.left) : current.left,
    right: preset.right !== undefined ? clampPanelRight(preset.right) : current.right,
    libraryOpen: preset.libraryOpen,
    inspectorOpen: preset.inspectorOpen,
  }
}
