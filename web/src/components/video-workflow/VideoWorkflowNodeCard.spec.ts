import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowNodeCard from './VideoWorkflowNodeCard.vue'

function imageNode(): VideoWorkflowNode {
  return {
    id: 'background_2',
    type: 'background',
    title: 'S02 图片',
    position: { x: 0, y: 0 },
    config: {
      preview_url: '/p/vwf/transformed-version',
      image_transform: {
        crop: { x: .1, y: .1, width: .8, height: .8 },
        rotation: 90,
        flip_horizontal: false,
        flip_vertical: true,
      },
    },
  }
}

describe('VideoWorkflowNodeCard image preview', () => {
  it('does not transform an image version already baked by the server', () => {
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node: imageNode() } })
    expect(wrapper.find('.node-media img').attributes('style')).toBeUndefined()
  })

  it('opens an accessible full-screen preview without bubbling canvas gestures', async () => {
    const node = imageNode()
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node } })
    const button = wrapper.find('.node-preview-button')
    let pointerdowns = 0
    let clicks = 0
    wrapper.element.addEventListener('pointerdown', () => { pointerdowns += 1 })
    wrapper.element.addEventListener('click', () => { clicks += 1 })

    expect(button.classes()).toEqual(expect.arrayContaining(['nodrag', 'nopan', 'nowheel']))
    expect(button.attributes('aria-label')).toBe('全屏预览：S02 图片')
    await button.trigger('pointerdown')
    await button.trigger('click')

    expect(pointerdowns).toBe(0)
    expect(clicks).toBe(0)
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([node])
  })

  it('uses generated video output as the playable preview source', async () => {
    const node: VideoWorkflowNode = {
      id: 'video_1',
      type: 'video',
      title: 'S01 视频',
      position: { x: 0, y: 0 },
      config: { preview_url: '/poster.jpg' },
      output: { preview_url: '/generated.mp4' },
    }
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node } })
    expect(wrapper.find('.node-media video').attributes('src')).toBe('/generated.mp4')
    expect(wrapper.find('.node-preview-button').classes()).toContain('video')
    await wrapper.find('.node-preview-button').trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([node])
  })

  it('can request a freshly signed preview when the thumbnail URL is missing', async () => {
    const node = imageNode()
    delete node.config.preview_url
    node.asset_id = 'asset_1'
    node.asset_version_id = 'version_1'
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node } })

    expect(wrapper.find('.media-placeholder').exists()).toBe(true)
    await wrapper.find('.node-preview-button').trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([node])
  })

  it('collapses character inputs into one shared asset handle in smart mode', () => {
    const node: VideoWorkflowNode = {
      id: 'video_shared',
      type: 'video',
      title: '共享角色视频',
      position: { x: 0, y: 0 },
      config: {},
      inputs: [
        { id: 'background', type: 'image', label: '背景' },
        { id: 'character_1', type: 'image', label: '角色一' },
        { id: 'character_2', type: 'image', label: '角色二' },
      ],
    }
    const wrapper = mount(VideoWorkflowNodeCard, {
      props: { node, collapsedInputPortIDs: ['character_1', 'character_2'] },
      global: { stubs: { Handle: true } },
    })

    const inputs = wrapper.findAll('.node-port.input')
    expect(inputs).toHaveLength(2)
    expect(inputs[0].attributes('title')).toContain('背景')
    expect(inputs[1].classes()).toContain('shared-character-port')
    expect(inputs[1].attributes('title')).toBe('公共角色资产 · 2 个角色')
  })
})
