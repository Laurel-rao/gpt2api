import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import ImagePreviewDialog from '@/components/ImagePreviewDialog.vue'
import VideoPreviewDialog from '@/components/VideoPreviewDialog.vue'
import VideoWorkflowMediaPreview from './VideoWorkflowMediaPreview.vue'

function node(type: 'background' | 'video'): VideoWorkflowNode {
  return {
    id: type === 'video' ? 'video_2' : 'background_2',
    type,
    title: type === 'video' ? 'S02 视频' : 'S02 图片',
    scene_id: 'scene_2',
    position: { x: 0, y: 0 },
    config: {},
    status: 'succeeded',
  }
}

describe('VideoWorkflowMediaPreview', () => {
  it('图片节点复用电商工作台图片预览组件', () => {
    const wrapper = mount(VideoWorkflowMediaPreview, {
      props: { modelValue: true, node: node('background'), src: '/image.png', loading: true, error: '签发失败' },
      shallow: true,
    })

    const preview = wrapper.findComponent(ImagePreviewDialog)
    expect(preview.exists()).toBe(true)
    expect(preview.props()).toMatchObject({
      modelValue: true,
      src: '/image.png',
      originalSrc: '/image.png',
      title: 'S02 图片',
      loading: true,
      error: '签发失败',
    })
    expect(wrapper.findComponent(VideoPreviewDialog).exists()).toBe(false)
  })

  it('视频节点复用电商工作台视频预览组件', () => {
    const wrapper = mount(VideoWorkflowMediaPreview, {
      props: { modelValue: true, node: node('video'), src: '/video.mp4', error: '签发失败' },
      shallow: true,
    })

    const preview = wrapper.findComponent(VideoPreviewDialog)
    expect(preview.exists()).toBe(true)
    expect(preview.props()).toMatchObject({
      modelValue: true,
      src: '/video.mp4',
      title: 'S02 视频',
      error: '签发失败',
    })
    expect(wrapper.findComponent(ImagePreviewDialog).exists()).toBe(false)
  })

  it.each([
    ['background', ImagePreviewDialog],
    ['video', VideoPreviewDialog],
  ] as const)('透传 %s 共享预览组件的关闭与重试事件', async (type, component) => {
    const wrapper = mount(VideoWorkflowMediaPreview, {
      props: { modelValue: true, node: node(type), src: '' },
      shallow: true,
    })
    const preview = wrapper.findComponent(component)

    await preview.vm.$emit('update:modelValue', false)
    await preview.vm.$emit('retry')

    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    expect(wrapper.emitted('retry')).toEqual([[]])
  })
})
