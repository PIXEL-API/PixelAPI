<template>
  <BaseDialog
    :show="show"
    :title="t('userAccounts.moderationSettings')"
    width="wide"
    @close="close"
  >
    <div v-if="account" class="space-y-5">
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800">
        <div class="min-w-0">
          <div class="truncate font-medium text-gray-900 dark:text-gray-100">{{ account.name }}</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ account.platform }} / {{ account.type }}</div>
        </div>
        <span
          class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium"
          :class="form.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'"
        >
          {{ form.enabled ? t('common.enabled') : t('common.disabled') }}
        </span>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <label class="flex items-center justify-between rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-600">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('userAccounts.moderationEnable') }}</span>
          <button
            type="button"
            class="relative inline-flex h-6 w-11 rounded-full transition-colors"
            :class="form.enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'"
            @click="form.enabled = !form.enabled"
          >
            <span
              class="mt-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform"
              :class="form.enabled ? 'translate-x-5' : 'translate-x-0.5'"
            />
          </button>
        </label>

        <div class="space-y-1.5">
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationMode') }}</label>
          <Select v-model="form.mode" :options="modeOptions" />
        </div>

        <div class="space-y-1.5">
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationProvider') }}</label>
          <Select v-model="form.provider" :options="providerOptions" />
        </div>

        <div class="space-y-1.5">
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationModel') }}</label>
          <input :value="form.model" class="input bg-gray-50 text-gray-500 dark:bg-dark-800 dark:text-dark-300" disabled />
        </div>

        <div class="space-y-1.5 md:col-span-2">
          <div class="flex items-center justify-between gap-3">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationApiKey') }}</label>
            <span v-if="config?.api_key_configured" class="text-xs text-gray-500 dark:text-dark-300">
              {{ t('userAccounts.moderationApiKeyConfigured', { key: config.api_key_masked || '********' }) }}
            </span>
          </div>
          <input
            v-model.trim="form.api_key"
            class="input"
            type="password"
            autocomplete="off"
            :placeholder="config?.api_key_configured ? t('userAccounts.moderationApiKeyKeep') : 'sk-...'"
          />
        </div>

        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationSampleRate') }}</label>
            <span class="text-sm font-medium text-gray-600 dark:text-dark-200">{{ form.sample_rate }}%</span>
          </div>
          <input v-model.number="form.sample_rate" type="range" min="1" max="100" step="1" class="w-full accent-primary-500" />
        </div>

        <div class="space-y-1.5">
          <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('userAccounts.moderationBlockMessage') }}</label>
          <input v-model.trim="form.block_message" class="input" />
        </div>
      </div>

      <div class="rounded-lg border border-gray-200 dark:border-dark-600">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-600">
          <div class="font-medium text-gray-900 dark:text-gray-100">{{ t('userAccounts.moderationTest') }}</div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="testing" @click="runTest">
            <Icon name="play" size="sm" class="mr-1.5" />
            {{ testing ? t('admin.accounts.testing') : t('userAccounts.moderationRunTest') }}
          </button>
        </div>
        <div class="space-y-3 p-4">
          <textarea v-model="testPrompt" class="input min-h-[88px]" :placeholder="t('userAccounts.moderationTestPlaceholder')"></textarea>
          <div v-if="testResult" class="grid gap-3 text-sm sm:grid-cols-2">
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <div class="text-xs text-gray-500 dark:text-dark-300">{{ t('userAccounts.moderationResult') }}</div>
              <div class="mt-1 font-semibold" :class="testResult.flagged ? 'text-red-600' : 'text-emerald-600'">
                {{ testResult.flagged ? t('userAccounts.moderationHit') : t('userAccounts.moderationPass') }}
              </div>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <div class="text-xs text-gray-500 dark:text-dark-300">{{ t('userAccounts.moderationCategory') }}</div>
              <div class="mt-1 font-semibold text-gray-900 dark:text-gray-100">{{ testResult.highest_category || '-' }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="rounded-lg border border-gray-200 dark:border-dark-600">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-600">
          <div class="font-medium text-gray-900 dark:text-gray-100">{{ t('userAccounts.moderationLogs') }}</div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="logsLoading" @click="loadLogs">
            <Icon name="refresh" size="sm" :class="['mr-1.5', logsLoading ? 'animate-spin' : '']" />
            {{ t('common.refresh') }}
          </button>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-600">
            <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-300">
              <tr>
                <th class="px-4 py-2 text-left">{{ t('userAccounts.moderationTime') }}</th>
                <th class="px-4 py-2 text-left">{{ t('userAccounts.moderationResult') }}</th>
                <th class="px-4 py-2 text-left">{{ t('userAccounts.moderationCategory') }}</th>
                <th class="px-4 py-2 text-left">{{ t('userAccounts.moderationAction') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="log in logs" :key="log.id" class="text-gray-700 dark:text-gray-200">
                <td class="whitespace-nowrap px-4 py-2">{{ formatDateTime(new Date(log.created_at)) }}</td>
                <td class="px-4 py-2">
                  <span :class="log.flagged ? 'text-red-600' : log.error ? 'text-amber-600' : 'text-emerald-600'">
                    {{ log.error ? t('common.error') : log.flagged ? t('userAccounts.moderationHit') : t('userAccounts.moderationPass') }}
                  </span>
                </td>
                <td class="px-4 py-2">{{ log.highest_category || '-' }}</td>
                <td class="px-4 py-2">{{ actionLabel(log.action) }}</td>
              </tr>
              <tr v-if="logs.length === 0">
                <td colspan="4" class="px-4 py-8 text-center text-gray-500 dark:text-dark-300">
                  {{ t('userAccounts.moderationNoLogs') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI } from '@/api'
import type {
  UserContentModerationConfig,
  UserContentModerationLog,
  UserContentModerationMode,
  UserContentModerationProvider,
  UserContentModerationTestResult
} from '@/api/accounts'
import type { Account } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved', config: UserContentModerationConfig): void
}>()

const { t } = useI18n()
const appStore = useAppStore()

const config = ref<UserContentModerationConfig | null>(null)
const logs = ref<UserContentModerationLog[]>([])
const testResult = ref<UserContentModerationTestResult | null>(null)
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const logsLoading = ref(false)
const testPrompt = ref('')
const form = reactive({
  enabled: false,
  mode: 'observe' as UserContentModerationMode,
  provider: 'openai' as UserContentModerationProvider,
  model: 'omni-moderation-latest',
  api_key: '',
  sample_rate: 100,
  block_message: ''
})

const modeOptions = computed(() => [
  { value: 'observe', label: t('userAccounts.moderationModeObserve') },
  { value: 'pre_block', label: t('userAccounts.moderationModePreBlock') }
])

const providerOptions = computed(() => [
  { value: 'openai', label: t('userAccounts.moderationProviderOpenAI') },
  { value: 'zhipu', label: t('userAccounts.moderationProviderZhipu') }
])

watch(
  () => props.show,
  (visible) => {
    if (visible && props.account) {
      void load()
    }
  }
)

watch(
  () => form.provider,
  (provider) => {
    form.model = provider === 'zhipu' ? 'moderation' : 'omni-moderation-latest'
  }
)

async function load(): Promise<void> {
  if (!props.account || loading.value) return
  loading.value = true
  try {
    config.value = await accountsAPI.getModerationConfig(props.account.id)
    applyConfig(config.value)
    await loadLogs()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.moderationLoadFailed')))
  } finally {
    loading.value = false
  }
}

function applyConfig(next: UserContentModerationConfig): void {
  form.enabled = next.enabled
  form.mode = next.mode
  form.provider = next.provider || 'openai'
  form.model = next.model || 'omni-moderation-latest'
  form.api_key = ''
  form.sample_rate = next.sample_rate || 100
  form.block_message = next.block_message || t('userAccounts.moderationDefaultBlockMessage')
  testResult.value = null
}

async function save(): Promise<boolean> {
  if (!props.account || saving.value) return false
  if (form.enabled && !form.api_key && !config.value?.api_key_configured) {
    appStore.showError(t('userAccounts.moderationApiKeyRequired'))
    return false
  }
  saving.value = true
  try {
    const next = await accountsAPI.updateModerationConfig(props.account.id, {
      enabled: form.enabled,
      mode: form.mode,
      provider: form.provider,
      api_key: form.api_key || undefined,
      sample_rate: Math.max(1, Math.min(100, Number(form.sample_rate) || 100)),
      block_message: form.block_message
    })
    config.value = next
    applyConfig(next)
    emit('saved', next)
    appStore.showSuccess(t('userAccounts.moderationSaved'))
    return true
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.moderationSaveFailed')))
    return false
  } finally {
    saving.value = false
  }
}

async function runTest(): Promise<void> {
  if (!props.account || testing.value) return
  if (isConfigDirtyForTest()) {
    const saved = await save()
    if (!saved) return
  }
  testing.value = true
  try {
    testResult.value = await accountsAPI.testModeration(props.account.id, testPrompt.value)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.moderationTestFailed')))
  } finally {
    testing.value = false
  }
}

async function loadLogs(): Promise<void> {
  if (!props.account || logsLoading.value) return
  logsLoading.value = true
  try {
    const response = await accountsAPI.listModerationLogs(props.account.id, 1, 20)
    logs.value = response.items
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.moderationLogsFailed')))
  } finally {
    logsLoading.value = false
  }
}

function close(): void {
  emit('close')
}

function actionLabel(action: string): string {
  switch (action) {
    case 'block':
      return t('userAccounts.moderationBlocked')
    case 'error':
      return t('common.error')
    default:
      return t('userAccounts.moderationAllowed')
  }
}

function isConfigDirtyForTest(): boolean {
  if (!config.value?.api_key_configured || form.api_key) return true
  return (
    form.enabled !== config.value.enabled ||
    form.mode !== config.value.mode ||
    form.provider !== config.value.provider ||
    Number(form.sample_rate) !== config.value.sample_rate ||
    form.block_message !== config.value.block_message
  )
}
</script>
