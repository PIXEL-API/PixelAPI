import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { CyberPolicyRequestDetail, CyberPolicyRequestRecord } from '@/api/admin/riskControl'
import CyberPolicyRequestsPanel from '../CyberPolicyRequestsPanel.vue'

const {
  exportCyberPolicyRequests,
  getCyberPolicyRequest,
  listCyberPolicyRequests,
  saveAs,
  showError,
  showSuccess,
  showWarning,
} = vi.hoisted(() => ({
  exportCyberPolicyRequests: vi.fn(),
  getCyberPolicyRequest: vi.fn(),
  listCyberPolicyRequests: vi.fn(),
  saveAs: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      exportCyberPolicyRequests,
      getCyberPolicyRequest,
      listCyberPolicyRequests,
    },
  },
}))

vi.mock('file-saver', () => ({ saveAs }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess, showWarning }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => `formatted:${value}`,
}))

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const PaginationStub = defineComponent({
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: '<div data-testid="pagination-stub">{{ page }} / {{ total }} / {{ pageSize }}</div>',
})

const record: CyberPolicyRequestRecord = {
  id: 101,
  created_at: '2026-08-11T03:00:00Z',
  request_id: 'req-cyber-101',
  user_id: 445,
  user_name: 'alice',
  user_email: 'alice@example.com',
  group_id: 1198,
  group_name: 'PRO 共享号池',
  api_key_id: 9,
  api_key_name: 'production-key',
  account_id: 22,
  account_name: 'openai-pro',
  requested_model: 'gpt-5.6',
  upstream_model: 'gpt-5.6',
  inbound_endpoint: '/v1/responses',
  upstream_endpoint: '/backend-api/codex/responses',
  status_code: 403,
  upstream_status_code: 403,
  provider_error_code: 'cyber_policy',
  upstream_error_message: 'blocked by cyber policy',
  request_content_preview: 'redacted preview',
  request_content_truncated: true,
  request_content_bytes: 8192,
}

const detail: CyberPolicyRequestDetail = {
  ...record,
  request_content: '<script>alert("never execute")</script>\nredacted content',
  upstream_error_detail: 'upstream detail',
  upstream_errors: '[]',
}

function listResponse(items: CyberPolicyRequestRecord[] = [record]) {
  return {
    items,
    total: items.length,
    page: 1,
    page_size: 20,
    pages: 1,
  }
}

function mountPanel() {
  return mount(CyberPolicyRequestsPanel, {
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Pagination: PaginationStub,
        Icon: true,
      },
    },
  })
}

describe('CyberPolicyRequestsPanel', () => {
  beforeEach(() => {
    exportCyberPolicyRequests.mockReset()
    getCyberPolicyRequest.mockReset()
    listCyberPolicyRequests.mockReset()
    saveAs.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    listCyberPolicyRequests.mockResolvedValue(listResponse())
  })

  it('loads only after the panel mounts and renders identity, group, and truncated content state', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(listCyberPolicyRequests).toHaveBeenCalledOnce()
    expect(listCyberPolicyRequests).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1, page_size: 20 }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('PRO 共享号池')
    expect(wrapper.text()).toContain('redacted preview')
    expect(wrapper.text()).toContain('admin.riskControl.cyberRequests.truncated')
    expect(wrapper.get('[data-testid="cyber-requests-table-scroll"]').classes()).toContain('overflow-x-auto')
  })

  it('applies user and group fuzzy filters to the first page', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    listCyberPolicyRequests.mockClear()

    await wrapper.get('[data-testid="cyber-requests-user-query"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="cyber-requests-group-query"]').setValue('PRO 共享号池')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(listCyberPolicyRequests).toHaveBeenCalledWith(
      expect.objectContaining({
        page: 1,
        user_query: 'alice@example.com',
        group_query: 'PRO 共享号池',
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('loads request content as plain text and shows the mandatory redaction notice', async () => {
    getCyberPolicyRequest.mockResolvedValue(detail)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('tbody button').trigger('click')
    await flushPromises()

    expect(getCyberPolicyRequest).toHaveBeenCalledWith(
      101,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    const detailPanel = wrapper.get('[data-testid="cyber-request-detail"]')
    expect(detailPanel.text()).toContain('admin.riskControl.cyberRequests.contentNotice')
    expect(detailPanel.text()).toContain('<script>alert("never execute")</script>')
    expect(detailPanel.find('script').exists()).toBe(false)
  })

  it('exports the current filters and warns when the server truncates the CSV', async () => {
    const blob = new Blob(['csv'], { type: 'text/csv' })
    exportCyberPolicyRequests.mockResolvedValue({
      blob,
      filename: 'cyber-policy-requests.csv',
      truncated: true,
      limit: 1000,
    })
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="cyber-requests-user-query"]').setValue('alice')

    await wrapper.get('[data-testid="cyber-requests-export"]').trigger('click')
    await flushPromises()

    expect(exportCyberPolicyRequests).toHaveBeenCalledWith(
      expect.objectContaining({ user_query: 'alice', page: undefined, page_size: undefined }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(saveAs).toHaveBeenCalledWith(blob, 'cyber-policy-requests.csv')
    expect(showWarning).toHaveBeenCalledWith(expect.stringContaining('1000'))
    expect(wrapper.get('[data-testid="cyber-requests-export-truncated"]').text()).toContain('1000')
  })

  it('keeps the export action disabled and aria-busy while exporting', async () => {
    let resolveExport!: (value: unknown) => void
    exportCyberPolicyRequests.mockReturnValue(new Promise((resolve) => { resolveExport = resolve }))
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="cyber-requests-export"]').trigger('click')
    await nextTick()
    const button = wrapper.get('[data-testid="cyber-requests-export"]')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-busy')).toBe('true')

    resolveExport({ blob: new Blob(['csv']), filename: 'requests.csv', truncated: false, limit: 1000 })
    await flushPromises()
    expect(button.attributes('disabled')).toBeUndefined()
    expect(button.attributes('aria-busy')).toBe('false')
  })

  it('rejects an invalid time range before calling the list API', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    listCyberPolicyRequests.mockClear()

    await wrapper.get('#cyber-requests-from').setValue('2026-08-12T00:00')
    await wrapper.get('#cyber-requests-to').setValue('2026-08-11T00:00')
    await wrapper.get('form').trigger('submit')

    expect(listCyberPolicyRequests).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.riskControl.cyberRequests.invalidRange')
  })
})
