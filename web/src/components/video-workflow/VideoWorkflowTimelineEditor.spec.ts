import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowTimelineClip } from '@/api/videoWorkflow'
import VideoWorkflowTimelineEditor from './VideoWorkflowTimelineEditor.vue'

function clip(
  id: string,
  sourceNodeID: string,
  trimIn = 0,
  trimOut = 15_000,
): VideoWorkflowTimelineClip {
  return {
    id,
    source_node_id: sourceNodeID,
    source_port: 'video',
    trim_in_ms: trimIn,
    trim_out_ms: trimOut,
  }
}

function mountEditor(clips: VideoWorkflowTimelineClip[], sourceTitles?: Record<string, string>) {
  return mount(VideoWorkflowTimelineEditor, {
    props: {
      clips,
      sourceTitles: sourceTitles || {
        video_1: 'S01 视频',
        video_2: 'S02 视频',
        video_3: 'S03 视频',
      },
    },
  })
}

function latestClips(wrapper: ReturnType<typeof mountEditor>): VideoWorkflowTimelineClip[] {
  const events = wrapper.emitted('update:clips')
  expect(events?.length).toBeGreaterThan(0)
  return events![events!.length - 1][0] as VideoWorkflowTimelineClip[]
}

describe('VideoWorkflowTimelineEditor', () => {
  it('renders source titles and total duration', () => {
    const wrapper = mountEditor([
      clip('clip_1', 'video_1', 0, 5_000),
      clip('clip_2', 'video_2', 1_000, 6_000),
    ])
    expect(wrapper.text()).toContain('S01 视频')
    expect(wrapper.text()).toContain('S02 视频')
    expect(wrapper.text()).toContain('2 段 · 合计 10.0 秒')
  })

  it('moves clips up and down via update:clips without mutating props', async () => {
    const clips = [
      clip('clip_1', 'video_1'),
      clip('clip_2', 'video_2'),
      clip('clip_3', 'video_3'),
    ]
    const snapshot = structuredClone(clips)
    const wrapper = mountEditor(clips)

    await wrapper.get('[aria-label="下移片段 1"]').trigger('click')
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2', 'clip_1', 'clip_3'])
    expect(clips).toEqual(snapshot)

    await wrapper.setProps({ clips: latestClips(wrapper) })
    await wrapper.get('[aria-label="上移片段 3"]').trigger('click')
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2', 'clip_3', 'clip_1'])
  })

  it('disables boundary move actions', () => {
    const wrapper = mountEditor([
      clip('clip_1', 'video_1'),
      clip('clip_2', 'video_2'),
    ])
    expect(wrapper.get('[aria-label="上移片段 1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[aria-label="下移片段 2"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[aria-label="下移片段 1"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[aria-label="上移片段 2"]').attributes('disabled')).toBeUndefined()
  })

  it('snaps trim edits to 0.1s and enforces the 1s minimum duration', async () => {
    const clips = [clip('clip_1', 'video_1', 0, 15_000), clip('clip_2', 'video_2')]
    const wrapper = mountEditor(clips)

    await wrapper.get('[aria-label="片段 1 入点"]').setValue('1.049')
    await wrapper.get('[aria-label="片段 1 入点"]').trigger('change')
    expect(latestClips(wrapper)[0]).toMatchObject({ trim_in_ms: 1_000, trim_out_ms: 15_000 })
    expect(clips[0]).toMatchObject({ trim_in_ms: 0, trim_out_ms: 15_000 })

    await wrapper.setProps({ clips: latestClips(wrapper) })
    await wrapper.get('[aria-label="片段 1 出点"]').setValue('1.89')
    await wrapper.get('[aria-label="片段 1 出点"]').trigger('change')
    expect(latestClips(wrapper)[0]).toMatchObject({ trim_in_ms: 1_000, trim_out_ms: 2_000 })
  })

  it('deletes a clip when more than one remains', async () => {
    const clips = [clip('clip_1', 'video_1'), clip('clip_2', 'video_2')]
    const snapshot = structuredClone(clips)
    const wrapper = mountEditor(clips)

    await wrapper.get('[aria-label="删除片段 1"]').trigger('click')
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2'])
    expect(clips).toEqual(snapshot)
  })

  it('blocks deleting the last clip and exposes an accessible hint', async () => {
    const clips = [clip('clip_1', 'video_1')]
    const wrapper = mountEditor(clips)
    const deleteButton = wrapper.get('[aria-label="删除片段 1"]')

    expect(deleteButton.attributes('disabled')).toBeDefined()
    expect(deleteButton.attributes('aria-describedby')).toBe('timeline-last-clip-hint')
    expect(wrapper.get('#timeline-last-clip-hint').attributes('role')).toBe('status')
    expect(wrapper.get('#timeline-last-clip-hint').text()).toContain('无法删除最后一个片段')

    await deleteButton.trigger('click')
    expect(wrapper.emitted('update:clips')).toBeUndefined()
  })

  it('supports keyboard activation on reorder and delete buttons', async () => {
    const clips = [clip('clip_1', 'video_1'), clip('clip_2', 'video_2')]
    const wrapper = mount(VideoWorkflowTimelineEditor, {
      props: {
        clips,
        sourceTitles: { video_1: 'S01 视频', video_2: 'S02 视频' },
      },
      attachTo: document.body,
    })
    const down = wrapper.get('[aria-label="下移片段 1"]')

    ;(down.element as HTMLElement).focus()
    expect(document.activeElement).toBe(down.element)
    await down.trigger('keydown.enter')
    await down.trigger('click')
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2', 'clip_1'])

    await wrapper.setProps({ clips: latestClips(wrapper) })
    const del = wrapper.get('[aria-label="删除片段 2"]')
    ;(del.element as HTMLElement).focus()
    expect(document.activeElement).toBe(del.element)
    await del.trigger('keydown.space')
    await del.trigger('click')
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2'])
    wrapper.unmount()
  })

  it('reorders clips through drag and drop', async () => {
    const clips = [
      clip('clip_1', 'video_1'),
      clip('clip_2', 'video_2'),
      clip('clip_3', 'video_3'),
    ]
    const wrapper = mountEditor(clips)
    const items = wrapper.findAll('.timeline-clip')
    const dataTransfer = {
      data: '' as string,
      effectAllowed: '',
      dropEffect: '',
      setData(type: string, value: string) {
        if (type === 'text/plain') this.data = value
      },
      getData(type: string) {
        return type === 'text/plain' ? this.data : ''
      },
    }

    await items[0].trigger('dragstart', { dataTransfer })
    await items[2].trigger('dragover', { dataTransfer })
    await items[2].trigger('drop', { dataTransfer })
    expect(latestClips(wrapper).map((item) => item.id)).toEqual(['clip_2', 'clip_3', 'clip_1'])
  })
})
