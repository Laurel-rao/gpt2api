import { describe, expect, it } from 'vitest'
import type { VideoWorkflowEdge, VideoWorkflowGraph, VideoWorkflowRunMode } from '@/api/videoWorkflow'
import {
  DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
  VIDEO_WORKFLOW_MAX_EDGES,
  VIDEO_WORKFLOW_MAX_NODES,
  clearVideoWorkflowImageAssetBinding,
  cloneWorkflowGraph,
  connectionError,
  createStarterVideoWorkflowGraph,
  desktopVideoWorkflowReady,
  makeVideoWorkflowNode,
  migrateVideoWorkflowGraph,
  mobileWorkspaceView,
  moveTimelineClip,
  nextVideoWorkflowImageTransform,
  normalizeImageTransform,
  normalizeTimelineClip,
  removeNodeFromGraph,
  resolveVideoWorkflowAssetBinding,
  resolveVideoWorkflowDisplayedStatus,
  resolveVideoWorkflowVideoModel,
  rotateImageTransform,
  toggleImageTransformFlip,
  updateTimelineClipTrim,
  validateVideoWorkflowGraph,
  videoWorkflowImageVersionTransformState,
  videoWorkflowNodePreviewURL,
  videoWorkflowNodeRunOutputVersionID,
  videoWorkflowModelLabel,
} from './videoWorkflowGraph'

function withoutEdge(graph: VideoWorkflowGraph, predicate: (edge: VideoWorkflowEdge) => boolean) {
  graph.edges = graph.edges.filter((edge) => !predicate(edge))
  return graph
}

describe('video workflow graph v2', () => {
  it('uses the backend run-mode contract', () => {
    const modes: VideoWorkflowRunMode[] = ['full', 'node_only', 'downstream']
    expect(modes).toEqual(['full', 'node_only', 'downstream'])
  })

  it('prefers baked image previews and generated video outputs', () => {
    const image = makeVideoWorkflowNode('background')
    image.config.preview_url = '/image-version'
    image.output = { preview_url: '/old-image-output' }
    const video = makeVideoWorkflowNode('video')
    video.config.preview_url = '/video-poster'
    video.output = { output_url: '/generated-video.mp4' }

    expect(videoWorkflowNodePreviewURL(image)).toBe('/image-version')
    expect(videoWorkflowNodePreviewURL(video)).toBe('/generated-video.mp4')
    expect(videoWorkflowNodePreviewURL(null)).toBe('')
  })

  it('creates the 19-node starter graph with v2 output and four full clips', () => {
    const graph = createStarterVideoWorkflowGraph()
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    expect(graph.schema_version).toBe(2)
    expect(graph.nodes).toHaveLength(19)
    expect(graph.settings).toMatchObject({
      resolution: '1080p',
      scene_duration_ms: 15_000,
      character_approval_policy: 'manual',
      storyboard_approval_policy: 'manual',
      video_model: DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
    })
    expect(timeline.config.clips).toHaveLength(4)
    expect(timeline.config.clips![0]).toMatchObject({ trim_in_ms: 0, trim_out_ms: 15_000 })
    expect(validateVideoWorkflowGraph(graph, { requireComplete: true })).toEqual([])
  })

  it('accepts compatible ports and rejects type mismatches', () => {
    const graph = withoutEdge(
      createStarterVideoWorkflowGraph(),
      (edge) => edge.source === 'background_1' && edge.target === 'video_1',
    )
    expect(connectionError(graph, {
      source: 'background_1',
      source_port: 'image',
      target: 'video_1',
      target_port: 'background',
    })).toBe('')

    expect(connectionError(graph, {
      source: 'brief',
      source_port: 'text',
      target: 'video_1',
      target_port: 'background',
    })).toBe('text 不能连接 image')
  })

  it('enforces single-value inputs and uses distinct timeline clip ports', () => {
    const graph = createStarterVideoWorkflowGraph()
    expect(connectionError(graph, {
      source: 'background_2',
      source_port: 'image',
      target: 'video_1',
      target_port: 'background',
    })).toBe('单值输入端口只能连接 1 条边')

    withoutEdge(graph, (edge) => edge.source === 'video_4' && edge.target === 'timeline')
    expect(connectionError(graph, {
      source: 'video_4',
      source_port: 'video',
      target: 'timeline',
      target_port: 'clip_4',
    })).toBe('')
    expect(graph.nodes.find((node) => node.id === 'timeline')?.inputs?.map((port) => port.id)).toEqual([
      'clip_1', 'clip_2', 'clip_3', 'clip_4',
    ])
  })

  it('rejects self connections, graph cycles and duplicate connections', () => {
    const graph = createStarterVideoWorkflowGraph()
    const compose = graph.nodes.find((node) => node.id === 'compose')!
    compose.outputs = [{ id: 'clips', label: '回流', type: 'text' }]
    const brief = graph.nodes.find((node) => node.id === 'brief')!
    brief.inputs = [{ id: 'input', label: '回流', type: 'text' }]
    expect(connectionError(graph, {
      source: compose.id, source_port: 'clips', target: brief.id, target_port: 'input',
    })).toBe('连接会形成循环')
    expect(connectionError(graph, {
      source: brief.id, source_port: 'text', target: brief.id, target_port: 'input',
    })).toBe('节点不能连接自身')
    expect(connectionError(graph, {
      source: 'brief', source_port: 'text', target: 'script', target_port: 'brief',
    })).toBe('连接已存在')
  })

  it('reports node and edge hard limits', () => {
    const nodeLimited = createStarterVideoWorkflowGraph()
    while (nodeLimited.nodes.length <= VIDEO_WORKFLOW_MAX_NODES) {
      const node = makeVideoWorkflowNode('story_brief')
      node.id = `extra_${nodeLimited.nodes.length}`
      nodeLimited.nodes.push(node)
    }
    expect(validateVideoWorkflowGraph(nodeLimited).map((issue) => issue.code)).toContain('node_limit_exceeded')

    const edgeLimited = createStarterVideoWorkflowGraph()
    while (edgeLimited.edges.length <= VIDEO_WORKFLOW_MAX_EDGES) {
      edgeLimited.edges.push({
        id: `extra_edge_${edgeLimited.edges.length}`,
        source: 'video_1', source_port: 'video', target: 'timeline', target_port: 'clip_1',
      })
    }
    expect(validateVideoWorkflowGraph(edgeLimited).map((issue) => issue.code)).toContain('edge_limit_exceeded')
    expect(connectionError({ ...edgeLimited, edges: edgeLimited.edges.slice(0, VIDEO_WORKFLOW_MAX_EDGES) }, {
      source: 'video_2', source_port: 'video', target: 'timeline', target_port: 'clip_2',
    })).toBe(`连线不能超过 ${VIDEO_WORKFLOW_MAX_EDGES} 条`)
  })

  it('validates system nodes, typed edges, single inputs and cycles', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.nodes = graph.nodes.filter((node) => node.type !== 'compose')
    graph.edges.push({
      id: 'wrong-type', source: 'brief', source_port: 'text', target: 'video_1', target_port: 'background',
    })
    graph.edges.push({
      id: 'second-background', source: 'background_2', source_port: 'image', target: 'video_1', target_port: 'background',
    })
    graph.edges.push({
      id: 'cycle', source: 'video_1', source_port: 'video', target: 'timeline', target_port: 'clip_1',
    })
    const issues = validateVideoWorkflowGraph(graph)
    expect(issues.map((issue) => issue.code)).toEqual(expect.arrayContaining([
      'invalid_compose_count', 'port_type_mismatch', 'single_input_multiple_edges',
    ]))

    const cyclic = createStarterVideoWorkflowGraph()
    const compose = cyclic.nodes.find((node) => node.type === 'compose')!
    const brief = cyclic.nodes.find((node) => node.type === 'story_brief')!
    compose.outputs = [{ id: 'text', type: 'text' }]
    brief.inputs = [{ id: 'feedback', type: 'text' }]
    cyclic.edges.push({ id: 'compose-brief', source: compose.id, source_port: 'text', target: brief.id, target_port: 'feedback' })
    expect(validateVideoWorkflowGraph(cyclic).map((issue) => issue.code)).toContain('graph_cycle')
  })

  it('protects timeline and compose from deletion', () => {
    const graph = createStarterVideoWorkflowGraph()
    expect(removeNodeFromGraph(graph, 'timeline')).toEqual(graph)
    expect(removeNodeFromGraph(graph, 'compose')).toEqual(graph)
  })

  it('removes node references from edges, groups and both timeline representations', () => {
    const graph = removeNodeFromGraph(createStarterVideoWorkflowGraph(), 'video_2')
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    expect(graph.nodes.some((node) => node.id === 'video_2')).toBe(false)
    expect(graph.edges.some((edge) => edge.source === 'video_2' || edge.target === 'video_2')).toBe(false)
    expect(graph.groups.some((group) => group.node_ids.includes('video_2'))).toBe(false)
    expect(timeline.config.clip_node_ids).not.toContain('video_2')
    expect(timeline.config.clips!.some((clip: any) => clip.source_node_id === 'video_2')).toBe(false)
  })

  it('clones graph without sharing nested configuration', () => {
    const graph = createStarterVideoWorkflowGraph()
    const copy = cloneWorkflowGraph(graph)
    copy.nodes[0].config.prompt = 'changed'
    expect(graph.nodes[0].config.prompt).not.toBe('changed')
  })

  it('creates typed nodes with stable generation and image defaults', () => {
    const character = makeVideoWorkflowNode('character')
    const background = makeVideoWorkflowNode('background')
    const video = makeVideoWorkflowNode('video')
    expect(character.config.candidate_count).toBe(2)
    expect(background.config.image_transform).toEqual({
      crop: { x: 0, y: 0, width: 1, height: 1 },
      rotation: 0,
      flip_horizontal: false,
      flip_vertical: false,
    })
    expect(video.config.duration_ms).toBe(15_000)
    expect(video.config.model).toBe(DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL)
    expect(videoWorkflowModelLabel(video.config.model)).toBe('Wan2.7-r2v')
    expect(resolveVideoWorkflowVideoModel('backend-video-model', video.config.model)).toBe('backend-video-model')
  })

  it('maps legacy default aliases to Wan2.7 while preserving explicit models', () => {
    expect(resolveVideoWorkflowVideoModel('seedance-2.0', 'provider-video-model')).toBe(DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL)
    expect(resolveVideoWorkflowVideoModel('default')).toBe(DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL)
    expect(resolveVideoWorkflowVideoModel(undefined, 'Seedance 2.0')).toBe(DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL)
    expect(resolveVideoWorkflowVideoModel(undefined, 'provider-video-model')).toBe('provider-video-model')
    expect(videoWorkflowModelLabel('seedance-2.0')).toBe('Wan2.7-r2v')
  })

  it('normalizes, rotates and flips image transforms without mutating the input', () => {
    const source = {
      crop: { x: -1, y: 0.2, width: 2, height: 0.9 },
      rotation: 13,
      flip_horizontal: false,
      flip_vertical: false,
    }
    const normalized = normalizeImageTransform(source)
    expect(normalized).toEqual({
      crop: { x: 0, y: 0.2, width: 1, height: 0.8 },
      rotation: 0,
      flip_horizontal: false,
      flip_vertical: false,
    })
    const rotated = rotateImageTransform(normalized, -90)
    const flipped = toggleImageTransformFlip(rotated, 'horizontal')
    expect(flipped).toMatchObject({ rotation: 270, flip_horizontal: true, flip_vertical: false })
    expect(normalized).toMatchObject({ rotation: 0, flip_horizontal: false })
  })
})

describe('video workflow graph migration', () => {
  it('upgrades v1 settings and clip IDs without changing source node IDs', () => {
    const graph = createStarterVideoWorkflowGraph() as any
    graph.schema_version = 1
    delete graph.settings.resolution
    delete graph.settings.scene_duration_ms
    delete graph.settings.character_approval_policy
    delete graph.settings.storyboard_approval_policy
    delete graph.settings.video_model
    const timeline = graph.nodes.find((node: any) => node.type === 'timeline')
    delete timeline.config.clips

    const migrated = migrateVideoWorkflowGraph(graph)
    const migratedTimeline = migrated.nodes.find((node) => node.type === 'timeline')!
    expect(migrated.schema_version).toBe(2)
    expect(migrated.settings).toMatchObject({
      resolution: '720p',
      scene_duration_ms: 15_000,
      character_approval_policy: 'manual',
      storyboard_approval_policy: 'manual',
      video_model: DEFAULT_VIDEO_WORKFLOW_VIDEO_MODEL,
    })
    expect(migratedTimeline.config.clips!.map((clip: any) => clip.source_node_id)).toEqual([
      'video_1', 'video_2', 'video_3', 'video_4',
    ])
    expect(migrated.nodes.some((node) => node.id === 'video_1')).toBe(true)
    expect(validateVideoWorkflowGraph(migrated, { requireComplete: true })).toEqual([])
  })

  it('preserves a backend-selected video model', () => {
    const graph = createStarterVideoWorkflowGraph()
    graph.settings.video_model = 'provider-video-model'
    expect(migrateVideoWorkflowGraph(graph).settings.video_model).toBe('provider-video-model')
  })

  it('preserves finite edge layout data and drops invalid values', () => {
    const graph = createStarterVideoWorkflowGraph() as any
    graph.edges[0].curve = { x: 120, y: -80 }
    graph.edges[0].route = [{ x: 240, y: 80 }, { x: 320, y: 80 }]
    graph.edges[1].curve = { x: Number.NaN, y: 10 }
    graph.edges[1].route = [{ x: null, y: 10 }]

    const migrated = migrateVideoWorkflowGraph(graph)

    expect(migrated.edges[0].curve).toEqual({ x: 120, y: -80 })
    expect(migrated.edges[0].route).toEqual([{ x: 240, y: 80 }, { x: 320, y: 80 }])
    expect(migrated.edges[1].curve).toBeUndefined()
    expect(migrated.edges[1].route).toBeUndefined()
  })

  it('restores missing protected system nodes and their connection', () => {
    const graph = createStarterVideoWorkflowGraph() as any
    graph.schema_version = 1
    graph.nodes = graph.nodes.filter((node: any) => node.type !== 'timeline' && node.type !== 'compose')
    graph.edges = graph.edges.filter((edge: any) => edge.target !== 'timeline' && edge.target !== 'compose')
    const migrated = migrateVideoWorkflowGraph(graph)
    const timeline = migrated.nodes.find((node) => node.type === 'timeline')!
    const compose = migrated.nodes.find((node) => node.type === 'compose')!
    expect(timeline.locked).toBe(true)
    expect(compose.locked).toBe(true)
    expect(migrated.edges).toContainEqual(expect.objectContaining({ source: timeline.id, target: compose.id }))
  })
})

describe('video timeline', () => {
  it('moves complete clips and preserves the source array', () => {
    const source = ['video_1', 'video_2', 'video_3', 'video_4']
    expect(moveTimelineClip(source, 0, 2)).toEqual(['video_2', 'video_3', 'video_1', 'video_4'])
    expect(source).toEqual(['video_1', 'video_2', 'video_3', 'video_4'])
  })

  it('ignores out-of-range moves', () => {
    expect(moveTimelineClip(['a', 'b'], 0, 9)).toEqual(['a', 'b'])
  })

  it('snaps trims to 100ms and enforces a 1000ms minimum duration', () => {
    const clip = { id: 'clip', source_node_id: 'video', source_port: 'video', trim_in_ms: 14_760, trim_out_ms: 14_810 }
    expect(normalizeTimelineClip(clip)).toMatchObject({ trim_in_ms: 14_000, trim_out_ms: 15_000 })
    expect(updateTimelineClipTrim(clip, 1_049, 8_951)).toMatchObject({ trim_in_ms: 1_000, trim_out_ms: 9_000 })
  })

  it('rejects trims outside the fixed 100ms and 1–15 second contract', () => {
    const graph = createStarterVideoWorkflowGraph()
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    timeline.config.clips![0].trim_in_ms = 50
    timeline.config.clips![0].trim_out_ms = 500
    expect(validateVideoWorkflowGraph(graph).map((issue) => issue.code)).toContain('invalid_timeline_trim')
  })

  it('allows an empty timeline draft but requires a clip for a complete run', () => {
    const graph = createStarterVideoWorkflowGraph()
    const timeline = graph.nodes.find((node) => node.type === 'timeline')!
    timeline.config.clips = []
    timeline.config.clip_node_ids = []
    timeline.inputs = []
    graph.edges = graph.edges.filter((edge) => edge.target !== timeline.id)

    expect(validateVideoWorkflowGraph(graph, { requireComplete: false }).map((issue) => issue.code))
      .not.toContain('timeline_clip_count')
    expect(validateVideoWorkflowGraph(graph, { requireComplete: true }).map((issue) => issue.code))
      .toContain('timeline_clip_count')
  })
})

describe('mobile workspace compatibility', () => {
  it('keeps the legacy segmented helper stable for existing callers', () => {
    expect(mobileWorkspaceView(390, 'canvas')).toBe('canvas')
    expect(mobileWorkspaceView(1440, 'timeline')).toBe('all')
  })

  it('blocks the video canvas below the 1280px desktop boundary', () => {
    expect(desktopVideoWorkflowReady(1279)).toBe(false)
    expect(desktopVideoWorkflowReady(1280)).toBe(true)
  })
})

describe('generated image asset binding', () => {
  it('resolves a generated node output version back to its real asset', () => {
    const node = makeVideoWorkflowNode('background')
    const nodeRun = {
      id: 'node-run', node_id: node.id, node_type: 'background', status: 'succeeded' as const,
      output_version_id: 'version-generated', output: { selected_version_id: 'version-generated' },
    }
    const assets = [{
      id: 'asset-generated', name: '生成图片', kind: 'image' as const, current_version_id: 'version-generated',
      versions: [{ id: 'version-generated', version: 1, size_bytes: 1024, preview_url: '/p/vwf/version-generated' }],
    }]

    expect(videoWorkflowNodeRunOutputVersionID(nodeRun)).toBe('version-generated')
    expect(resolveVideoWorkflowAssetBinding(node, nodeRun, assets)).toMatchObject({
      assetID: 'asset-generated', versionID: 'version-generated', version: { preview_url: '/p/vwf/version-generated' },
    })
  })

  it('keeps an explicitly selected immutable version ahead of the last run output', () => {
    const node = makeVideoWorkflowNode('background')
    node.asset_id = 'asset-selected'
    node.asset_version_id = 'version-selected'
    const binding = resolveVideoWorkflowAssetBinding(node, {
      id: 'node-run', node_id: node.id, status: 'succeeded', output_version_id: 'version-run',
    }, [{
      id: 'asset-selected', name: '已选版本', kind: 'image', current_version_id: 'version-selected',
      versions: [{ id: 'version-selected', version: 2, size_bytes: 2048 }],
    }])
    expect(binding?.versionID).toBe('version-selected')
  })

  it('uses the immutable parent as the cumulative transform base', () => {
    const state = videoWorkflowImageVersionTransformState('version-rotated', {
      id: 'version-rotated', version: 2, size_bytes: 1024, parent_version_id: 'version-original',
      metadata: {
        image_transform: {
          crop: { x: 0, y: 0, width: 1, height: 1 }, rotation: 90,
          flip_horizontal: false, flip_vertical: false,
        },
      },
    })
    expect(state.transformBaseVersionID).toBe('version-original')
    expect(nextVideoWorkflowImageTransform(state.imageTransform, { rotation_delta: 90 }).rotation).toBe(180)
  })

  it('resets transform state when switching back to an unmodified version', () => {
    const state = videoWorkflowImageVersionTransformState('version-original', {
      id: 'version-original', version: 1, size_bytes: 1024, source_type: 'generated',
    })
    expect(state.transformBaseVersionID).toBe('version-original')
    expect(state.imageTransform).toEqual({
      crop: { x: 0, y: 0, width: 1, height: 1 }, rotation: 0,
      flip_horizontal: false, flip_vertical: false,
    })
  })

  it('clears all bound-version fields before regeneration', () => {
    const node = makeVideoWorkflowNode('background')
    node.asset_id = 'asset'
    node.asset_version_id = 'version'
    Object.assign(node.config, {
      asset_id: 'asset', asset_version_id: 'version', selected_version_id: 'version',
      transform_base_version_id: 'original', preview_url: '/preview', transform_versions: [{ id: 'local' }],
    })
    const cleared = clearVideoWorkflowImageAssetBinding(node)
    expect(cleared).toMatchObject({ asset_id: undefined, asset_version_id: undefined, status: 'stale' })
    expect(cleared.config).not.toHaveProperty('asset_id')
    expect(cleared.config).not.toHaveProperty('asset_version_id')
    expect(cleared.config).not.toHaveProperty('preview_url')
    expect(cleared.version).toBe(2)
    expect(cleared.config.image_transform).toMatchObject({ rotation: 0, flip_horizontal: false, flip_vertical: false })
  })

  it('keeps current stale state ahead of a historical successful run', () => {
    expect(resolveVideoWorkflowDisplayedStatus('stale', 'succeeded')).toBe('stale')
    expect(resolveVideoWorkflowDisplayedStatus('idle', 'succeeded')).toBe('succeeded')
  })
})
