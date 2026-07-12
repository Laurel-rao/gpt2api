import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { VideoWorkflowNode, VideoWorkflowTimelineClip } from '@/api/videoWorkflow'
import VideoWorkflowTimeline from './VideoWorkflowTimeline.vue'

const clips: VideoWorkflowTimelineClip[] = [
  { id: 'clip_1', source_node_id: 'video_1', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15_000 },
  { id: 'clip_2', source_node_id: 'video_2', source_port: 'video', trim_in_ms: 1_200, trim_out_ms: 14_500 },
]
const nodes: VideoWorkflowNode[] = clips.map((clip, index) => ({
  id: clip.source_node_id,
  type: 'video',
  title: `场景0${index + 1}`,
  position: { x: 0, y: 0 },
  config: {},
}))

describe('VideoWorkflowTimeline', () => {
  it('renders trimmed duration and emits selection and removal', async () => {
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips, nodes, selectedClipID: 'clip_2' } })
    expect(wrapper.text()).toContain('28.3秒')
    expect(wrapper.findAll('.timeline-clip')).toHaveLength(2)
    expect(wrapper.findAll('.timeline-clip')[1].classes()).toContain('selected')

    await wrapper.findAll('.timeline-clip')[1].trigger('click')
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ id: 'clip_2' })
    await wrapper.findAll('.remove-clip')[0].trigger('click')
    expect(wrapper.emitted('remove')?.[0]).toEqual(['clip_1'])
  })

  it('supports keyboard ordering and 100ms numeric trim inputs', async () => {
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips, nodes } })
    await wrapper.findAll('.timeline-clip')[1].trigger('keydown', { key: 'ArrowLeft', altKey: true })
    expect(wrapper.emitted('move')?.[0]).toEqual([1, 0])

    const inPoint = wrapper.findAll('.trim-inputs input')[2]
    await inPoint.setValue('2300')
    await inPoint.trigger('change')
    expect(wrapper.emitted('trim')?.at(-1)).toEqual(['clip_2', 2300, 14_500])

    await inPoint.trigger('keydown', { key: 'Backspace' })
    await inPoint.trigger('keydown', { key: 'ArrowLeft' })
    await inPoint.trigger('keydown', { key: ' ' })
    expect(wrapper.emitted('remove')).toBeUndefined()
    expect(wrapper.find('.play-button').attributes('title')).toContain('播放')
  })

  it('exposes playback control and advances the playhead', async () => {
    vi.useFakeTimers()
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips, nodes } })
    ;(wrapper.vm as any).togglePlayback()
    await vi.advanceTimersByTimeAsync(300)
    expect(wrapper.find('.play-button').attributes('title')).toContain('暂停')
    vi.useRealTimers()
  })

  it('starts a short one-clip template from zero instead of an out-of-range playhead', async () => {
    vi.useFakeTimers()
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips: [clips[0]], nodes: [nodes[0]] } })
    expect(wrapper.find('.timecode').text()).toContain('00:00.0 / 00:15.0')
    ;(wrapper.vm as any).togglePlayback()
    await vi.advanceTimersByTimeAsync(100)
    expect(wrapper.find('.timecode').text()).toContain('00:00.1 / 00:15.0')
    vi.useRealTimers()
  })

  it('sets selected clip in/out points with bracket shortcuts', async () => {
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips, nodes, selectedClipID: 'clip_2' } })
    await wrapper.trigger('keydown', { key: '[' })
    await wrapper.trigger('keydown', { key: ']' })
    expect(wrapper.emitted('trim')?.[0]).toEqual(['clip_2', 6_500, 14_500])
    expect(wrapper.emitted('trim')?.[1]).toEqual(['clip_2', 1_200, 6_500])
  })

  it('emits close from the timeline toolbar', async () => {
    const wrapper = mount(VideoWorkflowTimeline, { props: { clips, nodes } })
    await wrapper.find('button[aria-label="关闭时间线"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
