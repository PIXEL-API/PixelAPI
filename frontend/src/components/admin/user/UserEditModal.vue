<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.editUser')"
    width="normal"
    @close="$emit('close')"
  >
    <form v-if="user" id="edit-user-form" @submit.prevent="handleUpdateUser" class="space-y-5">
      <div>
        <label class="input-label">{{ t('admin.users.email') }}</label>
        <input v-model="form.email" type="email" class="input" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.password') }}</label>
        <div class="flex gap-2">
          <div class="relative flex-1">
            <input v-model="form.password" type="text" class="input pr-10" :placeholder="t('admin.users.enterNewPassword')" />
            <button v-if="form.password" type="button" @click="copyPassword" class="absolute right-2 top-1/2 -translate-y-1/2 rounded-lg p-1 transition-colors hover:bg-gray-100 dark:hover:bg-dark-700" :class="passwordCopied ? 'text-green-500' : 'text-gray-400'">
              <svg v-if="passwordCopied" class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
              <svg v-else class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" /></svg>
            </button>
          </div>
          <button type="button" @click="generatePassword" class="btn btn-secondary px-3">
            <Icon name="refresh" size="md" />
          </button>
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.username') }}</label>
        <input v-model="form.username" type="text" class="input" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.notes') }}</label>
        <textarea v-model="form.notes" rows="3" class="input"></textarea>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.columns.concurrency') }}</label>
        <input
          v-model.number="form.concurrency"
          type="number"
          min="1"
          class="input"
          @input="normalizeConcurrencyInput"
        />
        <p class="input-hint">{{ t('admin.users.concurrencyRangeHint') }}</p>
      </div>
      <div>
        <label class="input-label">{{ t('admin.users.form.rpmLimit') }}</label>
        <input
          v-model.number="form.rpm_limit"
          type="number"
          min="0"
          step="1"
          class="input"
          :placeholder="t('admin.users.form.rpmLimitPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.users.form.rpmLimitHint') }}</p>
      </div>
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
        <div class="mb-4 flex items-start justify-between gap-4">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.users.invitePolicy.title') }}
            </h3>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.users.invitePolicy.description') }}
            </p>
          </div>
          <span
            v-if="affiliatePolicy.loading"
            class="rounded bg-gray-100 px-2 py-1 text-xs text-gray-500 dark:bg-dark-700 dark:text-dark-300"
          >
            {{ t('common.loading') }}
          </span>
        </div>

        <div v-if="affiliatePolicy.loadError" class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-900/20 dark:text-red-300">
          {{ affiliatePolicy.loadError }}
        </div>

        <div v-else class="space-y-4">
          <div>
            <label class="input-label">{{ t('admin.users.invitePolicy.codeLabel') }}</label>
            <input
              v-model="affiliatePolicy.code"
              type="text"
              class="input font-mono"
              maxlength="32"
              :disabled="affiliatePolicy.loading || !affiliatePolicy.loaded"
              @input="normalizeAffiliateCodeInput"
            />
            <p class="input-hint">
              {{ t('admin.users.invitePolicy.codeHint') }}
            </p>
          </div>

          <div class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
            <div>
              <label class="input-label">{{ t('admin.users.invitePolicy.weeklyLimitLabel') }}</label>
              <input
                v-model.number="affiliatePolicy.weeklyLimit"
                type="number"
                class="input"
                min="0"
                max="100000"
                step="1"
                :disabled="affiliatePolicy.loading || !affiliatePolicy.loaded"
                @input="normalizeAffiliateWeeklyLimitInput"
              />
              <p class="input-hint">
                {{ t('admin.users.invitePolicy.weeklyLimitHint') }}
              </p>
            </div>
            <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {{ t('admin.users.invitePolicy.autoRotateLabel') }}
                  </p>
                  <p class="mt-0.5 text-xs text-gray-400 dark:text-dark-500">
                    {{ affiliatePolicyExpiryText }}
                  </p>
                </div>
                <Toggle
                  v-if="affiliatePolicy.loaded && !affiliatePolicy.loading"
                  v-model="affiliatePolicy.autoRotate"
                />
                <span v-else class="h-6 w-11 rounded-full bg-gray-200 dark:bg-dark-600"></span>
              </div>
            </div>
          </div>

          <div class="grid gap-3 rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400 sm:grid-cols-2">
            <span>{{ t('admin.users.invitePolicy.weeklyUsage', { used: affiliatePolicy.used, limit: affiliatePolicy.weeklyLimit }) }}</span>
            <span>{{ t('admin.users.invitePolicy.remaining', { remaining: affiliatePolicyRemaining }) }}</span>
          </div>
        </div>
      </div>
      <UserAttributeForm v-model="form.customAttributes" :user-id="user?.id" />
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="edit-user-form" :disabled="submitting" class="btn btn-primary">
          {{ submitting ? t('admin.users.updating') : t('common.update') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { adminAPI } from '@/api/admin'
import type { AdminUser, UserAttributeValuesMap } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import UserAttributeForm from '@/components/user/UserAttributeForm.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ show: boolean, user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n(); const appStore = useAppStore(); const { copyToClipboard } = useClipboard()

const submitting = ref(false); const passwordCopied = ref(false)
const form = reactive({ email: '', password: '', username: '', notes: '', concurrency: 1, rpm_limit: 0, customAttributes: {} as UserAttributeValuesMap })
const affiliatePolicy = reactive({
  loading: false,
  loaded: false,
  loadError: '',
  code: '',
  initialCode: '',
  used: 0,
  weeklyLimit: 2,
  initialWeeklyLimit: 2,
  autoRotate: true,
  initialAutoRotate: true,
  expiresAt: null as string | null,
})
let affiliatePolicyRequestSeq = 0

const normalizeConcurrencyInput = () => {
  form.concurrency = Math.max(1, form.concurrency || 1)
}

const normalizeAffiliateWeeklyLimitValue = (value: unknown) => {
  const normalized = Math.floor(Number(value) || 0)
  return Math.min(100000, Math.max(0, normalized))
}

const normalizeAffiliateWeeklyLimitInput = () => {
  affiliatePolicy.weeklyLimit = normalizeAffiliateWeeklyLimitValue(affiliatePolicy.weeklyLimit)
}

const normalizeAffiliateCodeInput = () => {
  affiliatePolicy.code = affiliatePolicy.code.trim().toUpperCase()
}

const resetAffiliatePolicy = () => {
  affiliatePolicyRequestSeq += 1
  Object.assign(affiliatePolicy, {
    loading: false,
    loaded: false,
    loadError: '',
    code: '',
    initialCode: '',
    used: 0,
    weeklyLimit: 2,
    initialWeeklyLimit: 2,
    autoRotate: true,
    initialAutoRotate: true,
    expiresAt: null,
  })
}

const loadAffiliatePolicy = async (userId: number) => {
  const seq = ++affiliatePolicyRequestSeq
  Object.assign(affiliatePolicy, {
    loading: true,
    loaded: false,
    loadError: '',
    code: '',
    initialCode: '',
    used: 0,
    weeklyLimit: 2,
    initialWeeklyLimit: 2,
    autoRotate: true,
    initialAutoRotate: true,
    expiresAt: null,
  })
  try {
    const entry = await adminAPI.affiliates.getUserSettings(userId)
    if (seq !== affiliatePolicyRequestSeq || props.user?.id !== userId) return
    const weeklyLimit = normalizeAffiliateWeeklyLimitValue(entry.aff_weekly_limit)
    const code = (entry.aff_code || '').trim().toUpperCase()
    Object.assign(affiliatePolicy, {
      loading: false,
      loaded: true,
      code,
      initialCode: code,
      used: entry.aff_weekly_used ?? 0,
      weeklyLimit,
      initialWeeklyLimit: weeklyLimit,
      autoRotate: entry.aff_code_auto_rotate ?? true,
      initialAutoRotate: entry.aff_code_auto_rotate ?? true,
      expiresAt: entry.aff_code_expires_at ?? null,
    })
  } catch (e: any) {
    if (seq !== affiliatePolicyRequestSeq || props.user?.id !== userId) return
    affiliatePolicy.loading = false
    affiliatePolicy.loaded = false
    affiliatePolicy.loadError = e.response?.data?.detail || e.message || t('admin.users.invitePolicy.loadFailed')
  }
}

const affiliatePolicyRemaining = computed(() => Math.max(0, affiliatePolicy.weeklyLimit - affiliatePolicy.used))

const affiliatePolicyDirty = computed(() => {
  if (!affiliatePolicy.loaded) return false
  return (
    affiliatePolicy.code.trim().toUpperCase() !== affiliatePolicy.initialCode ||
    affiliatePolicy.weeklyLimit !== affiliatePolicy.initialWeeklyLimit ||
    affiliatePolicy.autoRotate !== affiliatePolicy.initialAutoRotate
  )
})

const affiliatePolicyExpiryText = computed(() => {
  if (!affiliatePolicy.loaded) return t('admin.users.invitePolicy.loading')
  if (!affiliatePolicy.autoRotate) return t('admin.users.invitePolicy.keepCode')
  if (!affiliatePolicy.expiresAt) return t('admin.users.invitePolicy.expiresUnknown')
  return t('admin.users.invitePolicy.expiresAt', { time: formatDateTime(affiliatePolicy.expiresAt) })
})

watch(() => props.user, (u) => {
  if (u) {
    Object.assign(form, { email: u.email, password: '', username: u.username || '', notes: u.notes || '', concurrency: u.concurrency, rpm_limit: u.rpm_limit ?? 0, customAttributes: {} })
    passwordCopied.value = false
    void loadAffiliatePolicy(u.id)
  } else {
    resetAffiliatePolicy()
  }
}, { immediate: true })

const generatePassword = () => {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*'
  let p = ''; for (let i = 0; i < 16; i++) p += chars.charAt(Math.floor(Math.random() * chars.length))
  form.password = p
}
const copyPassword = async () => {
  if (form.password && await copyToClipboard(form.password, t('admin.users.passwordCopied'))) {
    passwordCopied.value = true; setTimeout(() => passwordCopied.value = false, 2000)
  }
}
const handleUpdateUser = async () => {
  if (!props.user) return
  if (!form.email.trim()) {
    appStore.showError(t('admin.users.emailRequired'))
    return
  }
  normalizeConcurrencyInput()
  if (form.concurrency < 1) {
    appStore.showError(t('admin.users.concurrencyRange'))
    return
  }
  normalizeAffiliateWeeklyLimitInput()
  normalizeAffiliateCodeInput()
  if (affiliatePolicy.loaded && !affiliatePolicy.code) {
    appStore.showError(t('admin.users.invitePolicy.codeRequired'))
    return
  }
  submitting.value = true
  try {
    const data: any = { email: form.email, username: form.username, notes: form.notes, concurrency: form.concurrency, rpm_limit: form.rpm_limit }
    if (form.password.trim()) data.password = form.password.trim()
    await adminAPI.users.update(props.user.id, data)
    if (affiliatePolicyDirty.value) {
      const payload: Parameters<typeof adminAPI.affiliates.updateUserSettings>[1] = {}
      if (affiliatePolicy.code !== affiliatePolicy.initialCode) payload.aff_code = affiliatePolicy.code
      if (affiliatePolicy.weeklyLimit !== affiliatePolicy.initialWeeklyLimit) payload.aff_weekly_limit = affiliatePolicy.weeklyLimit
      if (affiliatePolicy.autoRotate !== affiliatePolicy.initialAutoRotate) payload.aff_code_auto_rotate = affiliatePolicy.autoRotate
      await adminAPI.affiliates.updateUserSettings(props.user.id, payload)
    }
    if (Object.keys(form.customAttributes).length > 0) await adminAPI.userAttributes.updateUserAttributeValues(props.user.id, form.customAttributes)
    appStore.showSuccess(t('admin.users.userUpdated'))
    emit('success'); emit('close')
  } catch (e: any) {
    appStore.showError(e.response?.data?.detail || t('admin.users.failedToUpdate'))
  } finally { submitting.value = false }
}
</script>
