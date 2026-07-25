<template>
  <article
    class="group relative overflow-hidden rounded-2xl border border-slate-200 bg-white p-4 shadow-sm transition-colors duration-200 dark:border-slate-700 dark:bg-slate-900/70"
  >
    <div class="absolute inset-y-0 left-0 w-1" :class="accentClass" aria-hidden="true"></div>

    <div class="flex items-start justify-between gap-4 pl-1">
      <div>
        <div class="flex items-center gap-2">
          <span
            class="inline-flex min-h-7 items-center rounded-lg bg-slate-950 px-2.5 font-mono text-xs font-semibold tracking-wide text-white dark:bg-white dark:text-slate-950"
          >
            {{ label }}
          </span>
          <span class="text-sm font-semibold text-slate-900 dark:text-white">
            {{ title }}
          </span>
        </div>
        <p class="mt-1.5 text-xs leading-5 text-slate-500 dark:text-slate-400">
          {{ rangeLabel }}
        </p>
      </div>

      <div v-if="progress" class="text-right">
        <div class="font-mono text-xl font-semibold tabular-nums text-slate-950 dark:text-white">
          {{ Math.round(progress.utilization) }}%
        </div>
        <div class="mt-0.5 text-[11px] font-medium" :class="statusTextClass">
          {{ resetLabel }}
        </div>
      </div>
    </div>

    <div v-if="progress" class="mt-4 pl-1">
      <div
        class="h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800"
        role="progressbar"
        :aria-valuenow="Math.min(Math.max(progress.utilization, 0), 100)"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="`${title} ${Math.round(progress.utilization)}%`"
      >
        <div
          class="h-full rounded-full transition-[width] duration-300 motion-reduce:transition-none"
          :class="accentClass"
          :style="{ width: `${Math.min(Math.max(progress.utilization, 0), 100)}%` }"
        ></div>
      </div>
    </div>

    <div
      v-if="progress?.window_stats && progress.window_start"
      class="mt-4 grid grid-cols-3 divide-x divide-slate-100 rounded-xl border border-slate-100 bg-slate-50/80 py-3 dark:divide-slate-800 dark:border-slate-800 dark:bg-slate-950/60"
    >
      <div class="min-w-0 px-3">
        <div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
          {{ t('admin.accounts.stats.pixelCost') }}
        </div>
        <div class="mt-1 truncate font-mono text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
          ${{ formatCost(progress.window_stats.cost) }}
        </div>
      </div>
      <div class="min-w-0 px-3">
        <div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
          {{ t('admin.accounts.stats.requests') }}
        </div>
        <div class="mt-1 truncate font-mono text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
          {{ formatCompact(progress.window_stats.requests) }}
        </div>
      </div>
      <div class="min-w-0 px-3">
        <div class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
          {{ t('admin.accounts.stats.tokens') }}
        </div>
        <div class="mt-1 truncate font-mono text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
          {{ formatCompact(progress.window_stats.tokens) }}
        </div>
      </div>
    </div>

    <div
      v-if="progress?.window_stats && progress.window_start"
      class="mt-3 flex items-start gap-2 rounded-lg px-3 py-2 text-[11px] leading-4"
      :class="
        progress.stats_complete
          ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
          : 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
      "
    >
      <span
        class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full"
        :class="progress.stats_complete ? 'bg-emerald-500' : 'bg-amber-500'"
        aria-hidden="true"
      ></span>
      <span>
        {{
          progress.stats_complete
            ? t('admin.accounts.stats.windowCompleteCoverage')
            : t('admin.accounts.stats.windowPartialCoverage', {
                start:
                  formatDate(progress.stats_available_from) ||
                  t('admin.accounts.stats.unknown')
              })
        }}
      </span>
    </div>

    <div
      v-else
      class="mt-4 rounded-xl border border-dashed border-slate-200 px-3 py-3 text-xs leading-5 text-slate-500 dark:border-slate-700 dark:text-slate-400"
    >
      {{ progress ? t('admin.accounts.stats.windowBoundaryUnavailable') : t('admin.accounts.stats.windowSnapshotUnavailable') }}
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageProgress } from '@/types'
import { formatCompactNumber } from '@/utils/format'

const props = defineProps<{
  label: string
  title: string
  progress?: UsageProgress | null
}>()

const { t, locale } = useI18n()

const accentClass = computed(() => {
  const utilization = props.progress?.utilization ?? 0
  if (utilization >= 100) return 'bg-rose-500'
  if (utilization >= 80) return 'bg-amber-500'
  return 'bg-blue-600'
})

const statusTextClass = computed(() => {
  const utilization = props.progress?.utilization ?? 0
  if (utilization >= 100) return 'text-rose-600 dark:text-rose-400'
  if (utilization >= 80) return 'text-amber-600 dark:text-amber-400'
  return 'text-slate-500 dark:text-slate-400'
})

const formatDate = (value?: string | null): string => {
  if (!value) return ''
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

const rangeLabel = computed(() => {
  if (!props.progress?.window_start || !props.progress.resets_at) {
    return t('admin.accounts.stats.exactWindowPending')
  }
  return t('admin.accounts.stats.exactWindowRange', {
    start: formatDate(props.progress.window_start),
    end: formatDate(props.progress.resets_at)
  })
})

const resetLabel = computed(() => {
  if (!props.progress?.resets_at) return t('admin.accounts.stats.resetUnknown')
  const resetAt = new Date(props.progress.resets_at)
  const diffMs = resetAt.getTime() - Date.now()
  if (!Number.isFinite(diffMs) || diffMs <= 0) return t('admin.accounts.stats.awaitingSnapshot')
  const totalHours = Math.floor(diffMs / 3_600_000)
  if (totalHours >= 24) {
    return t('admin.accounts.stats.resetsInDaysHours', {
      days: Math.floor(totalHours / 24),
      hours: totalHours % 24
    })
  }
  const minutes = Math.max(1, Math.floor(diffMs / 60_000))
  return t('admin.accounts.stats.resetsInHoursMinutes', {
    hours: Math.floor(minutes / 60),
    minutes: minutes % 60
  })
})

const formatCompact = (value: number): string => formatCompactNumber(value)

const formatCost = (value: number): string => {
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}
</script>
