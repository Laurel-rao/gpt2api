import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoAsset, VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowInspector from './VideoWorkflowInspector.vue'

const node: VideoWorkflowNode = {
  id: 'background_2',
  type: 'background',
  title: 'S02 图片',
  position: { x: 0, y: 0 },
  asset_id: 'asset_1',
  config: {
    prompt: '雨夜，城市街道，湿润反光',
    asset_id: 'asset_1',
    asset_version_id: 'version_2',
    image_transform: {
      crop: { x: 0, y: 0, width: 1, height: 1 },
      rotation: 0,
      flip_horizontal: false,
      flip_vertical: false,
    },
  },
  inputs: [{ id: 'scene', label: '场景描述', type: 'scene' }],
  outputs: [{ id: 'image', label: '场景图片', type: 'image' }],
  status: 'succeeded',
}
const assets: VideoAsset[] = [{
  id: 'asset_1',
  name: '雨夜场景',
  kind: 'image',
  current_version_id: 'version_2',
  versions: [
    { id: 'version_1', version: 1, mime: 'image/png', size_bytes: 10 },
    { id: 'version_2', version: 2, mime: 'image/png', size_bytes: 12 },
  ],
}]

describe('VideoWorkflowInspector', () => {
  it('shows immutable versions and emits version selection', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    const version = wrapper.find('#image-version')
    expect(version.findAll('option')).toHaveLength(2)
    await version.setValue('version_1')
    expect(wrapper.emitted('select-version')?.[0]).toEqual(['version_1'])
  })

  it('emits rotate, flip and crop transforms', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    const buttons = wrapper.findAll('.transform-grid button')
    await buttons[2].trigger('click')
    await buttons[3].trigger('click')
    expect(wrapper.emitted('transform')?.[0]).toEqual([{ rotation_delta: 90 }])
    expect(wrapper.emitted('transform')?.[1]).toEqual([{ flip_horizontal_toggle: true }])

    await buttons[0].trigger('click')
    const cropInputs = wrapper.findAll('.crop-editor input')
    await cropInputs[0].setValue('10')
    await cropInputs[2].setValue('80')
    await wrapper.find('.crop-editor button').trigger('click')
    expect(wrapper.emitted('transform')?.at(-1)?.[0]).toMatchObject({ crop: { x: .1, width: .8 } })
  })

  it('provides a keyboard-accessible connection action', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    await wrapper.find('.ports-section button').trigger('click')
    expect(wrapper.emitted('add-connection')?.[0]).toEqual(['scene'])
  })

  it('allows closing the empty inspector to reclaim workspace width', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node: null, assets } })
    expect(wrapper.text()).toContain('未选择节点')
    await wrapper.find('button[aria-label="关闭检查器"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('shows the Wan2.7 model label for video nodes', () => {
    const videoNode: VideoWorkflowNode = {
      id: 'video_1',
      type: 'video',
      title: 'S01 视频',
      position: { x: 0, y: 0 },
      config: { model: 'wan2.7-r2v' },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: videoNode, assets: [] } })
    expect(wrapper.find('.locked-section b').text()).toBe('Wan2.7-r2v')
  })

  it('never reapplies metadata transforms to a server-baked image version', () => {
    const transformedNode: VideoWorkflowNode = {
      ...node,
      config: {
        ...node.config,
        preview_url: '/p/vwf/version_2',
        image_transform: {
          crop: { x: .1, y: .1, width: .8, height: .8 },
          rotation: 90,
          flip_horizontal: true,
          flip_vertical: false,
        },
      },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: transformedNode, assets } })
    expect(wrapper.find('.image-preview img').attributes('style')).toBeUndefined()
  })

  it('keeps regeneration separate from running an already-bound image node', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    await wrapper.find('.action-grid button').trigger('click')
    expect(wrapper.emitted('regenerate')).toHaveLength(1)
    expect(wrapper.emitted('run')).toBeUndefined()
  })
})
