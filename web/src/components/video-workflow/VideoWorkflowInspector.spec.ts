import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoAsset, VideoWorkflowNode } from '@/api/videoWorkflow'
import { resolveDefaultInspectorTab } from '@/utils/videoWorkflowInspectorTabs'
import VideoWorkflowInspector from './VideoWorkflowInspector.vue'

const node: VideoWorkflowNode = {
  id: 'background_2',
  type: 'background',
  title: 'S02 图片',
  position: { x: 0, y: 0 },
  asset_id: 'asset_1',
  config: {
    prompt: '雨夜，城市街道，湿润反光',
    asset_id: 'asset_1',
    asset_version_id: 'version_2',
    image_transform: {
      crop: { x: 0, y: 0, width: 1, height: 1 },
      rotation: 0,
      flip_horizontal: false,
      flip_vertical: false,
    },
  },
  inputs: [{ id: 'environment', label: '环境（地点/灯光/静物）', type: 'scene' }],
  outputs: [{ id: 'image', label: '空镜背景图', type: 'image' }],
  status: 'succeeded',
}
const assets: VideoAsset[] = [{
  id: 'asset_1',
  name: '雨夜场景',
  kind: 'image',
  current_version_id: 'version_2',
  versions: [
    { id: 'version_1', version: 1, mime: 'image/png', size_bytes: 10 },
    { id: 'version_2', version: 2, mime: 'image/png', size_bytes: 12 },
  ],
}]

async function openSettingsTab(wrapper: ReturnType<typeof mount>) {
  await wrapper.findAll('.inspector-tabs button')[4].trigger('click')
}

async function openUpstreamTab(wrapper: ReturnType<typeof mount>) {
  await wrapper.findAll('.inspector-tabs button')[0].trigger('click')
}

describe('resolveDefaultInspectorTab', () => {
  it('picks output for media, settings for timeline/text, and status for failures', () => {
    expect(resolveDefaultInspectorTab({ type: 'background', status: 'succeeded' })).toBe('output')
    expect(resolveDefaultInspectorTab({ type: 'image', status: 'idle' })).toBe('output')
    expect(resolveDefaultInspectorTab({ type: 'video', status: 'succeeded' })).toBe('output')
    expect(resolveDefaultInspectorTab({ type: 'timeline', status: 'idle' })).toBe('settings')
    expect(resolveDefaultInspectorTab({ type: 'script', status: 'idle' })).toBe('settings')
    expect(resolveDefaultInspectorTab({ type: 'background', status: 'failed' })).toBe('status')
    expect(resolveDefaultInspectorTab({ type: 'script', status: 'idle', run_error: '超时' })).toBe('status')
  })
})

describe('VideoWorkflowInspector', () => {
  it('defaults media nodes to the output tab and keeps a manual tab until the node changes', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('输出')
    expect(wrapper.classes()).toContain('has-pinned-preview')
    expect(wrapper.find('.inspector-scroll').exists()).toBe(true)
    expect(wrapper.find('.inspector-media-preview .image-preview').exists()).toBe(true)

    await wrapper.findAll('.inspector-tabs button')[4].trigger('click')
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('参数')

    await wrapper.setProps({
      node: { ...node, status: 'failed', run_error: '失败' },
    })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('参数')

    await wrapper.setProps({
      node: {
        id: 'script_1',
        type: 'script',
        title: '剧本',
        position: { x: 0, y: 0 },
        config: { prompt: '开场' },
      },
    })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('参数')
    expect(wrapper.classes()).not.toContain('has-pinned-preview')
  })

  it('defaults failed nodes to the status tab', () => {
    const failedNode: VideoWorkflowNode = { ...node, status: 'failed', run_error: '上游超时' }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: failedNode, assets } })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('状态')
  })

  it('defaults timeline nodes to settings', () => {
    const timelineNode: VideoWorkflowNode = {
      id: 'timeline',
      type: 'timeline',
      title: '顺序时间线',
      position: { x: 0, y: 0 },
      config: { clips: [] },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: timelineNode, assets: [] } })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('参数')
  })

  it('shows immutable versions and emits version selection', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    const version = wrapper.find('#image-version')
    expect(version.findAll('option')).toHaveLength(2)
    await version.setValue('version_1')
    expect(wrapper.emitted('select-version')?.[0]).toEqual(['version_1'])
  })

  it('emits rotate, flip and crop transforms', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    await openSettingsTab(wrapper)
    const buttons = wrapper.findAll('.transform-grid button')
    await buttons[2].trigger('click')
    await buttons[3].trigger('click')
    expect(wrapper.emitted('transform')?.[0]).toEqual([{ rotation_delta: 90 }])
    expect(wrapper.emitted('transform')?.[1]).toEqual([{ flip_horizontal_toggle: true }])

    await buttons[0].trigger('click')
    const cropInputs = wrapper.findAll('.crop-editor input')
    await cropInputs[0].setValue('10')
    await cropInputs[2].setValue('80')
    await wrapper.find('.crop-editor button').trigger('click')
    expect(wrapper.emitted('transform')?.at(-1)?.[0]).toMatchObject({ crop: { x: .1, width: .8 } })
  })

  it('provides a keyboard-accessible connection action', async () => {
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node,
        assets,
        upstreams: [{ id: 'environment', label: '环境（地点/灯光/静物）', type: 'scene', required: true, sources: [] }],
      },
    })
    await openUpstreamTab(wrapper)
    await wrapper.find('.unconnected button').trigger('click')
    expect(wrapper.emitted('add-connection')?.[0]).toEqual(['environment'])
  })

  it('allows closing the empty inspector to reclaim workspace width', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node: null, assets } })
    expect(wrapper.text()).toContain('未选择节点')
    await wrapper.find('button[aria-label="关闭检查器"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('shows the Wan2.7 model label for video nodes', async () => {
    const videoNode: VideoWorkflowNode = {
      id: 'video_1',
      type: 'video',
      title: 'S01 视频',
      position: { x: 0, y: 0 },
      config: { model: 'wan2.7-r2v' },
    }
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node: videoNode,
        assets: [],
        modelValue: 'wan2.7-r2v',
        modelOptions: [{ value: 'wan2.7-r2v', label: 'Wan2.7-r2v' }],
      },
    })
    await openSettingsTab(wrapper)
    expect((wrapper.find('#node-model').element as HTMLSelectElement).value).toBe('wan2.7-r2v')
    expect(wrapper.find('#node-model option').text()).toBe('Wan2.7-r2v')
  })

  it('uses the same five information tabs for every node', () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    expect(wrapper.findAll('.inspector-tabs button').map((item) => item.text())).toEqual(['上游', '状态', '历史', '输出', '参数'])
    expect(wrapper.findAll('.inspector-panel')).toHaveLength(5)
  })

  it('loads real node history on demand and renders revision metadata', async () => {
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node,
        assets,
        history: [{
          run_id: 'run-1', workflow_revision: 12, run_status: 'succeeded', run_created_at: '2026-07-13T08:00:00Z',
          node_run: {
            id: 'node-run-1', node_id: node.id, status: 'succeeded', attempt: 2, credit_cost: 8, cache_hit: false,
            output: { summary: '历史雨巷画面' },
          },
        }],
      },
    })
    await wrapper.findAll('.inspector-tabs button')[2].trigger('click')
    expect(wrapper.emitted('request-history')).toHaveLength(1)
    expect(wrapper.find('.node-history-list').text()).toContain('R12')
    expect(wrapper.find('.node-history-list').text()).toContain('尝试 2')
    await wrapper.find('.history-output-button').trigger('click')
    expect(wrapper.find('.output-panel').text()).toContain('历史 R12')
    expect(wrapper.find('.output-panel').text()).toContain('历史雨巷画面')
  })

  it('shows structured node output and emits model changes', async () => {
    const outputNode: VideoWorkflowNode = {
      ...node,
      output: { summary: '雨巷对峙', image_prompt: '电影感雨夜街道', camera: { shot: '中景' } },
    }
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node: outputNode,
        assets,
        modelValue: 'gpt-image-2',
        modelOptions: [
          { value: 'gpt-image-2', label: 'SD-Turbo (本地)' },
          { value: 'image-next', label: 'Image Next' },
        ],
      },
    })
    expect(wrapper.find('.output-fields').text()).toContain('雨巷对峙')
    expect(wrapper.find('.output-fields').text()).toContain('图片提示词')
    await openSettingsTab(wrapper)
    await wrapper.find('#node-model').setValue('image-next')
    expect(wrapper.emitted('update-model')?.[0]).toEqual(['image-next'])
  })

  it('renders compose output as media preview instead of a signed URL field', async () => {
    const composeNode: VideoWorkflowNode = {
      id: 'compose',
      type: 'compose',
      title: '成片输出',
      position: { x: 0, y: 0 },
      config: {},
      outputs: [{ id: 'video', label: 'video', type: 'video' }],
      output: {
        output_url: '/p/vwf/vwv_final?purpose=preview&sig=poster',
        duration_ms: 40800,
        width: 1080,
        height: 1920,
      },
    }
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node: composeNode,
        assets: [],
        composePlaybackUrl: '/p/vwf/vwv_final?purpose=seedance&sig=video',
      },
    })
    await wrapper.findAll('.inspector-tabs button')[3].trigger('click')

    expect(wrapper.find('.compose-output-player video').attributes('src')).toBe('/p/vwf/vwv_final?purpose=seedance&sig=video')
    expect(wrapper.find('.compose-output-player video').attributes('poster')).toBe('/p/vwf/vwv_final?purpose=preview&sig=poster')
    const fieldRows = wrapper.findAll('.output-fields > div').map((item) => item.text()).join('|')
    expect(fieldRows).not.toContain('output_url')
    expect(fieldRows).toContain('输出时长')
  })

  it('never reapplies metadata transforms to a server-baked image version', () => {
    const transformedNode: VideoWorkflowNode = {
      ...node,
      config: {
        ...node.config,
        preview_url: '/p/vwf/version_2',
        image_transform: {
          crop: { x: .1, y: .1, width: .8, height: .8 },
          rotation: 90,
          flip_horizontal: true,
          flip_vertical: false,
        },
      },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: transformedNode, assets } })
    expect(wrapper.find('.image-preview img').attributes('style')).toBeUndefined()
  })

  it('opens the selected image in the full-screen media preview', async () => {
    const previewNode: VideoWorkflowNode = {
      ...node,
      config: { ...node.config, preview_url: '/p/vwf/version_2' },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: previewNode, assets } })
    const button = wrapper.find('.inspector-preview-button')
    expect(button.attributes('aria-label')).toBe('全屏预览：S02 图片')
    await button.trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([previewNode])
  })

  it('keeps preview-button keyboard events away from canvas shortcuts', async () => {
    const previewNode: VideoWorkflowNode = {
      ...node,
      config: { ...node.config, preview_url: '/p/vwf/version_2' },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: previewNode, assets } })
    let keydowns = 0
    let keyups = 0
    wrapper.element.addEventListener('keydown', () => { keydowns += 1 })
    wrapper.element.addEventListener('keyup', () => { keyups += 1 })

    const button = wrapper.find('.inspector-preview-button')
    await button.trigger('keydown', { key: ' ' })
    await button.trigger('keyup', { key: ' ' })
    expect(keydowns).toBe(0)
    expect(keyups).toBe(0)
  })

  it('can request a signed preview for an asset without a cached thumbnail URL', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    expect(wrapper.find('.image-preview img').exists()).toBe(false)
    await wrapper.find('.inspector-preview-button').trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([node])
  })

  it('shows a playable video thumbnail and relays its full-screen preview request', async () => {
    const videoNode: VideoWorkflowNode = {
      id: 'video_1',
      type: 'video',
      title: 'S01 视频',
      position: { x: 0, y: 0 },
      config: { model: 'wan2.7-r2v', preview_url: '/poster.jpg' },
      output: { preview_url: '/generated.mp4' },
    }
    const wrapper = mount(VideoWorkflowInspector, { props: { node: videoNode, assets: [] } })
    expect(wrapper.find('.image-preview video').attributes('src')).toBe('/generated.mp4')
    expect(wrapper.find('.inspector-preview-button').classes()).toContain('video')
    await wrapper.find('.inspector-preview-button').trigger('click')
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([videoNode])
  })

  it('keeps regeneration separate from running an already-bound image node', async () => {
    const wrapper = mount(VideoWorkflowInspector, { props: { node, assets } })
    await openSettingsTab(wrapper)
    await wrapper.find('.action-grid button').trigger('click')
    expect(wrapper.emitted('regenerate')).toHaveLength(1)
    expect(wrapper.emitted('run')).toBeUndefined()
  })

  it('separates stale reason from translated run errors', async () => {
    const failedNode: VideoWorkflowNode = {
      ...node,
      status: 'failed',
      stale_reason: '上游输入已修改，请重新运行',
      run_error: '上游超时\n请稍后重试',
      run_error_code: 'runtime_failed',
    }
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node: failedNode,
        assets,
        latestRun: {
          id: 'node-run',
          node_id: failedNode.id,
          status: 'failed',
          error_code: 'runtime_failed',
          error_message: '上游超时\n请稍后重试',
        },
      },
    })
    expect(wrapper.find('.inspector-tabs button.active').text()).toContain('状态')
    expect(wrapper.find('.status-message').text()).toContain('上游输入已修改')
    expect(wrapper.find('.run-error b').text()).toContain('工作流运行失败')
    expect(wrapper.find('.run-error span').text()).toContain('上游超时')
  })

  it('emits timeline clip updates from the embedded editor', async () => {
    const timelineNode: VideoWorkflowNode = {
      id: 'timeline',
      type: 'timeline',
      title: '顺序时间线',
      position: { x: 0, y: 0 },
      config: {
        clips: [
          { id: 'clip_1', source_node_id: 'video_1', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15_000 },
          { id: 'clip_2', source_node_id: 'video_2', source_port: 'video', trim_in_ms: 0, trim_out_ms: 15_000 },
        ],
      },
    }
    const wrapper = mount(VideoWorkflowInspector, {
      props: {
        node: timelineNode,
        assets: [],
        timelineSourceTitles: { video_1: 'S01 视频', video_2: 'S02 视频' },
      },
    })
    expect(wrapper.text()).toContain('S01 视频')
    await wrapper.get('[aria-label="下移片段 1"]').trigger('click')
    const emittedClips = wrapper.emitted('update-timeline-clips')?.[0]?.[0] as Array<{ id: string }>
    expect(emittedClips.map((clip) => clip.id)).toEqual(['clip_2', 'clip_1'])
  })
})
