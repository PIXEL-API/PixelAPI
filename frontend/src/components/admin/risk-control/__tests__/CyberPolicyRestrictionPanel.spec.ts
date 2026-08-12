import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { CyberPolicyRestriction } from '@/api/admin/riskControl'
import CyberPolicyRestrictionPanel from '../CyberPolicyRestrictionPanel.vue'

const {
  clearCyberPolicyRestriction,
  getCyberPolicyRestriction,
  listUsers,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  clearCyberPolicyRestriction: vi.fn(),
  getCyberPolicyRestriction: vi.fn(),
  listUsers: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { list: listUsers },
    riskControl: {
      clearCyberPolicyRestriction,
      getCyberPolicyRestriction,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => `formatted:${value}`,
}))

const SelectStub = defineComponent({
  name: 'RiskGroupSelectStub',
  inheritAttrs: false,
  props: {
    modelValue: { type: [String, Number, Boolean], default: null },
    options: { type: Array, default: () => [] },
  },
  emits: ['update:modelValue', 'change'],
  template: `
    <button
      type="button"
      data-testid="cyber-restriction-group-select"
      @click="$emit('update:modelValue', 1198); $emit('change', 1198)"
    >
      {{ JSON.stringify(options) }}
    </button>
  `,
})

const blockedRestriction: CyberPolicyRestriction = {
  user_id: 445,
  group_id: 1198,
  blocked: true,
  scope: 'user_group_day',
  blocked_until: '2026-08-12T00:00:00+08:00',
  retry_after_seconds: 3600,
}

const unblockedRestriction: CyberPolicyRestriction = {
  user_id: 445,
  group_id: 1198,
  blocked: false,
  scope: '',
  blocked_until: null,
  retry_after_seconds: 0,
}

const selectedUser = {
  id: 445,
  username: 'alice',
  email: 'alice@example.com',
}

function mountPanel() {
  return mount(CyberPolicyRestrictionPanel, {
    props: {
      groups: [
        { id: 1198, name: 'PRO 共享号池', platform: 'openai', status: 'active' },
        { id: 2000, name: 'Anthropic Pool', platform: 'anthropic', status: 'active' },
      ] as any,
    },
    global: {
      stubs: { Icon: true, Select: SelectStub },
    },
  })
}

async function selectUserAndGroup(wrapper: ReturnType<typeof mountPanel>) {
  listUsers.mockResolvedValue({ items: [selectedUser], total: 1, page: 1, page_size: 20, pages: 1 })
  await wrapper.get('[data-testid="cyber-restriction-user-search"]').setValue('alice@example.com')
  vi.advanceTimersByTime(300)
  await flushPromises()
  await wrapper.get('[role="option"]').trigger('click')
  await wrapper.get('[data-testid="cyber-restriction-group-select"]').trigger('click')
  await nextTick()
}

async function submitQuery(wrapper: ReturnType<typeof mountPanel>) {
  await selectUserAndGroup(wrapper)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
}

describe('CyberPolicyRestrictionPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    clearCyberPolicyRestriction.mockReset()
    getCyberPolicyRestriction.mockReset()
    listUsers.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('searches users by ID, username, or email and requires an explicit user selection', async () => {
    const wrapper = mountPanel()
    listUsers.mockResolvedValue({ items: [selectedUser], total: 1 })

    const input = wrapper.get('[data-testid="cyber-restriction-user-search"]')
    await input.setValue('alice@example.com')
    expect(wrapper.get('[data-testid="cyber-restriction-query"]').attributes('disabled')).toBeDefined()
    vi.advanceTimersByTime(300)
    await flushPromises()

    expect(listUsers).toHaveBeenCalledWith(
      1,
      20,
      { search: 'alice@example.com' },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.get('[role="option"]').text()).toContain('alice')
    expect(wrapper.get('[role="option"]').text()).toContain('alice@example.com')
    expect(wrapper.get('[role="option"]').text()).toContain('445')

    await wrapper.get('[role="option"]').trigger('click')
    await wrapper.get('[data-testid="cyber-restriction-group-select"]').trigger('click')
    expect(wrapper.get('[data-testid="cyber-restriction-query"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="cyber-restriction-group-select"]').text()).toContain('PRO 共享号池')
    expect(wrapper.get('[data-testid="cyber-restriction-group-select"]').text()).toContain('1198')
    expect(wrapper.get('[data-testid="cyber-restriction-group-select"]').text()).not.toContain('Anthropic Pool')
  })

  it('does not let an older user search response overwrite the latest result', async () => {
    let resolveFirst!: (value: unknown) => void
    listUsers
      .mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ items: [selectedUser], total: 1 })
    const wrapper = mountPanel()
    const input = wrapper.get('[data-testid="cyber-restriction-user-search"]')

    await input.setValue('old')
    vi.advanceTimersByTime(300)
    await nextTick()
    await input.setValue('alice')
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(wrapper.get('[role="option"]').text()).toContain('alice')

    resolveFirst({ items: [{ id: 99, username: 'old', email: 'old@example.com' }], total: 1 })
    await flushPromises()
    expect(wrapper.get('[role="option"]').text()).toContain('alice')
    expect(wrapper.get('[role="option"]').text()).not.toContain('old@example.com')
  })

  it('queries and displays an active user-and-group restriction', async () => {
    getCyberPolicyRestriction.mockResolvedValue(blockedRestriction)
    const wrapper = mountPanel()

    await submitQuery(wrapper)

    expect(getCyberPolicyRestriction).toHaveBeenCalledWith(445, 1198)
    expect(wrapper.get('[data-testid="cyber-restriction-result"]').text()).toContain('445')
    expect(wrapper.get('[data-testid="cyber-restriction-result"]').text()).toContain('1198')
    expect(wrapper.get('[data-testid="cyber-restriction-result"]').text()).toContain(
      'formatted:2026-08-12T00:00:00+08:00'
    )
    expect(wrapper.find('[data-testid="cyber-restriction-clear"]').exists()).toBe(true)
  })

  it('does not offer a clear action when the user is not restricted', async () => {
    getCyberPolicyRestriction.mockResolvedValue(unblockedRestriction)
    const wrapper = mountPanel()

    await submitQuery(wrapper)

    expect(wrapper.get('[data-testid="cyber-restriction-result"]').text()).toContain(
      'admin.riskControl.cyberRestriction.notBlocked'
    )
    expect(wrapper.find('[data-testid="cyber-restriction-clear"]').exists()).toBe(false)
  })

  it('confirms, clears, and reloads the server state', async () => {
    getCyberPolicyRestriction
      .mockResolvedValueOnce(blockedRestriction)
      .mockResolvedValueOnce(unblockedRestriction)
    clearCyberPolicyRestriction.mockResolvedValue({ user_id: 445, group_id: 1198, removed: true })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mountPanel()
    await submitQuery(wrapper)

    await wrapper.get('[data-testid="cyber-restriction-clear"]').trigger('click')
    await flushPromises()

    expect(window.confirm).toHaveBeenCalledOnce()
    expect(clearCyberPolicyRestriction).toHaveBeenCalledWith(445, 1198)
    expect(getCyberPolicyRestriction).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('admin.riskControl.cyberRestriction.clearSuccess')
    expect(wrapper.find('[data-testid="cyber-restriction-clear"]').exists()).toBe(false)
  })

  it('reports query failures without presenting false success', async () => {
    getCyberPolicyRestriction.mockRejectedValueOnce(new Error('query failed'))
    const wrapper = mountPanel()
    await submitQuery(wrapper)

    expect(showError).toHaveBeenCalledWith('admin.riskControl.cyberRestriction.queryFailed')
    expect(wrapper.find('[data-testid="cyber-restriction-result"]').exists()).toBe(false)
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('disables the query action while the request is in flight', async () => {
    let resolveQuery!: (value: CyberPolicyRestriction) => void
    getCyberPolicyRestriction.mockReturnValue(new Promise<CyberPolicyRestriction>((resolve) => {
      resolveQuery = resolve
    }))
    const wrapper = mountPanel()

    await selectUserAndGroup(wrapper)
    await wrapper.get('form').trigger('submit')
    await nextTick()
    expect(wrapper.get('[data-testid="cyber-restriction-query"]').attributes('disabled')).toBeDefined()

    resolveQuery(unblockedRestriction)
    await flushPromises()
    expect(wrapper.get('[data-testid="cyber-restriction-query"]').attributes('disabled')).toBeUndefined()
  })
})
