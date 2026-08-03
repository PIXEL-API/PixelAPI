import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { extractApiErrorCode, extractApiErrorMetadata } from '@/utils/apiError'
import type { Account } from '@/types'

// 回归护栏：「广场用过的号不能删号」。
//
// 真正的主因不在后端，而在前端解析错误的方式：api client 的响应拦截器 reject 的是一个
// **扁平对象** { status, code, reason, message, metadata }，**没有 response 属性**。
// 旧实现 extractErrorDetail 读 error.response.data，于是 reason 恒为 ''、metadata 恒为 {}，
// isRoomAccountBlocked 永远 false —— 「退出房间并删除」的二次确认弹窗从来没机会出现，
// force 删除整条路径是死代码，号主只看到一句通用的「删除失败」。
//
// 下面所有构造的错误都刻意不带 response 字段，任何回退到 error.response.data 的写法都会红。

const {
  listAccounts,
  getBatchTodayStats,
  listProxies,
  getAvailableGroups,
  deleteAccount,
  bulkDeleteAccounts,
  showError,
  showSuccess,
  refreshUser
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  getBatchTodayStats: vi.fn(),
  listProxies: vi.fn(),
  getAvailableGroups: vi.fn(),
  deleteAccount: vi.fn(),
  bulkDeleteAccounts: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  refreshUser: vi.fn()
}))

vi.mock('@/api', () => ({
  accountsAPI: {
    list: listAccounts,
    getBatchTodayStats,
    delete: deleteAccount,
    bulkDelete: bulkDeleteAccounts,
    getUsage: vi.fn(),
    getStats: vi.fn(),
    exportData: vi.fn(),
    queryOpenAIQuota: vi.fn(),
    resetOpenAIQuota: vi.fn()
  },
  accountShareAPI: { listProxies },
  userGroupsAPI: { getAvailable: getAvailableGroups }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
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
  return {
    ...actual,
    // 把插值参数拼进返回值，这样断言可以看到房间名有没有真的传到弹窗文案里。
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params && Object.keys(params).length > 0 ? `${key}|${JSON.stringify(params)}` : key
    })
  }
})

import AccountsView from '../AccountsView.vue'

// 只关心「弹的是哪一个确认框」，所以 ConfirmDialog 打桩成可见即渲染，并把 title/message 摊平成属性。
const ConfirmDialogStub = {
  name: 'ConfirmDialog',
  props: ['show', 'title', 'message', 'confirmText', 'cancelText', 'danger'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-testid="confirm-dialog" :data-title="title" :data-message="message">
      <button data-testid="confirm" @click="$emit('confirm')">confirm</button>
      <button data-testid="cancel" @click="$emit('cancel')">cancel</button>
    </div>
  `
}

const DELETE_DIALOG_TITLE = 'userAccounts.deleteAccount'
const BULK_DELETE_DIALOG_TITLE = 'admin.accounts.bulkDeleteTitle'
const ROOM_DETACH_DIALOG_TITLE = 'userAccounts.deleteRoomDetachTitle'

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="actions" /><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: { props: ['columns', 'data'], template: '<div data-testid="table" />' },
        ConfirmDialog: ConfirmDialogStub,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        Pagination: true,
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

type Wrapper = ReturnType<typeof mountView>

function setupState(wrapper: Wrapper): any {
  return (wrapper.vm.$ as any).setupState
}

function findDialog(wrapper: Wrapper, title: string) {
  return wrapper
    .findAll('[data-testid="confirm-dialog"]')
    .find((dialog) => dialog.attributes('data-title') === title)
}

function buildAccount(id: number, name: string): Account {
  return {
    id,
    name,
    platform: 'anthropic',
    type: 'oauth',
    status: 'active',
    schedulable: true
  } as unknown as Account
}

/**
 * 复刻 api client 拦截器真实 reject 出来的形状：扁平对象，没有 response。
 * 见 frontend/src/api/client.ts —— Promise.reject({ status, code, reason, message, metadata })。
 */
function buildFlatApiError(payload: {
  status?: number
  reason: string
  message?: string
  metadata?: Record<string, string>
}) {
  return {
    status: payload.status ?? 409,
    code: payload.status ?? 409,
    reason: payload.reason,
    message: payload.message ?? 'account deletion blocked',
    metadata: payload.metadata ?? {}
  }
}

async function openSingleDeleteDialog(wrapper: Wrapper, account: Account) {
  setupState(wrapper).openDeleteDialog(account)
  await flushPromises()
  const dialog = findDialog(wrapper, DELETE_DIALOG_TITLE)
  expect(dialog).toBeTruthy()
  return dialog!
}

// selectedIds 是 computed，只能通过选择行为改变。
async function openBulkDeleteDialog(wrapper: Wrapper, accountIds: number[]) {
  const state = setupState(wrapper)
  accountIds.forEach((id) => state.toggleTableSelection(id))
  state.openBulkDeleteDialog()
  await flushPromises()
  const dialog = findDialog(wrapper, BULK_DELETE_DIALOG_TITLE)
  expect(dialog).toBeTruthy()
  return dialog!
}

describe('删除拦截错误的解析口径', () => {
  const blocked = buildFlatApiError({
    status: 409,
    reason: 'ACCOUNT_DELETION_BLOCKED',
    metadata: {
      blocker_types: 'room_account',
      detach_resolvable: 'true',
      room_listing_ids: '7',
      room_listing_names: '我的房间'
    }
  })

  it('旧写法（error.response.data）在真实错误形状上必然读空', () => {
    // 这就是「广场用过的号不能删号」的根：reason 恒为 ''、metadata 恒为 {}，
    // isRoomAccountBlocked 永远返回 false，二次确认框永远不出现。
    const legacyExtract = (error: any) => {
      const data = error?.response?.data ?? {}
      return {
        reason: String(data.reason ?? data.code ?? ''),
        metadata: (data.metadata ?? {}) as Record<string, string>
      }
    }
    expect(legacyExtract(blocked)).toEqual({ reason: '', metadata: {} })
  })

  it('apiError 工具能从扁平对象里拿到 reason 与 metadata', () => {
    expect(extractApiErrorCode(blocked)).toBe('ACCOUNT_DELETION_BLOCKED')
    expect(extractApiErrorMetadata(blocked)).toMatchObject({
      blocker_types: 'room_account',
      room_listing_names: '我的房间'
    })
  })
})

describe('user AccountsView 广场账号删除', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    getBatchTodayStats.mockResolvedValue({})
    getAvailableGroups.mockResolvedValue([])
    listProxies.mockResolvedValue([])
    deleteAccount.mockResolvedValue(undefined)
    bulkDeleteAccounts.mockResolvedValue({ success: 1, failed: 0 })
  })

  it('拦截器抛出的扁平错误（无 response 字段）也要弹「退房并删除」二次确认', async () => {
    const blocked = buildFlatApiError({
      status: 409,
      reason: 'ACCOUNT_DELETION_BLOCKED',
      metadata: {
        blocker_types: 'room_account',
        detach_resolvable: 'true',
        room_listing_ids: '7',
        room_listing_names: '我的房间'
      }
    })
    // 守死主因：错误对象里根本没有 response，旧的 error.response.data 写法必然读到 undefined。
    expect('response' in blocked).toBe(false)
    deleteAccount.mockRejectedValueOnce(blocked)

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    const detachDialog = findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)
    expect(detachDialog).toBeTruthy()
    // 房间名要真的从 metadata 落到文案里，而不是空串。
    expect(detachDialog!.attributes('data-message')).toContain('我的房间')
    // 原来的删除确认框要让位，避免两个框叠着。
    expect(findDialog(wrapper, DELETE_DIALOG_TITLE)).toBeUndefined()
    // 不能退化成通用的「删除失败」toast。
    expect(showError).not.toHaveBeenCalled()
  })

  it('二次确认后带 force=true 重新删除', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account',
          detach_resolvable: 'true',
          room_listing_ids: '7',
          room_listing_names: '我的房间'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    const detachDialog = findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)
    expect(detachDialog).toBeTruthy()
    await detachDialog!.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(deleteAccount).toHaveBeenNthCalledWith(1, 101)
    expect(deleteAccount).toHaveBeenNthCalledWith(2, 101, true)
    expect(showSuccess).toHaveBeenCalled()
    expect(findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)).toBeUndefined()
  })

  it('没有 room_listing_names 时回退用 room_listing_ids 作房间标识', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: { blocker_types: 'room_account', detach_resolvable: 'true', room_listing_ids: '7,9' }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    const detachDialog = findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)
    expect(detachDialog).toBeTruthy()
    const message = detachDialog!.attributes('data-message') ?? ''
    expect(message).toContain('7')
    expect(message).toContain('9')
  })

  // 反向守卫：房间里有活跃租户恰恰是退房**可解**的主流场景 —— 退房会把活跃 membership
  // 重绑到房间内的健康替补账号。按 blocker_types 一刀切拒绝，会把本来能删的号变成删不掉。
  // 判据只认后端精确算出的 detach_resolvable。
  it('活跃席位可被重绑时（detach_resolvable=true）仍要弹二次确认', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account,live_membership',
          detach_resolvable: 'true',
          room_listing_ids: '7',
          room_listing_names: '我的房间',
          live_membership_count: '2',
          unresolvable_membership_count: '0'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const dialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await dialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)).toBeTruthy()
    expect(showError).not.toHaveBeenCalled()
  })

  it('排队/退租中的席位（detach_resolvable=false）不弹二次确认，只说明原因', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account,live_membership',
          detach_resolvable: 'false',
          room_listing_ids: '7',
          room_listing_names: '我的房间',
          live_membership_count: '2',
          unresolvable_membership_count: '2'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    // 退房解不掉时弹「退房后删除」= 退房成功、删除照样失败，
    // 账号被不可逆摘出房间却没删掉，且下次连确认框都不会再弹。
    expect(findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)).toBeUndefined()
    expect(showError).toHaveBeenCalledTimes(1)
    // 断言的是 i18n key 命名空间，不是具体文案：必须走「解释拦截原因」而不是兜底的删除失败。
    expect(String(showError.mock.calls[0][0])).toContain('userAccounts.deleteBlockedSummary')
    // 也不能偷偷带 force 重删。
    expect(deleteAccount).toHaveBeenCalledTimes(1)
    expect(deleteAccount).toHaveBeenCalledWith(101)
  })

  it('完全与房间无关的拦截（live_membership）同样不弹二次确认', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'live_membership',
          detach_resolvable: 'false',
          live_membership_count: '3',
          unresolvable_membership_count: '3'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(102, '在用号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)).toBeUndefined()
    expect(showError).toHaveBeenCalledTimes(1)
    expect(String(showError.mock.calls[0][0])).toContain('userAccounts.deleteBlockedSummary')
    expect(deleteAccount).toHaveBeenCalledTimes(1)
  })

  it('批量删除：纯 room_account 拦截弹二次确认，并带 force 批量重删', async () => {
    bulkDeleteAccounts.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account',
          detach_resolvable: 'true',
          room_listing_ids: '7',
          room_listing_names: '我的房间'
        }
      })
    )
    bulkDeleteAccounts.mockResolvedValueOnce({ success: 2, failed: 0 })

    const wrapper = mountView()
    await flushPromises()

    const bulkDialog = await openBulkDeleteDialog(wrapper, [201, 202])
    await bulkDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    const detachDialog = findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)
    expect(detachDialog).toBeTruthy()
    await detachDialog!.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(bulkDeleteAccounts).toHaveBeenNthCalledWith(1, [201, 202])
    expect(bulkDeleteAccounts).toHaveBeenNthCalledWith(2, [201, 202], true)
  })

  it('批量删除：混合拦截不弹二次确认，只说明原因', async () => {
    bulkDeleteAccounts.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account,pending_billing_intent',
          detach_resolvable: 'false',
          room_listing_ids: '7',
          room_listing_names: '我的房间',
          pending_billing_intent_count: '4'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const bulkDialog = await openBulkDeleteDialog(wrapper, [201, 202])
    await bulkDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)).toBeUndefined()
    expect(showError).toHaveBeenCalledTimes(1)
    expect(String(showError.mock.calls[0][0])).toContain('userAccounts.deleteBlockedSummary')
    expect(bulkDeleteAccounts).toHaveBeenCalledTimes(1)
  })

  it('退房后仍失败：房间无健康替补账号时给出可读原因', async () => {
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_DELETION_BLOCKED',
        metadata: {
          blocker_types: 'room_account',
          detach_resolvable: 'true',
          room_listing_ids: '7',
          room_listing_names: '我的房间'
        }
      })
    )
    deleteAccount.mockRejectedValueOnce(
      buildFlatApiError({
        reason: 'ACCOUNT_SHARE_ROOM_OPERATION_CONFLICT',
        metadata: {
          blocker: 'no_healthy_replacement_account',
          membership_count: '3',
          room_listing_names: '我的房间'
        }
      })
    )

    const wrapper = mountView()
    await flushPromises()

    const deleteDialog = await openSingleDeleteDialog(wrapper, buildAccount(101, '广场号'))
    await deleteDialog.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    const detachDialog = findDialog(wrapper, ROOM_DETACH_DIALOG_TITLE)
    expect(detachDialog).toBeTruthy()
    await detachDialog!.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledTimes(1)
    // reason/metadata 依旧来自扁平错误对象，走的是专门文案而不是兜底的「删除失败」。
    const message = String(showError.mock.calls[0][0])
    expect(message).toContain('userAccounts.deleteRoomNoHealthyReplacement')
    expect(message).toContain('我的房间')
    expect(showSuccess).not.toHaveBeenCalled()
  })
})
