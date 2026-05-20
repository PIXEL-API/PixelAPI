<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <input v-model="keyword" class="input flex-1 sm:max-w-72" :placeholder="t('admin.store.searchProducts')" @input="handleSearch" />
          <div class="flex flex-1 justify-end gap-2">
            <button class="btn btn-secondary" :disabled="loading" @click="loadProducts">{{ t('common.refresh') }}</button>
            <button class="btn btn-primary" @click="openCreate">{{ t('admin.store.createProduct') }}</button>
          </div>
        </div>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="products" :loading="loading" row-key="id">
          <template #cell-price="{ value }">¥{{ Number(value || 0).toFixed(2) }}</template>
          <template #cell-category_id="{ value, row }">{{ row.category?.name || categoryName(value) }}</template>
          <template #cell-product_type="{ value }">{{ productTypeLabel(value) }}</template>
          <template #cell-stock="{ value, row }">{{ row.stock_unlimited ? t('admin.store.unlimitedStock') : value }}</template>
          <template #cell-status="{ value }">
            <span :class="['badge', value === 'active' ? 'badge-success' : 'badge-gray']">{{ t(`admin.store.status.${value}`) }}</span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex justify-end gap-2">
              <button class="btn btn-secondary btn-sm" @click="openEdit(row)">{{ t('common.edit') }}</button>
              <button class="btn btn-danger btn-sm" @click="deleteProduct(row)">{{ t('common.delete') }}</button>
            </div>
          </template>
        </DataTable>
      </template>
      <template #pagination>
        <Pagination v-if="pagination.total > 0" :page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @update:page="setPage" @update:pageSize="setPageSize" />
      </template>
    </TablePageLayout>

    <Teleport to="body">
      <div v-if="dialogOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="dialogOpen = false">
        <form class="w-full max-w-2xl rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900" @submit.prevent="submitForm">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ editingProduct ? t('admin.store.editProduct') : t('admin.store.createProduct') }}</h2>
          <div class="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div class="sm:col-span-2">
              <label class="input-label">{{ t('common.name') }}</label>
              <input v-model.trim="form.name" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.category') }}</label>
              <Select v-model="form.category_id" :options="categoryOptions" />
            </div>
            <div>
              <label class="input-label">{{ t('common.status') }}</label>
              <Select v-model="form.status" :options="statusOptions" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.productType') }}</label>
              <Select v-model="form.product_type" :options="productTypeOptions" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.price') }}</label>
              <input v-model.number="form.price" class="input" type="number" min="0.01" step="0.01" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.originalPrice') }}</label>
              <input v-model.number="form.original_price" class="input" type="number" min="0" step="0.01" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.sortOrder') }}</label>
              <input v-model.number="form.sort_order" class="input" type="number" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.store.purchaseLimit') }}</label>
              <input v-model.number="form.purchase_limit" class="input" type="number" min="0" :disabled="isDrawProduct" />
            </div>
            <div v-if="isDrawProduct">
              <label class="input-label">{{ t('admin.store.drawMinAmount') }}</label>
              <input v-model.number="form.draw_config.min_amount" class="input" type="number" min="0.01" step="0.01" required />
            </div>
            <div v-if="isDrawProduct">
              <label class="input-label">{{ t('admin.store.drawMaxAmount') }}</label>
              <input v-model.number="form.draw_config.max_amount" class="input" type="number" min="0" step="0.01" required />
            </div>
            <div v-if="isDrawProduct">
              <label class="input-label">{{ t('admin.store.drawGuaranteeCount') }}</label>
              <input v-model.number="form.draw_config.guarantee_count" class="input" type="number" min="1" step="1" required />
            </div>
            <div v-if="isDrawProduct">
              <label class="input-label">{{ t('admin.store.drawReturnRate') }}</label>
              <input v-model.number="form.draw_config.return_rate" class="input" type="number" min="0.0001" step="0.0001" required />
            </div>
            <div v-if="isDrawProduct" class="sm:col-span-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200">
              {{ t('admin.store.drawConfigHint', { amount: drawTargetAmount.toFixed(2) }) }}
            </div>
            <div class="sm:col-span-2">
              <label class="input-label">{{ t('admin.store.imageUrl') }}</label>
              <input v-model.trim="form.image_url" class="input" />
            </div>
            <div class="sm:col-span-2">
              <label class="input-label">{{ t('admin.store.description') }}</label>
              <textarea v-model.trim="form.description" class="input min-h-24"></textarea>
            </div>
          </div>
          <div class="mt-5 flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? t('common.saving') : t('common.save') }}</button>
          </div>
        </form>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminStoreAPI } from '@/api/admin/store'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { StoreCategory, StoreProduct, StoreProductStatus, StoreProductType } from '@/types/store'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'

const { t } = useI18n()
const appStore = useAppStore()
const products = ref<StoreProduct[]>([])
const categories = ref<StoreCategory[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const dialogOpen = ref(false)
const editingProduct = ref<StoreProduct | null>(null)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })
const form = reactive({
  category_id: null as number | null,
  name: '',
  description: '',
  price: 0,
  original_price: null as number | null,
  status: 'active' as StoreProductStatus,
  sort_order: 0,
  image_url: '',
  purchase_limit: null as number | null,
  product_type: 'card_key' as StoreProductType,
  balance_only: false,
  draw_config: {
    enabled: false,
    min_amount: 1,
    max_amount: 5,
    guarantee_count: 20,
    return_rate: 1,
  },
})
let searchTimer: ReturnType<typeof setTimeout> | undefined

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('common.name') },
  { key: 'category_id', label: t('admin.store.category') },
  { key: 'product_type', label: t('admin.store.productType') },
  { key: 'price', label: t('admin.store.price') },
  { key: 'stock', label: t('admin.store.stockLabel') },
  { key: 'status', label: t('common.status') },
  { key: 'actions', label: t('common.actions') },
])
const categoryOptions = computed(() => [
  { value: null, label: t('common.uncategorized') },
  ...categories.value.map(category => ({ value: category.id, label: category.name })),
])
const statusOptions = computed(() => [
  { value: 'active', label: t('admin.store.status.active') },
  { value: 'inactive', label: t('admin.store.status.inactive') },
])
const productTypeOptions = computed(() => [
  { value: 'card_key', label: t('admin.store.productTypes.cardKey') },
  { value: 'balance_draw', label: t('admin.store.productTypes.balanceDraw') },
])
const isDrawProduct = computed(() => form.product_type === 'balance_draw')
const drawTargetAmount = computed(() => Math.round(form.price * form.draw_config.guarantee_count * form.draw_config.return_rate * 100) / 100)

function categoryName(id?: number | null) {
  if (!id) return t('common.uncategorized')
  return categories.value.find(category => category.id === id)?.name || `#${id}`
}
function productTypeLabel(value?: string) {
  return value === 'balance_draw' ? t('admin.store.productTypes.balanceDraw') : t('admin.store.productTypes.cardKey')
}
function nullableNumber(value: number | null) {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}
function resetForm() {
  form.category_id = null
  form.name = ''
  form.description = ''
  form.price = 0
  form.original_price = null
  form.status = 'active'
  form.sort_order = 0
  form.image_url = ''
  form.purchase_limit = null
  form.product_type = 'card_key'
  form.balance_only = false
  form.draw_config.enabled = false
  form.draw_config.min_amount = 1
  form.draw_config.max_amount = 5
  form.draw_config.guarantee_count = 20
  form.draw_config.return_rate = 1
}
function openCreate() { editingProduct.value = null; resetForm(); dialogOpen.value = true }
function openEdit(product: StoreProduct) {
  editingProduct.value = product
  form.category_id = product.category_id ?? null
  form.name = product.name
  form.description = product.description || ''
  form.price = product.price
  form.original_price = product.original_price ?? null
  form.status = product.status ?? (product.enabled ? 'active' : 'inactive')
  form.sort_order = product.sort_order
  form.image_url = product.cover_url || product.image_url || ''
  form.purchase_limit = product.purchase_limit ?? null
  form.product_type = product.product_type || 'card_key'
  form.balance_only = product.balance_only === true
  form.draw_config.enabled = product.draw_config?.enabled ?? form.product_type === 'balance_draw'
  form.draw_config.min_amount = product.draw_config?.min_amount ?? 1
  form.draw_config.max_amount = product.draw_config?.max_amount ?? 5
  form.draw_config.guarantee_count = product.draw_config?.guarantee_count ?? 20
  form.draw_config.return_rate = product.draw_config?.return_rate ?? 1
  dialogOpen.value = true
}

async function loadCategories() {
  const { data } = await adminStoreAPI.listCategories({ page: 1, page_size: 1000, status: 'active' })
  categories.value = data.filter(category => category.enabled)
}
async function loadProducts() {
  loading.value = true
  try {
    const { data } = await adminStoreAPI.listProducts({ page: pagination.page, page_size: pagination.page_size, keyword: keyword.value || undefined })
    products.value = data.items
    pagination.total = data.total
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.store.loadFailed')))
  } finally {
    loading.value = false
  }
}
async function submitForm() {
  saving.value = true
  try {
    const payload = {
      ...form,
      description: form.description || null,
      image_url: form.image_url || null,
      category_id: form.category_id || null,
      clear_category: !!editingProduct.value && !!editingProduct.value.category_id && !form.category_id,
      original_price: nullableNumber(form.original_price),
      clear_original_price: !!editingProduct.value && typeof editingProduct.value.original_price === 'number' && nullableNumber(form.original_price) === null,
      purchase_limit: isDrawProduct.value ? 1 : nullableNumber(form.purchase_limit),
      min_purchase: 1,
      max_purchase: isDrawProduct.value ? 1 : nullableNumber(form.purchase_limit) || 1,
      auto_delivery: true,
      product_type: form.product_type,
      balance_only: isDrawProduct.value,
      draw_config: isDrawProduct.value
        ? { ...form.draw_config, enabled: true }
        : { enabled: false, min_amount: 0, max_amount: 0, guarantee_count: 0, return_rate: 1 },
    }
    if (editingProduct.value) await adminStoreAPI.updateProduct(editingProduct.value.id, payload)
    else await adminStoreAPI.createProduct(payload)
    appStore.showSuccess(t('common.saved'))
    dialogOpen.value = false
    await loadProducts()
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    saving.value = false
  }
}
async function deleteProduct(product: StoreProduct) {
  if (!window.confirm(t('admin.store.deleteProductConfirm'))) return
  try {
    await adminStoreAPI.deleteProduct(product.id)
    appStore.showSuccess(t('common.deleted'))
    await loadProducts()
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}
function handleSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { pagination.page = 1; loadProducts() }, 300)
}
function setPage(page: number) { pagination.page = page; loadProducts() }
function setPageSize(pageSize: number) { pagination.page_size = pageSize; pagination.page = 1; loadProducts() }
onMounted(async () => { await loadCategories(); await loadProducts() })
</script>
