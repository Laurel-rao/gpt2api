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
})
