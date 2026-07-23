import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'

import BaseDialog from '../BaseDialog.vue'
import Select from '../Select.vue'
import selectSource from '../Select.vue?raw'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const mountedWrappers: VueWrapper[] = []

const createRect = (
  left: number,
  top: number,
  width: number,
  height: number
): DOMRect => ({
  x: left,
  y: top,
  left,
  top,
  width,
  height,
  right: left + width,
  bottom: top + height,
  toJSON: () => ({})
}) as DOMRect

const mountSelect = (props: Record<string, unknown>) => {
  const wrapper = mount(Select, {
    attachTo: document.body,
    props: {
      modelValue: null,
      options: [],
      ...props
    },
    global: { stubs: { Icon: true } }
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

describe('Select keyboard accessibility', () => {
  afterEach(() => {
    mountedWrappers.splice(0).reverse().forEach((wrapper) => wrapper.unmount())
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('keeps the search control and its hit area at least 44px on coarse pointers', () => {
    expect(selectSource).toContain('@media (pointer: coarse), (any-pointer: coarse)')
    expect(selectSource).toMatch(/\.select-dropdown-portal \.select-search\s*\{\s*min-height: 44px;\s*padding-block: 0;/)
    expect(selectSource).toMatch(/\.select-dropdown-portal \.select-search-input\s*\{\s*min-height: 44px;/)
  })

  it('operates a non-searchable listbox from the trigger and skips non-options', async () => {
    const wrapper = mountSelect({
      options: [
        { value: 'group', label: 'Group', kind: 'group' },
        { value: 'disabled', label: 'Disabled', disabled: true },
        { value: 'one', label: 'One' },
        { value: 'two', label: 'Two' }
      ]
    })
    const trigger = wrapper.get<HTMLButtonElement>('.select-trigger')
    trigger.element.focus()

    await trigger.trigger('keydown', { key: 'ArrowDown' })

    const listbox = document.body.querySelector<HTMLElement>('[role="listbox"]')
    expect(listbox).not.toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-controls')).toBe(listbox?.id)
    expect(trigger.attributes('aria-activedescendant')).toBe(
      listbox?.querySelectorAll('[role="option"]')[1]?.id
    )
    expect(trigger.attributes('aria-label')).toBe('common.selectOption')

    const endEvent = new KeyboardEvent('keydown', {
      key: 'End',
      bubbles: true,
      cancelable: true
    })
    trigger.element.dispatchEvent(endEvent)
    await nextTick()
    expect(endEvent.defaultPrevented).toBe(true)
    expect(document.getElementById(trigger.attributes('aria-activedescendant'))?.textContent)
      .toContain('Two')

    const homeEvent = new KeyboardEvent('keydown', {
      key: 'Home',
      bubbles: true,
      cancelable: true
    })
    trigger.element.dispatchEvent(homeEvent)
    await nextTick()
    expect(homeEvent.defaultPrevented).toBe(true)
    expect(document.getElementById(trigger.attributes('aria-activedescendant'))?.textContent)
      .toContain('One')

    await trigger.trigger('keydown', { key: 'ArrowDown' })
    const activeOptionId = trigger.attributes('aria-activedescendant')
    expect(document.getElementById(activeOptionId)?.textContent).toContain('Two')

    await trigger.trigger('keydown', { key: 'Enter' })
    await nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['two'])
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('keeps active-descendant on the searchable combobox and restores focus on Escape', async () => {
    const wrapper = mountSelect({
      searchable: true,
      options: [
        { value: 'one', label: 'One' },
        { value: 'two', label: 'Two' }
      ]
    })
    const trigger = wrapper.get<HTMLButtonElement>('.select-trigger')

    await trigger.trigger('click')
    await nextTick()

    const searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')
    const listbox = document.body.querySelector<HTMLElement>('[role="listbox"]')
    expect(searchInput).not.toBeNull()
    expect(document.activeElement).toBe(searchInput)
    expect(searchInput?.getAttribute('aria-label')).toBe('common.searchPlaceholder')
    expect(searchInput?.getAttribute('aria-controls')).toBe(listbox?.id)
    expect(searchInput?.getAttribute('aria-activedescendant')).toBe(
      listbox?.querySelector('[role="option"]')?.id
    )

    searchInput?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })
    )
    await nextTick()
    const activeOptionId = searchInput?.getAttribute('aria-activedescendant') ?? ''
    expect(document.getElementById(activeOptionId)?.textContent).toContain('Two')

    searchInput!.value = 'query'
    searchInput!.setSelectionRange(2, 2)
    const homeEvent = new KeyboardEvent('keydown', {
      key: 'Home',
      bubbles: true,
      cancelable: true
    })
    searchInput?.dispatchEvent(homeEvent)
    expect(homeEvent.defaultPrevented).toBe(false)
    expect(searchInput?.selectionStart).toBe(2)
    expect(searchInput?.getAttribute('aria-activedescendant')).toBe(activeOptionId)

    const endEvent = new KeyboardEvent('keydown', {
      key: 'End',
      bubbles: true,
      cancelable: true
    })
    searchInput?.dispatchEvent(endEvent)
    expect(endEvent.defaultPrevented).toBe(false)
    expect(searchInput?.selectionStart).toBe(2)
    expect(searchInput?.getAttribute('aria-activedescendant')).toBe(activeOptionId)

    searchInput?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    )
    await nextTick()

    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('supports explicit accessible labels without changing default labeling', async () => {
    const plainSelect = mountSelect({
      ariaLabel: 'API group',
      options: [{ value: 'one', label: 'One' }]
    })
    expect(plainSelect.get('.select-trigger').attributes('aria-label')).toBe('API group')

    const externalLabel = document.createElement('span')
    externalLabel.id = 'searchable-select-label'
    externalLabel.textContent = 'Searchable API group'
    document.body.appendChild(externalLabel)
    const searchableSelect = mountSelect({
      searchable: true,
      ariaLabelledby: externalLabel.id,
      options: [{ value: 'one', label: 'One' }]
    })
    const trigger = searchableSelect.get<HTMLButtonElement>('.select-trigger')
    expect(trigger.attributes('aria-labelledby')).toBe(externalLabel.id)
    expect(trigger.attributes('aria-label')).toBeUndefined()

    await trigger.trigger('click')
    await nextTick()
    const searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')
    expect(searchInput?.getAttribute('aria-labelledby')).toBe(externalLabel.id)
    expect(searchInput?.getAttribute('aria-label')).toBeNull()
  })

  it('moves Tab from its teleported search input relative to its owning dialog trigger', async () => {
    const DialogWithSelect = defineComponent({
      setup() {
        return () => h(
          BaseDialog,
          { show: true, title: 'Dialog with Select' },
          {
            default: () => [
              h('button', { id: 'before-select' }, 'Before'),
              h(Select, {
                modelValue: null,
                options: [{ value: 'one', label: 'One' }],
                searchable: true
              }),
              h('button', { id: 'after-select' }, 'After')
            ]
          }
        )
      }
    })
    const wrapper = mount(DialogWithSelect, {
      attachTo: document.body,
      global: { stubs: { Icon: true } }
    })
    mountedWrappers.push(wrapper)
    await nextTick()

    const trigger = document.body.querySelector<HTMLButtonElement>('.select-trigger')!
    const beforeSelect = document.getElementById('before-select') as HTMLButtonElement
    const afterSelect = document.getElementById('after-select') as HTMLButtonElement

    trigger.click()
    await nextTick()
    let searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    expect(searchInput.closest('.select-dropdown-portal')?.getAttribute('data-dialog-focus-owner-id'))
      .toBe(trigger.id)

    const forwardEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    })
    searchInput.dispatchEvent(forwardEvent)
    await nextTick()
    expect(forwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(afterSelect)

    trigger.click()
    await nextTick()
    searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    const backwardEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true
    })
    searchInput.dispatchEvent(backwardEvent)
    await nextTick()
    expect(backwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(beforeSelect)

    const externalButton = document.createElement('button')
    document.body.appendChild(externalButton)
    externalButton.focus()
    const externalTabEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    })
    externalButton.dispatchEvent(externalTabEvent)
    const dialogCloseButton = document.body.querySelector<HTMLButtonElement>('.modal-header button')!
    expect(externalTabEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(dialogCloseButton)
  })

  it('moves Tab from a standalone teleported search input relative to its trigger', async () => {
    const StandaloneSelect = defineComponent({
      setup() {
        return () => h('div', [
          h('button', { id: 'standalone-before' }, 'Before'),
          h(Select, {
            modelValue: null,
            options: [{ value: 'one', label: 'One' }],
            searchable: true
          }),
          h('button', { id: 'standalone-after' }, 'After')
        ])
      }
    })
    const wrapper = mount(StandaloneSelect, {
      attachTo: document.body,
      global: { stubs: { Icon: true } }
    })
    mountedWrappers.push(wrapper)

    const trigger = wrapper.get<HTMLButtonElement>('.select-trigger')
    const beforeSelect = wrapper.get<HTMLButtonElement>('#standalone-before')
    const afterSelect = wrapper.get<HTMLButtonElement>('#standalone-after')

    await trigger.trigger('click')
    await nextTick()
    let searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    const forwardEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    })
    searchInput.dispatchEvent(forwardEvent)
    await nextTick()

    expect(forwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(afterSelect.element)

    await trigger.trigger('click')
    await nextTick()
    searchInput = document.body.querySelector<HTMLInputElement>('.select-search-input')!
    const backwardEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true
    })
    searchInput.dispatchEvent(backwardEvent)
    await nextTick()

    expect(backwardEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(beforeSelect.element)
  })

  it.each([320, 375])(
    'clamps a long teleported dropdown within a %dpx viewport and flips by available space',
    async (viewportWidth) => {
      vi.stubGlobal('innerWidth', viewportWidth)
      vi.stubGlobal('innerHeight', 500)
      let triggerRect = createRect(viewportWidth - 60, 260, 50, 40)
      const wrapper = mountSelect({
        searchable: true,
        options: [{ value: 'long', label: 'A'.repeat(600) }]
      })
      vi.spyOn(wrapper.element, 'getBoundingClientRect').mockImplementation(() => triggerRect)

      await wrapper.get('.select-trigger').trigger('click')
      await nextTick()
      const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')!
      Object.defineProperties(dropdown, {
        offsetWidth: { configurable: true, value: viewportWidth - 32 },
        offsetHeight: { configurable: true, value: 260 },
        scrollHeight: { configurable: true, value: 260 }
      })

      window.dispatchEvent(new Event('resize'))
      await nextTick()
      await nextTick()

      expect(dropdown.style.left).toBe('16px')
      expect(dropdown.style.maxWidth).toBe('calc(100vw - 2rem)')
      expect(dropdown.style.maxHeight).toBe('240px')
      expect(dropdown.style.bottom).toBe('244px')
      expect(dropdown.style.top).toBe('')

      vi.stubGlobal('innerHeight', 800)
      triggerRect = createRect(16, 16, 50, 40)
      window.dispatchEvent(new Event('resize'))
      await nextTick()
      await nextTick()

      expect(dropdown.style.left).toBe('16px')
      expect(dropdown.style.maxHeight).toBe('724px')
      expect(dropdown.style.top).toBe('60px')
      expect(dropdown.style.bottom).toBe('')
    }
  )
})
