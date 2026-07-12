import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowCanvas from './VideoWorkflowCanvas.vue'

const graphNodes: VideoWorkflowNode[] = [{
  id: 'video_1',
  type: 'video',
  title: 'S01 视频',
  position: { x: 400, y: 180 },
  config: {},
}]

const baseProps = {
  backendAvailable: true,
  conflictDraftKey: '',
  activeTool: 'select' as const,
  modKeyCode: 'Control',
  nodes: [],
  edges: [],
  alignmentGuides: { x: null, y: null },
  quickConnectMenu: null,
  quickConnectTypes: [],
  graphNodes,
  selectedNodeIDs: ['video_1'],
  selectedNodeLocked: false,
  zoom: 1,
  viewport: { x: 0, y: 0, zoom: 1 },
}

describe('VideoWorkflowCanvas', () => {
  it('renders canvas navigation and relays toolbar actions', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, { props: baseProps })
    expect(wrapper.find('.canvas-minimap i').classes()).toContain('selected')
    expect(wrapper.find('.zoom-controls b').text()).toBe('100%')

    await wrapper.find('button[title="抓手（H/空格）"]').trigger('click')
    await wrapper.find('button[title="建立分组（⌘/Ctrl+G）"]').trigger('click')
    await wrapper.find('button[title="删除选中项"]').trigger('click')
    expect(wrapper.emitted('update:activeTool')?.[0]).toEqual(['pan'])
    expect(wrapper.emitted('group-selected')).toHaveLength(1)
    expect(wrapper.emitted('delete-selected')).toHaveLength(1)
  })

  it('keeps recovery and compatible-node creation outside the page shell', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, {
      props: {
        ...baseProps,
        backendAvailable: false,
        conflictDraftKey: 'draft-key',
        quickConnectMenu: { x: 80, y: 120 },
        quickConnectTypes: [{ type: 'video', label: '视频生成' }],
      },
    })
    expect(wrapper.find('.offline-banner').exists()).toBe(true)
    await wrapper.find('.draft-recovery-banner button').trigger('click')
    await wrapper.find('.quick-connect-menu button:not(.cancel)').trigger('click')
    await wrapper.find('.quick-connect-menu button.cancel').trigger('click')
    expect(wrapper.emitted('recover-conflict-draft')).toHaveLength(1)
    expect(wrapper.emitted('create-connected-node')?.[0]).toEqual(['video'])
    expect(wrapper.emitted('cancel-quick-connect')).toHaveLength(1)
  })

  it('navigates the canvas by clicking, dragging and using the minimap keyboard controls', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, { props: baseProps })
    const minimap = wrapper.find('.canvas-minimap')
    expect(minimap.attributes()).toMatchObject({ role: 'button', tabindex: '0', 'aria-label': '画布小地图，点击或拖动定位画布' })
    Object.defineProperty(minimap.element, 'getBoundingClientRect', {
      value: () => ({ left: 10, top: 20, width: 150, height: 92, right: 160, bottom: 112, x: 10, y: 20, toJSON: () => ({}) }),
    })

    await minimap.trigger('pointerdown', { button: 0, pointerId: 1, clientX: 85, clientY: 66 })
    await minimap.trigger('pointermove', { pointerId: 1, clientX: 145, clientY: 100 })
    await minimap.trigger('pointerup', { pointerId: 1, clientX: 145, clientY: 100 })
    await minimap.trigger('pointermove', { pointerId: 1, clientX: 20, clientY: 25 })
    await minimap.trigger('pointerdown', { button: 2, pointerId: 2, clientX: 85, clientY: 66 })
    await minimap.trigger('keydown', { key: 'ArrowRight' })
    await minimap.trigger('keydown', { key: 'Home' })

    const navigation = wrapper.emitted('minimap-navigate') || []
    expect(navigation).toHaveLength(4)
    expect(navigation[0][0]).toMatchObject({ x: 504, y: 236 })
    expect(navigation[1][0]).toMatchObject({ x: 651.2, y: 321.7391304347826 })
    expect((navigation[2][0] as { x: number }).x).toBeCloseTo(36.8)
    expect((navigation[2][0] as { y: number }).y).toBe(0)
    expect(navigation[3][0]).toMatchObject({ x: 504, y: 236 })
  })
})
