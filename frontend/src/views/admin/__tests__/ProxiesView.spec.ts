import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { Proxy } from '@/types'
import ProxiesView from '../ProxiesView.vue'

const { listProxies, createProxy, updateProxy, searchUsers, showError } = vi.hoisted(() => ({
  listProxies: vi.fn(),
  createProxy: vi.fn(),
  updateProxy: vi.fn(),
  searchUsers: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: {
      list: listProxies,
      create: createProxy,
      update: updateProxy,
      delete: vi.fn(),
      batchCreate: vi.fn(),
      batchDelete: vi.fn(),
      testProxy: vi.fn(),
      checkProxyQuality: vi.fn(),
      getProxyAccounts: vi.fn(),
      exportData: vi.fn(),
      importData: vi.fn()
    },
    usage: {
      searchUsers
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    openAIAccountLevels: [],
    fetch: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: { value: false },
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const createProxyRow = (overrides: Partial<Proxy> = {}): Proxy => ({
  id: 1,
  name: 'proxy-1',
  protocol: 'http',
  host: '127.0.0.1',
  port: 8080,
  username: null,
  password: null,
  status: 'active',
  max_accounts: 0,
  account_count: 0,
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
  ...overrides
})

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map(col => col.key).join(',') }}</div>
      <div v-for="row in data" :key="row.id" :data-test="'row-' + row.id">
        <div data-test="owner-cell"><slot name="cell-owner" :row="row" :value="row.owner_user_id" /></div>
        <div data-test="actions-cell"><slot name="cell-actions" :row="row" /></div>
      </div>
    </div>
  `
}

const BaseDialogStub = {
  props: ['show', 'title', 'width'],
  emits: ['close'],
  template: '<div v-if="show" data-test="dialog"><slot /><slot name="footer" /></div>'
}

const mountView = (attachToBody = false): VueWrapper =>
  mount(ProxiesView, {
    attachTo: attachToBody ? document.body : undefined,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template:
            '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        ImportDataModal: true,
        Select: true,
        Icon: true,
        PlatformTypeBadge: true,
        Teleport: true
      }
    }
  })

describe('admin ProxiesView owner user', () => {
  beforeEach(() => {
    localStorage.clear()

    listProxies.mockReset()
    createProxy.mockReset()
    updateProxy.mockReset()
    searchUsers.mockReset()
    showError.mockReset()

    listProxies.mockResolvedValue({
      items: [
        createProxyRow(),
        createProxyRow({
          id: 2,
          name: 'proxy-2',
          owner_user_id: 7,
          owner_username: 'alice',
          owner_email: 'alice@example.com'
        })
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    createProxy.mockResolvedValue(createProxyRow({ id: 3 }))
    updateProxy.mockResolvedValue(createProxyRow({ id: 2 }))
    searchUsers.mockResolvedValue([{ id: 7, email: 'alice@example.com', deleted: false }])
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders an owner column showing platform badge or owner username', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="columns"]').text().split(',')).toContain('owner')

    const platformCell = wrapper.get('[data-test="row-1"] [data-test="owner-cell"]')
    expect(platformCell.text()).toContain('admin.proxies.ownerPlatform')

    const ownedCell = wrapper.get('[data-test="row-2"] [data-test="owner-cell"]')
    expect(ownedCell.text()).toContain('alice')
    expect(ownedCell.text()).not.toContain('admin.proxies.ownerPlatform')
  })

  it('prefills owner in edit dialog and only sends owner_user_id when it changed', async () => {
    const wrapper = mountView()
    await flushPromises()

    const findEditButton = () =>
      wrapper
        .get('[data-test="row-2"] [data-test="actions-cell"]')
        .findAll('button')
        .find((button) => button.text().includes('common.edit'))

    // Open edit for the owned proxy: owner is prefilled from the row.
    await findEditButton()!.trigger('click')
    expect(wrapper.get('[data-test="owner-selected"]').text()).toContain('alice')
    expect(wrapper.get('[data-test="owner-selected"]').text()).toContain('#7')

    // Saving without touching the owner must omit the field entirely: the backend
    // treats an absent owner_user_id as "leave unchanged" and skips its owner guard,
    // so an unrelated edit cannot be blocked by a stale/legacy ownership state.
    await wrapper.get('form#edit-proxy-form').trigger('submit')
    await flushPromises()
    expect(updateProxy).toHaveBeenCalledTimes(1)
    expect(updateProxy.mock.calls[0][1]).not.toHaveProperty('owner_user_id')

    // Re-open, clear the owner, save: owner_user_id must be 0 (reset to platform proxy).
    await findEditButton()!.trigger('click')
    await wrapper.get('[data-test="owner-clear"]').trigger('click')
    expect(wrapper.find('[data-test="owner-selected"]').exists()).toBe(false)

    await wrapper.get('form#edit-proxy-form').trigger('submit')
    await flushPromises()
    expect(updateProxy).toHaveBeenLastCalledWith(2, expect.objectContaining({ owner_user_id: 0 }))
  })

  it('hides deleted users from the owner search results', async () => {
    vi.useFakeTimers()
    searchUsers.mockResolvedValue([
      { id: 7, email: 'alice@example.com', deleted: false },
      { id: 8, email: 'ghost@example.com', deleted: true }
    ])

    const wrapper = mountView()
    await flushPromises()

    const createButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.proxies.createProxy'))
    await createButton!.trigger('click')

    await wrapper.get('[data-test="owner-search-input"]').setValue('example')
    vi.advanceTimersByTime(300)
    await flushPromises()

    const options = wrapper.findAll('[data-test="owner-option"]')
    expect(options).toHaveLength(1)
    expect(options[0].text()).toContain('alice@example.com')
    expect(wrapper.text()).not.toContain('ghost@example.com')
  })

  it('swallows Escape only while the owner dropdown is open', async () => {
    vi.useFakeTimers()
    const wrapper = mountView(true)
    await flushPromises()

    const createButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.proxies.createProxy'))
    await createButton!.trigger('click')

    await wrapper.get('[data-test="owner-search-input"]').setValue('alice')
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(wrapper.find('[data-test="owner-option"]').exists()).toBe(true)

    // BaseDialog closes on Escape via a document-level keydown listener, so the real
    // question is whether the event still reaches the document.
    const seenAtDocument: KeyboardEvent[] = []
    const listener = (event: Event) => seenAtDocument.push(event as KeyboardEvent)
    document.addEventListener('keydown', listener)

    const input = wrapper.get('[data-test="owner-search-input"]').element
    const escape = () =>
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

    // Dropdown open: Escape collapses it and must not reach the dialog.
    escape()
    await nextTick()
    expect(seenAtDocument).toHaveLength(0)
    expect(wrapper.find('[data-test="owner-option"]').exists()).toBe(false)

    // Dropdown closed: Escape falls through so the dialog can still close on it.
    escape()
    await nextTick()
    expect(seenAtDocument).toHaveLength(1)

    document.removeEventListener('keydown', listener)
    wrapper.unmount()
  })

  it('surfaces the backend reason for owner conflicts instead of a generic failure', async () => {
    createProxy.mockRejectedValue({ status: 409, code: 409, reason: 'PROXY_OWNER_CONFLICT' })

    const wrapper = mountView()
    await flushPromises()

    const createButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.proxies.createProxy'))
    await createButton!.trigger('click')
    await wrapper.get('input[placeholder="admin.proxies.enterProxyName"]').setValue('proxy-new')
    await wrapper.get('input[placeholder="admin.proxies.form.hostPlaceholder"]').setValue('10.0.0.1')

    await wrapper.get('form#create-proxy-form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.proxies.ownerConflict')
    expect(showError).not.toHaveBeenCalledWith('admin.proxies.failedToCreate')
  })

  it('searches users with debounce and includes owner_user_id in create payload only when selected', async () => {
    vi.useFakeTimers()
    const wrapper = mountView()
    await flushPromises()

    const openCreateDialog = async () => {
      const createButton = wrapper
        .findAll('button')
        .find((button) => button.text().includes('admin.proxies.createProxy'))
      await createButton!.trigger('click')
    }
    const fillRequiredFields = async () => {
      await wrapper
        .get('input[placeholder="admin.proxies.enterProxyName"]')
        .setValue('proxy-new')
      await wrapper
        .get('input[placeholder="admin.proxies.form.hostPlaceholder"]')
        .setValue('10.0.0.1')
    }

    // Create with an owner selected via the debounced user search.
    await openCreateDialog()
    await fillRequiredFields()

    await wrapper.get('[data-test="owner-search-input"]').setValue('alice')
    expect(searchUsers).not.toHaveBeenCalled()
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(searchUsers).toHaveBeenCalledWith('alice')

    await wrapper.get('[data-test="owner-option"]').trigger('click')
    expect(wrapper.get('[data-test="owner-selected"]').text()).toContain('alice@example.com')

    await wrapper.get('form#create-proxy-form').trigger('submit')
    await flushPromises()
    expect(createProxy).toHaveBeenCalledTimes(1)
    expect(createProxy.mock.calls[0][0]).toMatchObject({ owner_user_id: 7 })

    // Create without an owner: the field must be absent (platform proxy).
    await openCreateDialog()
    await fillRequiredFields()
    await wrapper.get('form#create-proxy-form').trigger('submit')
    await flushPromises()
    expect(createProxy).toHaveBeenCalledTimes(2)
    expect(createProxy.mock.calls[1][0]).not.toHaveProperty('owner_user_id')
  })
})
