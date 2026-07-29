<template>
  <button
    type="button"
    class="group flex min-h-16 w-full cursor-pointer items-center gap-3 rounded-xl border border-gray-200 bg-white px-3 py-2.5 text-left shadow-sm transition-[border-color,background-color,box-shadow] duration-200 hover:border-gray-300 hover:bg-gray-50 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60 dark:border-dark-700 dark:bg-dark-900/45 dark:hover:border-dark-600 dark:hover:bg-dark-800/70"
    :aria-label="t('availableChannels.viewModelDetails', { model: model.name })"
    :aria-expanded="expanded"
    @click="$emit('select')"
  >
    <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 ring-1 ring-inset ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
      <ModelIcon :model="model.name" size="20px" />
    </span>

    <span class="min-w-0 flex-1">
      <span class="block truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">
        {{ model.name }}
      </span>
      <span class="mt-1 block truncate text-xs tabular-nums text-gray-500 dark:text-gray-400">
        {{ priceSummary }}
      </span>
      <span
        v-if="monitorLoading"
        class="mt-1.5 block h-4 w-28 animate-pulse rounded bg-gray-100 dark:bg-dark-700"
      />
      <span
        v-else-if="monitorSummary"
        class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] tabular-nums"
      >
        <span
          class="inline-flex items-center gap-1 font-medium"
          :class="monitorStatusTextClass"
        >
          <span class="h-1.5 w-1.5 rounded-full" :class="monitorStatusDotClass" />
          {{ t(`monitorCommon.status.${monitorSummary.status}`) }}
        </span>
        <span v-if="monitorSummary.availability != null" class="text-gray-500 dark:text-gray-400">
          {{ monitorSummary.availability.toFixed(2) }}%
        </span>
        <span v-if="monitorSummary.latencyMs != null" class="text-gray-500 dark:text-gray-400">
          {{ monitorSummary.latencyMs }}ms
        </span>
        <span v-if="monitorSummary.monitorCount > 1" class="text-gray-400 dark:text-gray-500">
          {{ t('availableChannels.monitor.sources', { count: monitorSummary.monitorCount }) }}
        </span>
      </span>
      <span v-else class="mt-1.5 flex items-center gap-1 text-[11px] text-gray-400 dark:text-gray-500">
        <span class="h-1.5 w-1.5 rounded-full bg-gray-300 dark:bg-dark-600" />
        {{ t('availableChannels.monitor.unmonitored') }}
      </span>
    </span>

    <Icon
      name="chevronRight"
      size="sm"
      :class="[
        'shrink-0 text-gray-300 transition-transform duration-200 group-hover:text-gray-500 dark:text-dark-600 dark:group-hover:text-dark-300',
        expanded ? 'rotate-90' : '',
      ]"
    />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import type { UserSupportedModel } from '@/api/channels'
import type { MonitorStatus } from '@/api/channelMonitor'
import { availableModelPriceSummary } from '@/utils/availableModelPricing'

export interface AvailableModelMonitorSummary {
  status: MonitorStatus
  availability: number | null
  latencyMs: number | null
  monitorCount: number
}

const props = withDefaults(
  defineProps<{
    model: UserSupportedModel
    monitorSummary?: AvailableModelMonitorSummary | null
    monitorLoading?: boolean
    priceMultiplier?: number
    expanded?: boolean
    pricingKeyPrefix?: string
    noPricingLabel: string
  }>(),
  {
    monitorSummary: null,
    monitorLoading: false,
    priceMultiplier: 1,
    expanded: false,
    pricingKeyPrefix: 'availableChannels.pricing',
  },
)

defineEmits<{ (event: 'select'): void }>()

const { t } = useI18n()
const priceSummary = computed(() =>
  availableModelPriceSummary(
    props.model,
    (key) => t(key),
    props.pricingKeyPrefix,
    props.noPricingLabel,
    props.priceMultiplier,
  ),
)

const monitorStatusTextClass = computed(() => {
  switch (props.monitorSummary?.status) {
    case 'operational':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'degraded':
      return 'text-amber-600 dark:text-amber-400'
    case 'failed':
    case 'error':
      return 'text-red-600 dark:text-red-400'
    default:
      return 'text-gray-500 dark:text-gray-400'
  }
})

const monitorStatusDotClass = computed(() => {
  switch (props.monitorSummary?.status) {
    case 'operational':
      return 'bg-emerald-500'
    case 'degraded':
      return 'bg-amber-500'
    case 'failed':
    case 'error':
      return 'bg-red-500'
    default:
      return 'bg-gray-400'
  }
})
</script>
