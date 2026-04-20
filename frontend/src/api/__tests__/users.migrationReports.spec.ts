import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import {
  getAuthIdentityMigrationReportSummary,
  listAuthIdentityMigrationReports,
  resolveAuthIdentityMigrationReport,
} from '@/api/admin/users'

describe('admin users auth identity migration reports API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('lists migration reports with pagination and report type filter', async () => {
    const response = {
      items: [],
      total: 0,
      page: 2,
      page_size: 10,
      pages: 0,
    }
    get.mockResolvedValue({ data: response })

    const result = await listAuthIdentityMigrationReports({
      page: 2,
      pageSize: 10,
      reportType: 'oidc_synthetic_email_requires_manual_recovery',
    })

    expect(get).toHaveBeenCalledWith('/admin/users/auth-identity-migration-reports', {
      params: {
        page: 2,
        page_size: 10,
        report_type: 'oidc_synthetic_email_requires_manual_recovery',
      },
    })
    expect(result).toBe(response)
  })

  it('loads migration report summary', async () => {
    const response = {
      total: 2,
      open_total: 1,
      resolved_total: 1,
      by_type: {
        oidc_synthetic_email_requires_manual_recovery: 2,
      },
    }
    get.mockResolvedValue({ data: response })

    const result = await getAuthIdentityMigrationReportSummary()

    expect(get).toHaveBeenCalledWith('/admin/users/auth-identity-migration-reports/summary')
    expect(result).toBe(response)
  })

  it('submits report resolution note', async () => {
    const response = {
      id: 7,
      resolution_note: 'resolved by admin',
    }
    post.mockResolvedValue({ data: response })

    const result = await resolveAuthIdentityMigrationReport(7, 'resolved by admin')

    expect(post).toHaveBeenCalledWith('/admin/users/auth-identity-migration-reports/7/resolve', {
      resolution_note: 'resolved by admin',
    })
    expect(result).toBe(response)
  })
})
