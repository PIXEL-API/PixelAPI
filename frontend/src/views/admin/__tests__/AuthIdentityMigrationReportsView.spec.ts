import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'

import AuthIdentityMigrationReportsView from '../AuthIdentityMigrationReportsView.vue'

const {
  bindUserAuthIdentity,
  getAuthIdentityMigrationReportSummary,
  listAuthIdentityMigrationReports,
  resolveAuthIdentityMigrationReport,
} = vi.hoisted(() => ({
  bindUserAuthIdentity: vi.fn(),
  getAuthIdentityMigrationReportSummary: vi.fn(),
  listAuthIdentityMigrationReports: vi.fn(),
  resolveAuthIdentityMigrationReport: vi.fn(),
}))

const { showError, showSuccess } = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      bindUserAuthIdentity,
      getAuthIdentityMigrationReportSummary,
      listAuthIdentityMigrationReports,
      resolveAuthIdentityMigrationReport,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en' },
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string | null | undefined) => value ?? '',
}))

const sampleReport = {
  id: 1,
  report_type: 'oidc_synthetic_email_requires_manual_recovery',
  report_key: 'legacy@example.invalid',
  details: {
    user_id: 42,
    legacy_email: 'legacy@example.invalid',
    provider_key: 'https://issuer.example',
    provider_subject: 'subject-123',
  },
  created_at: '2026-04-20T01:02:03Z',
  resolved_at: null,
  resolved_by_user_id: null,
  resolution_note: '',
}

const summaryResponse = {
  total: 2,
  open_total: 1,
  resolved_total: 1,
  by_type: {
    oidc_synthetic_email_requires_manual_recovery: 2,
  },
}

const listResponse = {
  items: [sampleReport],
  total: 1,
  page: 1,
  page_size: 20,
  pages: 1,
}

const AppLayoutStub = defineComponent({
  setup(_, { slots }) {
    return () => h('div', slots.default?.())
  },
})

const TablePageLayoutStub = defineComponent({
  setup(_, { slots }) {
    return () => h('div', [
      slots.actions?.(),
      slots.filters?.(),
      slots.table?.(),
      slots.default?.(),
      slots.pagination?.(),
    ])
  },
})

const DataTableStub = defineComponent({
  props: {
    columns: { type: Array, default: () => [] },
    data: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false },
  },
  setup(props, { slots }) {
    return () => h('div', { 'data-test': 'data-table' }, [
      props.loading
        ? h('div', 'loading')
        : (props.data as Array<Record<string, unknown>>).map((row) =>
            h(
              'div',
              { key: String(row.id ?? row.report_key) },
              (props.columns as Array<{ key: string }>).map((column) => {
                const slot = slots[`cell-${column.key}`]
                return h(
                  'div',
                  { key: column.key, [`data-test-cell`]: `${String(row.id)}-${column.key}` },
                  slot
                    ? slot({ row, value: row[column.key] })
                    : String(row[column.key] ?? '')
                )
              })
            )
          ),
    ])
  },
})

const PaginationStub = defineComponent({
  props: {
    total: { type: Number, required: true },
    page: { type: Number, required: true },
    pageSize: { type: Number, required: true },
  },
  emits: ['update:page', 'update:pageSize'],
  setup(props, { emit }) {
    return () => h('div', { 'data-test': 'pagination' }, [
      h('button', {
        type: 'button',
        'data-test': 'next-page',
        onClick: () => emit('update:page', props.page + 1),
      }, 'next'),
      h('button', {
        type: 'button',
        'data-test': 'page-size-50',
        onClick: () => emit('update:pageSize', 50),
      }, '50'),
    ])
  },
})

describe('AuthIdentityMigrationReportsView', () => {
  beforeEach(() => {
    getAuthIdentityMigrationReportSummary.mockReset()
    listAuthIdentityMigrationReports.mockReset()
    resolveAuthIdentityMigrationReport.mockReset()
    bindUserAuthIdentity.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    getAuthIdentityMigrationReportSummary.mockResolvedValue(summaryResponse)
    listAuthIdentityMigrationReports.mockResolvedValue(listResponse)
    resolveAuthIdentityMigrationReport.mockResolvedValue({
      ...sampleReport,
      resolved_at: '2026-04-20T02:00:00Z',
      resolved_by_user_id: 100,
      resolution_note: 'resolved by admin',
    })
    bindUserAuthIdentity.mockResolvedValue({
      identity_id: 77,
      provider_type: 'oidc',
      provider_key: 'https://issuer.example',
      provider_subject: 'subject-123',
    })
  })

  const mountView = () =>
    mount(AuthIdentityMigrationReportsView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          DataTable: DataTableStub,
          Pagination: PaginationStub,
          Icon: true,
        },
      },
    })

  it('loads summary and first page of reports on mount', async () => {
    const wrapper = mountView()

    await flushPromises()

    expect(getAuthIdentityMigrationReportSummary).toHaveBeenCalledTimes(1)
    expect(listAuthIdentityMigrationReports).toHaveBeenCalledWith({
      page: 1,
      pageSize: 20,
      reportType: '',
    })
    expect(wrapper.get('[data-test="summary-total"]').text()).toContain('2')
    expect(wrapper.get('[data-test="summary-open"]').text()).toContain('1')
    expect(wrapper.get('[data-test="summary-resolved"]').text()).toContain('1')
    expect(wrapper.text()).toContain('legacy@example.invalid')
  })

  it('reloads list when the report type filter changes', async () => {
    const wrapper = mountView()

    await flushPromises()

    listAuthIdentityMigrationReports.mockClear()

    await wrapper.get('[data-test="report-type-filter"]').setValue(
      'oidc_synthetic_email_requires_manual_recovery'
    )
    await flushPromises()

    expect(listAuthIdentityMigrationReports).toHaveBeenCalledWith({
      page: 1,
      pageSize: 20,
      reportType: 'oidc_synthetic_email_requires_manual_recovery',
    })
  })

  it('submits resolve note for the selected report and refreshes data', async () => {
    const wrapper = mountView()

    await flushPromises()

    getAuthIdentityMigrationReportSummary.mockClear()
    listAuthIdentityMigrationReports.mockClear()

    await wrapper.get('[data-test="select-report-1"]').trigger('click')
    await wrapper.get('[data-test="resolution-note"]').setValue('resolved by admin')
    await wrapper.get('[data-test="resolve-submit"]').trigger('click')
    await flushPromises()

    expect(resolveAuthIdentityMigrationReport).toHaveBeenCalledWith(1, 'resolved by admin')
    expect(showSuccess).toHaveBeenCalled()
    expect(getAuthIdentityMigrationReportSummary).toHaveBeenCalledTimes(1)
    expect(listAuthIdentityMigrationReports).toHaveBeenCalledWith({
      page: 1,
      pageSize: 20,
      reportType: '',
    })
  })

  it('pre-fills and submits remediation binding for the selected report', async () => {
    const wrapper = mountView()

    await flushPromises()
    await wrapper.get('[data-test="select-report-1"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-test="remediation-user-id"]').element as HTMLInputElement).value).toBe('42')
    expect((wrapper.get('[data-test="remediation-provider-type"]').element as HTMLInputElement).value).toBe('oidc')
    expect((wrapper.get('[data-test="remediation-provider-key"]').element as HTMLInputElement).value).toBe(
      'https://issuer.example'
    )
    expect((wrapper.get('[data-test="remediation-provider-subject"]').element as HTMLInputElement).value).toBe(
      'subject-123'
    )

    await wrapper.get('[data-test="remediation-submit"]').trigger('click')
    await flushPromises()

    expect(bindUserAuthIdentity).toHaveBeenCalledWith(42, {
      provider_type: 'oidc',
      provider_key: 'https://issuer.example',
      provider_subject: 'subject-123',
      issuer: undefined,
      metadata: {},
    })
    expect(showSuccess).toHaveBeenCalled()
  })

  it('keeps report type filter options available from list data when summary fails', async () => {
    getAuthIdentityMigrationReportSummary.mockRejectedValueOnce(new Error('summary failed'))
    listAuthIdentityMigrationReports.mockResolvedValueOnce(listResponse)

    const wrapper = mountView()

    await flushPromises()

    const options = wrapper
      .get('[data-test="report-type-filter"]')
      .findAll('option')
      .map((node) => node.element.value)

    expect(showError).toHaveBeenCalled()
    expect(options).toContain('oidc_synthetic_email_requires_manual_recovery')
  })
})
