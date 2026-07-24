<template>
  <BaseDialog
    :show="show"
    :title="t('accountShare.roomAccounts.title', { name: roomDisplayName })"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <div
        v-if="listing"
        class="grid grid-cols-1 gap-3 rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800 sm:grid-cols-3"
      >
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.platform') }}</span>
          <strong>{{ listing.platform }}</strong>
        </div>
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.level') }}</span>
          <strong>{{ listing.account_level || 'unknown' }}</strong>
        </div>
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.health') }}</span>
          <strong>{{ healthyCount }}/{{ accounts.length }}</strong>
        </div>
      </div>

      <div
        v-if="loading"
        class="flex min-h-32 items-center justify-center gap-2 rounded-2xl border border-gray-200 text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
      >
        <Icon name="refresh" size="sm" class="animate-spin" />
        {{ t('common.loading') }}
      </div>

      <div
        v-else-if="errorMessage"
        class="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
        role="alert"
      >
        {{ errorMessage }}
      </div>

      <div
        v-else-if="accounts.length === 0"
        class="rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
      >
        {{ t('accountShare.roomAccounts.empty') }}
      </div>

      <div v-else class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <article
          v-for="account in accounts"
          :key="account.account_id"
          class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="flex min-w-0 items-start justify-between gap-3">
            <div class="min-w-0">
              <h4 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ account.account_name }}
              </h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">
                #{{ account.account_id }} · {{ account.account_level }}
              </p>
            </div>
            <span :class="accountHealthBadgeClass(account)">
              {{ accountHealthLabel(account) }}
            </span>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-300">
                {{ t('accountShare.roomAccounts.concurrency') }}
              </dt>
              <dd class="mt-1 font-semibold text-gray-900 dark:text-white">
                {{ account.current_concurrency }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-300">
                {{ t('accountShare.roomAccounts.priority') }}
              </dt>
              <dd class="mt-1 font-semibold text-gray-900 dark:text-white">
                {{ account.priority }}
              </dd>
            </div>
          </dl>
          <p v-if="account.last_used_at" class="mt-3 text-xs text-gray-500 dark:text-dark-300">
            {{ t('accountShare.roomAccounts.lastUsed', { time: formatDateTime(account.last_used_at) }) }}
          </p>
        </article>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button type="button" class="btn btn-secondary min-h-11" @click="emit('close')">
          {{ t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-primary min-h-11"
          :disabled="loading"
          @click="loadAccounts"
        >
          <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  accountShareAPI,
  type AccountShareListing,
  type AccountShareRoomAccount
} from '@/api/accountShare'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  show: boolean
  listing: AccountShareListing | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()

const { t } = useI18n()
const accounts = ref<AccountShareRoomAccount[]>([])
const loading = ref(false)
const errorMessage = ref('')
let requestVersion = 0
let loadingListingID: number | null = null

const roomDisplayName = computed(() => (
  props.listing?.room_name
  || props.listing?.account_name
  || (props.listing ? `#${props.listing.id}` : '')
))

const healthyCount = computed(() => (
  accounts.value.filter((account) => isAccountHealthy(account)).length
))

watch(
  () => [props.show, props.listing?.id] as const,
  ([show]) => {
    requestVersion += 1
    loading.value = false
    loadingListingID = null
    accounts.value = []
    errorMessage.value = ''
    if (show && props.listing) void loadAccounts()
  },
  { immediate: true }
)

function isAccountHealthy(account: AccountShareRoomAccount): boolean {
  return account.status === 'active'
    && account.schedulable
    && account.placement_state === 'active'
}

function accountHealthLabel(account: AccountShareRoomAccount): string {
  return isAccountHealthy(account)
    ? t('accountShare.roomAccounts.healthy')
    : t('accountShare.roomAccounts.unavailable')
}

function accountHealthBadgeClass(account: AccountShareRoomAccount): string {
  const base = 'shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold'
  return isAccountHealthy(account)
    ? `${base} bg-emerald-100 text-emerald-700 dark:bg-emerald-900/35 dark:text-emerald-300`
    : `${base} bg-amber-100 text-amber-700 dark:bg-amber-900/35 dark:text-amber-300`
}

async function loadAccounts(): Promise<void> {
  const listingID = props.listing?.id
  if (!listingID || (loading.value && loadingListingID === listingID)) return

  const currentVersion = ++requestVersion
  loading.value = true
  loadingListingID = listingID
  errorMessage.value = ''
  try {
    const result = await accountShareAPI.listRoomAccounts(listingID)
    if (currentVersion !== requestVersion) return
    accounts.value = result
  } catch (error) {
    if (currentVersion !== requestVersion) return
    errorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.loadFailed')
    )
  } finally {
    if (currentVersion === requestVersion) {
      loading.value = false
      loadingListingID = null
    }
  }
}
</script>

<style scoped>
.room-summary-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
}

.room-summary-cell span {
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.room-summary-cell strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.875rem;
  color: rgb(17 24 39);
}

:global(.dark) .room-summary-cell span {
  color: rgb(156 163 175);
}

:global(.dark) .room-summary-cell strong {
  color: rgb(255 255 255);
}
</style>
