import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'

import DataTable from '../DataTable.vue'
import { provideUiSkin, type UiSkin } from '@/composables/useUiSkin'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const columns = [
  { key: 'name', label: 'Name', sortable: true },
  { key: 'status', label: 'Status' }
]
const data = [
  { id: 1, name: 'Beta', status: 'active' },
  { id: 2, name: 'Alpha', status: 'inactive' }
]

const mountedWrappers: VueWrapper[] = []

const createMediaQueryList = (matches: boolean): MediaQueryList => ({
  matches,
  media: '(min-width: 768px)',
  onchange: null,
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  addListener: vi.fn(),
  removeListener: vi.fn(),
  dispatchEvent: vi.fn(() => true)
})

const mountTable = (skin?: UiSkin, propsOverride: Record<string, unknown> = {}) => {
  const component = skin
    ? defineComponent({
        setup() {
          provideUiSkin(ref(skin))
          return () => h(DataTable, { columns, data, serverSideSort: true, ...propsOverride })
        }
      })
    : DataTable
  const options = skin
    ? { attachTo: document.body, global: { stubs: { Icon: true } } }
    : {
        attachTo: document.body,
        props: { columns, data, serverSideSort: true, ...propsOverride },
        global: { stubs: { Icon: true } }
      }
  const wrapper = mount(component, options)
  mountedWrappers.push(wrapper)
  return wrapper.findComponent(DataTable)
}

describe('DataTable sortable headers and v2 skin', () => {
  beforeEach(() => {
    vi.stubGlobal('matchMedia', vi.fn(() => createMediaQueryList(true)))
  })

  afterEach(() => {
    mountedWrappers.splice(0).reverse().forEach((wrapper) => wrapper.unmount())
    document.body.innerHTML = ''
    vi.unstubAllGlobals()
  })

  it('uses a native button and updates aria-sort once per native activation', async () => {
    const table = mountTable()
    await nextTick()

    const sortableHeader = table.get<HTMLTableCellElement>('th[aria-sort]')
    const sortButton = sortableHeader.get<HTMLButtonElement>('button')
    expect(sortButton.element.tagName).toBe('BUTTON')
    expect(sortableHeader.attributes('aria-sort')).toBe('none')

    await sortButton.trigger('keydown', { key: 'Enter' })
    expect(table.emitted('sort')).toBeUndefined()
    await sortButton.trigger('click')
    expect(table.emitted('sort')).toEqual([['name', 'asc']])
    expect(sortableHeader.attributes('aria-sort')).toBe('ascending')

    await sortButton.trigger('keydown', { key: ' ', code: 'Space' })
    expect(table.emitted('sort')).toHaveLength(1)
    await sortButton.trigger('click')
    expect(table.emitted('sort')).toEqual([
      ['name', 'asc'],
      ['name', 'desc']
    ])
    expect(sortableHeader.attributes('aria-sort')).toBe('descending')
  })

  it('provides native mobile sort controls and emits exactly once per changed selection', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => createMediaQueryList(false)))
    const table = mountTable()
    await nextTick()

    const controls = table.get('[data-testid="mobile-sort-controls"]')
    const fieldSelect = controls.get<HTMLSelectElement>('[data-testid="mobile-sort-field"]')
    const orderSelect = controls.get<HTMLSelectElement>('[data-testid="mobile-sort-order"]')

    expect(fieldSelect.element.tagName).toBe('SELECT')
    expect(orderSelect.element.tagName).toBe('SELECT')
    expect(fieldSelect.element.labels?.[0]?.textContent).toContain('table.sortBy')
    expect(orderSelect.element.labels?.[0]?.textContent).toContain('table.sortDirection')
    expect(fieldSelect.classes()).toContain('min-h-11')
    expect(orderSelect.classes()).toContain('min-h-11')
    expect(fieldSelect.element.value).toBe('')
    expect(orderSelect.attributes('disabled')).toBeDefined()

    await fieldSelect.setValue('name')
    expect(table.emitted('sort')).toEqual([['name', 'asc']])
    expect(fieldSelect.element.value).toBe('name')
    expect(orderSelect.attributes('disabled')).toBeUndefined()
    expect(orderSelect.element.value).toBe('asc')

    await orderSelect.setValue('desc')
    expect(table.emitted('sort')).toEqual([
      ['name', 'asc'],
      ['name', 'desc']
    ])
    expect(orderSelect.element.value).toBe('desc')
  })

  it('uses the same mobile controls for client-side sorting without emitting server events', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => createMediaQueryList(false)))
    const table = mountTable(undefined, { serverSideSort: false })
    await nextTick()

    const fieldSelect = table.get<HTMLSelectElement>('[data-testid="mobile-sort-field"]')
    const orderSelect = table.get<HTMLSelectElement>('[data-testid="mobile-sort-order"]')

    await fieldSelect.setValue('name')
    expect(table.emitted('sort')).toBeUndefined()
    expect(table.findAll('.data-table-mobile-card:not(.data-table-mobile-sort)')[0].text())
      .toContain('Alpha')

    await orderSelect.setValue('desc')
    expect(table.emitted('sort')).toBeUndefined()
    expect(table.findAll('.data-table-mobile-card:not(.data-table-mobile-sort)')[0].text())
      .toContain('Beta')
  })

  it('does not render mobile sort controls without sortable columns', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => createMediaQueryList(false)))
    const table = mountTable(undefined, {
      columns: columns.map((column) => ({ ...column, sortable: false }))
    })
    await nextTick()

    expect(table.find('[data-testid="mobile-sort-controls"]').exists()).toBe(false)
  })

  it('enables semantic v2 table colors only when the route skin requests them', async () => {
    const v2Table = mountTable('v2')
    const legacyTable = mountTable()
    await nextTick()

    expect(v2Table.get('.table-wrapper').classes()).toContain('data-table-v2')
    expect(v2Table.get('.table-wrapper').attributes('data-ui-skin')).toBe('v2')
    expect(legacyTable.get('.table-wrapper').classes()).not.toContain('data-table-v2')
    expect(legacyTable.get('.table-wrapper').attributes('data-ui-skin')).toBe('legacy')
  })
})
