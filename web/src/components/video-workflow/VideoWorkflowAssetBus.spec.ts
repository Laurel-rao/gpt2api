import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import VideoWorkflowAssetBus from './VideoWorkflowAssetBus.vue'

describe('VideoWorkflowAssetBus', () => {
  it('shows shared character counts and connection handles', () => {
    const wrapper = mount(VideoWorkflowAssetBus, {
      props: { sourceCount: 3, targetCount: 4, selected: true },
      global: { stubs: { Handle: true } },
    })

    expect(wrapper.text()).toContain('公共角色资产')
    expect(wrapper.text()).toContain('3 → 4')
    expect(wrapper.find('.asset-bus').classes()).toContain('selected')
    expect(wrapper.findAll('.asset-bus-port')).toHaveLength(2)
    expect(wrapper.find('.asset-bus').attributes('aria-label')).toBe('公共角色资产，3 个角色供 4 个视频引用')
  })
})
