import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { h, nextTick } from 'vue'

import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const mountedWrappers: VueWrapper[] = []

const mountDialog = (
  props: { show: boolean; title: string; closeOnEscape?: boolean; zIndex?: number },
  slotButtons: Array<{ id: string; label: string; tabindex?: number }> = []
) => {
  const wrapper = mount(BaseDialog, {
    attachTo: document.body,
    props,
    slots: {
      default: () => slotButtons.map((button) => h('button', {
        id: button.id,
        tabindex: button.tabindex
      }, button.label))
    },
    global: { stubs: { Icon: true } }
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

const dispatchTab = (shiftKey = false) => {
  const event = new KeyboardEvent('keydown', {
    key: 'Tab',
    shiftKey,
    bubbles: true,
    cancelable: true
  })
  document.dispatchEvent(event)
  return event
}

describe('BaseDialog focus management', () => {
  afterEach(() => {
    mountedWrappers.splice(0).reverse().forEach((wrapper) => wrapper.unmount())
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('wraps Tab and Shift+Tab inside the top dialog', async () => {
    mountDialog(
      { show: true, title: 'Dialog' },
      [
        { id: 'first-action', label: 'First' },
        { id: 'last-action', label: 'Last' }
      ]
    )
    await nextTick()

    const panel = document.body.querySelector<HTMLElement>('.modal-content')!
    const focusableButtons = Array.from(panel.querySelectorAll<HTMLButtonElement>('button'))
    const firstFocusable = focusableButtons[0]
    const lastFocusable = focusableButtons[focusableButtons.length - 1]

    lastFocusable.focus()
    const forwardEvent = dispatchTab()
    expect(forwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(firstFocusable)

    firstFocusable.focus()
    const backwardEvent = dispatchTab(true)
    expect(backwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(lastFocusable)
  })

  it('focuses the dialog container when no enabled focus target remains', async () => {
    mountDialog(
      { show: true, title: 'No actions' },
      [{ id: 'disabled-action', label: 'Disabled' }]
    )
    await nextTick()

    const panel = document.body.querySelector<HTMLElement>('.modal-content')!
    panel.querySelectorAll<HTMLButtonElement>('button').forEach((button) => {
      button.disabled = true
    })
    panel.focus()

    const event = dispatchTab()
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(panel)
  })

  it('excludes native controls with tabindex -1 from the Tab loop', async () => {
    mountDialog(
      { show: true, title: 'Roving controls' },
      [
        { id: 'active-roving-action', label: 'Active' },
        { id: 'inactive-roving-action', label: 'Inactive', tabindex: -1 }
      ]
    )
    await nextTick()

    const closeButton = document.body.querySelector<HTMLButtonElement>('.modal-header button')!
    expect(closeButton.getAttribute('aria-label')).toBe('common.close')
    const activeAction = document.getElementById('active-roving-action') as HTMLButtonElement
    const inactiveAction = document.getElementById('inactive-roving-action') as HTMLButtonElement
    expect(inactiveAction.tabIndex).toBe(-1)

    activeAction.focus()
    const event = dispatchTab()
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(closeButton)
  })

  it('closes only the top nested dialog and restores focus through the stack', async () => {
    const outsideTrigger = document.createElement('button')
    outsideTrigger.id = 'outside-trigger'
    document.body.appendChild(outsideTrigger)
    outsideTrigger.focus()

    const outerDialog = mountDialog(
      { show: true, title: 'Outer' },
      [{ id: 'open-inner', label: 'Open inner' }]
    )
    await nextTick()

    const innerTrigger = document.getElementById('open-inner') as HTMLButtonElement
    innerTrigger.focus()
    const innerDialog = mountDialog(
      { show: true, title: 'Inner' },
      [{ id: 'inner-action', label: 'Inner action' }]
    )
    await nextTick()

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    )
    expect(innerDialog.emitted('close')).toHaveLength(1)
    expect(outerDialog.emitted('close')).toBeUndefined()

    await innerDialog.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(innerTrigger)

    await outerDialog.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(outsideTrigger)
  })

  it('keeps keyboard control on the highest z-index dialog regardless of activation order', async () => {
    const outsideTrigger = document.createElement('button')
    document.body.appendChild(outsideTrigger)
    outsideTrigger.focus()

    const highDialog = mountDialog(
      { show: true, title: 'High', zIndex: 70 },
      [{ id: 'high-action', label: 'High action' }]
    )
    await nextTick()

    const highAction = document.getElementById('high-action') as HTMLButtonElement
    highAction.focus()
    const lowDialog = mountDialog(
      { show: true, title: 'Low', zIndex: 40 },
      [{ id: 'low-action', label: 'Low action' }]
    )
    await nextTick()

    expect(document.activeElement).toBe(highAction)
    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    )
    expect(highDialog.emitted('close')).toHaveLength(1)
    expect(lowDialog.emitted('close')).toBeUndefined()

    await highDialog.setProps({ show: false })
    await nextTick()
    const lowPanel = Array.from(document.body.querySelectorAll<HTMLElement>('.modal-content'))
      .find((panel) => panel.textContent?.includes('Low'))!
    expect(lowPanel.contains(document.activeElement)).toBe(true)

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    )
    expect(lowDialog.emitted('close')).toHaveLength(1)

    await lowDialog.setProps({ show: false })
    await nextTick()
    expect(document.activeElement).toBe(outsideTrigger)
  })
})
