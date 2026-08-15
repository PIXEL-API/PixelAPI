import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account } from '@/types'
import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  listProxies,
  getAllGroups,
  revertProxyFallback,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  listProxies: vi.fn(),
  getAllGroups: vi.fn(),
  revertProxyFallback: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      revertProxyFallback
    },
    proxies: { list: listProxies },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess, showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isSimpleMode: false })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    openAIAccountLevels: [],
    fetch: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

const account: Account = {
  id: 9,
  name: 'fallback-account',
  platform: 'openai',
  account_level: 'plus',
  type: 'oauth',
  proxy_id: null,
  proxy_fallback_origin_id: 17,
  proxy_fallback_origin_name: 'expired-origin',
  concurrency: 1,
  priority: 1,
  status: 'active',
  error_message: null,
  error_since: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
  schedulable: true,
  rate_limited_at: null,
  rate_limit_reset_at: null,
  overload_until: null,
  temp_unschedulable_until: null,
  temp_unschedulable_reason: null,
  session_window_start: null,
  session_window_end: null,
  session_window_status: null
}

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" :data-test="'row-' + row.id">
        <slot name="cell-proxy" :row="row" />
      </div>
    </div>
  `
}

const ConfirmDialogStub = {
  props: ['show', 'title', 'message'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="confirm-dialog">
      <span>{{ title }}</span><span>{{ message }}</span>
      <button data-test="confirm-revert" @click="$emit('confirm')">confirm</button>
    </div>
  `
}

describe('admin AccountsView proxy fallback revert', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({
      items: [account],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    listProxies.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 1000, pages: 0 })
    getAllGroups.mockResolvedValue([])
    revertProxyFallback.mockResolvedValue({ message: 'ok' })
  })

  it('shows fallback state, confirms the operation, waits for the API, and refreshes the list', async () => {
    const wrapper = mount(AccountsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: {
            template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
          },
          DataTable: DataTableStub,
          ConfirmDialog: ConfirmDialogStub,
          AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
          AccountTableFilters: true,
          AccountBulkActionsBar: true,
          Pagination: true,
          AccountActionMenu: true,
          ImportDataModal: true,
          ReAuthAccountModal: true,
          AccountTestModal: true,
          AccountBatchTestModal: true,
          AccountStatsModal: true,
          ScheduledTestsPanel: true,
          SyncFromCrsModal: true,
          TempUnschedStatusModal: true,
          ErrorPassthroughRulesModal: true,
          TLSFingerprintProfilesModal: true,
          CreateAccountModal: true,
          EditAccountModal: true,
          BulkEditAccountModal: true,
          PlatformTypeBadge: true,
          AccountCapacityCell: true,
          AccountStatusIndicator: true,
          AccountTodayStatsCell: true,
          AccountGroupsCell: true,
          AccountUsageCell: true,
          CredentialImportModal: true,
          HelpTooltip: true,
          Icon: true
        }
      }
    })

    await flushPromises()
    expect(wrapper.get('[data-test="row-9"]').text()).toContain('admin.accounts.fallbackActive')

    const revertButton = wrapper
      .get('[data-test="row-9"]')
      .findAll('button')
      .find((button) => button.text().includes('admin.accounts.revertProxy'))
    await revertButton!.trigger('click')
    expect(wrapper.get('[data-test="confirm-dialog"]').text()).toContain('expired-origin')

    await wrapper.get('[data-test="confirm-revert"]').trigger('click')
    await flushPromises()

    expect(revertProxyFallback).toHaveBeenCalledWith(9)
    expect(listAccounts).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.revertProxySuccess')
    expect(showError).not.toHaveBeenCalled()
  })
})
