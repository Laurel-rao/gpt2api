import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowRun } from '@/api/videoWorkflow'
import VideoWorkflowRunHistory from './VideoWorkflowRunHistory.vue'

const runs: VideoWorkflowRun[] = [
  {
    id: 'run-success', workflow_id: 'workflow', workflow_revision: 12, status: 'succeeded', progress: 100,
    run_mode: 'full', output_version_id: 'version-final', actual_credits: 1280,
    created_at: '2026-07-11T11:03:00Z', started_at: '2026-07-11T11:03:05Z', finished_at: '2026-07-11T11:05:18Z',
  },
  {
    id: 'run-failed', workflow_id: 'workflow', workflow_revision: 11, status: 'failed', progress: 61,
    run_mode: 'downstream', error_message: 'S02 视频生成失败', created_at: '2026-07-11T10:40:00Z',
  },
]

const drawerStub = {
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<aside><slot name="header"/><slot/></aside>',
}

describe('VideoWorkflowRunHistory', () => {
  it('renders run summaries and emits explicit history actions', async () => {
    const wrapper = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs, total: 2 },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(wrapper.text()).toContain('生成历史')
    expect(wrapper.text()).toContain('完整成片')
    expect(wrapper.text()).toContain('当前及下游')
    expect(wrapper.text()).toContain('S02 视频生成失败')

    const firstActions = wrapper.findAll('.history-list article')[0].findAll('footer button')
    await firstActions[0].trigger('click')
    await firstActions[1].trigger('click')
    await firstActions[2].trigger('click')
    expect(wrapper.emitted('inspect')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
    expect(wrapper.emitted('preview')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
    expect(wrapper.emitted('download')?.[0]?.[0]).toMatchObject({ id: 'run-success' })

    const failedActions = wrapper.findAll('.history-list article')[1].findAll('footer button')
    expect(failedActions[1].attributes('disabled')).toBeDefined()
    expect(failedActions[2].attributes('disabled')).toBeDefined()
  })

  it('shows actionable empty state and node details without replacing the list source', async () => {
    const empty = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs: [], total: 0 },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(empty.text()).toContain('暂无生成记录')
    await empty.find('.history-empty button').trigger('click')
    expect(empty.emitted('generate')).toHaveLength(1)

    const detail: VideoWorkflowRun = {
      ...runs[1],
      node_runs: [{ id: 'node-run', node_id: 'video_2', node_type: 'video', status: 'failed', progress: 42, error_message: '上游服务暂时不可用' }],
    }
    const detailed = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs, total: 2, detail },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(detailed.text()).toContain('节点进度')
    expect(detailed.text()).toContain('video_2')
    expect(detailed.text()).toContain('上游服务暂时不可用')
    await detailed.find('.back-history').trigger('click')
    expect(detailed.emitted('back')).toHaveLength(1)
  })
})
