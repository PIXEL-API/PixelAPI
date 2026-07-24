<template>
  <BaseDialog
    :show="show"
    :title="t('userAccounts.externalPlacement.title', '切换外投位置')"
    width="normal"
    :close-on-escape="!submitting"
    @close="handleClose"
  >
    <div class="space-y-5">
      <div
        v-if="account"
        class="flex min-w-0 items-center gap-3 rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800"
      >
        <div
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
        >
          <Icon name="swap" size="lg" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="truncate text-base font-semibold text-gray-900 dark:text-white">
            {{ account.name }}
          </p>
          <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-300">
            {{ account.platform }} · {{ normalizedAccountLevel }}
          </p>
        </div>
        <span
          class="shrink-0 rounded-full bg-gray-200 px-2.5 py-1 text-xs font-semibold text-gray-700 dark:bg-dark-600 dark:text-dark-200"
        >
          {{ currentPlacementLabel }}
        </span>
      </div>

      <fieldset :disabled="submitting || !account" class="space-y-3">
        <legend class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('userAccounts.externalPlacement.destination', '选择目标位置') }}
        </legend>

        <label
          v-for="option in targetOptions"
          :key="option.value"
          :class="[
            'flex min-h-11 cursor-pointer gap-3 rounded-2xl border p-4 transition-colors',
            selectedTarget === option.value
              ? 'border-primary-500 bg-primary-50 ring-1 ring-primary-500/20 dark:border-primary-500 dark:bg-primary-950/25'
              : 'border-gray-200 bg-white hover:border-gray-300 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500 dark:hover:bg-dark-700',
            option.disabled ? 'cursor-not-allowed opacity-55' : ''
          ]"
        >
          <input
            v-model="selectedTarget"
            class="mt-1 h-4 w-4 shrink-0 border-gray-300 text-primary-600 focus:ring-primary-500"
            type="radio"
            name="external-placement-target"
            :value="option.value"
            :disabled="option.disabled"
            :data-testid="`placement-target-${option.value}`"
          />
          <span class="min-w-0">
            <span class="block text-sm font-semibold text-gray-900 dark:text-white">
              {{ option.label }}
            </span>
            <span class="mt-1 block text-sm leading-6 text-gray-500 dark:text-dark-300">
              {{ option.description }}
            </span>
          </span>
        </label>
      </fieldset>

      <section
        v-if="selectedTarget === 'room'"
        class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
      >
        <div class="flex items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('userAccounts.externalPlacement.roomLabel', '选择账号房间') }}
            </h4>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
              {{ t('userAccounts.externalPlacement.roomRule', '仅显示同号主、同平台、同等级的房间。') }}
            </p>
          </div>
          <button
            type="button"
            class="inline-flex min-h-11 min-w-11 items-center justify-center rounded-xl text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
            :disabled="loadingRooms || submitting"
            :aria-label="t('common.refresh')"
            data-testid="refresh-compatible-rooms"
            @click="loadCompatibleRooms"
          >
            <Icon :class="{ 'animate-spin': loadingRooms }" name="refresh" size="md" :stroke-width="2" />
          </button>
        </div>

        <div
          v-if="loadingRooms"
          class="mt-4 flex items-center gap-2 rounded-xl bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-dark-700 dark:text-dark-200"
        >
          <Icon class="animate-spin" name="refresh" size="sm" :stroke-width="2" />
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="roomLoadError"
          class="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm leading-6 text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
          role="alert"
        >
          {{ roomLoadError }}
        </div>

        <div
          v-else-if="compatibleRooms.length === 0"
          class="mt-4 rounded-xl bg-gray-50 px-4 py-3 text-sm leading-6 text-gray-600 dark:bg-dark-700 dark:text-dark-200"
        >
          {{ t('userAccounts.externalPlacement.noCompatibleRooms', '暂无可加入的同平台同等级房间。') }}
        </div>

        <div v-else class="mt-4 max-h-56 space-y-2 overflow-y-auto pr-1">
          <label
            v-for="room in compatibleRooms"
            :key="room.id"
            :class="[
              'flex min-h-11 cursor-pointer items-center gap-3 rounded-xl border px-3 py-3 transition-colors',
              selectedRoomID === room.id
                ? 'border-primary-500 bg-primary-50 dark:border-primary-500 dark:bg-primary-950/25'
                : 'border-gray-200 hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700'
            ]"
          >
            <input
              v-model="selectedRoomID"
              class="h-4 w-4 shrink-0 border-gray-300 text-primary-600 focus:ring-primary-500"
              type="radio"
              name="external-placement-room"
              :value="room.id"
              :disabled="submitting"
              :data-testid="`compatible-room-${room.id}`"
            />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ room.room_name || room.account_name || `#${room.id}` }}
              </span>
              <span class="mt-0.5 block text-xs text-gray-500 dark:text-dark-300">
                {{ formatRoomCapacity(room) }}
              </span>
            </span>
            <span class="shrink-0 rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-600 dark:text-dark-200">
              {{ room.status }}
            </span>
          </label>
        </div>
      </section>

      <div
        v-if="submitError"
        class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm leading-6 text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
        role="alert"
        data-testid="placement-submit-error"
      >
        {{ submitError }}
      </div>

      <div
        class="flex gap-3 rounded-2xl border border-sky-200 bg-sky-50 p-4 text-sm leading-6 text-sky-900 dark:border-sky-900/70 dark:bg-sky-950/25 dark:text-sky-200"
      >
        <Icon class="mt-0.5 shrink-0" name="infoCircle" size="md" :stroke-width="2" />
        <p>
          {{
            t(
              'userAccounts.externalPlacement.credentialsPreserved',
              '转换不会删除账号或重新登录，账号凭证、代理和私有自用能力保持不变。'
            )
          }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button
          type="button"
          class="min-h-11 w-full rounded-xl border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-700 sm:w-auto"
          :disabled="submitting"
          @click="handleClose"
        >
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-primary-600 px-4 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-primary-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto"
          :disabled="!canSubmit"
          data-testid="convert-external-placement"
          @click="handleSubmit"
        >
          <Icon
            :class="{ 'animate-spin': submitting }"
            :name="submitting ? 'refresh' : 'swap'"
            size="sm"
            :stroke-width="2"
          />
          {{
            submitting
              ? t('userAccounts.externalPlacement.converting', '转换中...')
              : t('userAccounts.externalPlacement.confirm', '确认转换')
          }}
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
  type ConvertAccountExternalPlacementResponse
} from '@/api/accountShare'
import { extractApiErrorMessage } from '@/utils/apiError'
import type {
  Account,
  AccountExternalPlacementTarget,
  AccountLevel
} from '@/types'

interface TargetOption {
  value: AccountExternalPlacementTarget
  label: string
  description: string
  disabled: boolean
}

const props = defineProps<{
  show: boolean
  account: Account | null
  ownerUserId: number
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'converted', result: ConvertAccountExternalPlacementResponse): void
}>()

const { t } = useI18n()

const selectedTarget = ref<AccountExternalPlacementTarget>('private')
const selectedRoomID = ref<number | null>(null)
const compatibleRooms = ref<AccountShareListing[]>([])
const loadingRooms = ref(false)
const roomLoadError = ref('')
const submitting = ref(false)
const submitError = ref('')
let roomRequestVersion = 0
let pendingIntentSignature = ''
let pendingIdempotencyKey = ''

const normalizedAccountLevel = computed<AccountLevel>(() => {
  const level = props.account?.account_level
  return typeof level === 'string' && level.trim()
    ? level.trim().toLowerCase() as AccountLevel
    : 'unknown'
})

const supportsAccountShareRoom = computed(() => (
  props.account?.platform === 'openai' || props.account?.platform === 'anthropic'
))

const currentTarget = computed<AccountExternalPlacementTarget>(() => {
  const account = props.account
  const target = account?.external_placement?.target
  if (target) return target
  if (
    Number(account?.account_share_mode_listing_id || 0) > 0
    || account?.extra?.account_share_mode === true
    || account?.extra?.account_share_mode === 'true'
  ) {
    return 'room'
  }
  return account?.share_mode === 'public' ? 'public_pool' : 'private'
})

const currentPlacementLabel = computed(() => {
  if (currentTarget.value === 'public_pool') {
    return t('userAccounts.externalPlacement.publicPoolShort', '公共号池')
  }
  if (currentTarget.value === 'room') {
    return props.account?.external_placement?.room_name
      || t('userAccounts.externalPlacement.roomShort', '账号房间')
  }
  return t('userAccounts.externalPlacement.privateShort', '仅本人')
})

const targetOptions = computed<TargetOption[]>(() => [
  {
    value: 'private',
    label: t('userAccounts.externalPlacement.privateTitle', '退出外投（仅本人）'),
    description: t(
      'userAccounts.externalPlacement.privateDescription',
      '移出公共号池或账号房间，仅保留号主自己的私有调用。'
    ),
    disabled: false
  },
  {
    value: 'public_pool',
    label: t('userAccounts.externalPlacement.publicPoolTitle', '公共号池'),
    description: t(
      'userAccounts.externalPlacement.publicPoolDescription',
      '账号可被公共调度，同时号主仍可按全局自用抽成调用。'
    ),
    disabled: false
  },
  {
    value: 'room',
    label: t('userAccounts.externalPlacement.roomTitle', '账号房间'),
    description: t(
      'userAccounts.externalPlacement.roomDescription',
      '加入同号主、同平台、同账号等级的房间。'
    ),
    disabled: !supportsAccountShareRoom.value || normalizedAccountLevel.value === 'unknown'
  }
])

const currentIntentSignature = computed(() => (
  selectedTarget.value === 'room'
    ? `${selectedTarget.value}:${selectedRoomID.value ?? ''}`
    : selectedTarget.value
))

const canSubmit = computed(() => {
  if (!props.account || submitting.value) return false
  if (selectedTarget.value === 'room') {
    return selectedRoomID.value !== null && !loadingRooms.value && !roomLoadError.value
  }
  return true
})

watch(
  () => [props.show, props.account?.id] as const,
  ([show]) => {
    if (!show) {
      roomRequestVersion += 1
      return
    }
    resetDialogState()
  },
  { immediate: true }
)

watch(selectedTarget, (target) => {
  submitError.value = ''
  clearPendingIdempotencyKey()
  if (target === 'room' && compatibleRooms.value.length === 0 && !loadingRooms.value) {
    void loadCompatibleRooms()
  }
})

watch(selectedRoomID, () => {
  submitError.value = ''
  clearPendingIdempotencyKey()
})

function resetDialogState(): void {
  roomRequestVersion += 1
  selectedTarget.value = currentTarget.value
  selectedRoomID.value = props.account?.external_placement?.room_id
    ?? props.account?.account_share_mode_listing_id
    ?? null
  compatibleRooms.value = []
  loadingRooms.value = false
  roomLoadError.value = ''
  submitting.value = false
  submitError.value = ''
  clearPendingIdempotencyKey()
  if (selectedTarget.value === 'room') {
    void loadCompatibleRooms()
  }
}

function clearPendingIdempotencyKey(): void {
  pendingIntentSignature = ''
  pendingIdempotencyKey = ''
}

function createIdempotencyKey(accountID: number): string {
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error(
      t(
        'userAccounts.externalPlacement.uuidUnavailable',
        '当前浏览器无法生成安全的幂等键，请升级浏览器后重试。'
      )
    )
  }
  return `account-placement-${accountID}-${requestID}`
}

function getIdempotencyKey(accountID: number): string {
  const signature = currentIntentSignature.value
  if (pendingIdempotencyKey && pendingIntentSignature === signature) {
    return pendingIdempotencyKey
  }
  pendingIntentSignature = signature
  pendingIdempotencyKey = createIdempotencyKey(accountID)
  return pendingIdempotencyKey
}

function formatRoomCapacity(room: AccountShareListing): string {
  const healthy = room.healthy_account_count ?? 0
  const total = room.account_count ?? 0
  const key = 'userAccounts.externalPlacement.roomCapacity'
  const translated = t(key, { healthy, total })
  return translated === key ? `健康账号 ${healthy}/${total}` : translated
}

async function loadCompatibleRooms(): Promise<void> {
  const account = props.account
  if (!account || selectedTarget.value !== 'room') return

  if (account.platform !== 'openai' && account.platform !== 'anthropic') {
    compatibleRooms.value = []
    roomLoadError.value = t(
      'userAccounts.externalPlacement.unsupportedPlatform',
      '当前账号平台暂不支持账号房间。'
    )
    return
  }
  if (normalizedAccountLevel.value === 'unknown') {
    compatibleRooms.value = []
    roomLoadError.value = t(
      'userAccounts.externalPlacement.unknownLevel',
      '账号等级未知，确认等级后才能加入房间。'
    )
    return
  }
  if (!Number.isInteger(props.ownerUserId) || props.ownerUserId <= 0) {
    compatibleRooms.value = []
    roomLoadError.value = t(
      'userAccounts.externalPlacement.ownerRequired',
      '无法确认账号号主，不能加载可加入房间。'
    )
    return
  }

  const requestVersion = ++roomRequestVersion
  const platform = account.platform
  loadingRooms.value = true
  roomLoadError.value = ''

  try {
    const firstPage = await accountShareAPI.listListings(1, 100, {
      tab: 'mine',
      owner_user_id: props.ownerUserId,
      platform,
      account_level: normalizedAccountLevel.value
    })
    if (requestVersion !== roomRequestVersion) return

    const pages = [firstPage]
    for (let page = 2; page <= firstPage.pages; page += 1) {
      const nextPage = await accountShareAPI.listListings(page, 100, {
        tab: 'mine',
        owner_user_id: props.ownerUserId,
        platform,
        account_level: normalizedAccountLevel.value
      })
      if (requestVersion !== roomRequestVersion) return
      pages.push(nextPage)
    }

    compatibleRooms.value = pages
      .flatMap((page) => page.items)
      .filter((room) => (
        room.owner_user_id === props.ownerUserId
        && room.platform.trim().toLowerCase() === platform
        && String(room.account_level || 'unknown').trim().toLowerCase() === normalizedAccountLevel.value
      ))
      .sort((left, right) => {
        const nameOrder = (left.room_name || left.account_name || '').localeCompare(
          right.room_name || right.account_name || '',
          'zh-CN'
        )
        return nameOrder || left.id - right.id
      })

    if (
      selectedRoomID.value !== null
      && !compatibleRooms.value.some((room) => room.id === selectedRoomID.value)
    ) {
      selectedRoomID.value = null
    }
  } catch (error) {
    if (requestVersion !== roomRequestVersion) return
    compatibleRooms.value = []
    roomLoadError.value = extractApiErrorMessage(
      error,
      t('userAccounts.externalPlacement.roomLoadFailed', '加载可加入房间失败，请重试。')
    )
  } finally {
    if (requestVersion === roomRequestVersion) {
      loadingRooms.value = false
    }
  }
}

async function handleSubmit(): Promise<void> {
  const account = props.account
  if (!account || submitting.value || !canSubmit.value) return

  submitting.value = true
  submitError.value = ''

  try {
    const result = await accountShareAPI.convertAccountExternalPlacement(account.id, {
      target: selectedTarget.value,
      ...(selectedTarget.value === 'room' && selectedRoomID.value !== null
        ? { room_id: selectedRoomID.value }
        : {}),
      idempotency_key: getIdempotencyKey(account.id)
    })
    clearPendingIdempotencyKey()
    emit('converted', result)
    emit('close')
  } catch (error) {
    submitError.value = extractApiErrorMessage(
      error,
      t('userAccounts.externalPlacement.convertFailed', '外投位置转换失败，请重试。')
    )
  } finally {
    submitting.value = false
  }
}

function handleClose(): void {
  if (!submitting.value) emit('close')
}
</script>
