import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import { makeVideoWorkflowNode } from '@/utils/videoWorkflowGraph'
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
    const video = wrapper.find('.node-media video')
    expect(video.attributes('src')).toBe('/generated.mp4')
    expect(video.attributes('poster')).toBe('/poster.jpg')
    expect(video.attributes('preload')).toBe('none')
    expect(wrapper.find('.node-preview-button').classes()).toContain('video')
    await wrapper.find('.node-preview-button').trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([node])
  })

  it('keeps metadata preload when video has no poster', () => {
    const node: VideoWorkflowNode = {
      id: 'video_2',
      type: 'video',
      title: 'S02 视频',
      position: { x: 0, y: 0 },
      config: {},
      output: { preview_url: '/generated.mp4' },
    }
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node } })
    const video = wrapper.find('.node-media video')
    expect(video.attributes('src')).toBe('/generated.mp4')
    expect(video.attributes('poster')).toBeUndefined()
    expect(video.attributes('preload')).toBe('metadata')
  })

  it('renders character passport thumbnails from run output', async () => {
    const node: VideoWorkflowNode = {
      id: 'role_heroine',
      type: 'character',
      title: '女主',
      position: { x: 0, y: 0 },
      status: 'succeeded',
      config: { name: '女主' },
      output: {
        selected_version_id: 'vwv_heroine',
        output_url: '/p/vwf/vwv_heroine?purpose=preview',
        candidates: [{ id: 'vwv_heroine', preview_url: '/p/vwf/vwv_heroine?purpose=preview' }],
      },
    }
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node } })
    const img = wrapper.find('.node-media img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('/p/vwf/vwv_heroine')
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

  it('keeps connection handles above the node body', () => {
    const node = makeVideoWorkflowNode('character')
    const wrapper = mount(VideoWorkflowNodeCard, {
      props: { node },
      global: { stubs: { Handle: true } },
    })
    expect(wrapper.find('.workflow-node-card').classes()).toContain('character')
    expect(wrapper.findAll('.node-port')).toHaveLength(2)
  })

  it('colors ports by type and shows catalog accent plus running progress', () => {
    const node = makeVideoWorkflowNode('character')
    node.status = 'running'
    node.progress = 42
    const wrapper = mount(VideoWorkflowNodeCard, {
      props: { node },
      global: { stubs: { Handle: true } },
    })
    expect(wrapper.find('.workflow-node-card').attributes('style')).toContain('--node-catalog-color: #fbbf24')
    expect(wrapper.find('.node-port.input').classes()).toContain('port-type-text')
    expect(wrapper.find('.node-port.output').classes()).toContain('port-type-image')
    expect(wrapper.find('.node-progress').attributes('style')).toContain('width: 42%')
    expect(wrapper.find('footer b').text()).toBe('42%')
  })

  it('summarizes timeline clip count from clips before falling back to clip_node_ids', () => {
    const withClips = makeVideoWorkflowNode('timeline')
    withClips.config = {
      title: '顺序时间线',
      clips: [
        { id: 'clip_1', source_node_id: 'video_1', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15_000 },
        { id: 'clip_2', source_node_id: 'video_2', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15_000 },
      ],
      clip_node_ids: ['video_1'],
    }
    expect(mount(VideoWorkflowNodeCard, {
      props: { node: withClips },
      global: { stubs: { Handle: true } },
    }).find('p').text()).toBe('2 个片段 · 单轨')

    const legacyOnly = makeVideoWorkflowNode('timeline')
    legacyOnly.config = { title: '顺序时间线', clip_node_ids: ['video_1', 'video_2', 'video_3'] }
    expect(mount(VideoWorkflowNodeCard, {
      props: { node: legacyOnly },
      global: { stubs: { Handle: true } },
    }).find('p').text()).toBe('3 个片段 · 单轨')

    const emptyClips = makeVideoWorkflowNode('timeline')
    emptyClips.config = { title: '顺序时间线', clips: [], clip_node_ids: ['video_1', 'video_2'] }
    expect(mount(VideoWorkflowNodeCard, {
      props: { node: emptyClips },
      global: { stubs: { Handle: true } },
    }).find('p').text()).toBe('0 个片段 · 单轨')
  })

  it('exposes translated run errors on the card title', () => {
    const failed = makeVideoWorkflowNode('video')
    failed.status = 'failed'
    failed.run_error_code = 'runtime_failed'
    failed.run_error = '上游超时'
    const wrapper = mount(VideoWorkflowNodeCard, {
      props: { node: failed },
      global: { stubs: { Handle: true } },
    })
    expect(wrapper.find('article').attributes('title')).toBe('工作流运行失败：上游超时')
  })
})
