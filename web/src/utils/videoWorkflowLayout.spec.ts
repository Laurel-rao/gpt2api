import { describe, expect, it } from 'vitest'
import { createStarterVideoWorkflowGraph } from './videoWorkflowGraph'
import { autoLayoutVideoWorkflowGraph, diagnoseVideoWorkflowLayout, videoWorkflowNodeSize } from './videoWorkflowLayout'

describe('video workflow automatic layout', () => {
  it('lays out nodes, scene groups and curves without visual overlaps', () => {
    const source = createStarterVideoWorkflowGraph()
    source.nodes.filter((node) => !node.locked).forEach((node) => { node.position = { x: 0, y: 0 } })

    const result = autoLayoutVideoWorkflowGraph(source)
    const diagnostics = diagnoseVideoWorkflowLayout(result)

    expect(diagnostics.nodeOverlaps).toEqual([])
    expect(diagnostics.groupOverlaps).toEqual([])
    expect(diagnostics.edgeNodeOverlaps).toEqual([])
    expect(result.edges.some((edge) => edge.route?.length)).toBe(true)
    expect(source.nodes.filter((node) => !node.locked).every((node) => node.position.x === 0 && node.position.y === 0)).toBe(true)
  })

  it('never moves locked nodes even when their saved positions overlap', () => {
    const source = createStarterVideoWorkflowGraph()
    const locked = source.nodes.filter((node) => node.locked)
    locked.forEach((node) => { node.position = { x: 400, y: 240 } })

    const result = autoLayoutVideoWorkflowGraph(source)

    expect(result.nodes.filter((node) => node.locked).map((node) => node.position)).toEqual([
      { x: 400, y: 240 },
      { x: 400, y: 240 },
    ])
  })

  it('keeps every scene node inside its recalculated group bounds', () => {
    const result = autoLayoutVideoWorkflowGraph(createStarterVideoWorkflowGraph())

    for (const group of result.groups) {
      for (const nodeID of group.node_ids) {
        const node = result.nodes.find((item) => item.id === nodeID)!
        const size = videoWorkflowNodeSize(node)
        expect(node.position.x).toBeGreaterThan(group.position.x)
        expect(node.position.y).toBeGreaterThan(group.position.y + 34)
        expect(node.position.x + size.width).toBeLessThan(group.position.x + group.size.width)
        expect(node.position.y + size.height).toBeLessThan(group.position.y + group.size.height)
      }
    }
  })

  it('is deterministic for the same graph', () => {
    const source = createStarterVideoWorkflowGraph()
    expect(autoLayoutVideoWorkflowGraph(source)).toEqual(autoLayoutVideoWorkflowGraph(source))
  })

  it('preserves manual curve offsets while rebuilding automatic routes', () => {
    const source = createStarterVideoWorkflowGraph()
    source.edges[0].curve = { x: 36, y: -28 }

    const result = autoLayoutVideoWorkflowGraph(source)

    expect(result.edges[0].curve).toEqual({ x: 36, y: -28 })
  })

  it('routes the dense character-to-video dependency graph around every node', () => {
    const source = createStarterVideoWorkflowGraph()
    const characters = source.nodes.filter((node) => node.type === 'character')
    const videos = source.nodes.filter((node) => node.type === 'video')
    for (const character of characters) {
      for (const video of videos) {
        source.edges.push({
          id: `dense-${character.id}-${video.id}`,
          source: character.id,
          source_port: 'selected',
          target: video.id,
          target_port: character.role_id || character.id,
        })
      }
    }

    const result = autoLayoutVideoWorkflowGraph(source)
    const diagnostics = diagnoseVideoWorkflowLayout(result)

    expect(diagnostics.nodeOverlaps).toEqual([])
    expect(diagnostics.groupOverlaps).toEqual([])
    expect(diagnostics.edgeNodeOverlaps).toEqual([])
    expect(result.edges.filter((edge) => edge.id.startsWith('dense-') && edge.route?.length).length).toBeGreaterThan(4)
  })
})
