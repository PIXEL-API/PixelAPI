<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-[1800px] space-y-4">
      <section
        v-if="channelMonitorEnabled"
        id="channel-status"
        class="scroll-mt-20 rounded-xl border border-gray-200 bg-white/90 p-3 shadow-sm backdrop-blur-sm dark:border-dark-700 dark:bg-dark-900/70 sm:p-4"
      >
        <header class="border-b border-gray-100 pb-2 dark:border-dark-700">
          <h2 class="text-base font-semibold tracking-tight text-gray-950 dark:text-white">
            {{ t('channelStatus.title') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('channelStatus.description') }}
          </p>
        </header>
        <ChannelStatusPanel
          compact
          :group-models-by-id="groupModelsById"
          :available-groups-by-id="availableGroupsById"
          :user-group-rates="userGroupRates"
          @items-change="handleMonitorItems"
          @visible-group-ids-change="handleVisibleGroupIds"
        />
      </section>

      <section
        v-if="availableChannelsEnabled && standaloneChannels.length > 0"
        class="rounded-xl border border-gray-200 bg-white/90 p-3 shadow-sm backdrop-blur-sm dark:border-dark-700 dark:bg-dark-900/70 sm:p-4"
      >
        <div class="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
          <div class="flex min-w-0 flex-1 flex-col gap-3 sm:flex-row sm:items-center">
            <SearchInput
              v-model="searchQuery"
              :placeholder="t('availableChannels.searchPlaceholder')"
              :debounce-ms="0"
              class="w-full sm:max-w-xl"
            />
            <p v-if="!loading" class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400">
              {{
                t('availableChannels.summary', {
                  channels: filteredChannels.length,
                  platforms: filteredPlatformCount,
                  models: filteredModelCount,
                })
              }}
            </p>
          </div>

          <button
            type="button"
            class="btn btn-secondary h-11 w-11 shrink-0 self-end p-0 lg:self-auto"
            :disabled="loading"
            :title="t('common.refresh', 'Refresh')"
            :aria-label="t('common.refresh', 'Refresh')"
            @click="loadChannels"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </section>

      <AvailableChannelsTable
        v-if="availableChannelsEnabled && standaloneChannels.length > 0"
        :rows="filteredChannels"
        :loading="loading"
        :user-group-rates="userGroupRates"
        :monitor-items="monitorItems"
        :monitor-loading="monitorLoading"
        pricing-key-prefix="availableChannels.pricing"
        :no-pricing-label="t('availableChannels.noPricing')"
        :no-models-label="t('availableChannels.noModels')"
        :empty-label="t('availableChannels.empty')"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import AvailableChannelsTable from '@/components/channels/AvailableChannelsTable.vue'
import ChannelStatusPanel from '@/components/user/monitor/ChannelStatusPanel.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import type { UserMonitorView } from '@/api/channelMonitor'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const monitorItems = ref<UserMonitorView[]>([])
const monitorLoaded = ref(false)
const visibleServiceGroupIds = ref<Set<number>>(new Set())
const monitorLoading = computed(
  () => channelMonitorEnabled.value && !monitorLoaded.value,
)
const channelMonitorEnabled = computed(() =>
  isFeatureFlagEnabled(FeatureFlags.channelMonitor),
)
const availableChannelsEnabled = computed(() =>
  isFeatureFlagEnabled(FeatureFlags.availableChannels),
)
const groupModelsById = computed<Record<number, UserAvailableChannel['platforms'][number]['supported_models']>>(
  () => {
    const result: Record<number, UserAvailableChannel['platforms'][number]['supported_models']> = {}
    for (const channel of channels.value) {
      for (const section of channel.platforms) {
        for (const group of section.groups) {
          result[group.id] = section.supported_models
        }
      }
    }
    return result
  },
)
const availableGroupsById = computed(() => {
  const result: Record<number, UserAvailableChannel['platforms'][number]['groups'][number]> = {}
  for (const channel of channels.value) {
    for (const section of channel.platforms) {
      for (const group of section.groups) {
        result[group.id] = group
      }
    }
  }
  return result
})
const standaloneChannels = computed<UserAvailableChannel[]>(() =>
  channels.value
    .map(channel => {
      const platforms = channel.platforms
        .map(section => {
          const groups = section.groups.filter(
            group => !visibleServiceGroupIds.value.has(group.id),
          )
          if (section.groups.length > 0 && groups.length === 0) return null
          return { ...section, groups }
        })
        .filter((section): section is UserAvailableChannel['platforms'][number] => section !== null)
      return platforms.length > 0 ? { ...channel, platforms } : null
    })
    .filter((channel): channel is UserAvailableChannel => channel !== null),
)

/**
 * 搜索过滤：
 * - 命中渠道名/描述 → 整个渠道（所有 platforms）都保留
 * - 否则按 platform/group/model 维度在 sections 里过滤，保留有匹配的 section
 * - 所有 sections 都不匹配时，渠道本身被过滤掉
 */
const filteredChannels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return standaloneChannels.value
  return standaloneChannels.value
    .map((ch) => {
      const nameHit = ch.name.toLowerCase().includes(q)
      const descHit = (ch.description || '').toLowerCase().includes(q)
      if (nameHit || descHit) return ch
      const matchingSections = ch.platforms.filter(
        (p) =>
          p.platform.toLowerCase().includes(q) ||
          p.groups.some((g) => g.name.toLowerCase().includes(q)) ||
          p.supported_models.some((m) => m.name.toLowerCase().includes(q)),
      )
      if (matchingSections.length === 0) return null
      return { ...ch, platforms: matchingSections }
    })
    .filter((ch): ch is UserAvailableChannel => ch !== null)
})

const filteredPlatformCount = computed(() =>
  filteredChannels.value.reduce((count, channel) => count + channel.platforms.length, 0),
)

const filteredModelCount = computed(() =>
  filteredChannels.value.reduce(
    (count, channel) =>
      count +
      channel.platforms.reduce(
        (platformCount, platform) => platformCount + platform.supported_models.length,
        0,
      ),
    0,
  ),
)

async function loadChannels() {
  loading.value = true
  try {
    // 渠道列表和用户专属倍率并发拉取。专属倍率失败不阻塞渠道展示——
    // 失败时只是无法渲染专属倍率角标，降级为仅显示默认倍率。
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

function handleMonitorItems(items: UserMonitorView[]): void {
  monitorItems.value = items
  monitorLoaded.value = true
}

function handleVisibleGroupIds(groupIds: number[]): void {
  visibleServiceGroupIds.value = new Set(groupIds)
}

onMounted(() => {
  if (availableChannelsEnabled.value) {
    void loadChannels()
  }
})
</script>
