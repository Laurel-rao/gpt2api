import { mount } from '@vue/test-utils'
import { Position } from '@vue-flow/core'
import { describe, expect, it } from 'vitest'
import VideoWorkflowBezierEdge from './VideoWorkflowBezierEdge.vue'

const baseProps = {
  id: 'edge-1',
  sourceX: 0,
  sourceY: 20,
  sourcePosition: Position.Right,
  targetX: 240,
  targetY: 100,
  targetPosition: Position.Left,
  selected: true,
  zoom: 1,
  data: { curve: { x: 12, y: -8 } },
}

describe('VideoWorkflowBezierEdge', () => {
  it('renders an accessible curve handle only for the selected edge', () => {
    const wrapper = mount(VideoWorkflowBezierEdge, { props: baseProps })
    expect(wrapper.find('.vue-flow__edge-path').attributes('d')).toContain(' C')
    expect(wrapper.find('.curve-handle').attributes()).toMatchObject({
      role: 'button',
      tabindex: '0',
      'aria-label': '调整连线曲线位置',
    })
  })

  it('supports keyboard adjustment and reset', async () => {
    const wrapper = mount(VideoWorkflowBezierEdge, { props: baseProps })
    const handle = wrapper.find('.curve-handle')

    await handle.trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('curve-change')?.[0]).toEqual([{ x: 16, y: -8 }])
    expect(wrapper.emitted('curve-change-start')).toHaveLength(1)
    expect(wrapper.emitted('curve-change-end')?.[0]).toEqual([true])

    await handle.trigger('dblclick')
    expect(wrapper.emitted('curve-change')?.[1]).toEqual([{ x: 0, y: 0 }])
  })
})
