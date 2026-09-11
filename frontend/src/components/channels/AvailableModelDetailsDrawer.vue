<template>
  <BaseDialog
    :show="show"
    :title="model?.name || ''"
    placement="right"
    width="full"
    close-on-click-outside
    panel-class="available-model-details-panel"
    body-class="px-4 py-5 sm:px-6 sm:py-6"
    @close="$emit('close')"
  >
    <template #title-prefix>
      <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 ring-1 ring-inset ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
        <ModelIcon v-if="model" :model="model.name" size="21px" />
      </span>
    </template>
    <template #title-extra>
      <span
        v-if="platform"
        :class="[
          'inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide',
          platformBadgeClass(platform),
        ]"
      >
        <PlatformIcon :platform="platform as GroupPlatform" size="xs" />
        <span class="truncate">{{ platform }}</span>
      </span>
    </template>

    <div v-if="model" class="space-y-6">
              <AvailableModelPricingPanel
                :model="model"
                :no-pricing-label="noPricingLabel"
                :price-multiplier="priceMultiplier"
                :pricing-key-prefix="pricingKeyPrefix"
              />

              <section v-if="showGroupRates">
                <div class="mb-3 flex items-center justify-between gap-3">
                  <h4 class="text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    {{ t('availableChannels.groupRates.title') }}
                  </h4>
                  <span class="text-xs tabular-nums text-gray-400">
                    {{ t('availableChannels.counts.groups', { count: groups.length }) }}
                  </span>
                </div>

                <div class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700">
                  <div
                    v-for="group in groups"
                    :key="group.id"
                    class="grid gap-2 border-b border-gray-100 px-3 py-3 last:border-b-0 dark:border-dark-700 sm:grid-cols-[minmax(9rem,auto)_5rem_minmax(0,1fr)] sm:items-center"
                  >
                    <GroupBadge
                      :name="group.name"
                      :platform="group.platform as GroupPlatform"
                      :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
                      :rate-multiplier="group.rate_multiplier"
                      :user-rate-multiplier="userGroupRates[group.id] ?? null"
                      always-show-rate
                    />
                    <span class="font-mono text-xs font-semibold tabular-nums text-gray-700 dark:text-gray-200">
                      {{ effectiveGroupRate(group, userGroupRates) }}x
                    </span>
                    <span class="min-w-0 text-xs leading-5 text-gray-500 dark:text-gray-400">
                      {{ groupPriceSummary(group) }}
                    </span>
                  </div>
                  <div
                    v-if="groups.length === 0"
                    class="px-3 py-8 text-center text-sm text-gray-400"
                  >
                    {{ t('availableChannels.noGroups') }}
                  </div>
                </div>
              </section>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import AvailableModelPricingPanel from './AvailableModelPricingPanel.vue'
import type { UserAvailableGroup, UserSupportedModel } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBadgeClass } from '@/utils/platformColors'
import {
  effectiveGroupPriceSummary,
  effectiveGroupRate,
} from '@/utils/availableModelPricing'

const props = withDefaults(
  defineProps<{
    show: boolean
    model: UserSupportedModel | null
    platform: string
    groups: UserAvailableGroup[]
    userGroupRates: Record<number, number>
    pricingKeyPrefix?: string
    noPricingLabel: string
    showGroupRates?: boolean
    priceMultiplier?: number
  }>(),
  {
    pricingKeyPrefix: 'availableChannels.pricing',
    showGroupRates: true,
    priceMultiplier: 1,
  },
)

const { t } = useI18n()
const translate = (key: string) => t(key)

function groupPriceSummary(group: UserAvailableGroup): string {
  if (!props.model) return props.noPricingLabel
  return effectiveGroupPriceSummary(
    props.model,
    group,
    props.userGroupRates,
    translate,
    props.pricingKeyPrefix,
    props.noPricingLabel,
  )
}

</script>

<style scoped>
:deep(.available-model-details-panel) {
  --dialog-drawer-width: 56rem;
}
</style>
