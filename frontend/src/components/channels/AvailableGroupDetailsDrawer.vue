<template>
  <Teleport to="body">
    <Transition name="drawer">
      <div
        v-if="show"
        class="fixed inset-0 z-50 flex justify-end bg-gray-950/30 backdrop-blur-[2px] dark:bg-black/55"
        @mousedown.self="emit('close')"
      >
        <aside
          ref="drawerRef"
          role="dialog"
          aria-modal="true"
          aria-labelledby="available-group-drawer-title"
          class="drawer-panel flex h-[100dvh] w-full min-w-0 flex-col overflow-hidden border-l border-gray-200 bg-white shadow-2xl dark:border-dark-700 dark:bg-dark-900 sm:max-w-2xl lg:max-w-3xl"
          @keydown="handleDrawerKeydown"
        >
          <header class="flex shrink-0 items-center justify-between gap-4 border-b border-gray-200 px-4 py-3.5 dark:border-dark-700 sm:px-6">
            <div class="flex min-w-0 items-center gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-gray-50 ring-1 ring-inset ring-gray-200 dark:bg-dark-800 dark:ring-dark-700">
                <PlatformIcon v-if="group" :platform="group.platform as GroupPlatform" size="md" />
              </span>
              <div class="min-w-0">
                <h3 id="available-group-drawer-title" class="truncate text-base font-semibold text-gray-950 dark:text-white sm:text-lg">
                  {{ group?.name || '' }}
                </h3>
                <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
                  {{ t('availableChannels.groupDrawer.description') }}
                </span>
              </div>
            </div>
            <button
              ref="closeButtonRef"
              type="button"
              class="flex h-11 w-11 shrink-0 cursor-pointer items-center justify-center rounded-xl text-gray-400 transition-colors duration-200 hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60 dark:hover:bg-dark-800 dark:hover:text-gray-200"
              :aria-label="t('common.close')"
              @click="emit('close')"
            >
              <Icon name="x" size="md" />
            </button>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-5 sm:px-6">
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
const drawerRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const selectedModelKey = ref<string | null>(null)
const effectiveRate = computed(() =>
  props.group ? effectiveGroupRate(props.group, props.userGroupRates) : 1,
)
let previousActiveElement: HTMLElement | null = null
let previousBodyOverflow = ''

function handleDrawerKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close')
    return
  }
  if (event.key !== 'Tab' || !drawerRef.value) return
  const focusableElements = Array.from(
    drawerRef.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
    ),
  )
  if (focusableElements.length === 0) return
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

function modelKey(model: UserSupportedModel): string {
  return `${model.platform}:${model.name}`
}

function toggleModel(model: UserSupportedModel): void {
  const key = modelKey(model)
  selectedModelKey.value = selectedModelKey.value === key ? null : key
}

function unlockBodyScroll(): void {
  document.body.style.overflow = previousBodyOverflow
}

watch(
  () => props.show,
  async show => {
    if (show) {
      previousActiveElement = document.activeElement as HTMLElement
      previousBodyOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
      await nextTick()
      closeButtonRef.value?.focus()
      return
    }
    selectedModelKey.value = null
    unlockBodyScroll()
    previousActiveElement?.focus()
    previousActiveElement = null
  },
)

onBeforeUnmount(unlockBodyScroll)
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
  .drawer-enter-active,
  .drawer-leave-active,
  .drawer-enter-active .drawer-panel,
  .drawer-leave-active .drawer-panel {
    transition-duration: 1ms;
  }

  .pricing-expand-enter-active,
  .pricing-expand-leave-active {
    transition-duration: 1ms;
  }
}
</style>
