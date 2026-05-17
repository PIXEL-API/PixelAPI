import { apiClient } from './client'
import type {
  CreateStoreOrderRequest,
  StoreCategory,
  StoreListParams,
  StoreOrder,
  StoreProduct,
} from '@/types/store'
import type { BasePaginationResponse } from '@/types'

function normalizeCategory<T extends StoreCategory>(category: T): T {
  return {
    ...category,
    status: category.enabled ? 'active' : 'inactive',
  }
}

function normalizeProduct<T extends StoreProduct>(product: T): T {
  return {
    ...product,
    image_url: product.cover_url,
    purchase_limit: product.max_purchase,
    status: product.enabled ? 'active' : 'inactive',
  }
}

function normalizeOrder(order: StoreOrder): StoreOrder {
  return {
    ...order,
    delivered_cards: order.delivered_cards || [],
    delivered_files: order.delivered_files || [],
  }
}

function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

export const storeAPI = {
  getCategories() {
    return apiClient.get<StoreCategory[]>('/shop/categories').then((response) => {
      response.data = (response.data || []).map(normalizeCategory)
      return response
    })
  },

  getProducts(params?: Pick<StoreListParams, 'category_id' | 'keyword'>) {
    return apiClient.get<BasePaginationResponse<StoreProduct>>('/shop/products', { params }).then((response) => {
      response.data.items = (response.data.items || []).map(normalizeProduct)
      return response
    })
  },

  createOrder(data: CreateStoreOrderRequest, idempotencyKey?: string) {
    return apiClient.post<StoreOrder>('/shop/orders', data, {
      headers: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : undefined,
    }).then((response) => {
      response.data = normalizeOrder(response.data)
      return response
    })
  },

  getOrder(orderId: number) {
    return apiClient.get<StoreOrder>(`/shop/orders/${orderId}`).then((response) => {
      response.data = normalizeOrder(response.data)
      return response
    })
  },

  async downloadOrderFile(orderId: number, cardId: number, filename: string) {
    const { data } = await apiClient.get<Blob>(`/shop/orders/${orderId}/files/${cardId}/download`, { responseType: 'blob' })
    downloadBlob(data, filename || `shop-file-${cardId}`)
  },

  async downloadOrderFilesZip(orderId: number, filename?: string) {
    const { data } = await apiClient.get<Blob>(`/shop/orders/${orderId}/files/download.zip`, { responseType: 'blob' })
    downloadBlob(data, filename || `shop-order-${orderId}-files.zip`)
  },
}

export default storeAPI
