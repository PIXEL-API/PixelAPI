<template>
  <div class="flex min-w-0 flex-1 items-start justify-between gap-3">
    <!-- Left: name + description -->
    <div
      class="flex min-w-0 flex-1 flex-col items-start"
      :title="description || undefined"
    >
      <!-- Row 1: availability warning + platform badge -->
      <div class="flex min-w-0 flex-wrap items-center gap-1.5">
        <span
          v-if="statusLabel"
          :class="[
            'inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-semibold leading-5',
            statusTone === 'warning'
              ? 'bg-red-50 text-red-600 dark:bg-red-950/40 dark:text-red-400'
              : 'bg-emerald-50 text-emerald-600 dark:bg-emerald-950/40 dark:text-emerald-400'
          ]"
        >
          <svg
            aria-hidden="true"
            class="h-3.5 w-3.5"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            stroke-width="2"
          >
            <path
              v-if="statusTone === 'warning'"
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M12 9v4m0 4h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"
            />
            <path
              v-else
              stroke-linecap="round"
              stroke-linejoin="round"
              d="m5 12 4 4L19 6"
            />
          </svg>
          {{ statusLabel }}
        </span>
        <GroupBadge
          :name="name"
          :platform="platform"
          :scope="scope"
          :subscription-type="subscriptionType"
          :show-rate="false"
          class="groupOptionItemBadge"
        />
      </div>
      <!-- Row 2: description with top spacing -->
      <span
        v-if="description"
        class="mt-1.5 w-full text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-2"
      >
        {{ description }}
      </span>
    </div>

    <!-- Right: rate pill + checkmark (vertically centered to first row) -->
    <div class="flex shrink-0 items-center gap-2 pt-0.5">
      <!-- Rate pill (platform color) -->
      <span v-if="rateMultiplier !== undefined" :class="['inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold', ratePillClass]">
        <template v-if="hasCustomRate">
          <span class="mr-1 line-through opacity-50">{{ rateMultiplier }}x</span>
          <span class="font-bold">{{ effectiveDisplayRate }}x</span>
          <span v-if="rateSourceLabel" class="ml-1 text-[11px] font-medium opacity-80">{{ rateSourceLabel }}</span>
        </template>
        <template v-else>
          {{ rateMultiplier }}x {{ rateSourceLabel || t('groups.rateLabel') }}
        </template>
      </span>
      <!-- Checkmark -->
      <svg
        v-if="showCheckmark && selected"
        class="h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
        stroke-width="2"
      >
        <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
      </svg>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import type { SubscriptionType, GroupPlatform, GroupScope } from '@/types'

interface Props {
  name: string
  platform: GroupPlatform
  scope?: GroupScope
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null
  effectiveRateMultiplier?: number | null
  rateMultiplierSource?: string | null
  description?: string | null
  statusLabel?: string | null
  statusTone?: 'recommended' | 'warning'
  selected?: boolean
  showCheckmark?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  subscriptionType: 'standard',
  selected: false,
  showCheckmark: true,
  statusLabel: null,
  statusTone: 'recommended',
  userRateMultiplier: null,
  effectiveRateMultiplier: null,
  rateMultiplierSource: null
})
const { t } = useI18n()

const effectiveDisplayRate = computed(() => props.effectiveRateMultiplier ?? props.userRateMultiplier ?? null)

// Whether user has an effective rate different from default
const hasCustomRate = computed(() => {
  return (
    effectiveDisplayRate.value !== null &&
    effectiveDisplayRate.value !== undefined &&
    props.rateMultiplier !== undefined &&
    effectiveDisplayRate.value !== props.rateMultiplier
  )
})

const rateSourceLabel = computed(() => {
  switch (props.rateMultiplierSource) {
    case 'new_user_group':
      return t('groups.rateSources.newUserGroup')
    case 'user_group':
      return t('groups.rateSources.userGroup')
    case 'account_share':
      return t('groups.rateSources.accountShare')
    case 'group_default':
      return t('groups.rateLabel')
    default:
      return props.userRateMultiplier != null ? t('groups.rateSources.userGroup') : t('groups.rateLabel')
  }
})

// Rate pill color matches platform badge color
const ratePillClass = computed(() => {
  switch (props.platform) {
    case 'anthropic':
      return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
    case 'openai':
      return 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400'
    case 'gemini':
      return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
    default: // antigravity and others
      return 'bg-violet-50 text-violet-700 dark:bg-violet-900/20 dark:text-violet-400'
  }
})
</script>

<style scoped>
/* Bold the group name inside GroupBadge when used in dropdown option */
.groupOptionItemBadge :deep(span.truncate) {
  font-weight: 600;
}
</style>
