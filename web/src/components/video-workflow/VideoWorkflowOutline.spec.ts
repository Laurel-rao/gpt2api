import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowOutline from './VideoWorkflowOutline.vue'

const nodes: VideoWorkflowNode[] = [{
  id: 'video_1',
  type: 'video',
  position: { x: 0, y: 0 },
  config: {},
  status: 'stale',
}]

describe('VideoWorkflowOutline', () => {
  it('renders graph summary and closes after selecting a node', async () => {
    const wrapper = mount(VideoWorkflowOutline, {
      props: { modelValue: true, nodes, edgeCount: 3, clipCount: 1 },
      global: {
        stubs: {
          ElDrawer: {
            props: ['modelValue'],
            emits: ['update:modelValue'],
            template: '<aside><slot /></aside>',
          },
        },
      },
    })
    expect(wrapper.find('.outline-summary').text()).toContain('1 节点 · 3 连线 · 1 片段')
    expect(wrapper.find('.outline-node').text()).toContain('场景视频')
    expect(wrapper.find('.outline-node').text()).toContain('需更新')

    await wrapper.find('.outline-node').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['video_1'])
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([false])
  })

  it('maps run statuses to Chinese labels instead of raw or empty values', () => {
    const wrapper = mount(VideoWorkflowOutline, {
      props: {
        modelValue: true,
        nodes: [
          { ...nodes[0], id: 'ready', status: 'succeeded' },
          { ...nodes[0], id: 'idle', status: undefined },
          { ...nodes[0], id: 'compose', type: 'compose', status: 'idle' },
        ],
        edgeCount: 0,
        clipCount: 0,
      },
      global: {
        stubs: {
          ElDrawer: {
            props: ['modelValue'],
            emits: ['update:modelValue'],
            template: '<aside><slot /></aside>',
          },
        },
      },
    })
    const texts = wrapper.findAll('.outline-node').map((item) => item.text())
    expect(texts[0]).toContain('已就绪')
    expect(texts[1]).toContain('待生成')
    expect(texts[2]).toContain('待生成')
  })
})
