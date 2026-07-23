<template>
  <div class="dashboard-section-card h-full">
    <div class="dashboard-section-header">
      <h2 class="dashboard-card-title">{{ t('dashboard.recentUsage') }}</h2>
      <span class="badge badge-gray">{{ t('dashboard.last7Days') }}</span>
    </div>
    <div class="p-4 sm:p-5">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner size="lg" />
      </div>
      <div v-else-if="data.length === 0" class="py-8">
        <EmptyState :title="t('dashboard.noUsageRecords')" :description="t('dashboard.startUsingApi')" />
      </div>
      <div v-else class="space-y-3">
        <div v-for="log in data" :key="log.id" class="dashboard-list-row">
          <div class="flex min-w-0 items-center gap-3">
            <div class="dashboard-action-icon">
              <Icon name="beaker" size="md" />
            </div>
            <div class="min-w-0">
              <p class="truncate text-sm font-medium text-content" :title="log.model">{{ log.model }}</p>
              <p class="text-xs text-content-subtle">{{ formatDateTime(log.created_at) }}</p>
            </div>
          </div>
          <div class="flex-shrink-0 text-right">
            <p class="text-sm font-semibold">
              <span class="text-positive" :title="t('dashboard.actual')">${{ formatCost(log.actual_cost) }}</span>
              <span class="font-normal text-content-subtle" :title="t('dashboard.standard')"> / ${{ formatCost(log.total_cost) }}</span>
            </p>
            <p class="text-xs text-content-subtle">{{ (log.input_tokens + log.output_tokens).toLocaleString() }} tokens</p>
          </div>
        </div>

        <router-link to="/usage" class="flex min-h-11 items-center justify-center gap-2 rounded-control px-3 py-2 text-sm font-semibold text-brand transition-colors hover:bg-brand-soft hover:text-brand-strong">
          {{ t('dashboard.viewAllUsage') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import type { UsageLog } from '@/types'

defineProps<{
  data: UsageLog[]
  loading: boolean
}>()
const { t } = useI18n()
const formatCost = (c: number) => c.toFixed(4)
</script>
