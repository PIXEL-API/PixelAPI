<template>
  <div class="channel-catalog">
    <div v-if="loading" class="space-y-4">
      <div
        v-for="index in 3"
        :key="index"
        class="h-48 animate-pulse rounded-xl border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800/60"
      />
    </div>

    <div
      v-else-if="rows.length === 0"
      class="flex min-h-72 flex-col items-center justify-center rounded-xl border border-gray-200 bg-white px-6 py-12 text-center shadow-sm dark:border-dark-700 dark:bg-dark-900/40"
    >
      <span class="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-gray-100 text-gray-400 dark:bg-dark-800 dark:text-gray-500">
        <Icon name="inbox" size="xl" class="h-7 w-7" />
      </span>
      <p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ emptyLabel }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('availableChannels.emptyHint') }}</p>
    </div>

    <div v-else class="space-y-4">
      <article
        v-for="(channel, channelIndex) in rows"
        :key="`${channel.name}-${channelIndex}`"
        class="min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm transition-shadow duration-200 hover:shadow-md dark:border-dark-700 dark:bg-dark-900/40"
      >
        <header class="relative border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-5">
          <div
            v-if="channel.platforms.length"
            :class="['absolute inset-x-0 top-0 h-0.5', platformAccentBarClass(channel.platforms[0].platform)]"
          />
          <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <h2 class="truncate text-base font-semibold tracking-tight text-gray-950 dark:text-white">{{ channel.name }}</h2>
              <p v-if="channel.description" class="mt-1 max-w-2xl text-sm leading-5 text-gray-500 dark:text-gray-400">
                {{ channel.description }}
              </p>
            </div>
            <div class="flex flex-wrap gap-1.5 text-xs text-gray-600 dark:text-gray-300">
              <span class="rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-800">
                {{ t('availableChannels.counts.platforms', { count: channel.platforms.length }) }}
              </span>
              <span class="rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-800">
                {{ t('availableChannels.counts.groups', { count: channelGroupCount(channel) }) }}
              </span>
              <span class="rounded-md bg-gray-100 px-2 py-1 dark:bg-dark-800">
                {{ t('availableChannels.counts.models', { count: channelModelCount(channel) }) }}
              </span>
            </div>
          </div>
        </header>

        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <section
            v-for="section in channel.platforms"
            :key="`${channel.name}-${section.platform}`"
            class="grid min-w-0 gap-5 p-4 sm:p-5 xl:grid-cols-[minmax(13rem,17rem)_minmax(0,1fr)]"
          >
            <aside
              :class="['min-w-0 rounded-xl border bg-gray-50/70 p-4 dark:bg-dark-800/45 xl:sticky xl:top-20 xl:self-start', platformBorderClass(section.platform)]"
            >
              <div class="flex items-center justify-between gap-3">
                <span
                  :class="['inline-flex min-h-7 min-w-0 items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-semibold uppercase tracking-wide', platformBadgeClass(section.platform)]"
                >
                  <PlatformIcon :platform="section.platform as GroupPlatform" size="sm" />
                  <span class="truncate">{{ section.platform }}</span>
                </span>
                <span class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400">
                  {{ t('availableChannels.counts.models', { count: section.supported_models.length }) }}
                </span>
              </div>

              <div class="mt-4 space-y-3">
                <div v-for="bucket in groupBuckets(section)" :key="bucket.key" v-show="bucket.groups.length">
                  <div
                    :class="['mb-1.5 flex items-center gap-1 text-[11px] font-medium', bucket.labelClass]"
                    :title="t(bucket.tooltipKey)"
                  >
                    <Icon :name="bucket.icon" size="xs" class="h-3 w-3" />
                    {{ t(bucket.labelKey) }}
                  </div>
                  <div class="flex flex-wrap gap-1.5">
                    <GroupBadge
                      v-for="group in bucket.groups"
                      :key="group.id"
                      :name="group.name"
                      :platform="group.platform as GroupPlatform"
                      :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
                      :rate-multiplier="group.rate_multiplier"
                      :user-rate-multiplier="userGroupRates[group.id] ?? null"
                      always-show-rate
                    />
                  </div>
                </div>
                <p v-if="section.groups.length === 0" class="text-xs text-gray-400">{{ t('availableChannels.noGroups') }}</p>
              </div>
            </aside>

            <div class="min-w-0">
              <div class="mb-3 flex items-center justify-between gap-3">
                <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                  {{ t('availableChannels.columns.supportedModels') }}
                </h3>
                <span class="text-xs tabular-nums text-gray-400">{{ section.supported_models.length }}</span>
              </div>
              <div class="grid min-w-0 gap-2 md:grid-cols-2 2xl:grid-cols-3">
                <AvailableModelCard
                  v-for="model in section.supported_models"
                  :key="`${section.platform}-${model.name}`"
                  :model="model"
                  :monitor-summary="resolveMonitorSummary(model.name, section)"
                  :monitor-loading="monitorLoading"
                  :pricing-key-prefix="pricingKeyPrefix"
                  :no-pricing-label="noPricingLabel"
                  @select="selectModel(model, section)"
                />
                <div
                  v-if="!section.supported_models.length"
                  class="flex min-h-24 items-center justify-center rounded-xl border border-dashed border-gray-200 text-xs text-gray-400 dark:border-dark-700"
                >
                  {{ noModelsLabel }}
                </div>
              </div>
            </div>
          </section>
        </div>
      </article>
    </div>

    <AvailableModelDetailsDrawer
      :show="selectedModel !== null"
      :model="selectedModel?.model ?? null"
      :platform="selectedModel?.platform ?? ''"
      :groups="selectedModel?.groups ?? []"
      :user-group-rates="userGroupRates"
      :pricing-key-prefix="pricingKeyPrefix"
      :no-pricing-label="noPricingLabel"
      @close="selectedModel = null"
    />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import AvailableModelCard from './AvailableModelCard.vue'
import AvailableModelDetailsDrawer from './AvailableModelDetailsDrawer.vue'
import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserChannelPlatformSection,
  UserSupportedModel,
} from '@/api/channels'
import type { MonitorStatus, UserMonitorView } from '@/api/channelMonitor'
import type { AvailableModelMonitorSummary } from './AvailableModelCard.vue'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformAccentBarClass, platformBadgeClass, platformBorderClass } from '@/utils/platformColors'

const props = defineProps<{
  rows: UserAvailableChannel[]
  loading: boolean
  pricingKeyPrefix: string
  noPricingLabel: string
  noModelsLabel: string
  emptyLabel: string
  userGroupRates: Record<number, number>
  monitorItems: UserMonitorView[]
  monitorLoading: boolean
}>()

const { t } = useI18n()

const selectedModel = ref<{
  model: UserSupportedModel
  platform: string
  groups: UserAvailableGroup[]
} | null>(null)

type GroupBucket = {
  key: string
  groups: UserAvailableGroup[]
  icon: 'shield' | 'globe'
  labelKey: string
  tooltipKey: string
  labelClass: string
}

function groupBuckets(section: UserChannelPlatformSection): GroupBucket[] {
  return [
    {
      key: 'exclusive',
      groups: section.groups.filter((group) => group.is_exclusive),
      icon: 'shield',
      labelKey: 'availableChannels.exclusive',
      tooltipKey: 'availableChannels.exclusiveTooltip',
      labelClass: 'text-purple-600 dark:text-purple-400',
    },
    {
      key: 'public',
      groups: section.groups.filter((group) => !group.is_exclusive),
      icon: 'globe',
      labelKey: 'availableChannels.public',
      tooltipKey: 'availableChannels.publicTooltip',
      labelClass: 'text-gray-500 dark:text-gray-400',
    },
  ]
}

function channelGroupCount(channel: UserAvailableChannel): number {
  return new Set(channel.platforms.flatMap((section) => section.groups.map((group) => group.id))).size
}

function channelModelCount(channel: UserAvailableChannel): number {
  return channel.platforms.reduce((count, section) => count + section.supported_models.length, 0)
}

function selectModel(model: UserSupportedModel, section: UserChannelPlatformSection): void {
  selectedModel.value = {
    model,
    platform: section.platform,
    groups: section.groups,
  }
}

const STATUS_PRIORITY: Record<MonitorStatus, number> = {
  operational: 0,
  degraded: 1,
  failed: 2,
  error: 3,
}

function resolveMonitorSummary(
  modelName: string,
  section: UserChannelPlatformSection,
): AvailableModelMonitorSummary | null {
  const platform = section.platform.trim().toLowerCase()
  const groupNames = new Set(
    section.groups.map(group => group.name.trim().toLowerCase()),
  )
  const candidates: AvailableModelMonitorSummary[] = []

  for (const monitor of props.monitorItems) {
    if (monitor.provider.toLowerCase() !== platform) continue
    const monitorGroup = monitor.group_name.trim().toLowerCase()
    if (monitorGroup && !groupNames.has(monitorGroup)) continue

    if (monitor.primary_model === modelName) {
      candidates.push({
        status: monitor.primary_status,
        availability: monitor.availability_7d,
        latencyMs: monitor.primary_latency_ms,
        monitorCount: 1,
      })
    }

    const extra = monitor.extra_models.find(item => item.model === modelName)
    if (extra) {
      candidates.push({
        status: extra.status,
        availability: null,
        latencyMs: extra.latency_ms,
        monitorCount: 1,
      })
    }
  }

  if (candidates.length === 0) return null
  const worst = candidates.reduce((current, candidate) =>
    STATUS_PRIORITY[candidate.status] > STATUS_PRIORITY[current.status]
      ? candidate
      : current,
  )
  return {
    ...worst,
    monitorCount: candidates.length,
  }
}
</script>

<style scoped>
.channel-catalog {
  min-width: 0;
}

@media (prefers-reduced-motion: reduce) {
  .channel-catalog :deep(*) {
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
