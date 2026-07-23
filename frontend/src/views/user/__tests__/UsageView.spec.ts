import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import UsageView from '../UsageView.vue'

const {
  query,
  queryBalanceLedger,
  getBalanceLedgerStats,
  getStatsByDateRange,
  list,
  showError,
  showWarning,
  showSuccess,
  showInfo,
} = vi.hoisted(() => ({
  query: vi.fn(),
  queryBalanceLedger: vi.fn(),
  getBalanceLedgerStats: vi.fn(),
  getStatsByDateRange: vi.fn(),
  list: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
}))

const messages: Record<string, string> = {
  'usage.costDetails': 'Cost Breakdown',
  'admin.usage.inputCost': 'Input Cost',
  'admin.usage.outputCost': 'Output Cost',
  'admin.usage.cacheCreationCost': 'Cache Creation Cost',
  'admin.usage.cacheReadCost': 'Cache Read Cost',
  'usage.inputTokenPrice': 'Input price',
  'usage.outputTokenPrice': 'Output price',
  'usage.perMillionTokens': '/ 1M tokens',
  'usage.serviceTier': 'Service tier',
  'usage.serviceTierPriority': 'Fast',
  'usage.serviceTierFlex': 'Flex',
  'usage.serviceTierStandard': 'Standard',
  'usage.rate': 'Rate',
  'usage.original': 'Original',
  'usage.billed': 'Billed',
  'usage.cacheHitRate': 'Cache Hit Rate',
  'usage.allApiKeys': 'All API Keys',
  'usage.apiKeyFilter': 'API Key',
  'usage.model': 'Model',
  'usage.reasoningEffort': 'Reasoning Effort',
  'usage.type': 'Type',
  'usage.tokens': 'Tokens',
  'usage.cost': 'Cost',
  'usage.firstToken': 'First Token',
  'usage.duration': 'Duration',
  'usage.time': 'Time',
  'usage.userAgent': 'User Agent',
  'usage.balanceLedger.reasons.account_share_income': 'Shared account income',
  'usage.balanceLedger.reasons.invite_share_income': 'Invite share income',
  'usage.balanceLedger.reasons.redeem_code': 'Recharge',
  'usage.balanceLedger.reasons.admin_adjustment': 'Admin balance adjustment',
  'usage.balanceLedger.labels.consumer': 'Consumer',
  'usage.balanceLedger.labels.apiKey': 'API Key',
  'usage.balanceLedger.labels.account': 'Account',
  'usage.balanceLedger.labels.requestId': 'Request',
  'usage.balanceLedger.labels.notes': 'Notes',
  'usage.balanceLedger.labels.operation': 'Operation',
  'usage.balanceLedger.labels.code': 'Code',
  'usage.balanceLedger.labels.reference': 'Reference ID',
}

vi.mock('@/api', () => ({
  usageAPI: {
    query,
    queryBalanceLedger,
    getBalanceLedgerStats,
    getStatsByDateRange,
  },
  keysAPI: {
    list,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: `
    <div>
      <div data-testid="usage-actions-slot"><slot name="actions" /></div>
      <div data-testid="usage-filters-slot"><slot name="filters" /></div>
      <div data-testid="usage-table-slot"><slot name="table" /></div>
      <div data-testid="usage-pagination-slot"><slot name="pagination" /></div>
    </div>
  `,
}

const mountUsageView = () =>
  mount(UsageView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        Pagination: true,
        EmptyState: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        Teleport: true,
      },
    },
  })

const createDeferred = <T,>() => {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const usageStats = (totalRequests: number) => ({
  total_requests: totalRequests,
  total_input_tokens: totalRequests,
  total_output_tokens: 0,
  total_cache_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: totalRequests,
  total_cost: totalRequests,
  total_actual_cost: totalRequests,
  average_duration_ms: totalRequests,
})

const usageLog = (index: number) => ({
  request_id: `req-export-${index}`,
  created_at: '2026-06-25T00:00:00Z',
  api_key: { name: 'snapshot-key' },
  model: 'gpt-5.4',
  reasoning_effort: null,
  inbound_endpoint: '/v1/responses',
  request_type: 'sync',
  billing_mode: 'standard',
  points_deducted: 0,
  balance_deducted: 0,
  input_tokens: 1,
  output_tokens: 1,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  rate_multiplier: 1,
  actual_cost: 0.01,
  total_cost: 0.01,
  first_token_ms: 1,
  duration_ms: 2,
})

let originalCreateObjectURL: typeof window.URL.createObjectURL | undefined
let originalRevokeObjectURL: typeof window.URL.revokeObjectURL | undefined

describe('user UsageView', () => {
  beforeEach(() => {
    query.mockReset()
    queryBalanceLedger.mockReset()
    getBalanceLedgerStats.mockReset()
    getStatsByDateRange.mockReset()
    list.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    originalCreateObjectURL = window.URL.createObjectURL
    originalRevokeObjectURL = window.URL.revokeObjectURL

    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      top: 20,
      left: 20,
      right: 120,
      bottom: 40,
      width: 100,
      height: 20,
      toJSON: () => ({}),
    } as DOMRect)

    ;(globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: true,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    window.localStorage.removeItem('table-page-size')
  })

  afterEach(() => {
    vi.restoreAllMocks()
    if (originalCreateObjectURL) {
      window.URL.createObjectURL = originalCreateObjectURL
    } else {
      Reflect.deleteProperty(window.URL, 'createObjectURL')
    }
    if (originalRevokeObjectURL) {
      window.URL.revokeObjectURL = originalRevokeObjectURL
    } else {
      Reflect.deleteProperty(window.URL, 'revokeObjectURL')
    }
  })

  it('keeps the latest usage stats when an aborted older request resolves last', async () => {
    query.mockResolvedValue({ items: [], total: 0, pages: 0 })
    list.mockResolvedValue({ items: [] })
    const olderRequest = createDeferred<ReturnType<typeof usageStats>>()
    getStatsByDateRange
      .mockImplementationOnce(() => olderRequest.promise)
      .mockResolvedValueOnce(usageStats(2))

    const wrapper = mountUsageView()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.filters.api_key_id = 42

    await setupState.loadUsageStats()

    expect(getStatsByDateRange).toHaveBeenCalledTimes(2)
    expect(getStatsByDateRange.mock.calls[0]?.[4]).toEqual(
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(getStatsByDateRange.mock.calls[0]?.[4].signal.aborted).toBe(true)
    expect(getStatsByDateRange.mock.calls[1]?.[4].signal.aborted).toBe(false)

    olderRequest.resolve(usageStats(1))
    await flushPromises()

    expect(setupState.usageStats.total_requests).toBe(2)
    wrapper.unmount()
  })

  it('ignores an aborted older stats failure after the latest request succeeds', async () => {
    query.mockResolvedValue({ items: [], total: 0, pages: 0 })
    list.mockResolvedValue({ items: [] })
    const olderRequest = createDeferred<ReturnType<typeof usageStats>>()
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    getStatsByDateRange
      .mockImplementationOnce(() => olderRequest.promise)
      .mockResolvedValueOnce(usageStats(2))

    const wrapper = mountUsageView()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.loadUsageStats()

    olderRequest.reject(new Error('stale stats failure'))
    await flushPromises()

    expect(setupState.usageStats.total_requests).toBe(2)
    expect(consoleError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('exports with one frozen query snapshot and the total returned by the export request', async () => {
    const firstExportPage = createDeferred<{
      items: ReturnType<typeof usageLog>[]
      total: number
      pages: number
    }>()
    query
      .mockResolvedValueOnce({ items: [], total: 500, pages: 25 })
      .mockImplementation((params: Record<string, unknown>) => {
        if (params.page === 1) return firstExportPage.promise
        if (params.page === 2) {
          return Promise.resolve({ items: [usageLog(101)], total: 101, pages: 2 })
        }
        return Promise.reject(new Error(`unexpected export page: ${String(params.page)}`))
      })
    getStatsByDateRange.mockResolvedValue(usageStats(0))
    list.mockResolvedValue({ items: [] })

    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export')
    window.URL.revokeObjectURL = vi.fn()
    let downloadedFilename = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
      downloadedFilename = this.download
    })

    const wrapper = mountUsageView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.filters.api_key_id = 7
    setupState.filters.start_date = '2026-06-01'
    setupState.filters.end_date = '2026-06-25'
    setupState.startDateTime = '2026-06-01T01:02:03'
    setupState.endDateTime = '2026-06-25T04:05:06'
    setupState.sortState.sort_by = 'model'
    setupState.sortState.sort_order = 'asc'

    const exportPromise = setupState.exportToCSV()
    const firstExportParams = query.mock.calls.at(-1)?.[0] as Record<string, unknown>

    setupState.filters.api_key_id = 99
    setupState.filters.start_date = '2025-01-01'
    setupState.filters.end_date = '2025-01-02'
    setupState.startDateTime = '2025-01-01T00:00:00'
    setupState.endDateTime = '2025-01-02T00:00:00'
    setupState.sortState.sort_by = 'created_at'
    setupState.sortState.sort_order = 'desc'

    firstExportPage.resolve({
      items: Array.from({ length: 100 }, (_, index) => usageLog(index + 1)),
      total: 101,
      pages: 2,
    })
    await exportPromise

    const exportCalls = query.mock.calls.slice(1)
    expect(exportCalls).toHaveLength(2)
    expect(firstExportParams).toEqual(
      expect.objectContaining({
        page: 1,
        page_size: 100,
        api_key_id: 7,
        start_date: '2026-06-01',
        end_date: '2026-06-25',
        sort_by: 'model',
        sort_order: 'asc',
      })
    )
    expect(exportCalls[1]?.[0]).toEqual({
      ...firstExportParams,
      page: 2,
    })
    expect(exportCalls[0]?.[1]).toEqual(
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(exportCalls[1]?.[1]).toEqual(
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(downloadedFilename).toBe('usage_2026-06-01_to_2026-06-25.csv')
    expect(showSuccess).toHaveBeenCalledTimes(1)
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('starts an export with a fresh query while the table total is still loading', async () => {
    const tableRequest = createDeferred<{ items: never[]; total: number; pages: number }>()
    query
      .mockImplementationOnce(() => tableRequest.promise)
      .mockResolvedValueOnce({ items: [usageLog(1)], total: 1, pages: 1 })
    getStatsByDateRange.mockResolvedValue(usageStats(0))
    list.mockResolvedValue({ items: [] })
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export')
    window.URL.revokeObjectURL = vi.fn()
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    const wrapper = mountUsageView()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.exportToCSV()

    expect(query).toHaveBeenCalledTimes(2)
    expect(query.mock.calls[1]?.[0]).toEqual(expect.objectContaining({ page: 1, page_size: 100 }))
    expect(showWarning).not.toHaveBeenCalledWith('usage.noDataToExport')
    expect(showSuccess).toHaveBeenCalledTimes(1)

    tableRequest.resolve({ items: [], total: 0, pages: 0 })
    await flushPromises()
    wrapper.unmount()
  })

  it('aborts in-flight stats and export requests when the view unmounts', async () => {
    const statsRequest = createDeferred<ReturnType<typeof usageStats>>()
    const exportRequest = createDeferred<{
      items: ReturnType<typeof usageLog>[]
      total: number
      pages: number
    }>()
    query
      .mockResolvedValueOnce({ items: [], total: 1, pages: 1 })
      .mockImplementationOnce(() => exportRequest.promise)
    getStatsByDateRange.mockImplementationOnce(() => statsRequest.promise)
    list.mockResolvedValue({ items: [] })

    const wrapper = mountUsageView()
    const setupState = (wrapper.vm as any).$?.setupState
    const exportPromise = setupState.exportToCSV()
    const statsConfig = getStatsByDateRange.mock.calls[0]?.[4]
    const exportConfig = query.mock.calls[1]?.[1]

    wrapper.unmount()

    expect(statsConfig?.signal.aborted).toBe(true)
    expect(exportConfig?.signal.aborted).toBe(true)

    statsRequest.reject(new DOMException('aborted', 'AbortError'))
    exportRequest.reject(new DOMException('aborted', 'AbortError'))
    await exportPromise
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
  })

  it('shows fast service tier and unit prices in user tooltip', async () => {
    query.mockResolvedValue({
      items: [
        {
          request_id: 'req-user-1',
          actual_cost: 0.092883,
          total_cost: 0.092883,
          rate_multiplier: 1,
          service_tier: 'priority',
          input_cost: 0.020285,
          output_cost: 0.00303,
          cache_creation_cost: 0,
          cache_read_cost: 0.069568,
          input_tokens: 4057,
          output_tokens: 101,
          cache_creation_tokens: 0,
          cache_read_tokens: 278272,
          cache_creation_5m_tokens: 0,
          cache_creation_1h_tokens: 0,
          image_count: 0,
          image_size: null,
          first_token_ms: null,
          duration_ms: 1,
          created_at: '2026-03-08T00:00:00Z',
        },
      ],
      total: 1,
      pages: 1,
    })
    getStatsByDateRange.mockResolvedValue({
      total_requests: 1,
      total_tokens: 100,
      total_cost: 0.1,
      avg_duration_ms: 1,
    })
    getBalanceLedgerStats.mockResolvedValue({
      total_entries: 0,
      credit_entries: 0,
      debit_entries: 0,
      credit_amount: '0',
      debit_amount: '0',
      net_amount: '0',
    })
    list.mockResolvedValue({ items: [] })

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Pagination: true,
          EmptyState: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await flushPromises()
    await nextTick()

    expect(wrapper.get('[data-testid="usage-actions-slot"] .data-stat-grid').exists()).toBe(true)
    expect(wrapper.get('[data-testid="usage-filters-slot"] .data-tabs-shell').exists()).toBe(true)
    expect(wrapper.get('[data-testid="usage-table-slot"] table').exists()).toBe(true)
    expect(wrapper.get('[data-testid="usage-pagination-slot"] pagination-stub').exists()).toBe(true)

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.tooltipData = {
      request_id: 'req-user-1',
      actual_cost: 0.092883,
      total_cost: 0.092883,
      rate_multiplier: 1,
      service_tier: 'priority',
      input_cost: 0.020285,
      output_cost: 0.00303,
      cache_creation_cost: 0,
      cache_read_cost: 0.069568,
      input_tokens: 4057,
      output_tokens: 101,
    }
    setupState.tooltipVisible = true
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Service tier')
    expect(text).toContain('Fast')
    expect(text).toContain('Rate')
    expect(text).toContain('1.00x')
    expect(text).toContain('Billed')
    expect(text).toContain('$0.092883')
    expect(text).toContain('$5.0000 / 1M tokens')
    expect(text).toContain('$30.0000 / 1M tokens')
  })

  it('exports csv with input and output unit price columns', async () => {
    const exportedLogs = [
      {
        request_id: 'req-user-export',
        actual_cost: 0.092883,
        total_cost: 0.092883,
        rate_multiplier: 1,
        service_tier: 'priority',
        input_cost: 0.020285,
        output_cost: 0.00303,
        cache_creation_cost: 0.000001,
        cache_read_cost: 0.069568,
        input_tokens: 4057,
        output_tokens: 101,
        cache_creation_tokens: 4,
        cache_read_tokens: 278272,
        cache_creation_5m_tokens: 0,
        cache_creation_1h_tokens: 0,
        image_count: 0,
        image_size: null,
        first_token_ms: 12,
        duration_ms: 345,
        created_at: '2026-03-08T00:00:00Z',
        model: 'gpt-5.4',
        reasoning_effort: null,
        api_key: { name: 'demo-key' },
      },
    ]

    query.mockResolvedValue({
      items: exportedLogs,
      total: 1,
      pages: 1,
    })
    getStatsByDateRange.mockResolvedValue({
      total_requests: 1,
      total_tokens: 100,
      total_cost: 0.1,
      avg_duration_ms: 1,
    })
    getBalanceLedgerStats.mockResolvedValue({
      total_entries: 0,
      credit_entries: 0,
      debit_entries: 0,
      credit_amount: '0',
      debit_amount: '0',
      net_amount: '0',
    })
    list.mockResolvedValue({ items: [] })

    let exportedBlob: Blob | null = null
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn((blob: Blob | MediaSource) => {
      exportedBlob = blob as Blob
      return 'blob:usage-export'
    }) as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Pagination: true,
          EmptyState: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.exportToCSV()

    expect(exportedBlob).not.toBeNull()
    const csvText = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result || ''))
      reader.onerror = () => reject(reader.error)
      reader.readAsText(exportedBlob!)
    })
    const csvBytes = await new Promise<Uint8Array>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer))
      reader.onerror = () => reject(reader.error)
      reader.readAsArrayBuffer(exportedBlob!)
    })
    expect(Array.from(csvBytes.slice(0, 3))).toEqual([0xef, 0xbb, 0xbf])
    expect(csvText).toContain('Cache Hit Rate')
    expect(csvText).toContain('98.6%')
    const hasSortedExportQuery = query.mock.calls.some((call) => {
      const params = call[0] as Record<string, unknown> | undefined
      const config = call[1]
      return (
        params?.page_size === 100 &&
        params?.sort_by === 'created_at' &&
        params?.sort_order === 'desc' &&
        config?.signal instanceof AbortSignal
      )
    })
    expect(hasSortedExportQuery).toBe(true)
    expect(clickSpy).toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalled()

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    clickSpy.mockRestore()
  })

  it('formats balance ledger income reasons and exact decimal amounts', async () => {
    query.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    getStatsByDateRange.mockResolvedValue({
      total_requests: 0,
      total_tokens: 0,
      total_cost: 0,
      avg_duration_ms: 0,
    })
    getBalanceLedgerStats.mockResolvedValue({
      total_entries: 1,
      credit_entries: 1,
      debit_entries: 0,
      credit_amount: '0.1234567891',
      debit_amount: '0',
      net_amount: '0.1234567891',
    })
    list.mockResolvedValue({ items: [] })

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Pagination: true,
          EmptyState: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    const row = {
      id: 1,
      user_id: 2,
      direction: 'credit',
      amount: '0.1234567891',
      reason: 'account_share_income',
      ref_type: 'usage_log',
      ref_id: 3,
      balance_after: '10.0000000001',
      metadata: {
        consumer_user_id: 9,
        api_key_id: 8,
        account_id: 7,
        request_id: 'req-income',
      },
      created_at: '2026-06-16T00:00:00Z',
    }

    expect(setupState.getLedgerReasonLabel('account_share_income')).toBe('Shared account income')
    expect(setupState.getLedgerReasonLabel('invite_share_income')).toBe('Invite share income')
    expect(setupState.getLedgerReasonLabel('redeem_code')).toBe('Recharge')
    expect(setupState.formatLedgerAmount(row)).toBe('+$0.1234567891')
    expect(setupState.formatLedgerCurrency(row.balance_after)).toBe('$10.0000000001')
    expect(setupState.getLedgerRemark(row)).toBe('Consumer: 9 · API Key: 8 · Account: 7 · Request: req-income')

    const adminRow = {
      ...row,
      id: 2,
      reason: 'admin_adjustment',
      ref_type: 'redeem_code',
      ref_id: 88,
      metadata: {
        notes: 'manual top-up',
        operation: 'add',
        code: 'ADJUST-88',
      },
    }
    expect(setupState.getLedgerReasonLabel('admin_adjustment')).toBe('Admin balance adjustment')
    expect(setupState.getLedgerRemark(adminRow)).toBe('Notes: manual top-up · Operation: add · Code: ADJUST-88 · Reference ID: 88')
  })

  it('loads user balance ledger stats with exact time and reference filters', async () => {
    query.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    queryBalanceLedger.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    getBalanceLedgerStats.mockResolvedValue({
      total_entries: 2,
      credit_entries: 1,
      debit_entries: 1,
      credit_amount: '0.25',
      debit_amount: '0.10',
      net_amount: '0.15',
    })
    getStatsByDateRange.mockResolvedValue({
      total_requests: 0,
      total_tokens: 0,
      total_cost: 0,
      avg_duration_ms: 0,
    })
    list.mockResolvedValue({ items: [] })

    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          Pagination: true,
          EmptyState: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.startDateTime = '2026-06-25T00:00:00'
    setupState.endDateTime = '2026-06-25T01:00:00'
    setupState.ledgerFilters = {
      direction: 'credit',
      reason: 'account_share_income',
      ref_type: 'usage_log',
      ref_id: 123,
    }
    await setupState.switchUsageTab('balanceLedger')
    await flushPromises()

    expect(queryBalanceLedger).toHaveBeenCalledWith(
      expect.objectContaining({
        direction: 'credit',
        reason: 'account_share_income',
        ref_type: 'usage_log',
        ref_id: 123,
        start_time: expect.any(String),
        end_time: expect.any(String),
        timezone: expect.any(String),
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(getBalanceLedgerStats).toHaveBeenCalledWith(
      expect.objectContaining({
        direction: 'credit',
        reason: 'account_share_income',
        ref_type: 'usage_log',
        ref_id: 123,
        start_time: expect.any(String),
        end_time: expect.any(String),
        timezone: expect.any(String),
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    const statsParams = getBalanceLedgerStats.mock.calls.at(-1)?.[0] as Record<string, unknown>
    expect(statsParams.page).toBeUndefined()
    expect(statsParams.page_size).toBeUndefined()
    expect(statsParams.sort_order).toBeUndefined()
    expect(setupState.ledgerSummaryCards[0].value).toBe('2')
    expect(setupState.ledgerSummaryCards[3].value).toBe('+$0.15')
  })
})
