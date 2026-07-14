import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoAsset } from '@/api/videoWorkflow'
import VideoWorkflowLibrary from './VideoWorkflowLibrary.vue'

const assets: VideoAsset[] = [{ id: 'asset_1', name: '雨夜图片', kind: 'image', current_version: 3 }]

describe('VideoWorkflowLibrary', () => {
  it('adds editable nodes while keeping system nodes fixed', async () => {
    const wrapper = mount(VideoWorkflowLibrary, { props: { tab: 'nodes', assets } })
    const nodes = wrapper.findAll('.library-node')
    await nodes[0].trigger('click')
    expect(wrapper.emitted('add-node')?.[0]).toEqual(['story_brief'])

    const timeline = nodes.find((item) => item.text().includes('顺序时间线'))!
    await timeline.trigger('click')
    expect(wrapper.emitted('add-node')).toHaveLength(1)
    expect(timeline.attributes('draggable')).toBe('false')
  })

  it('switches to assets and emits selected media', async () => {
    const wrapper = mount(VideoWorkflowLibrary, { props: { tab: 'assets', assets } })
    await wrapper.find('.asset-tile').trigger('click')
    expect(wrapper.emitted('select-asset')?.[0]?.[0]).toMatchObject({ id: 'asset_1' })
    await wrapper.findAll('.library-tabs button')[0].trigger('click')
    expect(wrapper.emitted('update:tab')?.[0]).toEqual(['nodes'])
  })

  it('emits close when collapsing the library panel', async () => {
    const wrapper = mount(VideoWorkflowLibrary, { props: { tab: 'nodes', assets } })
    await wrapper.find('button[aria-label="隐藏节点侧边栏"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('emits upload for library files without requiring a selected node', async () => {
    const wrapper = mount(VideoWorkflowLibrary, { props: { tab: 'assets', assets } })
    const input = wrapper.find('input[type="file"]')
    const file = new File(['fake'], 'clip.mp4', { type: 'video/mp4' })
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    expect(wrapper.emitted('upload')?.[0]?.[0]).toMatchObject({ name: 'clip.mp4', type: 'video/mp4' })
  })
})
