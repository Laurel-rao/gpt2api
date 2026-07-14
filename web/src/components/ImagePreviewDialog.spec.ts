import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ImagePreviewDialog from './ImagePreviewDialog.vue'

const dialogStub = { template: '<section><slot /></section>' }
const tooltipStub = { template: '<span><slot /></span>' }
const buttonStub = {
  emits: ['click'],
  template: '<button type="button" @click="$emit(\'click\')"><slot /></button>',
}
const loadingDirective = { mounted() {}, updated() {} }

describe('ImagePreviewDialog', () => {
  it('原图地址与当前预览相同时不重新进入加载状态', async () => {
    const wrapper = mount(ImagePreviewDialog, {
      props: {
        modelValue: true,
        src: '/assets/image.png',
        originalSrc: '/assets/image.png',
      },
      global: {
        directives: { loading: loadingDirective },
        stubs: {
          ElButton: buttonStub,
          ElDialog: dialogStub,
          ElTooltip: tooltipStub,
          ElEmpty: true,
        },
      },
    })

    await wrapper.find('img').trigger('load')
    await wrapper.get('button').trigger('click')

    expect(wrapper.get('.image-preview-stage').attributes('aria-busy')).toBe('false')
  })

  it('切换到不同原图地址时保持加载状态直至图片完成', async () => {
    const wrapper = mount(ImagePreviewDialog, {
      props: {
        modelValue: true,
        src: '/assets/preview.png',
        originalSrc: '/assets/original.png',
      },
      global: {
        directives: { loading: loadingDirective },
        stubs: {
          ElButton: buttonStub,
          ElDialog: dialogStub,
          ElTooltip: tooltipStub,
          ElEmpty: true,
        },
      },
    })

    await wrapper.get('button').trigger('click')
    expect(wrapper.get('.image-preview-stage').attributes('aria-busy')).toBe('true')

    await wrapper.find('img').trigger('load')
    expect(wrapper.get('.image-preview-stage').attributes('aria-busy')).toBe('false')
  })
})
