import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import DashboardView from '../DashboardView.vue'

const {
  refreshUser,
  getDashboardStats,
  getDashboardTrend,
  getDashboardModels,
  getDashboardAccountSharing,
  getByDateRange
} = vi.hoisted(() => ({
  refreshUser: vi.fn(),
  getDashboardStats: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getDashboardAccountSharing: vi.fn(),
  getByDateRange: vi.fn()
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 0 },
    isSimpleMode: false,
    refreshUser
  })
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats,
    getDashboardTrend,
    getDashboardModels,
    getDashboardAccountSharing,
    getByDateRange
  }
}))

const DashboardChartsStub = {
  name: 'DashboardChartsStub',
  props: {
    loading: Boolean,
    trend: {
      type: Array,
      default: () => []
    },
    models: {
      type: Array,
      default: () => []
    }
  },
  emits: [
    'update:startDate',
    'update:endDate',
    'update:granularity',
    'dateRangeChange',
    'granularityChange',
    'refresh'
  ],
  methods: {
    selectLast24Hours() {
      this.$emit('update:startDate', '2026-07-22')
      this.$emit('update:endDate', '2026-07-23')
      this.$emit('dateRangeChange', {
        startDate: '2026-07-22',
        endDate: '2026-07-23',
        preset: 'last24Hours'
      })
    },
    selectCustomRange() {
      this.$emit('update:startDate', '2026-07-01')
      this.$emit('update:endDate', '2026-07-07')
      this.$emit('dateRangeChange', {
        startDate: '2026-07-01',
        endDate: '2026-07-07',
        preset: null
      })
    }
  },
  template: `
    <div>
      <button data-test="last-24-hours" @click="selectLast24Hours">last 24 hours</button>
      <button data-test="custom-range" @click="selectCustomRange">custom range</button>
    </div>
  `
}

const AccountSharingStatsStub = {
  name: 'AccountSharingStatsStub',
  props: {
    stats: {
      type: Object,
      default: null
    },
    loading: Boolean,
    error: {
      type: String,
      default: ''
    }
  },
  template: '<section />'
}

const createDeferred = <T>() => {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const mountDashboard = () => mount(DashboardView, {
  global: {
    stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      LoadingSpinner: true,
      UserDashboardStats: true,
      UserDashboardCharts: DashboardChartsStub,
      UserDashboardRecentUsage: true,
      UserDashboardQuickActions: true,
      UserAccountSharingStats: AccountSharingStatsStub
    }
  }
})

describe('user DashboardView time range requests', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-23T12:34:56.789Z'))

    refreshUser.mockReset().mockResolvedValue(undefined)
    getDashboardStats.mockReset().mockResolvedValue({})
    getDashboardTrend.mockReset().mockResolvedValue({ trend: [] })
    getDashboardModels.mockReset().mockResolvedValue({ models: [] })
    getDashboardAccountSharing.mockReset().mockResolvedValue({})
    getByDateRange.mockReset().mockResolvedValue({ items: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses one exact rolling 24-hour range for all dashboard requests', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    getDashboardTrend.mockClear()
    getDashboardModels.mockClear()
    getDashboardAccountSharing.mockClear()

    await wrapper.get('[data-test="last-24-hours"]').trigger('click')
    await flushPromises()

    expect(getDashboardTrend).toHaveBeenCalledTimes(1)
    expect(getDashboardModels).toHaveBeenCalledTimes(1)
    expect(getDashboardAccountSharing).toHaveBeenCalledTimes(1)

    const trendParams = getDashboardTrend.mock.calls[0]?.[0]
    const modelParams = getDashboardModels.mock.calls[0]?.[0]
    const accountSharingParams = getDashboardAccountSharing.mock.calls[0]?.[0]
    const expectedTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone

    expect(trendParams).toEqual(expect.objectContaining({
      start_time: '2026-07-22T12:34:56.789Z',
      end_time: '2026-07-23T12:34:56.789Z',
      timezone: expectedTimezone,
      granularity: 'day'
    }))
    expect(modelParams).toEqual({
      start_time: trendParams.start_time,
      end_time: trendParams.end_time,
      timezone: expectedTimezone
    })
    expect(accountSharingParams).toEqual({
      start_time: trendParams.start_time,
      end_time: trendParams.end_time,
      timezone: expectedTimezone,
      granularity: 'day'
    })
    expect(trendParams).not.toHaveProperty('start_date')
    expect(trendParams).not.toHaveProperty('end_date')
  })

  it('keeps calendar-date parameters for non-last-24-hours ranges', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    getDashboardTrend.mockClear()
    getDashboardModels.mockClear()
    getDashboardAccountSharing.mockClear()

    await wrapper.get('[data-test="custom-range"]').trigger('click')
    await flushPromises()

    expect(getDashboardTrend).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-07',
      granularity: 'day'
    })
    expect(getDashboardModels).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-07'
    })
    expect(getDashboardAccountSharing).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-07',
      granularity: 'day'
    })
  })

  it('keeps the newest chart response when an older request finishes last', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const olderTrend = createDeferred<{ trend: Array<{ marker: string }> }>()
    const newerTrend = createDeferred<{ trend: Array<{ marker: string }> }>()
    const olderModels = createDeferred<{ models: Array<{ marker: string }> }>()
    const newerModels = createDeferred<{ models: Array<{ marker: string }> }>()
    getDashboardTrend
      .mockReset()
      .mockReturnValueOnce(olderTrend.promise)
      .mockReturnValueOnce(newerTrend.promise)
    getDashboardModels
      .mockReset()
      .mockReturnValueOnce(olderModels.promise)
      .mockReturnValueOnce(newerModels.promise)

    const charts = wrapper.findComponent(DashboardChartsStub)
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-01',
      endDate: '2026-07-07',
      preset: null
    })
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-08',
      endDate: '2026-07-14',
      preset: null
    })

    newerTrend.resolve({ trend: [{ marker: 'newer-trend' }] })
    newerModels.resolve({ models: [{ marker: 'newer-models' }] })
    await flushPromises()

    expect(charts.props('trend')).toEqual([{ marker: 'newer-trend' }])
    expect(charts.props('models')).toEqual([{ marker: 'newer-models' }])

    olderTrend.resolve({ trend: [{ marker: 'older-trend' }] })
    olderModels.resolve({ models: [{ marker: 'older-models' }] })
    await flushPromises()

    expect(charts.props('trend')).toEqual([{ marker: 'newer-trend' }])
    expect(charts.props('models')).toEqual([{ marker: 'newer-models' }])
    wrapper.unmount()
  })

  it('does not clear chart loading when an older request finishes first', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const olderTrend = createDeferred<{ trend: [] }>()
    const newerTrend = createDeferred<{ trend: [] }>()
    const olderModels = createDeferred<{ models: [] }>()
    const newerModels = createDeferred<{ models: [] }>()
    const olderSharing = createDeferred<Record<string, unknown>>()
    const newerSharing = createDeferred<Record<string, unknown>>()
    getDashboardTrend
      .mockReset()
      .mockReturnValueOnce(olderTrend.promise)
      .mockReturnValueOnce(newerTrend.promise)
    getDashboardModels
      .mockReset()
      .mockReturnValueOnce(olderModels.promise)
      .mockReturnValueOnce(newerModels.promise)
    getDashboardAccountSharing
      .mockReset()
      .mockReturnValueOnce(olderSharing.promise)
      .mockReturnValueOnce(newerSharing.promise)

    const charts = wrapper.findComponent(DashboardChartsStub)
    const sharing = wrapper.findComponent(AccountSharingStatsStub)
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-01',
      endDate: '2026-07-07',
      preset: null
    })
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-08',
      endDate: '2026-07-14',
      preset: null
    })

    olderTrend.resolve({ trend: [] })
    olderModels.resolve({ models: [] })
    olderSharing.resolve({ marker: 'older-sharing' })
    await flushPromises()

    expect(charts.props('loading')).toBe(true)
    expect(sharing.props('loading')).toBe(true)

    newerTrend.resolve({ trend: [] })
    newerModels.resolve({ models: [] })
    newerSharing.resolve({ marker: 'newer-sharing' })
    await flushPromises()

    expect(charts.props('loading')).toBe(false)
    expect(sharing.props('loading')).toBe(false)
    wrapper.unmount()
  })

  it('does not let an older account-sharing error replace newer success', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const olderSharing = createDeferred<Record<string, unknown>>()
    const newerSharing = createDeferred<Record<string, unknown>>()
    getDashboardAccountSharing
      .mockReset()
      .mockReturnValueOnce(olderSharing.promise)
      .mockReturnValueOnce(newerSharing.promise)

    const charts = wrapper.findComponent(DashboardChartsStub)
    const sharing = wrapper.findComponent(AccountSharingStatsStub)
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-01',
      endDate: '2026-07-07',
      preset: null
    })
    charts.vm.$emit('dateRangeChange', {
      startDate: '2026-07-08',
      endDate: '2026-07-14',
      preset: null
    })

    newerSharing.resolve({ marker: 'newer-sharing' })
    await flushPromises()

    expect(sharing.props('stats')).toEqual({ marker: 'newer-sharing' })
    expect(sharing.props('error')).toBe('')

    olderSharing.reject(new Error('older request failed'))
    await flushPromises()

    expect(sharing.props('stats')).toEqual({ marker: 'newer-sharing' })
    expect(sharing.props('error')).toBe('')
    wrapper.unmount()
  })

  it('invalidates pending dashboard responses when the view unmounts', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const pendingTrend = createDeferred<{ trend: Array<{ marker: string }> }>()
    const pendingModels = createDeferred<{ models: Array<{ marker: string }> }>()
    const pendingSharing = createDeferred<Record<string, unknown>>()
    const pendingStats = createDeferred<Record<string, unknown>>()
    const pendingRecent = createDeferred<{ items: Array<{ marker: string }> }>()
    getDashboardTrend.mockReset().mockReturnValueOnce(pendingTrend.promise)
    getDashboardModels.mockReset().mockReturnValueOnce(pendingModels.promise)
    getDashboardAccountSharing.mockReset().mockReturnValueOnce(pendingSharing.promise)
    getDashboardStats.mockReset().mockReturnValueOnce(pendingStats.promise)
    getByDateRange.mockReset().mockReturnValueOnce(pendingRecent.promise)

    const charts = wrapper.findComponent(DashboardChartsStub)
    charts.vm.$emit('refresh')
    await flushPromises()

    type DashboardSetupState = {
      stats: Record<string, unknown> | null
      trendData: Array<{ marker: string }>
      modelStats: Array<{ marker: string }>
      accountSharingStats: Record<string, unknown> | null
      recentUsage: Array<{ marker: string }>
      loading: boolean
      loadingCharts: boolean
      loadingAccountSharing: boolean
      loadingUsage: boolean
    }
    const internalInstance = Reflect.get(wrapper.vm, '$') as { setupState: DashboardSetupState }
    const dashboardState = internalInstance.setupState
    wrapper.unmount()

    pendingTrend.resolve({ trend: [{ marker: 'late-trend' }] })
    pendingModels.resolve({ models: [{ marker: 'late-models' }] })
    pendingSharing.resolve({ marker: 'late-sharing' })
    pendingStats.resolve({ marker: 'late-stats' })
    pendingRecent.resolve({ items: [{ marker: 'late-recent' }] })
    await flushPromises()

    expect(dashboardState.stats).toEqual({})
    expect(dashboardState.trendData).toEqual([])
    expect(dashboardState.modelStats).toEqual([])
    expect(dashboardState.accountSharingStats).toEqual({})
    expect(dashboardState.recentUsage).toEqual([])
    expect(dashboardState.loading).toBe(true)
    expect(dashboardState.loadingCharts).toBe(true)
    expect(dashboardState.loadingAccountSharing).toBe(true)
    expect(dashboardState.loadingUsage).toBe(true)
  })
})
