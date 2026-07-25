<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="w-full sm:w-64">
            <input
              v-model="searchQuery"
              type="search"
              :placeholder="t('admin.redeem.searchCodes')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.type"
            :options="filterTypeOptions"
            class="w-full sm:w-36"
            @change="applyFilters"
          />
          <Select
            v-model="filters.status"
            :options="filterStatusOptions"
            class="w-full sm:w-36"
            @change="applyFilters"
          />
          <Select
            v-model="filters.category"
            :options="filterCategoryOptions"
            class="w-full sm:w-44"
            searchable
            :search-placeholder="t('admin.redeem.searchCategories')"
            @change="applyFilters"
          />

          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <template v-if="selectedCount > 0">
              <span class="text-sm font-medium text-gray-600 dark:text-gray-300">
                {{ t('admin.redeem.selectedCodes', { count: selectedCount }) }}
              </span>
              <button
                type="button"
                class="btn btn-danger btn-sm"
                :disabled="deletingBatch"
                @click="showBatchDeleteDialog = true"
              >
                {{ t('admin.redeem.batchDelete') }}
              </button>
              <button type="button" class="btn btn-secondary btn-sm" @click="clearSelection">
                {{ t('common.clear') }}
              </button>
            </template>
            <button
              @click="refreshRedeemData"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button
              @click="handleExportCodes"
              :disabled="exporting"
              class="btn btn-secondary"
            >
              {{ exporting ? t('common.processing') : t('admin.redeem.exportCsv') }}
            </button>
            <button @click="showGenerateDialog = true" class="btn btn-primary">
              {{ t('admin.redeem.generateCodes') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="codes"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="id"
          default-sort-order="desc"
          row-key="id"
          @sort="handleSort"
        >
          <template #header-select>
            <input
              type="checkbox"
              class="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allVisibleSelected"
              :indeterminate="someVisibleSelected"
              :disabled="selectableCodes.length === 0"
              :aria-label="t('admin.redeem.selectPage')"
              @click.stop
              @change="toggleSelectAllVisible"
            />
          </template>

          <template #cell-select="{ row }">
            <input
              v-if="row.status === 'unused'"
              type="checkbox"
              class="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="isSelected(row.id)"
              :aria-label="t('admin.redeem.selectCode', { code: row.code })"
              @click.stop
              @change="toggle(row.id)"
            />
            <span v-else class="text-gray-300 dark:text-dark-600">-</span>
          </template>

          <template #cell-code="{ value }">
            <div class="flex items-center space-x-2">
              <code class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ value }}</code>
              <button
                @click="copyToClipboard(value)"
                :class="[
                  'flex items-center transition-colors',
                  copiedCode === value
                    ? 'text-green-500'
                    : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'
                ]"
                :title="copiedCode === value ? t('admin.redeem.copied') : t('keys.copyToClipboard')"
              >
                <Icon v-if="copiedCode !== value" name="copy" size="sm" :stroke-width="2" />
                <svg v-else class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M5 13l4 4L19 7"
                  />
                </svg>
              </button>
            </div>
          </template>

          <template #cell-category="{ value }">
            <span v-if="value" class="badge badge-primary">{{ value }}</span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">
              {{ t('common.uncategorized') }}
            </span>
          </template>

          <template #cell-type="{ value }">
            <span
              :class="[
                'badge',
                value === 'balance'
                  ? 'badge-success'
                  : value === 'subscription'
                    ? 'badge-warning'
                    : 'badge-primary'
              ]"
            >
              {{ t('admin.redeem.types.' + value) }}
            </span>
          </template>

          <template #cell-value="{ value, row }">
            <span class="text-sm font-medium text-gray-900 dark:text-white">
              <template v-if="row.type === 'balance'">${{ value.toFixed(2) }}</template>
              <template v-else-if="row.type === 'points'">{{ value.toFixed(10).replace(/\.?0+$/, '') || '0' }}</template>
              <template v-else-if="row.type === 'subscription'">
                {{ row.validity_days || 30 }} {{ t('admin.redeem.days') }}
                <span v-if="row.group" class="ml-1 text-xs text-gray-500 dark:text-gray-400"
                  >({{ row.group.name }})</span
                >
              </template>
              <template v-else>{{ value }}</template>
            </span>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'unused'
                  ? 'badge-success'
                  : value === 'used'
                    ? 'badge-gray'
                    : 'badge-danger'
              ]"
            >
              {{ t('admin.redeem.status.' + value) }}
            </span>
          </template>

          <template #cell-used_by="{ value, row }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ row.user?.email || (value ? t('admin.redeem.userPrefix', { id: value }) : '-') }}
            </span>
          </template>

          <template #cell-used_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{
              value ? formatDateTime(value) : '-'
            }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ formatDateTime(value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-2">
              <button
                v-if="row.status === 'unused'"
                @click="handleDelete(row)"
                :disabled="deleting"
                class="flex min-h-11 min-w-11 flex-col items-center justify-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500/50 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :aria-label="t('admin.redeem.deleteCodeLabel', { code: row.code })"
              >
                <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                  />
                </svg>
                <span class="text-xs">{{ t('common.delete') }}</span>
              </button>
              <span v-else class="text-gray-400 dark:text-dark-500">-</span>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.redeem.deleteCode')"
      :message="t('admin.redeem.deleteCodeConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    <!-- Batch Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showBatchDeleteDialog"
      :title="t('admin.redeem.batchDelete')"
      :message="t('admin.redeem.batchDeleteConfirm', { count: selectedCount })"
      :confirm-text="t('admin.redeem.batchDelete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmBatchDelete"
      @cancel="showBatchDeleteDialog = false"
    />

    <!-- Generate Codes Dialog -->
    <BaseDialog
      :show="showGenerateDialog"
      :title="t('admin.redeem.generateCodesTitle')"
      width="narrow"
      @close="showGenerateDialog = false"
    >
          <form
            id="generate-redeem-codes-form"
            class="space-y-4"
            @submit.prevent="handleGenerateCodes"
          >
            <div>
              <label class="input-label">{{ t('admin.redeem.codeType') }}</label>
              <Select v-model="generateForm.type" :options="typeOptions" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.redeem.category') }}</label>
              <Select
                v-model="generateForm.category"
                :options="generationCategoryOptions"
                :placeholder="t('admin.redeem.categoryPlaceholder')"
                searchable
                creatable
                :search-placeholder="t('admin.redeem.searchCategories')"
                :creatable-prefix="t('admin.redeem.createCategory')"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.redeem.categoryHint') }}
              </p>
            </div>
            <!-- 余额/并发类型：显示数值输入 -->
            <div v-if="generateForm.type !== 'subscription' && generateForm.type !== 'invitation'">
              <label class="input-label">
                {{
                  generateForm.type === 'balance'
                    ? t('admin.redeem.amount')
                    : generateForm.type === 'points'
                      ? t('admin.redeem.pointsValue')
                    : t('admin.redeem.columns.value')
                }}
              </label>
              <input
                v-model.number="generateForm.value"
                type="number"
                :step="generateForm.type === 'balance' ? '0.01' : generateForm.type === 'points' ? '0.0000000001' : '1'"
                :min="generateForm.type === 'balance' || generateForm.type === 'points' ? '0.01' : '1'"
                required
                class="input"
              />
            </div>
            <!-- 邀请码类型：显示提示信息 -->
            <div v-if="generateForm.type === 'invitation'" class="rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
              <p class="text-sm text-blue-700 dark:text-blue-300">
                {{ t('admin.redeem.invitationHint') }}
              </p>
            </div>
            <!-- 订阅类型：显示分组选择和有效天数 -->
            <template v-if="generateForm.type === 'subscription'">
              <div>
                <label class="input-label">{{ t('admin.redeem.selectGroup') }}</label>
                <Select
                  v-model="generateForm.group_id"
                  :options="subscriptionGroupOptions"
                  :placeholder="t('admin.redeem.selectGroupPlaceholder')"
                >
                  <template #selected="{ option }">
                    <GroupBadge
                      v-if="option"
                      :name="(option as unknown as GroupOption).label"
                      :platform="(option as unknown as GroupOption).platform"
                      :subscription-type="(option as unknown as GroupOption).subscriptionType"
                      :rate-multiplier="(option as unknown as GroupOption).rate"
                    />
                    <span v-else class="text-gray-400">{{
                      t('admin.redeem.selectGroupPlaceholder')
                    }}</span>
                  </template>
                  <template #option="{ option, selected }">
                    <GroupOptionItem
                      :name="(option as unknown as GroupOption).label"
                      :platform="(option as unknown as GroupOption).platform"
                      :subscription-type="(option as unknown as GroupOption).subscriptionType"
                      :rate-multiplier="(option as unknown as GroupOption).rate"
                      :description="(option as unknown as GroupOption).description"
                      :selected="selected"
                    />
                  </template>
                </Select>
              </div>
              <div>
                <label class="input-label">{{ t('admin.redeem.validityDays') }}</label>
                <input
                  v-model.number="generateForm.validity_days"
                  type="number"
                  min="1"
                  max="365"
                  required
                  class="input"
                />
              </div>
            </template>
            <div>
              <label class="input-label">{{ t('admin.redeem.count') }}</label>
              <input
                v-model.number="generateForm.count"
                type="number"
                min="1"
                :max="MAX_GENERATE_COUNT"
                step="1"
                required
                class="input"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.redeem.countHint', { max: MAX_GENERATE_COUNT }) }}
              </p>
            </div>
          </form>
      <template #footer>
        <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button
            type="button"
            @click="showGenerateDialog = false"
            class="btn btn-secondary min-h-11"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="generate-redeem-codes-form"
            :disabled="generating"
            class="btn btn-primary min-h-11"
          >
            {{ generating ? t('admin.redeem.generating') : t('admin.redeem.generate') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Generated Codes Result Dialog -->
    <BaseDialog
      :show="showResultDialog"
      :title="t('admin.redeem.generatedSuccessfully')"
      width="normal"
      @close="closeResultDialog"
    >
      <div class="mb-4 flex items-center gap-3">
        <div
          class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30"
        >
          <svg
            class="h-5 w-5 text-green-600 dark:text-green-400"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M5 13l4 4L19 7"
            />
          </svg>
        </div>
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.redeem.codesCreated', { count: generatedCodes.length }) }}
        </p>
      </div>
      <textarea
        readonly
        :value="generatedCodesText"
        :style="{ height: textareaHeight }"
        class="w-full resize-none rounded-lg border border-gray-200 bg-gray-50 p-3 font-mono text-sm text-gray-800 focus:outline-none dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200"
      ></textarea>
      <template #footer>
        <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button
            @click="copyGeneratedCodes"
            :class="[
              'btn min-h-11 flex items-center justify-center gap-2 transition-all',
              copiedAll ? 'btn-success' : 'btn-secondary'
            ]"
          >
            <Icon v-if="!copiedAll" name="copy" size="sm" :stroke-width="2" />
            <svg v-else class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M5 13l4 4L19 7"
              />
            </svg>
            {{ copiedAll ? t('admin.redeem.copied') : t('admin.redeem.copyAll') }}
          </button>
          <button
            @click="downloadGeneratedCodes"
            class="btn btn-primary min-h-11 flex items-center justify-center gap-2"
          >
            <Icon name="download" size="sm" :stroke-width="2" />
            {{ t('admin.redeem.download') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useTableSelection } from '@/composables/useTableSelection'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { RedeemCode, RedeemCodeType, Group, GroupPlatform, SubscriptionType } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard: clipboardCopy } = useClipboard()
const MAX_GENERATE_COUNT = 500
const MAX_CATEGORY_LENGTH = 64

interface GroupOption {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}

const showGenerateDialog = ref(false)
const showResultDialog = ref(false)
const generatedCodes = ref<RedeemCode[]>([])
const subscriptionGroups = ref<Group[]>([])
const categories = ref<string[]>([])

// 订阅类型分组选项
const subscriptionGroupOptions = computed(() => {
  return subscriptionGroups.value
    .filter((g) => g.subscription_type === 'subscription')
    .map((g) => ({
      value: g.id,
      label: g.name,
      description: g.description,
      platform: g.platform,
      subscriptionType: g.subscription_type,
      rate: g.rate_multiplier
    }))
})

const generatedCodesText = computed(() => {
  return generatedCodes.value.map((code) => code.code).join('\n')
})

const textareaHeight = computed(() => {
  const lineCount = generatedCodes.value.length
  const lineHeight = 24 // approximate line height in px
  const padding = 24 // top + bottom padding
  const minHeight = 60
  const maxHeight = 240
  const calculatedHeight = Math.min(
    Math.max(lineCount * lineHeight + padding, minHeight),
    maxHeight
  )
  return `${calculatedHeight}px`
})

const copiedAll = ref(false)

const closeResultDialog = () => {
  showResultDialog.value = false
  generatedCodes.value = []
  copiedAll.value = false
}

const copyGeneratedCodes = async () => {
  const success = await clipboardCopy(generatedCodesText.value, t('admin.redeem.copied'))
  if (success) {
    copiedAll.value = true
    setTimeout(() => {
      copiedAll.value = false
    }, 2000)
  }
}

const downloadGeneratedCodes = () => {
  const blob = new Blob([generatedCodesText.value], { type: 'text/plain' })
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `redeem-codes-${new Date().toISOString().split('T')[0]}.txt`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', class: 'w-12' },
  { key: 'code', label: t('admin.redeem.columns.code') },
  { key: 'category', label: t('admin.redeem.columns.category'), sortable: true },
  { key: 'type', label: t('admin.redeem.columns.type'), sortable: true },
  { key: 'value', label: t('admin.redeem.columns.value'), sortable: true },
  { key: 'status', label: t('admin.redeem.columns.status'), sortable: true },
  { key: 'used_by', label: t('admin.redeem.columns.usedBy') },
  { key: 'used_at', label: t('admin.redeem.columns.usedAt'), sortable: true },
  { key: 'created_at', label: t('admin.redeem.columns.createdAt'), sortable: true },
  { key: 'actions', label: t('admin.redeem.columns.actions') }
])

const typeOptions = computed(() => [
  { value: 'balance', label: t('admin.redeem.balance') },
  { value: 'points', label: t('admin.redeem.points') },
  { value: 'concurrency', label: t('admin.redeem.concurrency') },
  { value: 'subscription', label: t('admin.redeem.subscription') },
  { value: 'invitation', label: t('admin.redeem.invitation') }
])

const filterTypeOptions = computed(() => [
  { value: '', label: t('admin.redeem.allTypes') },
  { value: 'balance', label: t('admin.redeem.balance') },
  { value: 'points', label: t('admin.redeem.points') },
  { value: 'concurrency', label: t('admin.redeem.concurrency') },
  { value: 'subscription', label: t('admin.redeem.subscription') },
  { value: 'invitation', label: t('admin.redeem.invitation') }
])

const filterStatusOptions = computed(() => [
  { value: '', label: t('admin.redeem.allStatus') },
  { value: 'unused', label: t('admin.redeem.unused') },
  { value: 'used', label: t('admin.redeem.used') },
  { value: 'expired', label: t('admin.redeem.status.expired') }
])

const generationCategoryOptions = computed(() =>
  categories.value.map((category) => ({ value: category, label: category }))
)

const filterCategoryOptions = computed(() => [
  { value: '', label: t('admin.redeem.allCategories') },
  { value: true, label: t('common.uncategorized') },
  ...generationCategoryOptions.value
])

const codes = ref<RedeemCode[]>([])
const loading = ref(false)
const generating = ref(false)
const exporting = ref(false)
const deleting = ref(false)
const deletingBatch = ref(false)
const searchQuery = ref('')
const filters = reactive({
  type: '',
  status: '',
  category: '' as string | boolean
})
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const sortState = reactive({
  sort_by: 'id',
  sort_order: 'desc' as 'asc' | 'desc'
})

let abortController: AbortController | null = null

const showDeleteDialog = ref(false)
const showBatchDeleteDialog = ref(false)
const deletingCode = ref<RedeemCode | null>(null)
const copiedCode = ref<string | null>(null)

const generateForm = reactive({
  type: 'balance' as RedeemCodeType,
  category: '',
  value: 10,
  count: 1,
  group_id: null as number | null,
  validity_days: 30
})

const selectableCodes = computed(() => codes.value.filter((code) => code.status === 'unused'))
const {
  selectedIds,
  selectedCount,
  allVisibleSelected,
  isSelected,
  toggle,
  clear: clearSelection,
  setSelectedIds,
  toggleVisible
} = useTableSelection<RedeemCode>({
  rows: selectableCodes,
  getId: (code) => code.id
})
const selectedVisibleCount = computed(
  () => selectableCodes.value.filter((code) => isSelected(code.id)).length
)
const someVisibleSelected = computed(
  () => selectedVisibleCount.value > 0 && !allVisibleSelected.value
)

// 监听类型变化，邀请码类型时自动设置 value 为 0
watch(
  () => generateForm.type,
  (newType) => {
    if (newType === 'invitation') {
      generateForm.value = 0
    } else if (generateForm.value === 0) {
      generateForm.value = 10
    }
  }
)

const buildRedeemQueryFilters = () => ({
  type: (filters.type || undefined) as RedeemCodeType | undefined,
  status: (filters.status || undefined) as 'used' | 'expired' | 'unused' | undefined,
  category:
    typeof filters.category === 'string' && filters.category ? filters.category : undefined,
  uncategorized: filters.category === true ? true : undefined,
  search: searchQuery.value || undefined,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order
})

const loadCodes = async (): Promise<void> => {
  if (abortController) {
    abortController.abort()
  }
  const currentController = new AbortController()
  abortController = currentController
  loading.value = true
  try {
    const response = await adminAPI.redeem.list(
      pagination.page,
      pagination.page_size,
      buildRedeemQueryFilters(),
      {
        signal: currentController.signal
      }
    )
    if (currentController.signal.aborted) {
      return
    }
    const lastPage = Math.max(1, response.pages)
    if (pagination.page > lastPage) {
      pagination.page = lastPage
      clearSelection()
      await loadCodes()
      return
    }
    codes.value = response.items
    pagination.total = response.total
    pagination.pages = response.pages
    const selectableIds = new Set(
      response.items.filter((code) => code.status === 'unused').map((code) => code.id)
    )
    setSelectedIds(selectedIds.value.filter((id) => selectableIds.has(id)))
  } catch (error: unknown) {
    if (
      currentController.signal.aborted ||
      (error instanceof DOMException && error.name === 'AbortError') ||
      (typeof error === 'object' &&
        error !== null &&
        'code' in error &&
        error.code === 'ERR_CANCELED')
    ) {
      return
    }
    appStore.showError(extractApiErrorMessage(error, t('admin.redeem.failedToLoad')))
    console.error('Error loading redeem codes:', error)
  } finally {
    if (abortController === currentController && !currentController.signal.aborted) {
      loading.value = false
      abortController = null
    }
  }
}

let searchTimeout: ReturnType<typeof setTimeout>
const handleSearch = () => {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    clearSelection()
    void loadCodes()
  }, 300)
}

const applyFilters = () => {
  pagination.page = 1
  clearSelection()
  loadCodes()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  clearSelection()
  loadCodes()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  clearSelection()
  loadCodes()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  clearSelection()
  loadCodes()
}

const toggleSelectAllVisible = (event: Event) => {
  toggleVisible((event.target as HTMLInputElement).checked)
}

const handleGenerateCodes = async () => {
  // 订阅类型必须选择分组
  if (generateForm.type === 'subscription' && !generateForm.group_id) {
    appStore.showError(t('admin.redeem.groupRequired'))
    return
  }
  if (
    !Number.isInteger(generateForm.count) ||
    generateForm.count < 1 ||
    generateForm.count > MAX_GENERATE_COUNT
  ) {
    appStore.showError(t('admin.redeem.invalidCount', { max: MAX_GENERATE_COUNT }))
    return
  }
  const category = generateForm.category.trim()
  if ([...category].length > MAX_CATEGORY_LENGTH) {
    appStore.showError(t('admin.redeem.categoryTooLong', { max: MAX_CATEGORY_LENGTH }))
    return
  }

  generating.value = true
  try {
    const result = await adminAPI.redeem.generate({
      count: generateForm.count,
      type: generateForm.type,
      category: category || undefined,
      value: generateForm.value,
      group_id: generateForm.type === 'subscription' ? generateForm.group_id : undefined,
      validity_days:
        generateForm.type === 'subscription' ? generateForm.validity_days : undefined
    })
    showGenerateDialog.value = false
    generatedCodes.value = result
    showResultDialog.value = true
    if (category && !categories.value.includes(category)) {
      categories.value = [...categories.value, category].sort((a, b) => a.localeCompare(b))
    }
    // 重置表单
    generateForm.group_id = null
    generateForm.validity_days = 30
    pagination.page = 1
    clearSelection()
    loadCodes()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.redeem.failedToGenerate')))
    console.error('Error generating codes:', error)
  } finally {
    generating.value = false
  }
}

const copyToClipboard = async (text: string) => {
  const success = await clipboardCopy(text, t('admin.redeem.copied'))
  if (success) {
    copiedCode.value = text
    setTimeout(() => {
      copiedCode.value = null
    }, 2000)
  }
}

const handleExportCodes = async () => {
  if (exporting.value) return
  exporting.value = true
  try {
    const blob = await adminAPI.redeem.exportCodes(buildRedeemQueryFilters())

    // Create download link
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `redeem-codes-${new Date().toISOString().split('T')[0]}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    appStore.showSuccess(t('admin.redeem.codesExported'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.redeem.failedToExport')))
    console.error('Error exporting codes:', error)
  } finally {
    exporting.value = false
  }
}

const handleDelete = (code: RedeemCode) => {
  deletingCode.value = code
  showDeleteDialog.value = true
}

const confirmDelete = async () => {
  if (!deletingCode.value || deleting.value) return

  deleting.value = true
  try {
    await adminAPI.redeem.delete(deletingCode.value.id)
    const deletedID = deletingCode.value.id
    appStore.showSuccess(t('admin.redeem.codeDeleted'))
    showDeleteDialog.value = false
    deletingCode.value = null
    setSelectedIds(selectedIds.value.filter((id) => id !== deletedID))
    void Promise.all([loadCodes(), loadCategories()])
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.redeem.failedToDelete')))
    console.error('Error deleting code:', error)
  } finally {
    deleting.value = false
  }
}

const confirmBatchDelete = async () => {
  if (selectedIds.value.length === 0 || deletingBatch.value) return

  const requestedCount = selectedIds.value.length
  deletingBatch.value = true
  try {
    const result = await adminAPI.redeem.batchDelete(selectedIds.value)
    if (result.deleted < requestedCount) {
      appStore.showWarning(
        t('admin.redeem.batchDeletePartial', {
          deleted: result.deleted,
          skipped: requestedCount - result.deleted
        })
      )
    } else {
      appStore.showSuccess(t('admin.redeem.batchDeleted', { count: result.deleted }))
    }
    showBatchDeleteDialog.value = false
    clearSelection()
    void Promise.all([loadCodes(), loadCategories()])
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.redeem.failedToBatchDelete')))
    console.error('Error batch deleting redeem codes:', error)
  } finally {
    deletingBatch.value = false
  }
}

const loadCategories = async () => {
  try {
    categories.value = await adminAPI.redeem.listCategories()
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t('admin.redeem.failedToLoadCategories'))
    )
    console.error('Error loading redeem code categories:', error)
  }
}

const refreshRedeemData = async () => {
  clearSelection()
  await Promise.all([loadCodes(), loadCategories()])
}

// 加载订阅类型分组
const loadSubscriptionGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    subscriptionGroups.value = groups
  } catch (error) {
    console.error('Error loading subscription groups:', error)
  }
}

onMounted(() => {
  void refreshRedeemData()
  void loadSubscriptionGroups()
})

onUnmounted(() => {
  clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>
