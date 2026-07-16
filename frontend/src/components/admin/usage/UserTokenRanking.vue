<template>
  <div>
    <div class="flex flex-col gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700/50 sm:flex-row sm:items-center sm:justify-between sm:px-6">
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.usage.tokenRanking.subtitle') }}
      </p>
      <div class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
        <label class="sr-only" for="usage-token-ranking-search">
          {{ t('admin.usage.tokenRanking.searchPlaceholder') }}
        </label>
        <input
          id="usage-token-ranking-search"
          v-model="search"
          type="search"
          class="input min-h-11 w-full sm:w-56"
          :placeholder="t('admin.usage.tokenRanking.searchPlaceholder')"
        />
        <div class="ranking-limit-select w-full sm:w-28">
          <Select v-model="limit" :options="limitOptions" @change="load" />
        </div>
        <button
          type="button"
          class="btn btn-secondary min-h-11 min-w-11 px-3"
          :disabled="loading"
          @click="load"
        >
          {{ t('common.refresh') }}
        </button>
      </div>
    </div>

    <div
      v-if="errorMessage"
      class="m-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300 sm:m-6"
      role="alert"
    >
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <span>{{ errorMessage }}</span>
        <button type="button" class="btn btn-secondary min-h-11 min-w-11 shrink-0" @click="load">
          {{ t('admin.usage.tokenRanking.retry') }}
        </button>
      </div>
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[56rem] divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th scope="col" class="w-16 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400 sm:px-6">#</th>
            <th scope="col" class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">
              {{ t('admin.usage.tokenRanking.columns.user') }}
            </th>
            <th
              v-for="column in sortableColumns"
              :key="column.key"
              scope="col"
              class="whitespace-nowrap px-2 py-1 text-right text-xs font-medium uppercase tracking-wider"
              :class="sortBy === column.key ? 'text-primary-600 dark:text-primary-400' : 'text-gray-500 dark:text-dark-400'"
              :aria-sort="sortBy === column.key ? 'descending' : 'none'"
            >
              <button
                type="button"
                class="inline-flex min-h-11 min-w-11 w-full cursor-pointer select-none items-center justify-end gap-1 rounded px-2 text-inherit transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700"
                @click="setSort(column.key)"
              >
                {{ t(column.label) }}
                <span v-if="sortBy === column.key" aria-hidden="true">↓</span>
              </button>
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-if="loading">
            <td :colspan="sortableColumns.length + 2" class="py-12 text-center">
              <LoadingSpinner />
            </td>
          </tr>
          <tr v-else-if="displayItems.length === 0">
            <td :colspan="sortableColumns.length + 2" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.dashboard.noDataAvailable') }}
            </td>
          </tr>
          <tr
            v-for="item in displayItems"
            v-else
            :key="item.user_id"
            class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/40"
          >
            <td class="px-4 py-3 text-sm tabular-nums text-gray-400 sm:px-6">{{ item.rank }}</td>
            <td class="max-w-[16rem] px-4 py-1 text-sm font-medium">
              <button
                type="button"
                class="inline-flex min-h-11 min-w-11 max-w-full cursor-pointer items-center rounded text-left text-gray-700 transition-colors hover:text-primary-600 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-200 dark:hover:text-primary-400"
                :title="t('admin.usage.tokenRanking.rowHint')"
                @click="selectUser(item)"
              >
                <span class="truncate">{{ item.email || t('admin.usage.tokenRanking.columns.user') }}</span>
                <span class="ml-1 shrink-0 font-normal text-gray-400">#{{ item.user_id }}</span>
                <span class="sr-only">— {{ t('admin.usage.tokenRanking.rowHint') }}</span>
              </button>
            </td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums text-gray-500 dark:text-gray-400">{{ item.requests.toLocaleString() }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums text-gray-500 dark:text-gray-400">{{ formatTokens(item.input_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums text-gray-500 dark:text-gray-400">{{ formatTokens(item.output_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm tabular-nums text-gray-500 dark:text-gray-400">{{ formatTokens(item.cache_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ formatTokens(item.total_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-sm font-medium tabular-nums text-emerald-600 dark:text-emerald-400">${{ formatCost(item.actual_cost) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="!loading && !errorMessage && items.length > 0" class="border-t border-gray-100 px-4 py-3 text-right text-xs text-gray-500 dark:border-dark-700/50 dark:text-gray-400 sm:px-6">
      {{ t('admin.usage.tokenRanking.userCount', { count: displayItems.length }) }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUserBreakdown, type UserBreakdownParams } from '@/api/admin/dashboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import type { UserBreakdownItem } from '@/types'
import Select from '@/components/common/Select.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = defineProps<{
  startDate: string
  endDate: string
  filters: Record<string, unknown>
  model?: string
}>()

const emit = defineEmits<{
  selectUser: [userId: number, email: string]
}>()

const { t } = useI18n()

type SortKey = NonNullable<UserBreakdownParams['sort_by']>
const sortableColumns: Array<{ key: SortKey; label: string }> = [
  { key: 'requests', label: 'admin.usage.tokenRanking.columns.requests' },
  { key: 'input_tokens', label: 'admin.usage.tokenRanking.columns.inputTokens' },
  { key: 'output_tokens', label: 'admin.usage.tokenRanking.columns.outputTokens' },
  { key: 'cache_tokens', label: 'admin.usage.tokenRanking.columns.cacheTokens' },
  { key: 'total_tokens', label: 'admin.usage.tokenRanking.columns.totalTokens' },
  { key: 'actual_cost', label: 'admin.usage.tokenRanking.columns.cost' }
]

const limitOptions = [20, 50, 100, 200].map((value) => ({ value, label: `Top ${value}` }))
const items = ref<UserBreakdownItem[]>([])
const loading = ref(false)
const errorMessage = ref('')
const sortBy = ref<SortKey>('total_tokens')
const limit = ref(50)
const search = ref('')
let requestSequence = 0

type RankedUserBreakdownItem = UserBreakdownItem & { rank: number }

const rankedItems = computed<RankedUserBreakdownItem[]>(() =>
  items.value.map((item, index) => ({ ...item, rank: index + 1 }))
)

const displayItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return rankedItems.value
  return rankedItems.value.filter((item) =>
    (item.email || `${t('admin.usage.tokenRanking.columns.user')} #${item.user_id}`).toLowerCase().includes(keyword)
  )
})

const formatTokens = (value: number) => formatCompactNumber(value || 0)
const formatCost = (value: number) => formatCostFixed(value || 0, 4)

const setSort = (key: SortKey) => {
  if (sortBy.value === key) return
  sortBy.value = key
  void load()
}

const selectUser = (item: UserBreakdownItem) => {
  emit('selectUser', item.user_id, item.email || '')
}

const load = async () => {
  const sequence = ++requestSequence
  loading.value = true
  errorMessage.value = ''

  try {
    const params: UserBreakdownParams = {
      ...props.filters,
      start_date: props.startDate,
      end_date: props.endDate,
      sort_by: sortBy.value,
      limit: limit.value
    }
    if (props.model) params.model = props.model

    const response = await getUserBreakdown(params)
    if (sequence !== requestSequence) return
    items.value = response.users || []
  } catch (error) {
    if (sequence !== requestSequence) return
    items.value = []
    errorMessage.value = extractApiErrorMessage(error, t('admin.usage.tokenRanking.loadFailed'))
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

watch(
  () => [props.startDate, props.endDate, props.model, JSON.stringify(props.filters)],
  () => void load(),
  { immediate: true }
)

defineExpose({ reload: load })
</script>

<style scoped>
.ranking-limit-select :deep(.select-trigger) {
  min-height: 2.75rem;
}
</style>
