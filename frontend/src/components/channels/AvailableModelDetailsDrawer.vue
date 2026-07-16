<template>
  <Teleport to="body">
    <Transition name="drawer">
      <div
        v-if="show"
        class="fixed inset-0 z-50 flex justify-end bg-gray-950/30 backdrop-blur-[2px] dark:bg-black/55"
        @mousedown.self="$emit('close')"
      >
        <aside
          ref="drawerRef"
          role="dialog"
          aria-modal="true"
          aria-labelledby="available-model-drawer-title"
          class="drawer-panel flex h-[100dvh] w-full min-w-0 flex-col overflow-hidden border-l border-gray-200 bg-white shadow-2xl dark:border-dark-700 dark:bg-dark-900 sm:max-w-2xl lg:max-w-3xl xl:max-w-4xl"
          @keydown="handleDrawerKeydown"
        >
          <header class="flex shrink-0 items-center justify-between gap-4 border-b border-gray-200 px-4 py-3.5 dark:border-dark-700 sm:px-6">
            <div class="flex min-w-0 items-center gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 ring-1 ring-inset ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
                <ModelIcon v-if="model" :model="model.name" size="21px" />
              </span>
              <div class="min-w-0">
                <h3 id="available-model-drawer-title" class="truncate font-mono text-base font-semibold text-gray-950 dark:text-white sm:text-lg">
                  {{ model?.name || '' }}
                </h3>
                <span
                  v-if="platform"
                  :class="[
                    'mt-1 inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide',
                    platformBadgeClass(platform),
                  ]"
                >
                  <PlatformIcon :platform="platform as GroupPlatform" size="xs" />
                  <span class="truncate">{{ platform }}</span>
                </span>
              </div>
            </div>
            <button
              ref="closeButtonRef"
              type="button"
              class="flex h-11 w-11 shrink-0 cursor-pointer items-center justify-center rounded-xl text-gray-400 transition-colors duration-200 hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60 dark:hover:bg-dark-800 dark:hover:text-gray-200"
              :aria-label="t('common.close')"
              @click="$emit('close')"
            >
              <Icon name="x" size="md" />
            </button>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-5 sm:px-6 sm:py-6">
            <div v-if="model" class="space-y-6">
              <section>
                <h4 class="mb-3 text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
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

              <section>
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
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { UserAvailableGroup, UserSupportedModel } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { BILLING_MODE_IMAGE, BILLING_MODE_TOKEN } from '@/constants/channel'
import { platformBadgeClass } from '@/utils/platformColors'
import {
  TOKEN_PRICE_SCALE,
  availableIntervalLabel,
  availablePricingItems,
  effectiveGroupPriceSummary,
  effectiveGroupRate,
  formatAvailablePrice,
  intervalHasPrice,
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
  }>(),
  { pricingKeyPrefix: 'availableChannels.pricing' },
)

const emit = defineEmits<{ (event: 'close'): void }>()

const { t } = useI18n()
const translate = (key: string) => t(key)
const drawerRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
let previousActiveElement: HTMLElement | null = null
let previousBodyOverflow = ''

const pricingItems = computed(() =>
  props.model ? availablePricingItems(props.model, translate, props.pricingKeyPrefix) : [],
)
const pricedIntervals = computed(() =>
  props.model?.pricing?.intervals?.filter(intervalHasPrice) ?? [],
)
const usesTokenPricing = computed(
  () => props.model?.pricing?.billing_mode === BILLING_MODE_TOKEN,
)
const intervalRequestLabel = computed(() =>
  t(
    `${props.pricingKeyPrefix}.${
      props.model?.pricing?.billing_mode === BILLING_MODE_IMAGE
        ? 'imageOutputPrice'
        : 'perRequestPrice'
    }`,
  ),
)

function formatIntervalPrice(value: number | null, scale = TOKEN_PRICE_SCALE): string {
  return formatAvailablePrice(value, scale, translate, props.pricingKeyPrefix)
}

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

function handleDrawerKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
    return
  }
  if (event.key !== 'Tab' || !drawerRef.value) return

  const focusableElements = Array.from(
    drawerRef.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  )
  if (focusableElements.length === 0) {
    event.preventDefault()
    return
  }

  const first = focusableElements[0]
  const last = focusableElements[focusableElements.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function unlockBodyScroll(): void {
  document.body.style.overflow = previousBodyOverflow
}

watch(
  () => props.show,
  async (show) => {
    if (show) {
      previousActiveElement = document.activeElement as HTMLElement
      previousBodyOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
      await nextTick()
      closeButtonRef.value?.focus()
      return
    }

    unlockBodyScroll()
    previousActiveElement?.focus()
    previousActiveElement = null
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  unlockBodyScroll()
})
</script>

<style scoped>
.drawer-enter-active,
.drawer-leave-active {
  transition: opacity 200ms ease;
}

.drawer-enter-active .drawer-panel,
.drawer-leave-active .drawer-panel {
  transition: transform 240ms ease;
}

.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}

.drawer-enter-from .drawer-panel,
.drawer-leave-to .drawer-panel {
  transform: translateX(100%);
}

@media (prefers-reduced-motion: reduce) {
  .drawer-enter-active,
  .drawer-leave-active,
  .drawer-enter-active .drawer-panel,
  .drawer-leave-active .drawer-panel {
    transition-duration: 1ms;
  }
}
</style>
