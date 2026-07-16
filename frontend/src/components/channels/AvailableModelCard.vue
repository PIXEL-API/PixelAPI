<template>
  <button
    type="button"
    class="group flex min-h-16 w-full cursor-pointer items-center gap-3 rounded-xl border border-gray-200 bg-white px-3 py-2.5 text-left shadow-sm transition-[border-color,background-color,box-shadow] duration-200 hover:border-gray-300 hover:bg-gray-50 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60 dark:border-dark-700 dark:bg-dark-900/45 dark:hover:border-dark-600 dark:hover:bg-dark-800/70"
    :aria-label="t('availableChannels.viewModelDetails', { model: model.name })"
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
    </span>

    <Icon
      name="chevronRight"
      size="sm"
      class="shrink-0 text-gray-300 transition-colors duration-200 group-hover:text-gray-500 dark:text-dark-600 dark:group-hover:text-dark-300"
    />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import type { UserSupportedModel } from '@/api/channels'
import { availableModelPriceSummary } from '@/utils/availableModelPricing'

const props = withDefaults(
  defineProps<{
    model: UserSupportedModel
    pricingKeyPrefix?: string
    noPricingLabel: string
  }>(),
  { pricingKeyPrefix: 'availableChannels.pricing' },
)

defineEmits<{ (event: 'select'): void }>()

const { t } = useI18n()
const priceSummary = computed(() =>
  availableModelPriceSummary(
    props.model,
    (key) => t(key),
    props.pricingKeyPrefix,
    props.noPricingLabel,
  ),
)
</script>
