<template>
  <section class="py-3 md:py-4">
    <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
      <div class="grid min-w-0 flex-1 grid-cols-1 gap-2 sm:grid-cols-2 xl:max-w-3xl">
        <div
          class="min-w-0 rounded-xl border border-gray-200/80 bg-white/70 px-3.5 py-3 dark:border-dark-700/70 dark:bg-dark-800/60"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('channelStatus.summary.currentDispatchStatus') }}
            </span>
            <span
              class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold tracking-wide"
              :class="overallChipClass"
            >
              <span
                class="mr-1.5 h-1.5 w-1.5 rounded-full"
                :class="overallDotClass"
              ></span>
              {{ overallLabel }}
            </span>
          </div>
          <p
            class="mt-1.5 truncate text-xs text-gray-500 dark:text-gray-400"
            :title="overallDetailDisplay"
          >
            {{ overallDetailDisplay }}
          </p>
        </div>

        <div
          class="min-w-0 rounded-xl border border-gray-200/80 bg-white/70 px-3.5 py-3 dark:border-dark-700/70 dark:bg-dark-800/60"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ minimumAvailabilityLabel }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
                {{ t('channelStatus.summary.minimumAvailabilityHint') }}
              </p>
            </div>
            <span
              class="shrink-0 text-xl font-bold tabular-nums"
              :style="minimumAvailabilityStyle"
            >
              {{ minimumAvailabilityDisplay }}
            </span>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <div
          role="tablist"
          class="inline-flex rounded-xl border border-gray-200/60 bg-gray-100 p-0.5 text-xs dark:border-dark-700/60 dark:bg-dark-800"
        >
          <button
            v-for="opt in windowOptions"
            :key="opt.value"
            type="button"
            role="tab"
            :aria-selected="window === opt.value"
            class="min-h-11 rounded-lg px-3 transition-colors"
            :class="window === opt.value
              ? 'bg-white dark:bg-dark-700 shadow-sm text-gray-900 dark:text-white font-semibold'
              : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
            @click="emit('update:window', opt.value)"
          >
            {{ opt.label }}
          </button>
        </div>

        <button
          type="button"
          class="flex min-h-11 min-w-11 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200"
          :disabled="loading"
          :title="t('common.refresh')"
          @click="emit('refresh')"
        >
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>

        <AutoRefreshButton
          v-if="autoRefresh"
          :enabled="autoRefresh.enabled.value"
          :interval-seconds="autoRefresh.intervalSeconds.value"
          :countdown="autoRefresh.countdown.value"
          :intervals="autoRefresh.intervals"
          @update:enabled="autoRefresh.setEnabled"
          @update:interval="autoRefresh.setInterval"
        />
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import AutoRefreshButton from '@/components/common/AutoRefreshButton.vue'
import { hslForPct } from '@/composables/useChannelMonitorFormat'

export type MonitorWindow = '7d' | '15d' | '30d'
export type OverallStatus = 'operational' | 'degraded' | 'constrained' | 'unavailable' | 'unknown'

const props = defineProps<{
  overallStatus: OverallStatus
  overallDetail: string
  window: MonitorWindow
  loading: boolean
  statusLoading: boolean
  minimumAvailability: number | null
  availabilityLoading: boolean
  autoRefresh?: {
    enabled: { value: boolean }
    intervalSeconds: { value: number }
    countdown: { value: number }
    intervals: readonly number[]
    setEnabled: (v: boolean) => void
    setInterval: (v: number) => void
  }
}>()

const emit = defineEmits<{
  (e: 'update:window', value: MonitorWindow): void
  (e: 'refresh'): void
}>()

const { t } = useI18n()

const windowOptions = computed<{ value: MonitorWindow; label: string }[]>(() => [
  { value: '7d', label: t('channelStatus.windowTab.7d') },
  { value: '15d', label: t('channelStatus.windowTab.15d') },
  { value: '30d', label: t('channelStatus.windowTab.30d') },
])

const overallLabel = computed(() =>
  props.statusLoading
    ? t('common.loading')
    : t(`channelStatus.overall.${props.overallStatus}`)
)

const overallDetailDisplay = computed(() =>
  props.statusLoading
    ? t('channelStatus.summary.loadingStatus')
    : props.overallDetail
)

const minimumAvailabilityLabel = computed(() =>
  t('channelStatus.summary.minimumAvailability', {
    window: t(`channelStatus.windowTab.${props.window}`),
  })
)

const minimumAvailabilityDisplay = computed(() => {
  if (props.availabilityLoading) return t('common.loading')
  if (props.minimumAvailability == null || Number.isNaN(props.minimumAvailability)) {
    return t('channelStatus.summary.noAvailabilityData')
  }
  return `${props.minimumAvailability.toFixed(2)}%`
})

const minimumAvailabilityStyle = computed(() => {
  if (props.availabilityLoading) return { color: 'rgb(107 114 128)' }
  const colour = hslForPct(props.minimumAvailability)
  return { color: colour ?? 'rgb(156 163 175)' }
})

const overallChipClass = computed(() => {
  if (props.statusLoading) {
    return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'degraded':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    case 'constrained':
      return 'bg-orange-100 text-orange-700 dark:bg-orange-500/15 dark:text-orange-300'
    case 'unavailable':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    case 'unknown':
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
})

const overallDotClass = computed(() => {
  if (props.statusLoading) return 'bg-gray-400 animate-pulse'
  switch (props.overallStatus) {
    case 'operational':
      return 'bg-emerald-500 animate-pulse'
    case 'degraded':
      return 'bg-amber-500 animate-pulse'
    case 'constrained':
      return 'bg-orange-500 animate-pulse'
    case 'unavailable':
      return 'bg-red-500 animate-pulse'
    case 'unknown':
    default:
      return 'bg-gray-400'
  }
})
</script>
