import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest'
import { nextTick } from 'vue'
import type { VideoWorkflowNode } from '@/api/videoWorkflow'
import VideoWorkflowMediaPreview from './VideoWorkflowMediaPreview.vue'

function imageNode(): VideoWorkflowNode {
  return {
    id: 'background_2',
    type: 'background',
    title: 'S02 图片',
    scene_id: 'scene_2',
    position: { x: 0, y: 0 },
    config: {},
    status: 'succeeded',
  }
}

function videoNode(): VideoWorkflowNode {
  return {
    id: 'video_2',
    type: 'video',
    title: 'S02 视频',
    scene_id: 'scene_2',
    position: { x: 0, y: 0 },
    config: { model: 'wan2.7-r2v' },
    status: 'succeeded',
  }
}

async function mountPreview(props: {
  modelValue?: boolean
  node?: VideoWorkflowNode | null
  src?: string
  loading?: boolean
  error?: string
}) {
  const wrapper = mount(VideoWorkflowMediaPreview, {
    props: {
      modelValue: props.modelValue ?? true,
      node: props.node ?? imageNode(),
      src: props.src ?? '/p/vwf/image-version?sig=fresh',
      loading: props.loading ?? false,
      error: props.error ?? '',
    },
    global: {
      stubs: {
        teleport: true,
        transition: false,
      },
    },
  })
  await nextTick()
  return wrapper
}

describe('VideoWorkflowMediaPreview', () => {
  let wrapper: VueWrapper | null = null
  let pause: MockInstance<[], void>

  beforeEach(() => {
    pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.restoreAllMocks()
    document.body.style.overflow = ''
  })

  it('renders an image in the full-screen dialog with an accessible title', async () => {
    wrapper = await mountPreview({})

    const dialog = wrapper.find('[role="dialog"]')
    const image = wrapper.find('.media-preview-stage img')
    expect(dialog.attributes()).toMatchObject({ 'aria-modal': 'true', tabindex: '-1' })
    expect(wrapper.text()).toContain('S02 图片')
    expect(wrapper.text()).toContain('图片节点')
    expect(image.attributes()).toMatchObject({
      src: '/p/vwf/image-version?sig=fresh',
      alt: 'S02 图片',
    })
    expect(document.body.style.overflow).toBe('hidden')
    expect(wrapper.find('video').exists()).toBe(false)
  })

  it('locks and restores the page when opening through v-model', async () => {
    wrapper = await mountPreview({ modelValue: false })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(document.body.style.overflow).toBe('')

    await wrapper.setProps({ modelValue: true })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    expect(document.body.style.overflow).toBe('hidden')

    await wrapper.setProps({ modelValue: false })
    expect(document.body.style.overflow).toBe('')
  })

  it('zooms images from the full-screen controls and resets to 100%', async () => {
    wrapper = await mountPreview({})
    const zoomIn = wrapper.find('button[aria-label="放大图片"]')
    const reset = wrapper.find('button[aria-label="恢复图片为 100%"]')

    await zoomIn.trigger('click')
    expect(reset.text()).toBe('125%')
    expect(wrapper.find('.media-preview-stage img').attributes('style')).toContain('scale(1.25)')

    await reset.trigger('click')
    expect(reset.text()).toBe('100%')
  })

  it('uses native, inline video controls for generated video playback', async () => {
    wrapper = await mountPreview({
      node: videoNode(),
      src: '/p/vwf/video-version?sig=fresh',
    })

    const video = wrapper.find('video')
    expect(video.exists()).toBe(true)
    expect(video.attributes('src')).toBe('/p/vwf/video-version?sig=fresh')
    expect(video.attributes('controls')).toBeDefined()
    expect(video.attributes('playsinline')).toBeDefined()
    expect(video.attributes('aria-label')).toContain('S02 视频')
    expect(wrapper.text()).toContain('Wan2.7-r2v')
    expect(wrapper.find('img').exists()).toBe(false)
    await video.trigger('loadedmetadata')
    expect(wrapper.find('.media-preview-loading').exists()).toBe(false)
  })

  it('shows signing progress and exposes signing failures through retry', async () => {
    wrapper = await mountPreview({ loading: true, src: '' })
    expect(wrapper.find('[role="status"]').text()).toContain('正在获取最新预览地址')
    expect(wrapper.find('.media-preview-stage img').exists()).toBe(false)

    await wrapper.setProps({ loading: false, error: '预览链接签发失败' })
    const alert = wrapper.find('[role="alert"]')
    expect(alert.text()).toContain('预览链接签发失败')
    await alert.find('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('emits close on Escape and pauses video when the dialog closes', async () => {
    wrapper = await mountPreview({
      node: videoNode(),
      src: '/p/vwf/video-version?sig=fresh',
    })

    document.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    }))
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])

    await wrapper.setProps({ modelValue: false })
    expect(pause).toHaveBeenCalled()
  })

  it('closes from the explicit close control', async () => {
    wrapper = await mountPreview({})
    await wrapper.find('button[aria-label="关闭全屏预览"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })
})
