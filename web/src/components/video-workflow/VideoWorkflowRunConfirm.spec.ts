import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import VideoWorkflowRunConfirm from './VideoWorkflowRunConfirm.vue'

const { confirmMock } = vi.hoisted(() => ({ confirmMock: vi.fn() }))

vi.mock('element-plus/es/components/message-box/index.mjs', () => ({
  ElMessageBox: { confirm: confirmMock },
}))

describe('VideoWorkflowRunConfirm', () => {
  beforeEach(() => {
    confirmMock.mockReset().mockResolvedValue('confirm')
  })

  it('preserves estimate copy and run-mode title', async () => {
    const wrapper = mount(VideoWorkflowRunConfirm)
    await (wrapper.vm as any).open('downstream', { token: 'estimate-token', total_credits: 42, cached_credits: 8 })
    expect(confirmMock).toHaveBeenCalledWith(
      '预计消耗 42 积分，缓存节省 8 积分。确认后开始生成。',
      '运行当前及下游',
      { confirmButtonText: '确认运行', cancelButtonText: '取消', type: 'warning' },
    )
    await (wrapper.vm as any).open('upstream', { token: 'estimate-token-2', total_credits: 10 })
    expect(confirmMock).toHaveBeenLastCalledWith(
      '预计消耗 10 积分。确认后开始生成。',
      '运行当前节点并重跑上游',
      { confirmButtonText: '确认运行', cancelButtonText: '取消', type: 'warning' },
    )
  })
})
