import { describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import {
  LatestVideoWorkflowOperation,
  SerialVideoWorkflowOperationQueue,
  runConfirmedVideoWorkflowMutation,
} from './videoWorkflowAsync'
import { clearVideoWorkflowImageAssetBinding, cloneWorkflowGraph, makeVideoWorkflowNode } from './videoWorkflowGraph'

function boundImageNode() {
  const node = makeVideoWorkflowNode('background')
  node.version = 1
  node.asset_id = 'asset-original'
  node.asset_version_id = 'version-original'
  node.config.asset_id = 'asset-original'
  node.config.asset_version_id = 'version-original'
  node.config.preview_url = '/preview-original'
  return node
}

describe('LatestVideoWorkflowOperation', () => {
  it('rejects a stale image-transform response after a newer operation starts', () => {
    const operations = new LatestVideoWorkflowOperation()
    const first = operations.begin('background_2')
    const second = operations.begin('background_2')
    expect(operations.isCurrent('background_2', first)).toBe(false)
    expect(operations.isCurrent('background_2', second)).toBe(true)
  })

  it('invalidates in-flight transforms when the bound asset changes', () => {
    const operations = new LatestVideoWorkflowOperation()
    const transform = operations.begin('background_2')
    operations.invalidate('background_2')
    expect(operations.isCurrent('background_2', transform)).toBe(false)
  })
})

describe('SerialVideoWorkflowOperationQueue', () => {
  it('runs image transforms for one node strictly in submission order', async () => {
    const queue = new SerialVideoWorkflowOperationQueue()
    const events: string[] = []
    let releaseFirst!: () => void
    const firstGate = new Promise<void>((resolve) => { releaseFirst = resolve })
    const first = queue.enqueue('background_2', async () => {
      events.push('A:start')
      await firstGate
      events.push('A:end')
    })
    const second = queue.enqueue('background_2', async () => { events.push('B') })
    await Promise.resolve()
    await Promise.resolve()
    expect(events).toEqual(['A:start'])
    releaseFirst()
    await Promise.all([first, second])
    expect(events).toEqual(['A:start', 'A:end', 'B'])
  })

  it('does not strand later transforms after an earlier request fails', async () => {
    const queue = new SerialVideoWorkflowOperationQueue()
    const events: string[] = []
    const failed = queue.enqueue('background_2', async () => { throw new Error('upstream failed') })
    const recovered = queue.enqueue('background_2', async () => { events.push('recovered') })
    await expect(failed).rejects.toThrow('upstream failed')
    await recovered
    expect(events).toEqual(['recovered'])
  })
})

describe('runConfirmedVideoWorkflowMutation', () => {
  it('does not touch the graph when regeneration confirmation is canceled', async () => {
    let node = boundImageNode()
    const apply = vi.fn(async () => { node = clearVideoWorkflowImageAssetBinding(node) })
    const submit = vi.fn(async () => 'run')
    const rollback = vi.fn(async () => undefined)

    await expect(runConfirmedVideoWorkflowMutation({
      confirm: async () => { throw 'cancel' },
      apply,
      submit,
      rollback,
    })).rejects.toBe('cancel')

    expect(apply).not.toHaveBeenCalled()
    expect(submit).not.toHaveBeenCalled()
    expect(rollback).not.toHaveBeenCalled()
    expect(node).toMatchObject({ asset_id: 'asset-original', asset_version_id: 'version-original', version: 1 })
    expect(node.config.preview_url).toBe('/preview-original')
  })

  it('restores the original image binding when re-estimation or submission fails', async () => {
    const original = boundImageNode()
    let node = cloneWorkflowGraph({
      schema_version: 2,
      settings: { aspect_ratio: '9:16', resolution: '1080p', fps: 30 },
      nodes: [original], edges: [], groups: [],
    }).nodes[0]
    const rollback = vi.fn(async () => { node = original })

    await expect(runConfirmedVideoWorkflowMutation({
      confirm: async () => undefined,
      apply: async () => { node = clearVideoWorkflowImageAssetBinding(node) },
      submit: async () => { throw new Error('submit failed') },
      rollback,
    })).rejects.toThrow('submit failed')

    expect(rollback).toHaveBeenCalledOnce()
    expect(node).toMatchObject({ asset_id: 'asset-original', asset_version_id: 'version-original', version: 1 })
    expect(node.config.preview_url).toBe('/preview-original')
  })
})
