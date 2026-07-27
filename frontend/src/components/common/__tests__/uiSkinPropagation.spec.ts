import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref, type Component } from 'vue'

import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { provideUiSkin } from '@/composables/useUiSkin'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const withV2Skin = (component: Component, props: Record<string, unknown>) =>
  defineComponent({
    setup() {
      provideUiSkin(ref<'v2'>('v2'))
      return () => h(component, props)
    }
  })

describe('UI skin propagation through Teleport', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('marks a teleported dialog with the route skin', async () => {
    const dialogRoot = document.createElement('div')
    dialogRoot.id = 'dialog-root'
    document.body.appendChild(dialogRoot)
    const wrapper = mount(withV2Skin(BaseDialog, { show: true, title: 'Dialog' }), {
      attachTo: document.body,
      global: { stubs: { Icon: true } }
    })

    await nextTick()

    expect(document.body.querySelector('.modal-overlay')?.getAttribute('data-ui-skin')).toBe('v2')
    wrapper.unmount()
  })

  it('marks a teleported select dropdown with the route skin', async () => {
    const wrapper = mount(
      withV2Skin(Select, {
        modelValue: null,
        options: [{ value: 'one', label: 'One' }]
      }),
      { attachTo: document.body, global: { stubs: { Icon: true } } }
    )

    await wrapper.get('.select-trigger').trigger('click')
    await nextTick()

    expect(document.body.querySelector('.select-dropdown-portal')?.getAttribute('data-ui-skin')).toBe('v2')
    wrapper.unmount()
  })
})
