import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import VideoWorkflowValidationIssues from './VideoWorkflowValidationIssues.vue'

describe('VideoWorkflowValidationIssues', () => {
  it('lists issues and emits select on click', async () => {
    const issues = [
      { code: 'missing_input', message: '缺少上游输入', node_id: 'video_1' },
      { code: 'invalid_edge', message: '连线类型不匹配', edge_id: 'edge_1' },
    ]
    const wrapper = mount(VideoWorkflowValidationIssues, {
      props: { modelValue: true, issues },
      global: {
        stubs: {
          'el-drawer': {
            props: ['modelValue'],
            template: '<div class="drawer"><slot name="header" /><slot /></div>',
          },
        },
      },
    })
    expect(wrapper.text()).toContain('共 2 项')
    expect(wrapper.text()).toContain('缺少上游输入')
    expect(wrapper.text()).toContain('节点 video_1')
    await wrapper.findAll('.issues-list button')[0].trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([issues[0]])
  })
})
