<template>
  <BaseDialog
    :show="show"
    :title="group?.name || ''"
    placement="right"
    width="full"
    close-on-click-outside
    body-class="px-4 py-5 sm:px-6"
    @close="emit('close')"
  >
    <template #title-prefix>
      <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 ring-1 ring-inset ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
        <PlatformIcon v-if="group" :platform="group.platform as GroupPlatform" size="md" />
      </span>
    </template>
    <template #title-extra>
      <span class="hidden text-xs font-normal text-gray-500 dark:text-gray-400 sm:inline">
        {{ t('availableChannels.groupDrawer.description') }}
      </span>
    </template>

    <section v-if="group">
              <div class="mb-5 rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-800/60">
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <GroupBadge
                    :name="group.name"
                    :platform="group.platform as GroupPlatform"
                    :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="group.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[group.id] ?? null"
                    always-show-rate
                  />
                  <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
                    {{ t('availableChannels.counts.models', { count: models.length }) }}
                  </span>
                </div>
              </div>

              <h4 class="mb-3 text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                {{ t('availableChannels.columns.supportedModels') }}
              </h4>
              <div class="grid min-w-0 grid-cols-1 gap-2">
                <div
                  v-for="model in models"
                  :key="`${model.platform}:${model.name}`"
                  class="min-w-0"
                >
                  <AvailableModelCard
                    :model="model"
                    :no-pricing-label="noPricingLabel"
                    :price-multiplier="effectiveRate"
                    :expanded="selectedModelKey === modelKey(model)"
                    @select="toggleModel(model)"
                  />
                  <Transition name="pricing-expand">
                    <div
                      v-if="selectedModelKey === modelKey(model)"
                      class="mt-2 overflow-hidden rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900/35 sm:p-4"
                    >
                      <AvailableModelPricingPanel
                        :model="model"
                        :no-pricing-label="noPricingLabel"
                        :price-multiplier="effectiveRate"
                        :show-title="false"
                      />
                    </div>
                  </Transition>
                </div>
              </div>
    </section>
  </BaseDialog>

</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import AvailableModelCard from './AvailableModelCard.vue'
import AvailableModelPricingPanel from './AvailableModelPricingPanel.vue'
import type { UserAvailableGroup, UserSupportedModel } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { effectiveGroupRate } from '@/utils/availableModelPricing'

const props = defineProps<{
  show: boolean
  group: UserAvailableGroup | null
  models: UserSupportedModel[]
  userGroupRates: Record<number, number>
  noPricingLabel: string
}>()

const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()
const selectedModelKey = ref<string | null>(null)
const effectiveRate = computed(() =>
  props.group ? effectiveGroupRate(props.group, props.userGroupRates) : 1,
)
function modelKey(model: UserSupportedModel): string {
  return `${model.platform}:${model.name}`
}

function toggleModel(model: UserSupportedModel): void {
  const key = modelKey(model)
  selectedModelKey.value = selectedModelKey.value === key ? null : key
}

watch(
  () => props.show,
  show => {
    if (!show) selectedModelKey.value = null
  },
)
</script>

<style scoped>
.pricing-expand-enter-active,
.pricing-expand-leave-active {
  transition: opacity 180ms ease, transform 180ms ease;
}

.pricing-expand-enter-from,
.pricing-expand-leave-to {
  opacity: 0;
  transform: translateY(-0.25rem);
}

@media (prefers-reduced-motion: reduce) {
  .pricing-expand-enter-active,
  .pricing-expand-leave-active {
    transition-duration: 1ms;
  }
}
</style>
