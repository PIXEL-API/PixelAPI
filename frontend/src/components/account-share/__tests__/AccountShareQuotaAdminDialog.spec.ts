import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  AccountShareGrandfatherCandidate,
  AccountShareQuotaAdminState,
  AccountShareQuotaPolicy
} from '@/api/admin/accountShareQuota'
import AccountShareQuotaAdminDialog from '../AccountShareQuotaAdminDialog.vue'

const {
  batchGrandfather,
  getGlobal,
  getOwner,
  grandfatherOwner,
  listAudit,
  listGrandfatherCandidates,
  revokeOwner,
  showSuccess,
  showWarning,
  updateGlobal,
  upsertOwner
} = vi.hoisted(() => ({
  batchGrandfather: vi.fn(),
  getGlobal: vi.fn(),
  getOwner: vi.fn(),
  grandfatherOwner: vi.fn(),
  listAudit: vi.fn(),
  listGrandfatherCandidates: vi.fn(),
  revokeOwner: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
  updateGlobal: vi.fn(),
  upsertOwner: vi.fn()
}))

vi.mock('@/api/admin/accountShareQuota', () => ({
  default: {
    batchGrandfather,
    getGlobal,
    getOwner,
    grandfatherOwner,
    listAudit,
    listGrandfatherCandidates,
    revokeOwner,
    updateGlobal,
    upsertOwner
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showWarning
  })
}))

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show', 'title', 'closeDisabled'],
  emits: ['close'],
  template: `
    <section
      v-if="show"
      data-testid="base-dialog"
      :data-title="title"
      :data-close-disabled="String(closeDisabled)"
    >
      <button type="button" data-testid="base-dialog-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `
}

function globalPolicy(): AccountShareQuotaPolicy {
  return {
    id: 1,
    scope_type: 'global',
    version: 3,
    status: 'active',
    override_kind: 'default',
    limits: {
      max_live_rooms: 5,
      max_room_creates_24_hours: 5,
      max_accounts_per_room: 20,
      max_room_accounts_per_owner: 100
    },
    effective_at: '2026-07-01T00:00:00Z',
    reason: '默认配额',
    actor_user_id_snapshot: 1,
    created_at: '2026-07-01T00:00:00Z'
  }
}

function candidate(ownerUserID: number): AccountShareGrandfatherCandidate {
  return {
    owner_user_id: ownerUserID,
    usage: {
      live_rooms: 6,
      room_creates_24_hours: 5,
      owner_room_accounts: 110,
      largest_room_accounts: 24
    },
    exceeded_dimensions: [
      'max_live_rooms',
      'max_accounts_per_room',
      'max_room_accounts_per_owner'
    ],
    effective_quota: {
      limits: globalPolicy().limits,
      source: 'global',
      policy_id: 1,
      policy_version: 3,
      override_kind: 'default',
      growth_blocked: false
    },
    latest_owner_version: ownerUserID === 41 ? 2 : 4,
    suggested_limits: {
      max_live_rooms: 6,
      max_room_creates_24_hours: 5,
      max_accounts_per_room: 24,
      max_room_accounts_per_owner: 110
    },
    preview_fingerprint: `candidate-${ownerUserID}`,
    as_of: '2026-07-27T00:00:00Z'
  }
}

function paginated<T>(items: T[]) {
  return {
    items,
    total: items.length,
    page: 1,
    page_size: 12,
    pages: 1
  }
}

function futureDateTimeLocal(daysFromNow = 365): string {
  const date = new Date(Date.now() + daysFromNow * 24 * 60 * 60 * 1000)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function ownerState(ownerUserID: number): AccountShareQuotaAdminState {
  return {
    global_policy: globalPolicy(),
    owner_policy: {
      ...globalPolicy(),
      id: 90,
      scope_type: 'owner',
      owner_user_id: ownerUserID,
      version: 5,
      override_kind: 'grandfather',
      expires_at: '2027-08-31T00:00:00Z'
    },
    effective_quota: {
      limits: candidate(ownerUserID).suggested_limits,
      source: 'owner_override',
      policy_id: 90,
      policy_version: 5,
      override_kind: 'grandfather',
      override_expires_at: '2027-08-31T00:00:00Z',
      growth_blocked: true
    },
    usage: candidate(ownerUserID).usage
  }
}

function mountDialog() {
  return mount(AccountShareQuotaAdminDialog, {
    props: {
      show: true
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Icon: true
      }
    }
  })
}

describe('AccountShareQuotaAdminDialog batch grandfather flow', () => {
  beforeEach(() => {
    batchGrandfather.mockReset()
    getGlobal.mockReset()
    getOwner.mockReset()
    grandfatherOwner.mockReset()
    listAudit.mockReset()
    listGrandfatherCandidates.mockReset()
    revokeOwner.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    updateGlobal.mockReset()
    upsertOwner.mockReset()

    getGlobal.mockResolvedValue(globalPolicy())
    getOwner.mockImplementation((ownerUserID: number) => Promise.resolve(ownerState(ownerUserID)))
    listAudit.mockResolvedValue(paginated([]))
    listGrandfatherCandidates.mockResolvedValue(paginated([candidate(42), candidate(41)]))
    batchGrandfather.mockResolvedValue([
      {
        owner_user_id: 41,
        status: 'applied',
        policy_id: 91,
        policy_version: 3,
        expires_at: '2027-08-31T00:00:00.000Z'
      },
      {
        owner_user_id: 42,
        status: 'conflict',
        result_code: 'ACCOUNT_SHARE_QUOTA_CANDIDATE_STALE',
        message: 'candidate changed'
      }
    ])
    vi.spyOn(globalThis.crypto, 'randomUUID')
      .mockReturnValue('11111111-1111-4111-8111-111111111111')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('previews, confirms, executes, and renders every batch item result', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="quota-batch-tab"]').trigger('click')
    await flushPromises()
    expect(listGrandfatherCandidates).toHaveBeenCalledWith(
      1,
      12,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    await wrapper.get('[data-testid="toggle-page-candidates"]').trigger('click')
    await wrapper.get('[data-testid="batch-quota-reason"]').setValue('历史超限统一冻结')
    const expiryInput = futureDateTimeLocal()
    await wrapper.get('[data-testid="batch-quota-expiry"]').setValue(expiryInput)
    await wrapper.get('[data-testid="prepare-batch-grandfather"]').trigger('click')

    expect(wrapper.get('[data-testid="quota-mutation-confirmation"]').text()).toContain('2 位房主')
    await wrapper.get('[data-testid="quota-mutation-confirmed"]').setValue(true)
    await wrapper.get('[data-testid="confirm-quota-mutation"]').trigger('click')
    await flushPromises()

    expect(batchGrandfather).toHaveBeenCalledWith(
      {
        items: [
          {
            owner_user_id: 41,
            expected_version: 2,
            preview_usage: candidate(41).usage,
            preview_fingerprint: 'candidate-41'
          },
          {
            owner_user_id: 42,
            expected_version: 4,
            preview_usage: candidate(42).usage,
            preview_fingerprint: 'candidate-42'
          }
        ],
        expires_at: new Date(expiryInput).toISOString(),
        reason: '历史超限统一冻结',
        confirmed: true
      },
      'account-share-quota-batch-grandfather-11111111-1111-4111-8111-111111111111'
    )
    expect(wrapper.get('[data-testid="batch-grandfather-results"]').text()).toContain('成功 1')
    expect(wrapper.get('[data-testid="batch-grandfather-results"]').text()).toContain('冲突 1')
    expect(wrapper.get('[data-testid="batch-grandfather-results"]').text()).toContain('候选快照已变化')
    expect(listGrandfatherCandidates).toHaveBeenCalledTimes(2)
    expect(showWarning).toHaveBeenCalledWith('成功 1 位，另有 1 位需要查看结果')
    expect(wrapper.emitted('updated')).toHaveLength(1)

    await wrapper.get('[data-testid="view-owner-42"]').trigger('click')
    await flushPromises()
    expect(getOwner).toHaveBeenCalledWith(
      42,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.text()).toContain('该房主处于历史保留模式')
    expect(listAudit).toHaveBeenLastCalledWith(
      'owner',
      1,
      12,
      42,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('guards closing when a batch selection has not been submitted', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="quota-batch-tab"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="candidate-42"]').setValue(true)

    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(true)
    await wrapper.get('[data-testid="discard-quota-draft"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('closes without a discard warning after server values and the batch default expiry load unchanged', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="quota-batch-tab"]').trigger('click')
    await flushPromises()
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(false)
  })

  it('guards closing after a global limit changes', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="global-max_live_rooms"]').setValue(6)
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(true)
  })

  it('guards closing after owner limits or the owner expiry change', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="quota-owner-tab"]').trigger('click')
    await wrapper.get('[data-testid="quota-owner-id"]').setValue('42')
    await wrapper.get('.quota-owner-search').trigger('submit')
    await flushPromises()

    await wrapper.get('[data-testid="owner-max_live_rooms"]').setValue(7)
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(true)

    await wrapper.get('[data-title="放弃未提交的配额修改？"] [data-testid="base-dialog-close"]').trigger('click')
    await wrapper.get('[data-testid="owner-max_live_rooms"]').setValue(6)
    await wrapper.get('[data-testid="owner-quota-expiry"]').setValue(futureDateTimeLocal(500))
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(true)
  })

  it('closes without a discard warning after owner values load unchanged', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="quota-owner-tab"]').trigger('click')
    await wrapper.get('[data-testid="quota-owner-id"]').setValue('42')
    await wrapper.get('.quota-owner-search').trigger('submit')
    await flushPromises()

    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(false)
  })

  it('guards closing when the generated batch expiry is edited', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="quota-batch-tab"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="batch-quota-expiry"]').setValue(futureDateTimeLocal(500))
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(true)
  })

  it('refreshes the global draft baseline after a successful submit', async () => {
    const updatedPolicy: AccountShareQuotaPolicy = {
      ...globalPolicy(),
      version: 4,
      limits: {
        ...globalPolicy().limits,
        max_live_rooms: 6
      }
    }
    getGlobal
      .mockResolvedValueOnce(globalPolicy())
      .mockResolvedValueOnce(updatedPolicy)
    updateGlobal.mockResolvedValue(updatedPolicy)

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="global-max_live_rooms"]').setValue(6)
    await wrapper.get('[data-testid="global-quota-reason"]').setValue('按当前资源容量调整')
    await wrapper.get('[data-testid="prepare-global-quota-update"]').trigger('click')
    await wrapper.get('[data-testid="quota-mutation-confirmed"]').setValue(true)
    await wrapper.get('[data-testid="confirm-quota-mutation"]').trigger('click')
    await flushPromises()

    expect(updateGlobal).toHaveBeenCalledOnce()
    wrapper.findAllComponents({ name: 'BaseDialog' })[0].vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.find('[data-title="放弃未提交的配额修改？"]').exists()).toBe(false)
  })
})
