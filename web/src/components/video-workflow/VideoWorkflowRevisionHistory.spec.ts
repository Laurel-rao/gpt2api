import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowRevisionListItem } from '@/api/videoWorkflow'
import VideoWorkflowRevisionHistory from './VideoWorkflowRevisionHistory.vue'

const items: VideoWorkflowRevisionListItem[] = [
  {
    workflow_id: 'workflow', revision: 28, name: '古风成片', node_count: 12, edge_count: 9,
    created_at: '2026-07-14T06:00:00Z', is_current: true,
  },
  {
    workflow_id: 'workflow', revision: 27, name: '古风成片', node_count: 11, edge_count: 8,
    created_at: '2026-07-14T05:40:00Z',
  },
]

const drawerStub = {
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<aside><slot name="header"/><slot/></aside>',
}

describe('VideoWorkflowRevisionHistory', () => {
  it('renders revision list and emits switch', async () => {
    const wrapper = mount(VideoWorkflowRevisionHistory, {
      props: {
        modelValue: true,
        items,
        total: 2,
        currentRevision: 28,
        viewingRevision: 28,
      },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(wrapper.text()).toContain('版本历史')
    expect(wrapper.text()).toContain('R28')
    expect(wrapper.text()).toContain('服务器当前')
    expect(wrapper.text()).toContain('画布查看中')
    expect(wrapper.text()).toContain('当前查看')

    const older = wrapper.findAll('.history-list article')[1]
    await older.find('footer button').trigger('click')
    expect(wrapper.emitted('switch')?.[0]?.[0]).toMatchObject({ revision: 27 })
  })
})
