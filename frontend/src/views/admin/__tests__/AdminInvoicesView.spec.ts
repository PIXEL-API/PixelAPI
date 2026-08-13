import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { BasePaginationResponse, InvoiceRequest } from '@/types'
import AdminInvoicesView from '../invoices/AdminInvoicesView.vue'

const { aoaToSheet, appendSheet, bookNew, showError, showSuccess, saveAs, writeXlsx, listInvoices } = vi.hoisted(() => ({
  aoaToSheet: vi.fn(() => ({})),
  appendSheet: vi.fn(),
  bookNew: vi.fn(() => ({ sheets: [] })),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  saveAs: vi.fn(),
  writeXlsx: vi.fn(() => new Uint8Array([1, 2, 3])),
  listInvoices: vi.fn()
}))

vi.mock('xlsx', () => ({
  utils: {
    aoa_to_sheet: aoaToSheet,
    book_new: bookNew,
    book_append_sheet: appendSheet
  },
  write: writeXlsx
}))

vi.mock('file-saver', () => ({ saveAs }))

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
    showError,
    showSuccess
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
    aoaToSheet.mockClear()
    appendSheet.mockClear()
    bookNew.mockClear()
    showError.mockClear()
    showSuccess.mockClear()
    saveAs.mockClear()
    writeXlsx.mockClear()
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

  it('exports selected invoices with the stable BIFF8 contract', async () => {
    const invoice = createInvoice(7)
    invoice.invoice_type = 'enterprise_special'
    invoice.title_name = '中文测试企业'
    invoice.tax_id = '00123456789'
    invoice.amount = 123.45
    invoice.bank_name = '测试银行'
    invoice.bank_account = '000012345678'
    invoice.remark = '长备注 & 特殊字符'
    listInvoices.mockResolvedValueOnce({ data: createPage(1, 50, 1, [invoice]) })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('tbody input[type="checkbox"]').setValue(true)
    const exportButton = wrapper.findAll('button').find((item) => item.text().startsWith('批量导出'))
    if (!exportButton) throw new Error('批量导出按钮不存在')
    await exportButton.trigger('click')
    await flushPromises()

    expect(aoaToSheet).toHaveBeenCalledOnce()
    const rows = aoaToSheet.mock.calls[0]?.[0]
    expect(rows).toHaveLength(2)
    expect(rows[1]).toEqual([
      { t: 'n', v: 1 },
      { t: 's', v: '专票' },
      { t: 's', v: '中文测试企业' },
      { t: 's', v: '00123456789' },
      { t: 's', v: '信息服务费' },
      { t: 'n', v: 123.45 },
      { t: 's', v: 'invoice-7@example.com' },
      { t: 's', v: '测试银行' },
      { t: 's', v: '000012345678' },
      { t: 's', v: '长备注 & 特殊字符' }
    ])
    expect(appendSheet).toHaveBeenCalledWith(expect.anything(), expect.anything(), 'Sheet1')
    expect(writeXlsx).toHaveBeenCalledWith(expect.anything(), {
      type: 'array',
      bookType: 'biff8',
      bookSST: true
    })
    expect(saveAs).toHaveBeenCalledOnce()
    const [blob, filename] = saveAs.mock.calls[0] ?? []
    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('application/vnd.ms-excel')
    expect(filename).toMatch(/^批量开票-\d{8}-\d{6}\.xls$/)
    expect(showSuccess).toHaveBeenCalledWith('已导出 1 条发票申请')
    expect(showError).not.toHaveBeenCalled()
  })
})
