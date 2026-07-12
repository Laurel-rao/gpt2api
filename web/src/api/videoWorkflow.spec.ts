import { describe, expect, it } from 'vitest'
import { videoAssetKindForFile } from './videoWorkflow'

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
