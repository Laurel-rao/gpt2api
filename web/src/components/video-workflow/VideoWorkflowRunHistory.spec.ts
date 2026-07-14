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
    expect(wrapper.text()).toContain('展开全部节点')

    const firstActions = wrapper.findAll('.history-list article')[0].findAll('footer button')
    await firstActions[0].trigger('click')
    await firstActions[1].trigger('click')
    await firstActions[2].trigger('click')
    await firstActions[3].trigger('click')
    expect(wrapper.emitted('switch')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
    expect(wrapper.emitted('inspect')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
    expect(wrapper.emitted('preview')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
    expect(wrapper.emitted('download')?.[0]?.[0]).toMatchObject({ id: 'run-success' })

    const failedActions = wrapper.findAll('.history-list article')[1].findAll('footer button')
    expect(failedActions[2].attributes('disabled')).toBeDefined()
    expect(failedActions[3].attributes('disabled')).toBeDefined()
  })

  it('marks the current run and keeps switch action available', async () => {
    const wrapper = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs, total: 2, currentRunID: 'run-success' },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    const current = wrapper.findAll('.history-list article')[0]
    expect(current.classes()).toContain('current')
    expect(current.text()).toContain('当前')
    expect(current.text()).toContain('当前查看')
    await current.findAll('footer button')[0].trigger('click')
    expect(wrapper.emitted('switch')?.[0]?.[0]).toMatchObject({ id: 'run-success' })
  })

  it('shows every node run status in the list card and emits expand when missing', async () => {
    const withNodes: VideoWorkflowRun = {
      ...runs[0],
      node_runs: [
        { id: 'nr-1', node_id: 'script_1', node_type: 'script', status: 'succeeded', progress: 100 },
        { id: 'nr-2', node_id: 'video_2', node_type: 'video', status: 'failed', progress: 42, error_message: '上游服务暂时不可用' },
        { id: 'nr-3', node_id: 'compose_1', node_type: 'compose', status: 'queued', progress: 0 },
      ],
    }
    const wrapper = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs: [withNodes, runs[1]], total: 2 },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    const card = wrapper.findAll('.history-list article')[0]
    expect(card.text()).toContain('节点进度')
    expect(card.text()).toContain('1/3 完成')
    expect(card.text()).toContain('分镜剧本 · script_1')
    expect(card.text()).toContain('已就绪')
    expect(card.text()).toContain('场景视频 · video_2')
    expect(card.text()).toContain('失败')
    expect(card.text()).toContain('上游服务暂时不可用')
    expect(card.text()).toContain('成片输出 · compose_1')
    expect(card.text()).toContain('排队中')
    expect(card.text()).toContain('42%')

    await wrapper.findAll('.load-nodes')[0].trigger('click')
    expect(wrapper.emitted('expand')?.[0]?.[0]).toMatchObject({ id: 'run-failed' })
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
      node_runs: [
        { id: 'node-run-a', node_id: 'script_1', node_type: 'script', status: 'succeeded', progress: 100 },
        { id: 'node-run', node_id: 'video_2', node_type: 'video', status: 'failed', progress: 42, error_message: '上游服务暂时不可用' },
      ],
    }
    const detailed = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs, total: 2, detail },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(detailed.text()).toContain('节点进度')
    expect(detailed.text()).toContain('1/2 完成')
    expect(detailed.text()).toContain('分镜剧本 · script_1')
    expect(detailed.text()).toContain('已就绪')
    expect(detailed.text()).toContain('场景视频 · video_2')
    expect(detailed.text()).toContain('上游服务暂时不可用')
    await detailed.find('.back-history').trigger('click')
    expect(detailed.emitted('back')).toHaveLength(1)
  })

  it('translates run error codes in history list and detail', () => {
    const failed: VideoWorkflowRun = {
      ...runs[1],
      error_code: 'runtime_failed',
      error_message: '上游超时',
    }
    const wrapper = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs: [failed], total: 1 },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(wrapper.find('.history-error').text()).toContain('工作流运行失败')
    expect(wrapper.find('.history-error').text()).toContain('上游超时')
    expect(wrapper.find('.history-error').attributes('title')).toBe('runtime_failed')

    const detailed = mount(VideoWorkflowRunHistory, {
      props: { modelValue: true, runs: [failed], total: 1, detail: failed },
      global: { stubs: { ElDrawer: drawerStub } },
    })
    expect(detailed.find('.run-error b').text()).toContain('工作流运行失败')
    expect(detailed.find('.run-error b').text()).toContain('runtime_failed')
    expect(detailed.find('.run-error span').text()).toBe('上游超时')
  })
})
