import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowNodeCard from './VideoWorkflowNodeCard.vue'

function imageNode(): VideoWorkflowNode {
  return {
    id: 'background_2',
    type: 'background',
    title: 'S02 图片',
    position: { x: 0, y: 0 },
    config: {
      preview_url: '/p/vwf/transformed-version',
      image_transform: {
        crop: { x: .1, y: .1, width: .8, height: .8 },
        rotation: 90,
        flip_horizontal: false,
        flip_vertical: true,
      },
    },
  }
}

describe('VideoWorkflowNodeCard image preview', () => {
  it('does not transform an image version already baked by the server', () => {
    const wrapper = mount(VideoWorkflowNodeCard, { props: { node: imageNode() } })
    expect(wrapper.find('.node-media img').attributes('style')).toBeUndefined()
  })
})
