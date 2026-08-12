import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import {
  exportCyberPolicyRequests,
  getCyberPolicyRequest,
  listCyberPolicyRequests,
  type CyberPolicyRequestDetail,
  type CyberPolicyRequestRecord,
} from '@/api/admin/riskControl'

const record: CyberPolicyRequestRecord = {
  id: 7,
  created_at: '2026-08-11T03:00:00Z',
  request_id: 'req-7',
  user_id: 445,
  user_name: 'alice',
  user_email: 'alice@example.com',
  group_id: 1198,
  group_name: 'PRO 共享号池',
  api_key_id: 9,
  api_key_name: 'key',
  account_id: 22,
  account_name: 'account',
  requested_model: 'gpt-5.6',
  upstream_model: 'gpt-5.6',
  inbound_endpoint: '/v1/responses',
  upstream_endpoint: '/backend-api/codex/responses',
  status_code: 403,
  upstream_status_code: 403,
  provider_error_code: 'cyber_policy',
  upstream_error_message: 'blocked',
  request_content_preview: 'preview',
  request_content_truncated: true,
  request_content_bytes: 8192,
}

describe('admin risk-control Cyber Policy request API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('lists requests with the complete server-side filter and abort signal', async () => {
    const controller = new AbortController()
    const response = { items: [record], total: 1, page: 2, page_size: 20, pages: 3 }
    get.mockResolvedValue({ data: response })
    const filters = {
      page: 2,
      page_size: 20,
      group_query: 'PRO 共享号池',
      user_query: 'alice@example.com',
      model: 'gpt-5.6',
      endpoint: '/v1/responses',
      from: '2026-08-10T00:00:00Z',
      to: '2026-08-11T00:00:00Z',
    }

    await expect(listCyberPolicyRequests(filters, { signal: controller.signal })).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/risk-control/cyber-policy/requests', {
      params: filters,
      signal: controller.signal,
    })
  })

  it('loads the exact request detail as redacted plain-text data', async () => {
    const controller = new AbortController()
    const detail: CyberPolicyRequestDetail = {
      ...record,
      request_content: 'redacted content',
      upstream_error_detail: 'detail',
      upstream_errors: '[]',
    }
    get.mockResolvedValue({ data: detail })

    await expect(getCyberPolicyRequest(7, { signal: controller.signal })).resolves.toEqual(detail)
    expect(get).toHaveBeenCalledWith('/admin/risk-control/cyber-policy/requests/7', {
      signal: controller.signal,
    })
  })

  it('returns the CSV blob, filename, export limit, and truncation marker', async () => {
    const blob = new Blob(['csv'], { type: 'text/csv' })
    const controller = new AbortController()
    get.mockResolvedValue({
      data: blob,
      headers: {
        'content-disposition': 'attachment; filename="fallback.csv"',
        'x-export-filename': 'cyber-policy-requests.csv',
        'x-export-truncated': 'true',
        'x-export-limit': '1000',
      },
    })
    const filters = { user_query: 'alice', group_query: '1198' }

    await expect(exportCyberPolicyRequests(filters, { signal: controller.signal })).resolves.toEqual({
      blob,
      filename: 'cyber-policy-requests.csv',
      truncated: true,
      limit: 1000,
    })
    expect(get).toHaveBeenCalledWith('/admin/risk-control/cyber-policy/requests/export', {
      params: filters,
      responseType: 'blob',
      signal: controller.signal,
    })
  })
})
