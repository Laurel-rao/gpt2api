import { describe, expect, it } from 'vitest'
import { createStarterVideoWorkflowGraph } from './videoWorkflowGraph'
import { elkLayoutVideoWorkflowGraph } from './videoWorkflowElk'
import { diagnoseVideoWorkflowLayout } from './videoWorkflowLayout'

describe('ELK video workflow layout', () => {
  it('lays out the full graph without moving locked nodes', async () => {
    const source = createStarterVideoWorkflowGraph()
    const locked = source.nodes.filter((node) => node.locked).map((node) => ({ id: node.id, position: { ...node.position } }))

    const result = await elkLayoutVideoWorkflowGraph(source, 'all')
    const diagnostics = diagnoseVideoWorkflowLayout(result)

    expect(diagnostics.nodeOverlaps).toEqual([])
    expect(locked.every((item) => {
      const node = result.nodes.find((candidate) => candidate.id === item.id)
      return node?.position.x === item.position.x && node.position.y === item.position.y
    })).toBe(true)
  })

  it('uses manual nodes as fixed anchors in incremental mode', async () => {
    const source = createStarterVideoWorkflowGraph()
    const manual = source.nodes.find((node) => node.id === 'character_1')!
    manual.position_mode = 'manual'
    manual.position = { x: 777, y: 333 }

    const result = await elkLayoutVideoWorkflowGraph(source, 'auto')

    expect(result.nodes.find((node) => node.id === manual.id)?.position).toEqual({ x: 777, y: 333 })
  })
})
