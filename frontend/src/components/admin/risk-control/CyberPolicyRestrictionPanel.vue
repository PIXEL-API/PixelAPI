<template>
  <section class="card" data-testid="cyber-policy-restriction-panel">
    <div class="flex flex-col gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6 lg:flex-row lg:items-start lg:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-red-50 text-red-600 dark:bg-red-900/20 dark:text-red-300">
          <Icon name="shield" size="md" />
        </span>
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.riskControl.cyberRestriction.title') }}
          </h2>
          <p class="mt-1 max-w-4xl text-sm leading-6 text-gray-500 dark:text-gray-400">
            {{ t('admin.riskControl.cyberRestriction.description') }}
          </p>
        </div>
      </div>
    </div>

    <div class="space-y-4 p-4 sm:p-6">
      <form class="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end" @submit.prevent="queryRestriction">
        <div ref="userSearchContainer" class="relative min-w-0">
          <label for="cyber-restriction-user-search" class="input-label">
            {{ t('admin.riskControl.cyberRestriction.user') }}
          </label>
          <div class="relative">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              id="cyber-restriction-user-search"
              v-model="userQuery"
              data-testid="cyber-restriction-user-search"
              type="search"
              autocomplete="off"
              role="combobox"
              aria-autocomplete="list"
              aria-controls="cyber-restriction-user-results"
              :aria-expanded="userDropdownOpen && Boolean(userQuery.trim())"
              :aria-activedescendant="activeUserOptionId"
              class="input min-h-11 w-full pl-10"
              :placeholder="t('admin.riskControl.cyberRestriction.userSearchPlaceholder')"
              @input="debounceUserSearch"
              @focus="openUserDropdown"
              @keydown="handleUserSearchKeydown"
            />
          </div>
          <div
            v-if="userDropdownOpen && userQuery.trim()"
            id="cyber-restriction-user-results"
            role="listbox"
            class="absolute left-0 right-0 z-50 mt-1 max-h-64 overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
          >
            <div v-if="userSearchLoading" class="flex min-h-11 items-center px-4 py-2 text-sm text-gray-500 dark:text-gray-400">
              {{ t('common.loading') }}
            </div>
            <div v-else-if="userSearchFailed" class="flex min-h-11 items-center px-4 py-2 text-sm text-red-600 dark:text-red-400">
              {{ t('admin.riskControl.cyberRestriction.userSearchFailed') }}
            </div>
            <div v-else-if="userResults.length === 0" class="flex min-h-11 items-center px-4 py-2 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.riskControl.cyberRestriction.userSearchEmpty') }}
            </div>
            <template v-else>
              <button
                v-for="(user, index) in userResults"
                :id="userOptionId(index)"
                :key="user.id"
                type="button"
                role="option"
                class="flex min-h-11 w-full min-w-0 items-center justify-between gap-3 px-4 py-2 text-left text-sm transition-colors hover:bg-gray-100 focus-visible:bg-gray-100 focus-visible:outline-none dark:hover:bg-dark-600 dark:focus-visible:bg-dark-600"
                :class="activeUserIndex === index ? 'bg-gray-100 dark:bg-dark-600' : ''"
                :aria-selected="selectedUser?.id === user.id"
                @mouseenter="activeUserIndex = index"
                @click="selectUser(user)"
              >
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-medium text-gray-900 dark:text-white">{{ user.username || user.email }}</span>
                  <span class="block truncate text-xs text-gray-400">{{ user.email }}</span>
                </span>
                <span class="flex-shrink-0 text-xs text-gray-400">UID {{ user.id }}</span>
              </button>
            </template>
          </div>
          <p v-if="selectedUser" class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400" :title="selectedUserSummary">
            {{ selectedUserSummary }}
          </p>
        </div>
        <div>
          <label id="cyber-restriction-group-label" class="input-label">
            {{ t('admin.riskControl.cyberRestriction.group') }}
          </label>
          <Select
            v-model="groupID"
            data-testid="cyber-restriction-group-select"
            :options="groupOptions"
            searchable
            :placeholder="t('admin.riskControl.cyberRestriction.groupSearchPlaceholder')"
            :search-placeholder="t('admin.riskControl.cyberRestriction.groupSearchPlaceholder')"
            :empty-text="t('admin.riskControl.cyberRestriction.groupSearchEmpty')"
            aria-labelledby="cyber-restriction-group-label"
            @change="clearResult"
          />
        </div>
        <button
          type="submit"
          data-testid="cyber-restriction-query"
          class="btn btn-primary inline-flex min-h-11 w-full items-center justify-center gap-2 md:w-auto"
          :disabled="!inputValid || querying || clearing"
          :aria-busy="querying"
        >
          <Icon name="search" size="sm" :class="querying ? 'animate-pulse' : ''" />
          {{ querying ? t('common.loading') : t('admin.riskControl.cyberRestriction.query') }}
        </button>
      </form>

      <div
        v-if="restriction"
        data-testid="cyber-restriction-result"
        class="rounded-xl border p-4"
        :class="restriction.blocked
          ? 'border-red-200 bg-red-50 dark:border-red-900/70 dark:bg-red-950/20'
          : 'border-emerald-200 bg-emerald-50 dark:border-emerald-900/70 dark:bg-emerald-950/20'"
        aria-live="polite"
      >
        <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <Icon
                :name="restriction.blocked ? 'shield' : 'checkCircle'"
                size="sm"
                :class="restriction.blocked ? 'text-red-600 dark:text-red-300' : 'text-emerald-600 dark:text-emerald-300'"
              />
              <p
                class="font-semibold"
                :class="restriction.blocked ? 'text-red-800 dark:text-red-200' : 'text-emerald-800 dark:text-emerald-200'"
              >
                {{ restriction.blocked
                  ? t('admin.riskControl.cyberRestriction.blocked')
                  : t('admin.riskControl.cyberRestriction.notBlocked') }}
              </p>
            </div>
            <dl class="mt-3 grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
              <div class="flex min-w-0 gap-2">
                <dt class="flex-shrink-0 text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRestriction.userId') }}</dt>
                <dd class="break-all font-medium text-gray-800 dark:text-gray-100">{{ restriction.user_id }}</dd>
              </div>
              <div class="flex min-w-0 gap-2">
                <dt class="flex-shrink-0 text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRestriction.groupId') }}</dt>
                <dd class="break-all font-medium text-gray-800 dark:text-gray-100">{{ restriction.group_id }}</dd>
              </div>
              <div v-if="restriction.blocked" class="flex min-w-0 gap-2 sm:col-span-2">
                <dt class="flex-shrink-0 text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRestriction.blockedUntil') }}</dt>
                <dd class="break-words font-medium text-gray-800 dark:text-gray-100">{{ blockedUntilText }}</dd>
              </div>
            </dl>
          </div>

          <button
            v-if="restriction.blocked"
            type="button"
            data-testid="cyber-restriction-clear"
            class="inline-flex min-h-11 w-full flex-shrink-0 items-center justify-center gap-2 rounded-lg border border-red-200 bg-white px-4 py-2 text-sm font-medium text-red-700 transition-colors hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-900/70 dark:bg-dark-800 dark:text-red-300 dark:hover:bg-red-950/30 sm:w-auto"
            :disabled="querying || clearing"
            :aria-busy="clearing"
            @click="clearRestriction"
          >
            <Icon name="checkCircle" size="sm" :class="clearing ? 'animate-spin' : ''" />
            {{ clearing ? t('common.processing') : t('admin.riskControl.cyberRestriction.clear') }}
          </button>
        </div>
      </div>

      <p v-else class="text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ t('admin.riskControl.cyberRestriction.hint') }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import { adminAPI } from '@/api/admin'
import type { CyberPolicyRestriction } from '@/api/admin/riskControl'
import type { AdminGroup, AdminUser, SelectOption } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const props = withDefaults(defineProps<{ groups?: AdminGroup[] }>(), {
  groups: () => [],
})

const userSearchContainer = ref<HTMLElement | null>(null)
const userQuery = ref('')
const selectedUser = ref<AdminUser | null>(null)
const userResults = ref<AdminUser[]>([])
const userSearchLoading = ref(false)
const userSearchFailed = ref(false)
const userDropdownOpen = ref(false)
const activeUserIndex = ref(-1)
const groupID = ref<number | null>(null)
const restriction = ref<CyberPolicyRestriction | null>(null)
const querying = ref(false)
const clearing = ref(false)
let userSearchTimer: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0
let userSearchAbortController: AbortController | null = null

const openAIGroups = computed(() => props.groups.filter((group) => (
  group.platform === 'openai' && group.status === 'active'
)))

const groupOptions = computed<SelectOption[]>(() => openAIGroups.value.map((group) => ({
  value: group.id,
  label: `${group.name} · ID ${group.id}`,
})))

const userID = computed(() => selectedUser.value?.id ?? 0)

const selectedUserSummary = computed(() => {
  if (!selectedUser.value) return ''
  const name = selectedUser.value.username || selectedUser.value.email
  return `${name} · ${selectedUser.value.email} · UID ${selectedUser.value.id}`
})

const activeUserOptionId = computed(() => {
  if (!userDropdownOpen.value || activeUserIndex.value < 0) return undefined
  return userOptionId(activeUserIndex.value)
})

const inputValid = computed(() => (
  Number.isInteger(userID.value)
  && userID.value > 0
  && Number.isInteger(Number(groupID.value))
  && Number(groupID.value) > 0
))

const blockedUntilText = computed(() => {
  if (!restriction.value?.blocked_until) return '-'
  return formatDateTime(restriction.value.blocked_until) || restriction.value.blocked_until
})

function clearResult() {
  restriction.value = null
}

function userOptionId(index: number): string {
  return `cyber-restriction-user-option-${index}`
}

function clearPendingUserSearch(): void {
  if (userSearchTimer) {
    clearTimeout(userSearchTimer)
    userSearchTimer = null
  }
  userSearchAbortController?.abort()
  userSearchAbortController = null
  userSearchSequence += 1
}

function debounceUserSearch(): void {
  clearResult()
  if (selectedUser.value) {
    selectedUser.value = null
  }
  clearPendingUserSearch()
  const query = userQuery.value.trim()
  userDropdownOpen.value = true
  userSearchFailed.value = false
  activeUserIndex.value = -1
  if (!query) {
    userResults.value = []
    userSearchLoading.value = false
    return
  }

  const sequence = userSearchSequence
  userSearchTimer = setTimeout(async () => {
    const controller = new AbortController()
    userSearchAbortController = controller
    userSearchLoading.value = true
    try {
      const result = await adminAPI.users.list(
        1,
        20,
        { search: query },
        { signal: controller.signal }
      )
      if (sequence !== userSearchSequence || controller.signal.aborted) return
      userResults.value = result.items || []
      activeUserIndex.value = userResults.value.length > 0 ? 0 : -1
    } catch (error: unknown) {
      const candidate = error as { name?: string; code?: string }
      const canceled = candidate?.name === 'AbortError'
        || candidate?.name === 'CanceledError'
        || candidate?.code === 'ERR_CANCELED'
      if (!canceled && sequence === userSearchSequence) {
        userResults.value = []
        userSearchFailed.value = true
      }
    } finally {
      if (sequence === userSearchSequence) {
        userSearchLoading.value = false
        userSearchAbortController = null
      }
    }
  }, 300)
}

function openUserDropdown(): void {
  userDropdownOpen.value = true
  if (userQuery.value.trim() && !selectedUser.value && userResults.value.length === 0) {
    debounceUserSearch()
  }
}

function handleUserSearchKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    userDropdownOpen.value = false
    return
  }
  if (!userDropdownOpen.value || userResults.value.length === 0) return
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    activeUserIndex.value = (activeUserIndex.value + 1) % userResults.value.length
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    activeUserIndex.value = (activeUserIndex.value - 1 + userResults.value.length) % userResults.value.length
  } else if (event.key === 'Enter' && activeUserIndex.value >= 0) {
    event.preventDefault()
    const user = userResults.value[activeUserIndex.value]
    if (user) selectUser(user)
  }
}

function selectUser(user: AdminUser): void {
  clearPendingUserSearch()
  selectedUser.value = user
  userQuery.value = user.username || user.email
  userResults.value = []
  userSearchLoading.value = false
  userSearchFailed.value = false
  userDropdownOpen.value = false
  activeUserIndex.value = -1
  clearResult()
}

function handleDocumentClick(event: MouseEvent): void {
  const target = event.target as Node | null
  if (target && !userSearchContainer.value?.contains(target)) {
    userDropdownOpen.value = false
  }
}

async function queryRestriction() {
  if (!inputValid.value || querying.value || clearing.value) return
  querying.value = true
  restriction.value = null
  try {
    restriction.value = await adminAPI.riskControl.getCyberPolicyRestriction(
      userID.value,
      Number(groupID.value)
    )
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.cyberRestriction.queryFailed')))
  } finally {
    querying.value = false
  }
}

async function clearRestriction() {
  const current = restriction.value
  if (!current?.blocked || clearing.value || querying.value) return
  const confirmed = window.confirm(t('admin.riskControl.cyberRestriction.clearConfirm', {
    userId: current.user_id,
    groupId: current.group_id,
  }))
  if (!confirmed) return

  clearing.value = true
  try {
    await adminAPI.riskControl.clearCyberPolicyRestriction(current.user_id, current.group_id)
    appStore.showSuccess(t('admin.riskControl.cyberRestriction.clearSuccess'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.cyberRestriction.clearFailed')))
    return
  } finally {
    clearing.value = false
  }

  querying.value = true
  try {
    restriction.value = await adminAPI.riskControl.getCyberPolicyRestriction(current.user_id, current.group_id)
  } catch (err: unknown) {
    restriction.value = null
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.cyberRestriction.queryFailed')))
  } finally {
    querying.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleDocumentClick)
})

onUnmounted(() => {
  clearPendingUserSearch()
  document.removeEventListener('click', handleDocumentClick)
})
</script>
