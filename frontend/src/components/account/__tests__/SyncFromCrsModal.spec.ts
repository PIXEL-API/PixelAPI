import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { PreviewFromCRSResult } from '@/api/admin/accounts'
import SyncFromCrsModal from '../SyncFromCrsModal.vue'

const {
  previewFromCrs,
  syncFromCrs,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  previewFromCrs: vi.fn(),
  syncFromCrs: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      previewFromCrs,
      syncFromCrs,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const BaseDialogStub = {
  props: ['show'],
  emits: ['close'],
  template: '<section v-if="show"><slot /><slot name="footer" /></section>',
}

const previewSecurityContract = {
  preview_token: 'signed-preview-token',
  expires_at: 4_102_444_800,
} as const

function mountModal() {
  return mount(SyncFromCrsModal, {
    props: { show: true },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
      },
    },
  })
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, reject, resolve }
}

function previewWithNewAccount(id: string, name: string): PreviewFromCRSResult {
  return {
    ...previewSecurityContract,
    existing_accounts: [],
    new_accounts: [{
      crs_account_id: id,
      kind: 'claude',
      name,
      platform: 'anthropic',
      type: 'oauth',
      requires_force_active_edit: false,
      room_bindings: [],
    }],
  }
}

async function submitPreview(wrapper: ReturnType<typeof mountModal>): Promise<void> {
  await wrapper.get('#crs-base-url').setValue('https://crs.example.com')
  await wrapper.get('#crs-username').setValue('admin')
  await wrapper.get('#crs-password').setValue('secret')
  await wrapper.get('#sync-from-crs-form').trigger('submit')
  await flushPromises()
}

describe('SyncFromCrsModal room mutation guard', () => {
  beforeEach(() => {
    previewFromCrs.mockReset()
    syncFromCrs.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    vi.stubGlobal('crypto', {
      randomUUID: vi.fn(() => 'sync-operation-id'),
    })
    syncFromCrs.mockResolvedValue({
      created: 0,
      updated: 1,
      skipped: 0,
      failed: 0,
      items: [],
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('requires explicit confirmation and sends preview listing versions for room accounts', async () => {
    previewFromCrs.mockResolvedValue({
      ...previewSecurityContract,
      new_accounts: [],
      existing_accounts: [{
        crs_account_id: 'crs-room-account',
        local_account_id: 44,
        kind: 'claude',
        name: '房间账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: true,
        room_bindings: [
          { listing_id: 71, row_version: 6 },
          { listing_id: 72, row_version: 4 },
        ],
      }],
    })

    const wrapper = mountModal()
    await submitPreview(wrapper)

    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )
    expect(wrapper.get('[data-testid="crs-force-confirmation"]').text()).toContain('检测到 1 个房间账号')
    expect(syncButton?.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="crs-force-confirmed"]').setValue(true)
    await wrapper.get('[data-testid="crs-force-reason"]').setValue('轮换 CRS 凭证并核对房间状态')
    expect(syncButton?.attributes('disabled')).toBeUndefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncFromCrs).toHaveBeenCalledWith({
      base_url: 'https://crs.example.com',
      username: 'admin',
      password: 'secret',
      sync_proxies: true,
      selected_account_ids: [],
      force_active_edit: true,
      confirmed: true,
      reason: '轮换 CRS 凭证并核对房间状态',
      expected_versions: {
        71: 6,
        72: 4,
      },
      preview_token: previewSecurityContract.preview_token,
    }, 'crs-sync-sync-operation-id')
    wrapper.unmount()
  })

  it('blocks force sync when the preview is missing a valid row version', async () => {
    previewFromCrs.mockResolvedValue({
      ...previewSecurityContract,
      new_accounts: [],
      existing_accounts: [{
        crs_account_id: 'crs-invalid-room-account',
        local_account_id: 45,
        kind: 'claude',
        name: '版本缺失账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: true,
        room_bindings: [{ listing_id: 73, row_version: 0 }],
      }],
    })

    const wrapper = mountModal()
    await submitPreview(wrapper)

    expect(wrapper.get('[data-testid="crs-force-preview-error"]').text())
      .toContain('预览包含无效的房间版本')
    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )
    expect(syncButton?.attributes('disabled')).toBeDefined()
    expect(syncFromCrs).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps a normal CRS sync non-forced by default', async () => {
    previewFromCrs.mockResolvedValue({
      ...previewSecurityContract,
      new_accounts: [],
      existing_accounts: [{
        crs_account_id: 'crs-private-account',
        local_account_id: 46,
        kind: 'claude',
        name: '普通账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: false,
        room_bindings: [],
      }],
    })

    const wrapper = mountModal()
    await submitPreview(wrapper)
    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )
    expect(syncButton?.attributes('disabled')).toBeUndefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncFromCrs).toHaveBeenCalledWith(expect.objectContaining({
      force_active_edit: false,
      confirmed: false,
      reason: undefined,
      expected_versions: undefined,
      preview_token: previewSecurityContract.preview_token,
    }), 'crs-sync-sync-operation-id')
    wrapper.unmount()
  })

  it('reuses the key for an identical retry and rotates it when the confirmed payload changes', async () => {
    vi.stubGlobal('crypto', {
      randomUUID: vi.fn()
        .mockReturnValueOnce('first-operation')
        .mockReturnValueOnce('second-operation'),
    })
    previewFromCrs.mockResolvedValue({
      ...previewSecurityContract,
      new_accounts: [],
      existing_accounts: [{
        crs_account_id: 'crs-retry-room-account',
        local_account_id: 47,
        kind: 'claude',
        name: '重试房间账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: true,
        room_bindings: [{ listing_id: 74, row_version: 8 }],
      }],
    })
    syncFromCrs
      .mockRejectedValueOnce(new Error('temporary network error'))
      .mockRejectedValueOnce(new Error('temporary network error'))
      .mockResolvedValueOnce({
        created: 0,
        updated: 1,
        skipped: 0,
        failed: 0,
        items: [],
      })

    const wrapper = mountModal()
    await submitPreview(wrapper)
    await wrapper.get('[data-testid="crs-force-confirmed"]').setValue(true)
    const reason = wrapper.get('[data-testid="crs-force-reason"]')
    await reason.setValue('第一次核对原因')
    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )!

    await syncButton.trigger('click')
    await flushPromises()
    await syncButton.trigger('click')
    await flushPromises()

    expect(syncFromCrs.mock.calls[0]?.[1]).toBe('crs-sync-first-operation')
    expect(syncFromCrs.mock.calls[1]?.[1]).toBe('crs-sync-first-operation')

    await reason.setValue('重新核对后的原因')
    await syncButton.trigger('click')
    await flushPromises()

    expect(syncFromCrs.mock.calls[2]?.[1]).toBe('crs-sync-second-operation')
    expect(syncFromCrs.mock.calls[2]?.[0]).toEqual(expect.objectContaining({
      reason: '重新核对后的原因',
    }))
    wrapper.unmount()
  })

  it('locks the connection inputs and ignores a duplicate preview submission while a request is pending', async () => {
    const pendingPreview = deferred<PreviewFromCRSResult>()
    previewFromCrs.mockReturnValue(pendingPreview.promise)
    const wrapper = mountModal()

    await wrapper.get('#crs-base-url').setValue('https://crs.example.com')
    await wrapper.get('#crs-username').setValue('admin')
    await wrapper.get('#crs-password').setValue('secret')
    await wrapper.get('#sync-from-crs-form').trigger('submit')
    await wrapper.get('#sync-from-crs-form').trigger('submit')

    expect(previewFromCrs).toHaveBeenCalledOnce()
    expect(wrapper.get('#crs-base-url').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#crs-username').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#crs-password').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="crs-sync-proxies"]').attributes('disabled')).toBeDefined()

    pendingPreview.resolve({
      ...previewSecurityContract,
      existing_accounts: [],
      new_accounts: [],
    })
    await flushPromises()
    wrapper.unmount()
  })

  it('fails closed when the preview response has no signed snapshot token', async () => {
    previewFromCrs.mockResolvedValue({
      existing_accounts: [],
      new_accounts: [],
      preview_token: '',
      expires_at: previewSecurityContract.expires_at,
    })
    const wrapper = mountModal()

    await submitPreview(wrapper)

    expect(showError).toHaveBeenCalledWith(expect.stringContaining('安全令牌'))
    expect(syncFromCrs).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').some(button =>
      button.text().includes('admin.accounts.syncNow')
    )).toBe(false)
    wrapper.unmount()
  })

  it('returns to preview input when the signed snapshot has expired', async () => {
    previewFromCrs.mockResolvedValue({
      ...previewSecurityContract,
      existing_accounts: [{
        crs_account_id: 'existing-account',
        local_account_id: 48,
        kind: 'claude',
        name: '普通账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: false,
        room_bindings: [],
      }],
      new_accounts: [],
    })
    syncFromCrs.mockRejectedValue({
      reason: 'CRS_PREVIEW_TOKEN_EXPIRED',
      message: 'preview expired',
    })
    const wrapper = mountModal()
    await submitPreview(wrapper)
    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )!

    await syncButton.trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(expect.stringContaining('重新预览'))
    expect(wrapper.find('#crs-base-url').exists()).toBe(true)
    expect(wrapper.get('#crs-password').element).toHaveProperty('value', 'secret')
    wrapper.unmount()
  })

  it('syncs with the immutable connection snapshot used by the successful preview', async () => {
    const pendingPreview = deferred<PreviewFromCRSResult>()
    previewFromCrs.mockReturnValue(pendingPreview.promise)
    const wrapper = mountModal()

    await wrapper.get('#crs-base-url').setValue('https://crs.example.com/original')
    await wrapper.get('#crs-username').setValue('preview-admin')
    await wrapper.get('#crs-password').setValue('preview-secret')
    await wrapper.get('#sync-from-crs-form').trigger('submit')

    await wrapper.get('#crs-base-url').setValue('https://changed.example.com')
    await wrapper.get('#crs-username').setValue('changed-admin')
    await wrapper.get('#crs-password').setValue('changed-secret')
    await wrapper.get('[data-testid="crs-sync-proxies"]').setValue(false)
    pendingPreview.resolve({
      ...previewSecurityContract,
      existing_accounts: [{
        crs_account_id: 'existing-account',
        local_account_id: 48,
        kind: 'claude',
        name: '普通账号',
        platform: 'anthropic',
        type: 'oauth',
        requires_force_active_edit: false,
        room_bindings: [],
      }],
      new_accounts: [],
    })
    await flushPromises()

    const syncButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.syncNow')
    )!
    await syncButton.trigger('click')
    await flushPromises()

    expect(syncFromCrs).toHaveBeenCalledWith(expect.objectContaining({
      base_url: 'https://crs.example.com/original',
      username: 'preview-admin',
      password: 'preview-secret',
      sync_proxies: true,
      preview_token: previewSecurityContract.preview_token,
    }), 'crs-sync-sync-operation-id')
    wrapper.unmount()
  })

  it('prevents an older preview response from replacing a newer preview', async () => {
    const firstPreview = deferred<PreviewFromCRSResult>()
    const secondPreview = deferred<PreviewFromCRSResult>()
    previewFromCrs
      .mockReturnValueOnce(firstPreview.promise)
      .mockReturnValueOnce(secondPreview.promise)
    const wrapper = mountModal()

    await wrapper.get('#crs-base-url').setValue('https://old.example.com')
    await wrapper.get('#crs-username').setValue('old-admin')
    await wrapper.get('#crs-password').setValue('old-secret')
    await wrapper.get('#sync-from-crs-form').trigger('submit')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await wrapper.get('#crs-base-url').setValue('https://new.example.com')
    await wrapper.get('#crs-username').setValue('new-admin')
    await wrapper.get('#crs-password').setValue('new-secret')
    await wrapper.get('#sync-from-crs-form').trigger('submit')

    secondPreview.resolve(previewWithNewAccount('new-preview', '新预览账号'))
    await flushPromises()
    expect(wrapper.text()).toContain('新预览账号')

    firstPreview.resolve(previewWithNewAccount('old-preview', '旧预览账号'))
    await flushPromises()
    expect(wrapper.text()).toContain('新预览账号')
    expect(wrapper.text()).not.toContain('旧预览账号')
    wrapper.unmount()
  })

  it('provides touch-sized selection controls with visible keyboard focus states', async () => {
    previewFromCrs.mockResolvedValue(previewWithNewAccount('new-account', '新增账号'))
    const wrapper = mountModal()
    await submitPreview(wrapper)

    expect(wrapper.get('[data-testid="crs-select-all"]').classes()).toEqual(expect.arrayContaining([
      'min-h-11',
      'focus-visible:ring-2',
    ]))
    expect(wrapper.get('[data-testid="crs-select-none"]').classes()).toEqual(expect.arrayContaining([
      'min-h-11',
      'focus-visible:ring-2',
    ]))
    expect(wrapper.get('[data-testid="crs-new-account-row"]').classes()).toEqual(expect.arrayContaining([
      'min-h-11',
      'focus-within:ring-2',
    ]))
    wrapper.unmount()
  })
})
