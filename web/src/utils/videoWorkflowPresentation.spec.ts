import { describe, expect, it } from 'vitest'
import { createStarterVideoWorkflowGraph } from './videoWorkflowGraph'
import { ensureSharedCharacterBusLayout, sharedCharacterBus, sharedCharacterBusTargetRoute } from './videoWorkflowPresentation'

describe('video workflow presentation graph', () => {
  it('collapses the dense character-to-video fanout into one visual bus', () => {
    const graph = createStarterVideoWorkflowGraph()
    const characters = graph.nodes.filter((node) => node.type === 'character')
    const videos = graph.nodes.filter((node) => node.type === 'video')
    for (const character of characters) {
      for (const video of videos) {
        graph.edges.push({
          id: `role-${character.id}-${video.id}`,
          source: character.id,
          source_port: 'selected',
          target: video.id,
          target_port: character.id,
        })
        video.inputs?.push({ id: character.id, type: 'image' })
      }
    }

    const bus = sharedCharacterBus(graph)

    expect(bus?.logicalEdges).toHaveLength(12)
    expect(bus?.sourceNodeIDs).toHaveLength(3)
    expect(bus?.targetNodeIDs).toHaveLength(4)
    const route = sharedCharacterBusTargetRoute(graph, bus!, bus!.targetNodeIDs[0], 0)
    expect(route).toHaveLength(4)
    expect(route[0].x).toBe(route[1].x)
    expect(route[1].y).toBeLessThan(Math.min(...graph.nodes.map((node) => node.position.y)))
    expect(route[2].x).toBe(route[3].x)
  })

  it('preserves a manual bus anchor until a full reset', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.layout = {
      shared_character_bus: { position: { x: 900, y: 500 }, position_mode: 'manual' },
    }

    expect(ensureSharedCharacterBusLayout(graph).position).toEqual({ x: 900, y: 500 })
    expect(ensureSharedCharacterBusLayout(graph, true).position_mode).toBe('auto')
  })
})
