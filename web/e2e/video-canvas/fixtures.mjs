const clone = (value) => JSON.parse(JSON.stringify(value))

const textPort = (id, label, type, required = false) => ({ id, label, type, required })

function mediaURL(baseURL, versionID, token = 'fixture-preview') {
  return `${baseURL}/p/vwf/${encodeURIComponent(versionID)}?token=${token}`
}

function createNodes(baseURL) {
  const nodes = [
    {
      id: 'brief', type: 'story_brief', title: '创意简报', position: { x: 40, y: 250 }, status: 'succeeded', enabled: true,
      config: { title: '创意简报', prompt: '雨夜重逢，成年男女主因旧案再会，在四幕冲突中解开误会。' },
      outputs: [textPort('text', '创意简报', 'text')],
    },
    {
      id: 'role_heroine', type: 'character', title: '女主 · 沈昭', role_id: 'heroine', position: { x: 290, y: 20 }, status: 'succeeded', enabled: true,
      config: { title: '女主 · 沈昭', name: '沈昭', adult_age: 24, prompt: '成年古风女主，冷静坚韧，青色披风，正侧背三视图。' },
      inputs: [textPort('brief', '创意简报', 'text', true)], outputs: [textPort('candidates', '候选图', 'image_set'), textPort('selected', '已选定妆', 'image')],
    },
    {
      id: 'role_hero', type: 'character', title: '男主 · 裴砚', role_id: 'hero', position: { x: 290, y: 170 }, status: 'succeeded', enabled: true,
      config: { title: '男主 · 裴砚', name: '裴砚', adult_age: 27, prompt: '成年古风男主，沉稳克制，玄色长袍，正侧背三视图。' },
      inputs: [textPort('brief', '创意简报', 'text', true)], outputs: [textPort('candidates', '候选图', 'image_set'), textPort('selected', '已选定妆', 'image')],
    },
    {
      id: 'role_cousin', type: 'character', title: '表小姐 · 柳棠', role_id: 'cousin', position: { x: 290, y: 320 }, status: 'succeeded', enabled: true,
      config: { title: '表小姐 · 柳棠', name: '柳棠', adult_age: 25, prompt: '成年古风表小姐，温婉警觉，绛红衣裙，正侧背三视图。' },
      inputs: [textPort('brief', '创意简报', 'text', true)], outputs: [textPort('candidates', '候选图', 'image_set'), textPort('selected', '已选定妆', 'image')],
    },
    {
      id: 'script', type: 'script', title: '四幕分镜', position: { x: 540, y: 200 }, status: 'succeeded', enabled: true,
      config: { title: '四幕分镜', scene_count: 4, prompt: '输出四幕结构化分镜；每幕 15 秒，包含镜头、动作、表情、灯光、台词与环境声。' },
      inputs: [
        textPort('brief', '创意简报', 'text', true), textPort('heroine', '女主', 'image', true),
        textPort('hero', '男主', 'image', true), textPort('cousin', '表小姐', 'image', true),
      ],
      outputs: [textPort('script', '结构化剧本', 'script')],
    },
  ]

  const sceneTitles = ['檐下初遇', '雨巷对峙', '旧案揭晓', '灯火重逢']
  for (let index = 1; index <= 4; index += 1) {
    const y = 20 + (index - 1) * 190
    const sceneID = `scene_${index}`
    const imageVersion = index === 2 ? 'img-s02-v2' : `img-s0${index}-v1`
    const imageAsset = index === 2 ? 'asset-s02-image' : `asset-s0${index}-image`
    nodes.push(
      {
        id: sceneID, type: 'scene', title: `S0${index} · ${sceneTitles[index - 1]}`, scene_id: sceneID,
        duration_seconds: 15, position: { x: 800, y }, status: 'succeeded', enabled: true,
        config: { title: `S0${index} · ${sceneTitles[index - 1]}`, index, prompt: `第 ${index} 幕：${sceneTitles[index - 1]}` },
        inputs: [textPort('script', '分镜剧本', 'script', true)], outputs: [textPort('scene', '场景描述', 'scene')],
      },
      {
        id: `background_${index}`, type: 'background', title: `S0${index} 图片`, scene_id: sceneID,
        position: { x: 1040, y }, status: 'succeeded', enabled: true, asset_id: imageAsset, asset_version_id: imageVersion,
        config: {
          title: `S0${index} 图片`, prompt: `电影感古风雨夜，${sceneTitles[index - 1]}，9:16 构图，细腻灯光。`,
          model: '灵境-图像生成 XL v2', asset_id: imageAsset, asset_version_id: imageVersion,
          transform_base_version_id: imageVersion, selected_version_id: imageVersion,
          version_note: index === 2 ? '已修改' : '已就绪',
          preview_url: mediaURL(baseURL, imageVersion),
          image_transform: { crop: { x: 0, y: 0, width: 1, height: 1 }, rotation: 0, flip_horizontal: false, flip_vertical: false },
        },
        inputs: [textPort('environment', '环境（地点/灯光/静物）', 'scene', true)], outputs: [textPort('image', '空镜背景图', 'image')],
      },
      {
        id: `video_${index}`, type: 'video', title: `S0${index} 视频`, scene_id: sceneID, duration_seconds: 15,
        position: { x: 1300, y }, status: index === 2 ? 'stale' : 'succeeded', enabled: true,
        stale_reason: index === 2 ? 'S02 图片已修改，需更新下游视频' : undefined,
        config: {
          title: `S0${index} 视频`, duration_seconds: 15, model: 'wan2.7-r2v',
          prompt: `保持人物一致性，生成 ${sceneTitles[index - 1]} 的 15 秒连续镜头。`,
          preview_url: mediaURL(baseURL, 'video-preview-v1'),
        },
        inputs: [
          textPort('scene', '场景描述', 'scene', true), textPort('background', '空镜背景图', 'image', true),
          textPort('heroine', '女主', 'image', true), textPort('hero', '男主', 'image', true), textPort('cousin', '表小姐', 'image', true),
        ],
        outputs: [textPort('video', '15 秒视频', 'video')],
      },
    )
  }

  const clips = [
    { id: 'clip_1', source_node_id: 'video_1', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15000 },
    { id: 'clip_2', source_node_id: 'video_2', source_port: 'video', trim_in_ms: 1200, trim_out_ms: 12000 },
    { id: 'clip_3', source_node_id: 'video_3', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15000 },
    { id: 'clip_4', source_node_id: 'video_4', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15000 },
  ]
  nodes.push(
    {
      id: 'timeline', type: 'timeline', title: '时间线', position: { x: 1570, y: 240 }, status: 'idle', enabled: true,
      config: { title: '时间线', clips, clip_node_ids: clips.map((clip) => clip.source_node_id) },
      inputs: clips.map((clip, index) => textPort(clip.id, `片段 ${index + 1}`, 'video', true)),
      outputs: [textPort('videos', '片段序列', 'video_list')],
    },
    {
      id: 'compose', type: 'compose', title: '最终成片', position: { x: 1810, y: 240 }, status: 'idle', enabled: true,
      config: { title: '最终成片', codec: 'H.264', fps: 30, audio: 'AAC 48kHz 双声道' },
      inputs: [textPort('videos', '片段序列', 'video_list', true)], outputs: [textPort('video', '最终视频', 'video')],
    },
  )
  return nodes
}

function createEdges() {
  const edges = [
    ['e_brief_heroine', 'brief', 'text', 'role_heroine', 'brief'],
    ['e_brief_hero', 'brief', 'text', 'role_hero', 'brief'],
    ['e_brief_cousin', 'brief', 'text', 'role_cousin', 'brief'],
    ['e_brief_script', 'brief', 'text', 'script', 'brief'],
    ['e_heroine_script', 'role_heroine', 'selected', 'script', 'heroine'],
    ['e_hero_script', 'role_hero', 'selected', 'script', 'hero'],
    ['e_cousin_script', 'role_cousin', 'selected', 'script', 'cousin'],
  ]
  for (let index = 1; index <= 4; index += 1) {
    edges.push(
      [`e_script_scene_${index}`, 'script', 'script', `scene_${index}`, 'script'],
      [`e_scene_bg_${index}`, `scene_${index}`, 'scene', `background_${index}`, 'environment'],
      [`e_scene_video_${index}`, `scene_${index}`, 'scene', `video_${index}`, 'scene'],
      [`e_bg_video_${index}`, `background_${index}`, 'image', `video_${index}`, 'background'],
      [`e_heroine_video_${index}`, 'role_heroine', 'selected', `video_${index}`, 'heroine'],
      [`e_hero_video_${index}`, 'role_hero', 'selected', `video_${index}`, 'hero'],
      [`e_cousin_video_${index}`, 'role_cousin', 'selected', `video_${index}`, 'cousin'],
      [`e_video_timeline_${index}`, `video_${index}`, 'video', 'timeline', `clip_${index}`],
    )
  }
  edges.push(['e_timeline_compose', 'timeline', 'videos', 'compose', 'videos'])
  return edges.map(([id, source, source_port, target, target_port]) => ({ id, source, source_port, target, target_port }))
}

function createGroups() {
  return Array.from({ length: 4 }, (_, offset) => {
    const index = offset + 1
    return {
      id: `group_scene_${index}`, type: 'scene', scene_id: `scene_${index}`, enabled: true, duration_seconds: 15,
      node_ids: [`scene_${index}`, `background_${index}`, `video_${index}`],
      position: { x: 770, y: offset * 190 }, size: { width: 770, height: 176 }, collapsed: false,
    }
  })
}

export function createGraph(baseURL) {
  return {
    schema_version: 2,
    settings: {
      aspect_ratio: '9:16', resolution: '1080p', fps: 30, scene_duration_ms: 15000,
      character_approval_policy: 'auto_first', storyboard_approval_policy: 'auto',
      text_model: 'default', image_model: 'gpt-image-2', video_model: 'wan2.7-r2v',
    },
    nodes: createNodes(baseURL), edges: createEdges(), groups: createGroups(),
  }
}

function imageAsset(baseURL, index) {
  const scene = String(index).padStart(2, '0')
  const assetID = index === 2 ? 'asset-s02-image' : `asset-s${scene}-image`
  const currentID = index === 2 ? 'img-s02-v2' : `img-s${scene}-v1`
  const versions = [{
    id: `img-s${scene}-v1`, version: 1, asset_id: assetID, status: 'ready', mime: 'image/svg+xml', size_bytes: 4096,
    width: 1080, height: 1920, source: 'generated', preview_url: mediaURL(baseURL, `img-s${scene}-v1`), created_at: '2026-07-11T09:00:00Z',
  }]
  if (index === 2) {
    versions.push({
      id: 'img-s02-v2', version: 2, asset_id: assetID, status: 'ready', mime: 'image/svg+xml', size_bytes: 4230,
      width: 1080, height: 1920, source: 'transform', parent_version_id: 'img-s02-v1',
      image_transform: { crop: { x: 0.04, y: 0.02, width: 0.92, height: 0.94 }, rotation: 0, flip_horizontal: false, flip_vertical: false },
      metadata: { image_transform: { crop: { x: 0.04, y: 0.02, width: 0.92, height: 0.94 }, rotation: 0, flip_horizontal: false, flip_vertical: false } },
      preview_url: mediaURL(baseURL, 'img-s02-v2'), created_at: '2026-07-11T09:12:00Z',
    })
  }
  return {
    id: assetID, name: `S${scene} 场景图片${index === 2 ? ' · 已修改' : ''}`, kind: 'image', status: 'ready',
    current_version_id: currentID, current_version: versions.length, versions,
    preview_url: mediaURL(baseURL, currentID), created_at: '2026-07-11T09:00:00Z',
  }
}

export function createFixtureState(baseURL) {
  const graph = createGraph(baseURL)
  const workflow = {
    id: 'wf-rain-reunion', name: '雨夜重逢 · 60秒短剧', template_id: 'ancient-drama-v3', template_version: 3,
    revision: 12, graph, latest_run: null, created_at: '2026-07-11T08:30:00Z', updated_at: '2026-07-11T09:12:00Z',
  }
  const template = {
    id: 'ancient-drama-v3', code: 'ancient_drama_seedance', version: 3, name: '古风四幕 · Wan2.7 · 19 节点',
    description: '三人定妆、四幕分镜、四张场景图、Wan2.7 四段视频、单轨裁剪与最终成片。', graph: clone(graph),
  }
  const assets = [1, 2, 3, 4].map((index) => imageAsset(baseURL, index))
  assets.push({
    id: 'asset-scene-video', name: 'S01 已就绪视频', kind: 'video', status: 'ready', current_version_id: 'video-preview-v1', current_version: 1,
    preview_url: mediaURL(baseURL, 'video-preview-v1'), versions: [{
      id: 'video-preview-v1', version: 1, asset_id: 'asset-scene-video', status: 'ready', mime: 'video/mp4', size_bytes: 1,
      width: 360, height: 640, duration_ms: 2000, source: 'generated', preview_url: mediaURL(baseURL, 'video-preview-v1'),
    }],
  })
  assets.push({
    id: 'asset-final', name: '雨夜重逢 · 最终成片', kind: 'video', status: 'ready', current_version_id: 'video-final-v1', current_version: 1,
    preview_url: mediaURL(baseURL, 'video-final-v1'), versions: [{
      id: 'video-final-v1', version: 1, asset_id: 'asset-final', status: 'ready', mime: 'video/mp4', size_bytes: 1,
      width: 1080, height: 1920, duration_ms: 2000, source: 'generated', preview_url: mediaURL(baseURL, 'video-final-v1'),
    }],
  })
  const nodeOutput = (node, index) => {
    const sceneIndex = Number(node.id.match(/_(\d+)$/)?.[1] || 1)
    if (node.type === 'story_brief') return { prompt: '雨夜重逢，成年男女主因旧案再会，在四幕冲突中解开误会。' }
    if (node.type === 'character') return { selected_version_id: `character-version-${index + 1}` }
    if (node.type === 'script') return {
      scenes: [1, 2, 3, 4].map((scene) => ({ index: scene, summary: `第 ${scene} 幕结构化分镜` })),
    }
    if (node.type === 'scene') return {
      summary: `S${String(sceneIndex).padStart(2, '0')} 雨夜场景`, location: '古城雨巷', time: '夜晚', duration: 15,
      camera: { shot: '中景', movement: '缓慢推进' }, action: '人物在雨中对峙', dialogue: '旧案真相逐渐揭晓',
      expression: '克制而警觉', lighting: '灯笼暖光与冷雨反光', audio: '雨声、脚步声',
      image_prompt: `电影感古风雨夜场景 ${sceneIndex}，9:16 构图`,
      video_prompt: `保持人物一致性，生成场景 ${sceneIndex} 的 15 秒连续镜头`,
    }
    if (node.type === 'background') return { version_id: `img-s${String(sceneIndex).padStart(2, '0')}-v${sceneIndex === 2 ? 2 : 1}` }
    if (node.type === 'video') return { version_id: 'video-preview-v1', duration_ms: 15_000 }
    if (node.type === 'timeline') return { clips: node.config.clips }
    if (node.type === 'compose') return { version_id: 'video-final-v1', duration_ms: 55_800, width: 1080, height: 1920 }
    return {}
  }
  const succeededRun = {
    id: 'run-history-success', workflow_id: workflow.id, workflow_revision: 11, status: 'succeeded', progress: 100,
    run_mode: 'full', estimated_credits: 1360, actual_credits: 1280, output_version_id: 'video-final-v1',
    output_asset_version_id: 'video-final-v1', output_url: mediaURL(baseURL, 'video-final-v1'),
    output: { id: 'video-final-v1', version: 1, asset_id: 'asset-final', status: 'ready', mime: 'video/mp4', size_bytes: 1 },
    graph_snapshot: clone(graph),
    node_runs: graph.nodes.map((node, index) => ({
      id: `history-success-node-${index + 1}`, node_id: node.id, node_type: node.type, status: 'succeeded', progress: 100,
      output_version_id: ['background', 'video', 'compose'].includes(node.type) ? nodeOutput(node, index).version_id : undefined,
      output: nodeOutput(node, index), credit_cost: 0, cache_hit: false, attempt: 1,
    })),
    created_at: '2026-07-11T11:03:00Z', started_at: '2026-07-11T11:03:05Z', finished_at: '2026-07-11T11:05:18Z', updated_at: '2026-07-11T11:05:18Z',
  }
  const failedRun = {
    id: 'run-history-failed', workflow_id: workflow.id, workflow_revision: 10, status: 'failed', progress: 61,
    run_mode: 'downstream', start_node_id: 'background_2', estimated_credits: 620, actual_credits: 280,
    error_code: 'UPSTREAM_VIDEO_FAILED', error_message: 'S02 视频生成失败：上游服务暂时不可用', graph_snapshot: clone(graph),
    node_runs: graph.nodes.map((node, index) => ({
      id: `history-failed-node-${index + 1}`, node_id: node.id, node_type: node.type,
      status: node.id === 'video_2' ? 'failed' : index < 10 ? 'succeeded' : 'canceled', progress: node.id === 'video_2' ? 42 : index < 10 ? 100 : 0,
      error_code: node.id === 'video_2' ? 'UPSTREAM_VIDEO_FAILED' : undefined,
      error_message: node.id === 'video_2' ? '上游服务暂时不可用' : undefined, credit_cost: 0, cache_hit: false,
      output: node.id === 'video_2' ? undefined : nodeOutput(node, index), attempt: 1,
    })),
    created_at: '2026-07-11T10:40:00Z', started_at: '2026-07-11T10:40:04Z', finished_at: '2026-07-11T10:42:31Z', updated_at: '2026-07-11T10:42:31Z',
  }
  return {
    templates: [template], workflows: [workflow], assets, runs: new Map([[succeededRun.id, succeededRun], [failedRun.id, failedRun]]), requestRuns: new Map(), estimates: new Map(),
    runCreationCount: 0, uploadCount: 0, transformCount: 0, mediaTokens: new Map([['fixture-preview', { purpose: 'preview' }]]),
  }
}

export function cloneFixture(value) {
  return clone(value)
}
