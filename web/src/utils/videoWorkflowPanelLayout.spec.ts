import { describe, expect, it } from 'vitest'
import {
  applyLayoutPreset,
  isPanelFocusMode,
  togglePanelFocusMode,
  VIDEO_WORKFLOW_LAYOUT_PRESETS,
} from './videoWorkflowPanelLayout'

describe('videoWorkflowPanelLayout', () => {
  it('toggles focus mode and restores the previous panel visibility', () => {
    const enter = togglePanelFocusMode(
      { libraryOpen: true, inspectorOpen: false },
      null,
    )
    expect(enter.next).toEqual({ libraryOpen: false, inspectorOpen: false })
    expect(enter.lastNonFocus).toEqual({ libraryOpen: true, inspectorOpen: false })
    expect(isPanelFocusMode(enter.next)).toBe(true)

    const leave = togglePanelFocusMode(enter.next, enter.lastNonFocus)
    expect(leave.next).toEqual({ libraryOpen: true, inspectorOpen: false })
    expect(isPanelFocusMode(leave.next)).toBe(false)
  })

  it('restores both panels when focus memory is empty', () => {
    const leave = togglePanelFocusMode(
      { libraryOpen: false, inspectorOpen: false },
      null,
    )
    expect(leave.next).toEqual({ libraryOpen: true, inspectorOpen: true })
  })

  it('applies the three workspace layout presets', () => {
    const base = { left: 300, right: 400, libraryOpen: true, inspectorOpen: false }
    expect(applyLayoutPreset(base, 'edit')).toEqual(VIDEO_WORKFLOW_LAYOUT_PRESETS.edit)
    expect(applyLayoutPreset(base, 'compose')).toEqual({
      left: 300,
      right: 280,
      libraryOpen: false,
      inspectorOpen: true,
    })
    expect(applyLayoutPreset(base, 'review')).toEqual({
      left: 300,
      right: 360,
      libraryOpen: false,
      inspectorOpen: true,
    })
  })
})
