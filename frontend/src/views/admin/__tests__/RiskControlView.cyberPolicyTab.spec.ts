import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RiskControlView from '../RiskControlView.vue'

const {
  getConfig,
  getGroups,
  getStatus,
  listAccountShareListings,
  listCyberPolicyRequests,
  listLogs,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  getGroups: vi.fn(),
  getStatus: vi.fn(),
  listAccountShareListings: vi.fn(),
  listCyberPolicyRequests: vi.fn(),
  listLogs: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { getAll: getGroups },
    riskControl: {
      getConfig,
      getStatus,
      listAccountShareListings,
      listCyberPolicyRequests,
      listLogs,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => value,
}))

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>',
})

const passiveStub = defineComponent({
  template: '<div><slot /><slot name="footer" /></div>',
})

const emptyRules = {
  standalone_block_markers: [],
  hard_markers: [],
  offensive_intent_markers: [],
  credential_abuse_intent_markers: [],
  technique_markers: [],
  credential_markers: [],
  target_markers: [],
  defensive_markers: [],
}

describe('RiskControlView Cyber Policy workspace', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })
    getConfig.mockReset()
    getGroups.mockReset()
    getStatus.mockReset()
    listAccountShareListings.mockReset()
    listCyberPolicyRequests.mockReset()
    listLogs.mockReset()

    getConfig.mockResolvedValue({
      enabled: true,
      cyber_preflight_enabled: false,
      cyber_preflight_rules: emptyRules,
      cyber_preflight_default_rules: emptyRules,
      mode: 'pre_block',
      provider: 'openai',
      base_url: 'https://api.openai.com',
      model: 'omni-moderation-latest',
      api_key_configured: false,
      api_key_masked: '',
      api_key_count: 0,
      api_key_masks: [],
      api_key_statuses: [],
      timeout_ms: 3000,
      sample_rate: 100,
      all_groups: true,
      group_ids: [],
      record_non_hits: false,
      worker_count: 1,
      queue_size: 100,
      block_status: 403,
      block_message: 'blocked',
      email_on_hit: false,
      auto_ban_enabled: false,
      ban_threshold: 10,
      violation_window_hours: 24,
      retry_count: 1,
      hit_retention_days: 30,
      non_hit_retention_days: 3,
      pre_hash_check_enabled: false,
    })
    getGroups.mockResolvedValue([])
    getStatus.mockResolvedValue({
      enabled: true,
      cyber_preflight_enabled: false,
      risk_control_enabled: true,
      mode: 'pre_block',
      worker_count: 1,
      max_workers: 1,
      active_workers: 0,
      idle_workers: 1,
      queue_size: 100,
      queue_length: 0,
      queue_usage_percent: 0,
      enqueued: 0,
      dropped: 0,
      processed: 0,
      errors: 0,
      dynamic_sampling: { enabled: false, skipped: 0, forced: 0, sampled: 0, audited: 0, risk_events: 0 },
      api_key_statuses: [],
      flagged_hash_count: 0,
      last_cleanup_deleted_hit: 0,
      last_cleanup_deleted_non_hit: 0,
    })
    listAccountShareListings.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 1 })
    listLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    listCyberPolicyRequests.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
  })

  function mountView() {
    return mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: passiveStub,
          CyberPolicyRestrictionPanel: true,
          Icon: true,
          Pagination: true,
          Select: true,
          Toggle: true,
        },
      },
    })
  }

  it('does not mount or request the Cyber list on the default content-moderation tab', async () => {
    const wrapper = mountView()
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs).toHaveLength(2)
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')
    expect(tabs[1]?.attributes('aria-selected')).toBe('false')
    expect(wrapper.find('[data-testid="cyber-policy-requests-panel"]').exists()).toBe(false)
    expect(listCyberPolicyRequests).not.toHaveBeenCalled()
  })

  it('lazily mounts the Cyber panel once and keeps tab ARIA state synchronized', async () => {
    const wrapper = mountView()
    await flushPromises()
    const tabs = wrapper.findAll('[role="tab"]')

    await tabs[1]!.trigger('click')
    await flushPromises()
    expect(tabs[1]?.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="cyber-policy-requests-panel"]').isVisible()).toBe(true)
    expect(listCyberPolicyRequests).toHaveBeenCalledOnce()

    await tabs[0]!.trigger('click')
    await nextTick()
    expect(wrapper.get('#admin-risk-control-panel-cyberPolicy').attributes('style')).toContain('display: none')
    await tabs[1]!.trigger('click')
    await nextTick()
    expect(listCyberPolicyRequests).toHaveBeenCalledOnce()
  })

  it('supports Arrow, Home, and End keyboard navigation', async () => {
    const wrapper = mountView()
    await flushPromises()
    const tabs = wrapper.findAll('[role="tab"]')

    await tabs[0]!.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(tabs[1]?.attributes('aria-selected')).toBe('true')

    await tabs[1]!.trigger('keydown', { key: 'Home' })
    await nextTick()
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')

    await tabs[0]!.trigger('keydown', { key: 'End' })
    await nextTick()
    expect(tabs[1]?.attributes('aria-selected')).toBe('true')
  })
})
