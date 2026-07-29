<template>
  <section>
    <h4
      v-if="showTitle"
      class="mb-3 text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400"
    >
      {{ t('availableChannels.pricing.title') }}
    </h4>

    <div
      v-if="!model.pricing"
      class="rounded-xl border border-dashed border-gray-200 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
    >
      {{ noPricingLabel }}
    </div>

    <template v-else>
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="item in pricingItems"
          :key="item.key"
          class="rounded-xl border border-gray-200 bg-gray-50/70 px-3 py-3 dark:border-dark-700 dark:bg-dark-900/45"
        >
          <div class="text-xs text-gray-500 dark:text-gray-400">
            {{ item.label }}
          </div>
          <div class="mt-1.5 break-words font-mono text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ item.value }}
          </div>
        </div>
      </div>

      <div
        v-if="pricedIntervals.length"
        class="mt-4 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700"
      >
        <div class="border-b border-gray-200 bg-gray-50/80 px-3 py-2.5 text-xs font-semibold text-gray-700 dark:border-dark-700 dark:bg-dark-900/60 dark:text-gray-300">
          {{ t(`${pricingKeyPrefix}.intervals`) }}
        </div>
        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div
            v-for="(interval, index) in pricedIntervals"
            :key="`${availableIntervalLabel(interval)}-${index}`"
            class="grid gap-2 px-3 py-3 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2 lg:grid-cols-[minmax(8rem,1.2fr)_repeat(4,minmax(6rem,1fr))]"
          >
            <span class="font-medium text-gray-800 dark:text-gray-100">
              {{ availableIntervalLabel(interval) }}
            </span>
            <template v-if="usesTokenPricing">
              <span>{{ t(`${pricingKeyPrefix}.inputPrice`) }} {{ formatIntervalPrice(interval.input_price) }}</span>
              <span>{{ t(`${pricingKeyPrefix}.outputPrice`) }} {{ formatIntervalPrice(interval.output_price) }}</span>
              <span>{{ t(`${pricingKeyPrefix}.cacheWritePrice`) }} {{ formatIntervalPrice(interval.cache_write_price) }}</span>
              <span>{{ t(`${pricingKeyPrefix}.cacheReadPrice`) }} {{ formatIntervalPrice(interval.cache_read_price) }}</span>
            </template>
            <span v-else class="lg:col-span-4">
              {{ intervalRequestLabel }}
              {{ formatIntervalPrice(interval.per_request_price, 1) }}
            </span>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSupportedModel } from '@/api/channels'
import { BILLING_MODE_IMAGE, BILLING_MODE_TOKEN } from '@/constants/channel'
import {
  TOKEN_PRICE_SCALE,
  availableIntervalLabel,
  availablePricingItems,
  formatAvailablePrice,
  intervalHasPrice,
} from '@/utils/availableModelPricing'

const props = withDefaults(
  defineProps<{
    model: UserSupportedModel
    noPricingLabel: string
    priceMultiplier?: number
    pricingKeyPrefix?: string
    showTitle?: boolean
  }>(),
  {
    priceMultiplier: 1,
    pricingKeyPrefix: 'availableChannels.pricing',
    showTitle: true,
  },
)

const { t } = useI18n()
const translate = (key: string) => t(key)
const pricingItems = computed(() =>
  availablePricingItems(
    props.model,
    translate,
    props.pricingKeyPrefix,
    props.priceMultiplier,
  ),
)
const pricedIntervals = computed(() =>
  props.model.pricing?.intervals?.filter(intervalHasPrice) ?? [],
)
const usesTokenPricing = computed(
  () => props.model.pricing?.billing_mode === BILLING_MODE_TOKEN,
)
const intervalRequestLabel = computed(() =>
  t(
    `${props.pricingKeyPrefix}.${
      props.model.pricing?.billing_mode === BILLING_MODE_IMAGE
        ? 'imageOutputPrice'
        : 'perRequestPrice'
    }`,
  ),
)

function formatIntervalPrice(value: number | null, scale = TOKEN_PRICE_SCALE): string {
  const effectiveValue = value == null ? null : value * props.priceMultiplier
  return formatAvailablePrice(effectiveValue, scale, translate, props.pricingKeyPrefix)
}
</script>
