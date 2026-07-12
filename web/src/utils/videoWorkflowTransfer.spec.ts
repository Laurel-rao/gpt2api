import { describe, expect, it } from 'vitest'
import {
  VIDEO_WORKFLOW_TRANSFER_FORMAT,
  VIDEO_WORKFLOW_TRANSFER_MAX_BYTES,
  VIDEO_WORKFLOW_TRANSFER_VERSION,
  VideoWorkflowTransferError,
  createVideoWorkflowTransferDocument,
  parseVideoWorkflowTransfer,
  prepareVideoWorkflowGraphForTransfer,
  serializeVideoWorkflowTransfer,
  videoWorkflowTransferFilename,
} from './videoWorkflowTransfer'
import {
  cloneWorkflowGraph,
  createStarterVideoWorkflowGraph,
} from './videoWorkflowGraph'

describe('video workflow JSON transfer', () => {
  it('exports a versioned document and round-trips the editable graph', () => {
    const graph = createStarterVideoWorkflowGraph()
    const now = new Date('2026-07-12T02:03:04.000Z')
    const text = serializeVideoWorkflowTransfer('雨夜重逢', graph, now)
    const raw = JSON.parse(text)

    expect(raw).toMatchObject({
      format: VIDEO_WORKFLOW_TRANSFER_FORMAT,
      format_version: VIDEO_WORKFLOW_TRANSFER_VERSION,
      exported_at: now.toISOString(),
      name: '雨夜重逢',
    })
    const imported = parseVideoWorkflowTransfer(text)
    expect(imported.source).toBe('document')
    expect(imported.name).toBe('雨夜重逢')
    expect(imported.graph).toEqual(raw.graph)
  })

  it('accepts a graph-only JSON and migrates Graph v1 to v2', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.schema_version = 1
    const imported = parseVideoWorkflowTransfer(JSON.stringify(graph))

    expect(imported.source).toBe('graph')
    expect(imported.graph.schema_version).toBe(2)
    expect(imported.graph.nodes.filter((node) => node.type === 'timeline')).toHaveLength(1)
    expect(imported.graph.nodes.filter((node) => node.type === 'compose')).toHaveLength(1)
  })

  it('accepts a graph-only JSON whose empty groups field was omitted by Go', () => {
    const graph = createStarterVideoWorkflowGraph()
    const raw = { ...graph, groups: undefined }
    const imported = parseVideoWorkflowTransfer(JSON.stringify(raw))
    expect(imported.graph.groups).toEqual([])
  })

  it('accepts an UTF-8 BOM', () => {
    const graph = createStarterVideoWorkflowGraph()
    expect(parseVideoWorkflowTransfer(`\uFEFF${JSON.stringify(graph)}`).graph.nodes).toHaveLength(graph.nodes.length)
  })

  it('round-trips a valid draft whose timeline has no clips', () => {
    const graph = createStarterVideoWorkflowGraph()
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    timeline.config.clips = []
    timeline.config.clip_node_ids = []
    timeline.inputs = []
    graph.edges = graph.edges.filter((edge) => edge.target !== timeline.id)

    const imported = parseVideoWorkflowTransfer(serializeVideoWorkflowTransfer('空时间线', graph))
    const importedTimeline = imported.graph.nodes.find((node) => node.type === 'timeline')!
    expect(importedTimeline.config.clips).toEqual([])
  })

  it('removes runtime state and signed preview fields without mutating the source', () => {
    const graph = createStarterVideoWorkflowGraph()
    const source = graph.nodes.find((node) => node.type === 'background')!
    source.status = 'running'
    source.progress = 63
    source.output = { preview_url: '/signed-output?sig=secret' }
    source.stale_reason = '旧状态'
    source.config.preview_url = '/signed-preview?sig=secret'
    source.config.preview_transform_pending = true
    source.config.transform_versions = { version_1: { preview_url: '/nested?sig=secret' } }
    ;(source as any).signed_url = '/top-level?sig=secret'

    const prepared = prepareVideoWorkflowGraphForTransfer(graph)
    const node = prepared.nodes.find((item) => item.id === source.id)!
    expect(node.status).toBeUndefined()
    expect(node.progress).toBeUndefined()
    expect(node.output).toBeUndefined()
    expect(node.stale_reason).toBeUndefined()
    expect(node.config.preview_url).toBeUndefined()
    expect(node.config.preview_transform_pending).toBeUndefined()
    expect(node.config.transform_versions).toBeUndefined()
    expect((node as any).signed_url).toBeUndefined()
    expect(source.config.preview_url).toContain('sig=secret')
  })

  it('retains account-local asset bindings and reports their count', () => {
    const graph = createStarterVideoWorkflowGraph()
    const background = graph.nodes.find((node) => node.type === 'background')!
    background.asset_id = 'asset-1'
    background.asset_version_id = 'version-1'
    background.config.asset_id = 'asset-1'
    background.config.asset_version_id = 'version-1'

    const imported = parseVideoWorkflowTransfer(JSON.stringify(createVideoWorkflowTransferDocument('绑定素材', graph)))
    expect(imported.asset_reference_count).toBe(1)
    expect(imported.graph.nodes.find((node) => node.id === background.id)).toMatchObject({
      asset_id: 'asset-1',
      asset_version_id: 'version-1',
    })
  })

  it.each([
    ['invalid syntax', '{'],
    ['array root', '[]'],
    ['null root', 'null'],
    ['missing graph', JSON.stringify({ format: VIDEO_WORKFLOW_TRANSFER_FORMAT, format_version: 1 })],
    ['missing format', JSON.stringify({ format_version: 1, graph: createStarterVideoWorkflowGraph() })],
    ['missing format version', JSON.stringify({ format: VIDEO_WORKFLOW_TRANSFER_FORMAT, graph: createStarterVideoWorkflowGraph() })],
    ['wrong format', JSON.stringify({ format: 'other', format_version: 1, graph: createStarterVideoWorkflowGraph() })],
    ['wrong version', JSON.stringify({ format: VIDEO_WORKFLOW_TRANSFER_FORMAT, format_version: 2, graph: createStarterVideoWorkflowGraph() })],
  ])('rejects %s', (_, text) => {
    expect(() => parseVideoWorkflowTransfer(text)).toThrow(VideoWorkflowTransferError)
  })

  it('rejects files larger than 1 MiB and objects deeper than 32 levels', () => {
    expect(() => parseVideoWorkflowTransfer(' '.repeat(VIDEO_WORKFLOW_TRANSFER_MAX_BYTES + 1))).toThrow('1 MiB')
    let nested: Record<string, any> = {}
    const root = nested
    for (let index = 0; index < 33; index += 1) {
      nested.child = {}
      nested = nested.child
    }
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(root))).toThrow('32 层')
  })

  it('rejects unknown node types before migration', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.nodes[0].type = 'remote-script'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(graph))).toThrow('不支持的节点类型')
  })

  it('rejects field types that the server cannot decode', () => {
    const invalidPort = createStarterVideoWorkflowGraph()
    ;(invalidPort.nodes[0].outputs![0] as any).required = 'yes'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(invalidPort))).toThrow('required 必须是布尔值')

    const invalidNode = createStarterVideoWorkflowGraph()
    ;(invalidNode.nodes[0] as any).enabled = 'yes'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(invalidNode))).toThrow('enabled 必须是布尔值')
  })

  it('rejects partial asset bindings and damaged scene groups', () => {
    const partialBinding = createStarterVideoWorkflowGraph()
    const background = partialBinding.nodes.find((node) => node.type === 'background')!
    background.asset_id = 'asset-only'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(partialBinding))).toThrow('素材 ID 与版本 ID 必须同时存在')

    const splitBinding = createStarterVideoWorkflowGraph()
    const splitBackground = splitBinding.nodes.find((node) => node.type === 'background')!
    splitBackground.asset_id = 'asset-only'
    splitBackground.config.asset_version_id = 'version-only'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(splitBinding))).toThrow('素材 ID 与版本 ID 必须同时存在')

    const damagedGroup = createStarterVideoWorkflowGraph()
    damagedGroup.groups[0].node_ids.push('missing-node')
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(damagedGroup))).toThrow('引用了不存在的节点')
  })

  it('rejects a damaged Graph v2 instead of silently adding system nodes', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.nodes = graph.nodes.filter((node) => node.type !== 'compose')
    graph.edges = graph.edges.filter((edge) => edge.target !== 'compose')
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(graph))).toThrow('时间线节点和一个成片节点')
  })

  it('rejects validation failures before replacing the canvas', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.edges.push({
      id: 'cycle-edge',
      source: 'compose',
      source_port: 'video',
      target: 'story_brief',
      target_port: 'text',
    })
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(graph))).toThrow('画布校验未通过')
  })

  it('requires every timeline clip to come from a video node', () => {
    const graph = createStarterVideoWorkflowGraph()
    const brief = graph.nodes.find((node) => node.type === 'story_brief')!
    brief.outputs!.push({ id: 'fake_video', type: 'video' })
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    const firstClip = timeline.config.clips![0]
    firstClip.source_node_id = brief.id
    firstClip.source_port = 'fake_video'
    expect(() => parseVideoWorkflowTransfer(JSON.stringify(graph))).toThrow('必须引用视频节点')
  })

  it('creates a safe, bounded filename', () => {
    const name = ` 雨夜/重逢:*?"<>| ${'片'.repeat(100)} `
    const filename = videoWorkflowTransferFilename(name, 12, new Date('2026-07-12T02:03:04.000Z'))
    expect(filename).toContain('雨夜-重逢-')
    expect(filename).toContain('-R12-20260712-020304.video-workflow.json')
    expect(filename).not.toMatch(/[\\/:*?"<>|]/)
    expect(filename.length).toBeLessThanOrEqual(132)
  })

  it('does not mutate the graph while serializing repeatedly', () => {
    const graph = createStarterVideoWorkflowGraph()
    const before = cloneWorkflowGraph(graph)
    serializeVideoWorkflowTransfer('测试', graph)
    serializeVideoWorkflowTransfer('测试', graph)
    expect(graph).toEqual(before)
  })
})
