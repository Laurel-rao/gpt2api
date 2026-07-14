import { describe, expect, it } from 'vitest'
import { formatErrorCode } from './format'

describe('formatErrorCode', () => {
  it('translates video workflow and billing codes', () => {
    expect(formatErrorCode('insufficient_balance')).toBe('积分不足')
    expect(formatErrorCode('insufficient')).toBe('积分不足')
    expect(formatErrorCode('insufficient_quota')).toBe('配额不足')
    expect(formatErrorCode('invalid_graph')).toBe('画布图校验未通过')
    expect(formatErrorCode('videoworkflow: invalid graph')).toBe('画布图校验未通过')
    expect(formatErrorCode('revision_conflict')).toBe('修订冲突，请刷新后重试')
    expect(formatErrorCode('videoworkflow: revision conflict')).toBe('修订冲突，请刷新后重试')
    expect(formatErrorCode('runtime_failed')).toBe('工作流运行失败')
  })

  it('returns the original code when unmapped', () => {
    expect(formatErrorCode('unknown_provider_xyz')).toBe('unknown_provider_xyz')
    expect(formatErrorCode('')).toBe('')
    expect(formatErrorCode(null)).toBe('')
    expect(formatErrorCode(undefined)).toBe('')
  })
})
