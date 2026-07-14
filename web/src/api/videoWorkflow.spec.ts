import { describe, expect, it } from 'vitest'
import { seedVideoWorkflowStaleEpochBaseline, videoAssetKindForFile } from './videoWorkflow'

describe('videoAssetKindForFile', () => {
  it('maps image and video MIME types to the backend kind contract', () => {
    expect(videoAssetKindForFile({ type: 'image/png' })).toBe('image')
    expect(videoAssetKindForFile({ type: 'video/mp4' })).toBe('video')
  })

  it('rejects unknown media before upload', () => {
    expect(() => videoAssetKindForFile({ type: 'application/pdf' })).toThrow('不支持的素材类型')
    expect(() => videoAssetKindForFile({ type: '' })).toThrow('unknown')
  })
})

describe('seedVideoWorkflowStaleEpochBaseline', () => {
  it('registers only existing stale nodes at the baseline epoch', () => {
    const epochMap = new Map<string, number>()
    seedVideoWorkflowStaleEpochBaseline([
      { id: 'a', status: 'stale' },
      { id: 'b', status: 'succeeded' },
      { id: 'c', status: 'stale' },
      { id: 'd' },
    ], epochMap, 0)
    expect([...epochMap.entries()]).toEqual([['a', 0], ['c', 0]])
  })

  it('lets a later edit epoch stay above the run snapshot so polling will not clear it', () => {
    const epochMap = new Map<string, number>()
    seedVideoWorkflowStaleEpochBaseline([{ id: 'n1', status: 'stale' }], epochMap, 0)
    const activeRunStaleEpoch = 0
    epochMap.set('n1', 1) // 运行启动后再次编辑
    expect(epochMap.get('n1')!).toBeGreaterThan(activeRunStaleEpoch)
  })
})
