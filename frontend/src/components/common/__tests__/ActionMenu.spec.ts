import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'

import ActionMenu from '../ActionMenu.vue'

const mounted: VueWrapper[] = []

describe('ActionMenu', () => {
  afterEach(() => {
    mounted.splice(0).forEach((wrapper) => wrapper.unmount())
    document.body.innerHTML = ''
  })

  it('renders the shared menu shell through body teleport', () => {
    const wrapper = mount(ActionMenu, {
      attachTo: document.body,
      props: { show: true, position: { top: 20, left: 30 } },
      slots: { default: '<button class="menu-action">操作</button>' }
    })
    mounted.push(wrapper)

    const panel = document.body.querySelector('.action-menu-content') as HTMLElement
    expect(panel).not.toBeNull()
    expect(panel.classList.contains('ui-menu')).toBe(true)
    expect(panel.getAttribute('role')).toBe('menu')
    expect(panel.style.top).toBe('20px')
    expect(panel.style.left).toBe('30px')
    expect(panel.querySelector('.menu-action')).not.toBeNull()
  })

  it('closes on backdrop click and Escape', async () => {
    const wrapper = mount(ActionMenu, {
      attachTo: document.body,
      props: { show: true, position: { top: 20, left: 30 } }
    })
    mounted.push(wrapper)

    const backdrop = document.body.querySelector('.ui-action-menu-backdrop') as HTMLElement
    expect(backdrop).not.toBeNull()
    backdrop.click()
    expect(wrapper.emitted('close')).toHaveLength(1)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(2)
  })
})
