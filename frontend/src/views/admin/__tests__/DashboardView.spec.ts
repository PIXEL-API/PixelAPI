import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getStats, getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getStats: vi.fn(),
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getStats,
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
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

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getStats.mockResolvedValue(createDashboardStats())
    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses last 24 hours as default dashboard range', async () => {
    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_time: expect.any(String),
      end_time: expect.any(String),
      granularity: 'hour'
    }), expect.objectContaining({ signal: expect.any(AbortSignal) }))

    const firstParams = getSnapshotV2.mock.calls[0]?.[0]
    const startTime = new Date(String(firstParams?.start_time)).getTime()
    const endTime = new Date(String(firstParams?.end_time)).getTime()
    expect(endTime - startTime).toBe(24 * 60 * 60 * 1000)
    expect(firstParams).not.toHaveProperty('start_date')
    expect(firstParams).not.toHaveProperty('end_date')
  })

  it('refreshes summary stats when date range changes', async () => {
    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: {
            template: '<button data-test="date-range" @click="emitChange">change</button>',
            emits: ['update:startDate', 'update:endDate', 'change'],
            methods: {
              emitChange() {
                this.$emit('update:startDate', '2026-05-01')
                this.$emit('update:endDate', '2026-05-07')
                this.$emit('change', {
                  startDate: '2026-05-01',
                  endDate: '2026-05-07',
                  preset: null
                })
              }
            }
          },
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()
    expect(getStats).toHaveBeenCalledTimes(1)
    getSnapshotV2.mockClear()

    await wrapper.get('[data-test="date-range"]').trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: '2026-05-01',
      end_date: '2026-05-07',
      granularity: 'day',
      include_stats: true,
      include_trend: true,
      include_model_stats: true
    }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('cancels the in-flight dashboard request when the range changes', async () => {
    let resolveFirstRequest: ((value: unknown) => void) | undefined
    getSnapshotV2
      .mockImplementationOnce(() => new Promise(resolve => {
        resolveFirstRequest = resolve
      }))
      .mockResolvedValue({
        stats: createDashboardStats(),
        trend: [],
        models: []
      })

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: {
            template: '<button data-test="date-range" @click="emitChange">change</button>',
            emits: ['update:startDate', 'update:endDate', 'change'],
            methods: {
              emitChange() {
                this.$emit('update:startDate', '2026-06-01')
                this.$emit('update:endDate', '2026-06-02')
                this.$emit('change', {
                  startDate: '2026-06-01',
                  endDate: '2026-06-02',
                  preset: null
                })
              }
            }
          },
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()
    const firstSignal = getSnapshotV2.mock.calls[0]?.[1]?.signal as AbortSignal
    expect(firstSignal.aborted).toBe(false)

    await wrapper.get('[data-test="date-range"]').trigger('click')
    expect(firstSignal.aborted).toBe(true)

    resolveFirstRequest?.({ stats: createDashboardStats(), trend: [], models: [] })
    await flushPromises()
  })
})
