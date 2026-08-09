import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { BasePaginationResponse, InvoiceRequest } from '@/types'
import AdminInvoicesView from '../invoices/AdminInvoicesView.vue'

const { listInvoices } = vi.hoisted(() => ({
  listInvoices: vi.fn()
}))

vi.mock('@/api/admin/invoices', () => ({
  default: {
    list: listInvoices,
    get: vi.fn(),
    issue: vi.fn(),
    reject: vi.fn()
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'pageSize', 'total'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div
      data-test="pagination"
      :data-page="page"
      :data-page-size="pageSize"
      :data-total="total"
    >
      <button data-test="page-2" @click="$emit('update:page', 2)">第2页</button>
      <button data-test="page-size-100" @click="$emit('update:pageSize', 100)">每页100条</button>
    </div>
  `
}

function createInvoice(id = 1): InvoiceRequest {
  return {
    id,
    request_no: `INV-${id}`,
    user_id: id,
    user_email: `buyer-${id}@example.com`,
    invoice_type: 'enterprise_normal',
    buyer_type: 'enterprise',
    title_name: `测试企业${id}`,
    tax_id: `TAX-${id}`,
    registered_address: '',
    registered_phone: '',
    bank_name: '',
    bank_account: '',
    recipient_email: `invoice-${id}@example.com`,
    recipient_phone: '',
    remark: '',
    amount: 100,
    currency: 'CNY',
    status: 'pending',
    submitted_at: '2026-08-10T00:00:00Z',
    created_at: '2026-08-10T00:00:00Z',
    updated_at: '2026-08-10T00:00:00Z'
  }
}

function createPage(
  page: number,
  pageSize: number,
  total = 120,
  items: InvoiceRequest[] = [createInvoice(page)]
): BasePaginationResponse<InvoiceRequest> {
  return {
    items,
    total,
    page,
    page_size: pageSize,
    pages: total === 0 ? 0 : Math.ceil(total / pageSize)
  }
}

function mountView() {
  return mount(AdminInvoicesView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Pagination: PaginationStub
      }
    }
  })
}

function queryButton(wrapper: ReturnType<typeof mountView>) {
  const button = wrapper.findAll('button').find((item) => item.text() === '查询')
  if (!button) throw new Error('查询按钮不存在')
  return button
}

function createDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolver) => {
    resolve = resolver
  })
  return { promise, resolve }
}

describe('admin AdminInvoicesView pagination', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('table-page-size', '50')
    listInvoices.mockReset()
    listInvoices.mockImplementation((params: { page?: number; page_size?: number }) => {
      const page = params.page ?? 1
      const pageSize = params.page_size ?? 50
      return Promise.resolve({ data: createPage(page, pageSize) })
    })
  })

  it('uses the backend pagination contract and renders its totals', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listInvoices).toHaveBeenCalledWith(expect.objectContaining({
      page: 1,
      page_size: 50
    }))
    expect(wrapper.get('[data-test="pagination"]').attributes()).toMatchObject({
      'data-page': '1',
      'data-page-size': '50',
      'data-total': '120'
    })
  })

  it('loads another page and clears the current-page batch selection', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('tbody input[type="checkbox"]').setValue(true)
    expect(wrapper.text()).toContain('批量导出（1）')

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()

    expect(listInvoices).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))
    expect(wrapper.text()).toContain('批量导出（0）')
  })

  it('returns to page one when status or keyword changes', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    await wrapper.get('select').setValue('issued')
    await flushPromises()
    expect(listInvoices).toHaveBeenLastCalledWith(expect.objectContaining({
      page: 1,
      status: 'issued'
    }))

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    await wrapper.get('input[type="search"]').setValue('INV-100')
    await queryButton(wrapper).trigger('click')
    await flushPromises()
    expect(listInvoices).toHaveBeenLastCalledWith(expect.objectContaining({
      page: 1,
      keyword: 'INV-100'
    }))
  })

  it('returns to page one when the page size changes', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="page-size-100"]').trigger('click')
    await flushPromises()

    expect(listInvoices).toHaveBeenLastCalledWith(expect.objectContaining({
      page: 1,
      page_size: 100
    }))
  })

  it('reloads the last valid page when the requested page becomes empty', async () => {
    listInvoices
      .mockResolvedValueOnce({ data: createPage(1, 50, 100) })
      .mockResolvedValueOnce({
        data: { ...createPage(2, 50, 1, []), pages: 1 }
      })
      .mockResolvedValueOnce({ data: createPage(1, 50, 1) })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()

    expect(listInvoices).toHaveBeenNthCalledWith(2, expect.objectContaining({ page: 2 }))
    expect(listInvoices).toHaveBeenNthCalledWith(3, expect.objectContaining({ page: 1 }))
    expect(wrapper.get('[data-test="pagination"]').attributes('data-page')).toBe('1')
  })

  it('ignores an older page response that arrives after a newer filter response', async () => {
    const olderPageResponse = createDeferred<{
      data: BasePaginationResponse<InvoiceRequest>
    }>()
    listInvoices.mockImplementation(
      (params: { page?: number; page_size?: number; status?: string }) => {
        if (params.page === 2) return olderPageResponse.promise
        if (params.status === 'issued') {
          return Promise.resolve({
            data: createPage(1, params.page_size ?? 50, 1, [createInvoice(99)])
          })
        }
        return Promise.resolve({
          data: createPage(params.page ?? 1, params.page_size ?? 50)
        })
      }
    )

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await wrapper.get('select').setValue('issued')
    await flushPromises()

    expect(wrapper.text()).toContain('INV-99')
    expect(wrapper.get('[data-test="pagination"]').attributes('data-page')).toBe('1')

    olderPageResponse.resolve({ data: createPage(2, 50, 120, [createInvoice(2)]) })
    await flushPromises()

    expect(wrapper.text()).toContain('INV-99')
    expect(wrapper.text()).not.toContain('INV-2')
    expect(wrapper.get('[data-test="pagination"]').attributes('data-page')).toBe('1')
  })
})
