import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Proxy } from '@/types'

const {
  listAccounts,
  getBatchTodayStats,
  listProxies,
  getAvailableGroups,
  showError,
  refreshUser
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  getBatchTodayStats: vi.fn(),
  listProxies: vi.fn(),
  getAvailableGroups: vi.fn(),
  showError: vi.fn(),
  refreshUser: vi.fn()
}))

vi.mock('@/api', () => ({
  accountsAPI: {
    list: listAccounts,
    getBatchTodayStats,
    getUsage: vi.fn(),
    getStats: vi.fn(),
    exportData: vi.fn()
  },
  accountShareAPI: { listProxies },
  userGroupsAPI: { getAvailable: getAvailableGroups }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn(),
    cachedPublicSettings: {}
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { id: 9 }, refreshUser })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import AccountsView from '../AccountsView.vue'

function buildProxy(id: number, name: string): Proxy {
  return {
    id,
    name,
    protocol: 'http',
    host: `${name}.example.com`,
    port: 8080,
    status: 'active',
    max_accounts: 0
  } as unknown as Proxy
}

// 只关心「父组件按什么范围拉代理、把哪一批代理喂给了模态」，所以模态整体打桩，
// 并暴露一个能主动上报范围的按钮，模拟用户在弹窗里切平台/等级。
const CreateAccountModalStub = {
  name: 'CreateAccountModal',
  props: ['show', 'proxies'],
  emits: ['close', 'created', 'proxy-scope-change'],
  template: `
    <div v-if="show" data-testid="create-modal" :data-proxy-ids="proxies.map(p => p.id).join(',')">
      <button
        data-testid="pick-anthropic"
        @click="$emit('proxy-scope-change', { platform: 'anthropic', account_level: 'unknown' })"
      >anthropic</button>
      <button
        data-testid="pick-openai-pro"
        @click="$emit('proxy-scope-change', { platform: 'openai', account_level: 'pro' })"
      >openai pro</button>
    </div>
  `
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: { props: ['columns', 'data'], template: '<div data-testid="table" />' },
        CreateAccountModal: CreateAccountModalStub,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        SearchInput: true,
        Icon: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        AccountTodayStatsCell: true,
        AccountStatsModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        UserAccountActionMenu: true,
        UserContentModerationModal: true,
        ImportAccountsModal: true,
        Teleport: true
      }
    }
  })
}

async function openCreateModal(wrapper: ReturnType<typeof mountView>) {
  const setupState = (wrapper.vm.$ as any).setupState
  setupState.openCreateModal()
  await flushPromises()
}

describe('user AccountsView proxy scope', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    getBatchTodayStats.mockResolvedValue({})
    getAvailableGroups.mockResolvedValue([])
    listProxies.mockResolvedValue([])
  })

  it('按模态上报的平台/等级拉取代理，而不是空范围', async () => {
    const anthropicProxy = buildProxy(11, 'anthropic-only')
    listProxies.mockImplementation(async (scope: { platform?: string; account_level?: string } = {}) =>
      scope.platform === 'anthropic' ? [anthropicProxy] : []
    )

    const wrapper = mountView()
    await flushPromises()
    await openCreateModal(wrapper)

    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()

    // 修复前这里是 listProxies()，只会拿到 platform='' 且 required_account_level='' 的通用代理
    expect(listProxies).toHaveBeenCalledWith({ platform: 'anthropic', account_level: 'unknown' })
    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('11')
  })

  it('在弹窗内换平台/等级会重新按新范围拉取', async () => {
    const anthropicProxy = buildProxy(11, 'anthropic-only')
    const openaiProProxy = buildProxy(22, 'openai-pro-only')
    listProxies.mockImplementation(async (scope: { platform?: string; account_level?: string } = {}) => {
      if (scope.platform === 'anthropic') return [anthropicProxy]
      if (scope.platform === 'openai' && scope.account_level === 'pro') return [openaiProProxy]
      return []
    })

    const wrapper = mountView()
    await flushPromises()
    await openCreateModal(wrapper)

    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('11')

    await wrapper.get('[data-testid="pick-openai-pro"]').trigger('click')
    await flushPromises()

    expect(listProxies).toHaveBeenCalledWith({ platform: 'openai', account_level: 'pro' })
    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('22')
  })

  it('先发的响应后到时不会覆盖后发范围的结果', async () => {
    const anthropicProxy = buildProxy(11, 'anthropic-only')
    const openaiProProxy = buildProxy(22, 'openai-pro-only')
    let resolveAnthropic: (value: Proxy[]) => void = () => {}
    let resolveOpenAI: (value: Proxy[]) => void = () => {}

    listProxies.mockImplementation((scope: { platform?: string; account_level?: string } = {}) => {
      if (scope.platform === 'anthropic') {
        return new Promise<Proxy[]>((resolve) => {
          resolveAnthropic = resolve
        })
      }
      return new Promise<Proxy[]>((resolve) => {
        resolveOpenAI = resolve
      })
    })

    const wrapper = mountView()
    await flushPromises()
    await openCreateModal(wrapper)

    // 连续切换：第一次请求还没回来就发了第二次
    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await wrapper.get('[data-testid="pick-openai-pro"]').trigger('click')

    // 后发的先回，先发的后回 —— 旧结果必须被丢弃
    resolveOpenAI([openaiProProxy])
    await flushPromises()
    resolveAnthropic([anthropicProxy])
    await flushPromises()

    // 修复前：第二次请求会被 `if (loading) return` 直接丢掉，列表停在 anthropic
    expect(listProxies).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('22')
  })

  it('切回已缓存的范围后，仍在飞的旧请求回来也不能盖掉列表', async () => {
    const anthropicProxy = buildProxy(11, 'anthropic-only')
    const openaiProProxy = buildProxy(22, 'openai-pro-only')
    let resolveOpenAI: (value: Proxy[]) => void = () => {}

    listProxies.mockImplementation((scope: { platform?: string; account_level?: string } = {}) => {
      if (scope.platform === 'anthropic') return Promise.resolve([anthropicProxy])
      return new Promise<Proxy[]>((resolve) => {
        resolveOpenAI = resolve
      })
    })

    const wrapper = mountView()
    await flushPromises()
    await openCreateModal(wrapper)

    // anthropic 先落地并进入缓存
    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('11')

    // 切到 openai/pro（请求挂起），再切回 anthropic —— 这一步命中缓存直接返回
    await wrapper.get('[data-testid="pick-openai-pro"]').trigger('click')
    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()

    // 挂起的 openai/pro 响应现在才回来：它必须已经被作废
    resolveOpenAI([openaiProProxy])
    await flushPromises()

    expect(wrapper.get('[data-testid="create-modal"]').attributes('data-proxy-ids')).toBe('11')
  })

  it('同一范围重复上报不会重复请求', async () => {
    listProxies.mockResolvedValue([buildProxy(11, 'anthropic-only')])

    const wrapper = mountView()
    await flushPromises()
    await openCreateModal(wrapper)

    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="pick-anthropic"]').trigger('click')
    await flushPromises()

    expect(listProxies).toHaveBeenCalledTimes(1)
  })
})
