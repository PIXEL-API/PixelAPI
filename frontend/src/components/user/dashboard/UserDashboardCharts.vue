<template>
  <div class="space-y-4">
    <!-- Date Range Filter -->
    <div class="dashboard-toolbar">
      <div class="dashboard-toolbar-row">
        <div class="dashboard-filter-group">
          <span class="dashboard-filter-label">{{ t('dashboard.timeRange') }}:</span>
          <DateRangePicker class="w-full min-w-0 max-w-full sm:w-auto" :start-date="startDate" :end-date="endDate" @update:startDate="$emit('update:startDate', $event)" @update:endDate="$emit('update:endDate', $event)" @change="$emit('dateRangeChange', $event)" />
        </div>
        <button @click="$emit('refresh')" :disabled="loading" class="btn btn-secondary w-full sm:w-auto">
          {{ t('common.refresh') }}
        </button>
        <div class="dashboard-filter-group sm:ml-auto">
          <span class="dashboard-filter-label">{{ t('dashboard.granularity') }}:</span>
          <div class="w-full sm:w-28">
            <Select :model-value="granularity" :options="[{value:'day', label:t('dashboard.day')}, {value:'hour', label:t('dashboard.hour')}]" @update:model-value="$emit('update:granularity', $event)" @change="$emit('granularityChange')" />
          </div>
        </div>
      </div>
    </div>

    <!-- Charts Grid -->
    <div class="dashboard-chart-grid">
      <!-- Model Distribution Chart -->
      <div class="dashboard-section-card relative p-4 sm:p-5">
        <div v-if="loading" class="dashboard-loading-overlay">
          <LoadingSpinner size="md" />
        </div>
        <h3 class="dashboard-card-title mb-4">{{ t('dashboard.modelDistribution') }}</h3>
        <div class="dashboard-chart-layout">
          <div class="h-44 w-full flex-shrink-0 md:h-48 md:w-48">
            <Doughnut v-if="modelData" :data="modelData" :options="doughnutOptions" />
            <div v-else class="flex h-full items-center justify-center text-sm text-content-subtle">{{ t('dashboard.noDataAvailable') }}</div>
          </div>
          <div class="dashboard-table-scroll max-h-48 flex-1 overflow-y-auto">
            <table class="w-full min-w-[30rem] text-xs">
              <thead>
                <tr class="text-content-subtle">
                  <th class="pb-2 text-left">{{ t('dashboard.model') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.requests') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.tokens') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.actual') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.standard') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in models" :key="model.model" class="border-t border-line">
                  <td class="max-w-[100px] truncate py-1.5 font-medium text-content" :title="model.model">{{ model.model }}</td>
                  <td class="py-1.5 text-right text-content-muted">{{ formatNumber(model.requests) }}</td>
                  <td class="py-1.5 text-right text-content-muted">{{ formatTokens(model.total_tokens) }}</td>
                  <td class="py-1.5 text-right text-positive">${{ formatCost(model.actual_cost) }}</td>
                  <td class="py-1.5 text-right text-content-subtle">${{ formatCost(model.cost) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Token Usage Trend Chart -->
      <TokenUsageTrend :trend-data="trend" :loading="loading" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import { Doughnut } from 'vue-chartjs'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler } from 'chart.js'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{ loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange', 'refresh'])
const { t } = useI18n()

const modelData = computed(() => !props.models?.length ? null : {
  labels: props.models.map((m: ModelStat) => m.model),
  datasets: [{
    data: props.models.map((m: ModelStat) => m.total_tokens),
    backgroundColor: ['#2563eb', '#0f766e', '#7c3aed', '#d97706', '#dc2626', '#0891b2', '#4f46e5', '#65a30d']
  }]
})

const doughnutOptions = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.label}: ${formatTokens(context.parsed)} tokens`
      }
    }
  }
}
</script>
