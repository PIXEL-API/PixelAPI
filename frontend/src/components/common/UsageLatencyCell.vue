<template>
  <div class="flex min-w-[8.5rem] items-stretch gap-2">
    <span
      class="w-1 shrink-0 rounded-full"
      :class="barClasses"
      aria-hidden="true"
    ></span>
    <div class="grid grid-cols-[max-content_max-content] items-baseline gap-x-2 gap-y-0.5 text-xs">
      <span class="text-gray-500 dark:text-gray-400">{{ t('usage.latencyFirstToken') }}</span>
      <span
        v-if="hasFirstToken"
        class="font-medium tabular-nums"
        :class="LATENCY_TEXT_CLASSES[firstTokenLevel]"
        :title="t(LATENCY_SEVERITY_LABEL_KEYS[firstTokenLevel])"
      >
        <span class="mr-0.5" aria-hidden="true">{{ LATENCY_SEVERITY_SYMBOLS[firstTokenLevel] }}</span>
        {{ formatUsageDuration(firstTokenMs) }}
        <span class="sr-only"> ({{ t(LATENCY_SEVERITY_LABEL_KEYS[firstTokenLevel]) }})</span>
      </span>
      <span v-else class="text-gray-400 dark:text-gray-500">-</span>
      <span class="text-gray-500 dark:text-gray-400">{{ t('usage.latencyDuration') }}</span>
      <span
        v-if="hasDuration"
        class="font-medium tabular-nums"
        :class="LATENCY_TEXT_CLASSES[durationLevel]"
        :title="t(LATENCY_SEVERITY_LABEL_KEYS[durationLevel])"
      >
        <span class="mr-0.5" aria-hidden="true">{{ LATENCY_SEVERITY_SYMBOLS[durationLevel] }}</span>
        {{ formatUsageDuration(durationMs) }}
        <span class="sr-only"> ({{ t(LATENCY_SEVERITY_LABEL_KEYS[durationLevel]) }})</span>
      </span>
      <span v-else class="text-gray-400 dark:text-gray-500">-</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  LATENCY_BAR_CLASSES,
  LATENCY_BAR_FROM_CLASSES,
  LATENCY_BAR_TO_CLASSES,
  LATENCY_SEVERITY_LABEL_KEYS,
  LATENCY_SEVERITY_SYMBOLS,
  LATENCY_TEXT_CLASSES,
  durationSeverity,
  firstTokenSeverity,
  formatUsageDuration
} from '@/utils/latencyHealth'

const props = defineProps<{
  firstTokenMs?: number | null
  durationMs?: number | null
}>()

const { t } = useI18n()

const hasFirstToken = computed(
  () => props.firstTokenMs != null && Number.isFinite(props.firstTokenMs) && props.firstTokenMs >= 0
)
const hasDuration = computed(
  () => props.durationMs != null && Number.isFinite(props.durationMs) && props.durationMs >= 0
)
const firstTokenLevel = computed(() => firstTokenSeverity(props.firstTokenMs ?? 0))
const durationLevel = computed(() => durationSeverity(props.durationMs ?? 0))
const barClasses = computed(() => {
  if (!hasFirstToken.value && !hasDuration.value) return 'bg-gray-300 dark:bg-dark-600'
  if (!hasFirstToken.value) return LATENCY_BAR_CLASSES[durationLevel.value]
  if (!hasDuration.value) return LATENCY_BAR_CLASSES[firstTokenLevel.value]
  return [
    'bg-gradient-to-b from-40% to-60%',
    LATENCY_BAR_FROM_CLASSES[firstTokenLevel.value],
    LATENCY_BAR_TO_CLASSES[durationLevel.value]
  ]
})
</script>
