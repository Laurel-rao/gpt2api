import { shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowCanvas from './VideoWorkflowCanvas.vue'
import VideoWorkflowNodeCard from './VideoWorkflowNodeCard.vue'

const graphNodes: VideoWorkflowNode[] = [{
  id: 'video_1',
  type: 'video',
  title: 'S01 视频',
  position: { x: 400, y: 180 },
  config: {},
}]

const baseProps = {
  backendAvailable: true,
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
  edgeDisplayMode: 'smart' as const,
  layoutBusy: false,
  autoArrange: false,
  canUndo: false,
  canRedo: false,
  canOperateWorkflow: true,
  zoom: 1,
  viewport: { x: 0, y: 0, zoom: 1 },
}

const VueFlowWithNodeSlot = defineComponent({
  props: { nodes: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    return () => h('div', { class: 'vue-flow-slot-stub' }, slots['node-workflow']?.({
      data: (props.nodes[0] as any)?.data,
      selected: true,
    }))
  },
})

const SlotStub = defineComponent({
  setup(_, { slots }) { return () => h('div', slots.default?.()) },
})
const dropdownStubs = {
  'el-dropdown': defineComponent({
    emits: ['command'],
    setup(_, { slots, emit }) {
      return () => h('div', { class: 'el-dropdown-stub' }, [
        slots.default?.(),
        h('div', { class: 'dropdown-menu-stub' }, slots.dropdown?.()),
      ])
    },
  }),
  'el-dropdown-menu': SlotStub,
  'el-dropdown-item': SlotStub,
  'el-tooltip': defineComponent({
    setup(_, { slots }) { return () => slots.default?.() ?? null },
  }),
}

describe('VideoWorkflowCanvas', () => {
  it('renders canvas navigation and relays toolbar actions', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, { props: baseProps, global: { stubs: dropdownStubs } })
    expect(wrapper.find('.canvas-minimap i').classes()).toContain('selected')
    expect(wrapper.find('.zoom-controls b').text()).toBe('100%')
    expect(wrapper.find('.canvas-nav-stack .canvas-minimap').exists()).toBe(true)
    expect(wrapper.find('.canvas-nav-stack .zoom-controls').exists()).toBe(true)
    expect(wrapper.find('.edge-display-controls').exists()).toBe(false)
    expect(wrapper.find('.draft-recovery-banner').exists()).toBe(false)

    await wrapper.find('button[aria-label="抓手"]').trigger('click')
    await wrapper.find('button[aria-label="删除选中项"]').trigger('click')
    expect(wrapper.emitted('update:activeTool')?.[0]).toEqual(['pan'])
    expect(wrapper.emitted('delete-selected')).toHaveLength(1)

    await wrapper.setProps({ canUndo: true, canRedo: true })
    await wrapper.find('button[aria-label="撤销"]').trigger('click')
    await wrapper.find('button[aria-label="重做"]').trigger('click')
    expect(wrapper.emitted('undo')).toHaveLength(1)
    expect(wrapper.emitted('redo')).toHaveLength(1)

    const layoutButton = wrapper.find('button[aria-label="整理画布"]')
    expect(layoutButton.exists()).toBe(true)
    expect(layoutButton.attributes('title')).toContain('整理画布')
    expect(wrapper.find('.canvas-layout-host').exists()).toBe(false)

    await wrapper.find('.canvas-tools button[aria-label="显示全部连线"]').trigger('click')
    await wrapper.find('.canvas-tools button[aria-label="隐藏非相关连线"]').trigger('click')
    expect(wrapper.emitted('update:edgeDisplayMode')).toEqual([['all'], ['hidden']])
  })

  it('keeps offline and compatible-node creation outside the page shell', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, {
      props: {
        ...baseProps,
        backendAvailable: false,
        quickConnectMenu: { x: 80, y: 120 },
        quickConnectTypes: [{ type: 'video', label: '视频生成' }],
      },
      global: { stubs: dropdownStubs },
    })
    expect(wrapper.find('.offline-banner').exists()).toBe(true)
    expect(wrapper.find('.draft-recovery-banner').exists()).toBe(false)
    await wrapper.find('.quick-connect-menu button:not(.cancel)').trigger('click')
    await wrapper.find('.quick-connect-menu button.cancel').trigger('click')
    expect(wrapper.emitted('create-connected-node')?.[0]).toEqual(['video'])
    expect(wrapper.emitted('cancel-quick-connect')).toHaveLength(1)
  })

  it('navigates the canvas by clicking, dragging and using the minimap keyboard controls', async () => {
    const wrapper = shallowMount(VideoWorkflowCanvas, { props: baseProps, global: { stubs: dropdownStubs } })
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

  it('relays media preview requests from node cards', async () => {
    const previewNode: VideoWorkflowNode = {
      ...graphNodes[0],
      output: { preview_url: '/generated.mp4' },
    }
    const wrapper = shallowMount(VideoWorkflowCanvas, {
      props: {
        ...baseProps,
        nodes: [{
          id: previewNode.id,
          type: 'workflow',
          position: previewNode.position,
          data: { node: previewNode },
        }],
      },
      global: { stubs: { ...dropdownStubs, VueFlow: VueFlowWithNodeSlot } },
    })

    wrapper.findComponent(VideoWorkflowNodeCard).vm.$emit('preview-media', previewNode)
    expect(wrapper.emitted('preview-media')?.[0]).toEqual([previewNode])
  })
})
