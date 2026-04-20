<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.oidc.callbackTitle', { providerName }) }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{
            isProcessing
              ? t('auth.oidc.callbackProcessing', { providerName })
              : t('auth.oidc.callbackHint')
          }}
        </p>
      </div>

      <transition name="fade">
        <div
          v-if="
            needsInvitation ||
            needsAdoptionConfirmation ||
            needsCreateAccount ||
            needsBindLogin ||
            needsTotpChallenge
          "
          class="space-y-4"
        >
          <div
            v-if="adoptionRequired && (suggestedDisplayName || suggestedAvatarUrl)"
            class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800/60"
          >
            <div class="space-y-3">
              <div class="space-y-1">
                <p class="text-sm font-medium text-gray-900 dark:text-white">
                  Use {{ providerName }} profile details
                </p>
                <p class="text-xs text-gray-500 dark:text-dark-400">
                  Choose whether to apply the nickname or avatar from {{ providerName }} to this
                  account.
                </p>
              </div>

              <label
                v-if="suggestedDisplayName"
                class="flex items-start gap-3 rounded-lg border border-gray-200 bg-white p-3 text-sm dark:border-dark-600 dark:bg-dark-900/50"
              >
                <input v-model="adoptDisplayName" type="checkbox" class="mt-1 h-4 w-4" />
                <span class="space-y-1">
                  <span class="block font-medium text-gray-900 dark:text-white">
                    Use display name
                  </span>
                  <span class="block text-gray-500 dark:text-dark-400">
                    {{ suggestedDisplayName }}
                  </span>
                </span>
              </label>

              <label
                v-if="suggestedAvatarUrl"
                class="flex items-start gap-3 rounded-lg border border-gray-200 bg-white p-3 text-sm dark:border-dark-600 dark:bg-dark-900/50"
              >
                <input v-model="adoptAvatar" type="checkbox" class="mt-1 h-4 w-4" />
                <img
                  :src="suggestedAvatarUrl"
                  :alt="`${providerName} avatar`"
                  class="h-10 w-10 rounded-full border border-gray-200 object-cover dark:border-dark-600"
                />
                <span class="space-y-1">
                  <span class="block font-medium text-gray-900 dark:text-white">
                    Use avatar
                  </span>
                  <span class="block break-all text-gray-500 dark:text-dark-400">
                    {{ suggestedAvatarUrl }}
                  </span>
                </span>
              </label>
            </div>
          </div>

          <template v-if="needsInvitation">
            <p class="text-sm text-gray-700 dark:text-gray-300">
              {{ t('auth.oidc.invitationRequired', { providerName }) }}
            </p>
            <div>
              <input
                v-model="invitationCode"
                type="text"
                class="input w-full"
                :placeholder="t('auth.invitationCodePlaceholder')"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitInvitation"
              />
            </div>
            <transition name="fade">
              <p v-if="invitationError" class="text-sm text-red-600 dark:text-red-400">
                {{ invitationError }}
              </p>
            </transition>
            <button
              class="btn btn-primary w-full"
              :disabled="isSubmitting || !invitationCode.trim()"
              @click="handleSubmitInvitation"
            >
              {{
                isSubmitting
                  ? t('auth.oidc.completing')
                  : t('auth.oidc.completeRegistration')
              }}
            </button>
          </template>

          <template v-else-if="needsAdoptionConfirmation">
            <p class="text-sm text-gray-700 dark:text-gray-300">
              Review the {{ providerName }} profile details before continuing.
            </p>
            <button class="btn btn-primary w-full" :disabled="isSubmitting" @click="handleContinueLogin">
              {{ isSubmitting ? t('common.processing') : 'Continue' }}
            </button>
          </template>

          <template v-else-if="needsCreateAccount">
            <p class="text-sm text-gray-700 dark:text-gray-300">
              Enter an email address to create your account and continue.
            </p>
            <PendingOAuthCreateAccountForm
              test-id-prefix="oidc"
              :initial-email="pendingAccountEmail"
              :is-submitting="isSubmitting"
              :error-message="accountActionError"
              @submit="handleCreateAccount"
              @switch-to-bind="switchToBindLoginMode"
            />
          </template>

          <template v-else-if="needsBindLogin">
            <p class="text-sm text-gray-700 dark:text-gray-300">
              Log in to an existing account to bind this {{ providerName }} sign-in.
            </p>
            <div class="space-y-3">
              <input
                v-model="bindLoginEmail"
                data-testid="oidc-bind-login-email"
                type="email"
                class="input w-full"
                placeholder="you@example.com"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <input
                v-model="bindLoginPassword"
                data-testid="oidc-bind-login-password"
                type="password"
                class="input w-full"
                placeholder="Password"
                :disabled="isSubmitting"
                @keyup.enter="handleBindLogin"
              />
              <button
                data-testid="oidc-bind-login-submit"
                class="btn btn-primary w-full"
                :disabled="isSubmitting || !bindLoginEmail.trim() || !bindLoginPassword"
                @click="handleBindLogin"
              >
                {{ isSubmitting ? t('common.processing') : 'Log in and bind' }}
              </button>
              <button
                v-if="canReturnToCreateAccount"
                class="btn btn-secondary w-full"
                :disabled="isSubmitting"
                @click="switchToCreateAccountMode"
              >
                Use a different email
              </button>
            </div>
            <transition name="fade">
              <p v-if="accountActionError" class="text-sm text-red-600 dark:text-red-400">
                {{ accountActionError }}
              </p>
            </transition>
          </template>

          <template v-else-if="needsTotpChallenge">
            <p class="text-sm text-gray-700 dark:text-gray-300">
              Enter the 6-digit verification code for
              <span class="font-medium">{{ totpUserEmailMasked || 'your account' }}</span>
              to finish binding this {{ providerName }} sign-in.
            </p>
            <div class="space-y-3">
              <input
                v-model="totpCode"
                data-testid="oidc-bind-login-totp"
                type="text"
                inputmode="numeric"
                maxlength="6"
                class="input w-full"
                placeholder="123456"
                :disabled="isSubmitting"
                @keyup.enter="handleSubmitTotpChallenge"
              />
              <button
                data-testid="oidc-bind-login-totp-submit"
                class="btn btn-primary w-full"
                :disabled="isSubmitting || totpCode.trim().length !== 6"
                @click="handleSubmitTotpChallenge"
              >
                {{ isSubmitting ? t('common.processing') : 'Verify and continue' }}
              </button>
            </div>
            <transition name="fade">
              <p v-if="totpError" class="text-sm text-red-600 dark:text-red-400">
                {{ totpError }}
              </p>
            </transition>
          </template>
        </div>
      </transition>

      <transition name="fade">
        <div
          v-if="errorMessage"
          class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-800/50 dark:bg-red-900/20"
        >
          <div class="flex items-start gap-3">
            <div class="flex-shrink-0">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
            <div class="space-y-2">
              <p class="text-sm text-red-700 dark:text-red-400">
                {{ errorMessage }}
              </p>
              <router-link to="/login" class="btn btn-primary">
                {{ t('auth.oidc.backToLogin') }}
              </router-link>
            </div>
          </div>
        </div>
      </transition>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import PendingOAuthCreateAccountForm, {
  type PendingOAuthCreateAccountPayload
} from '@/components/auth/PendingOAuthCreateAccountForm.vue'
import Icon from '@/components/icons/Icon.vue'
import { apiClient } from '@/api/client'
import { useAuthStore, useAppStore } from '@/stores'
import {
  completeOIDCOAuthRegistration,
  exchangePendingOAuthCompletion,
  getOAuthCompletionKind,
  getPublicSettings,
  isOAuthLoginCompletion,
  login2FA,
  persistOAuthTokenContext,
  type OAuthAdoptionDecision,
  type PendingOAuthExchangeResponse
} from '@/api/auth'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const isProcessing = ref(true)
const errorMessage = ref('')
const needsInvitation = ref(false)
const invitationCode = ref('')
const isSubmitting = ref(false)
const invitationError = ref('')
const redirectTo = ref('/dashboard')
const providerName = ref('OIDC')
const adoptionRequired = ref(false)
const suggestedDisplayName = ref('')
const suggestedAvatarUrl = ref('')
const adoptDisplayName = ref(true)
const adoptAvatar = ref(true)
const needsAdoptionConfirmation = ref(false)
const pendingAccountAction = ref<'none' | 'create_account' | 'bind_login'>('none')
const pendingAccountEmail = ref('')
const bindLoginEmail = ref('')
const bindLoginPassword = ref('')
const accountActionError = ref('')
const canReturnToCreateAccount = ref(false)
const bindSuccessMessage = t('profile.authBindings.bindSuccess')
const needsTotpChallenge = ref(false)
const totpTempToken = ref('')
const totpCode = ref('')
const totpError = ref('')
const totpUserEmailMasked = ref('')

const needsCreateAccount = computed(() => pendingAccountAction.value === 'create_account')
const needsBindLogin = computed(() => pendingAccountAction.value === 'bind_login')

type PendingOidcCompletion = PendingOAuthExchangeResponse & {
  step?: string
  pending_email?: string
  resolved_email?: string
  existing_account_email?: string
  email?: string
  provider_fallback?: string
  intent?: string
  requires_2fa?: boolean
  temp_token?: string
  user_email_masked?: string
}

function persistPendingAuthSession(redirect?: string) {
  authStore.setPendingAuthSession({
    token: '',
    token_field: 'pending_oauth_token',
    provider: 'oidc',
    redirect: sanitizeRedirectPath(redirect || redirectTo.value)
  })
}

function clearPendingAuthSession() {
  authStore.clearPendingAuthSession()
}

function parseFragmentParams(): URLSearchParams {
  const raw = typeof window !== 'undefined' ? window.location.hash : ''
  const hash = raw.startsWith('#') ? raw.slice(1) : raw
  return new URLSearchParams(hash)
}

function sanitizeRedirectPath(path: string | null | undefined): string {
  if (!path) return '/dashboard'
  if (!path.startsWith('/')) return '/dashboard'
  if (path.startsWith('//')) return '/dashboard'
  if (path.includes('://')) return '/dashboard'
  if (path.includes('\n') || path.includes('\r')) return '/dashboard'
  return path
}

async function loadProviderName() {
  try {
    const settings = await getPublicSettings()
    const name = settings.oidc_oauth_provider_name?.trim()
    if (name) {
      providerName.value = name
    }
  } catch {
    // Ignore; fallback remains OIDC
  }
}

function currentAdoptionDecision(): OAuthAdoptionDecision {
  return {
    adoptDisplayName: adoptDisplayName.value,
    adoptAvatar: adoptAvatar.value
  }
}

function serializeAdoptionDecision(decision: OAuthAdoptionDecision): Record<string, boolean> {
  const payload: Record<string, boolean> = {}
  if (typeof decision.adoptDisplayName === 'boolean') {
    payload.adopt_display_name = decision.adoptDisplayName
  }
  if (typeof decision.adoptAvatar === 'boolean') {
    payload.adopt_avatar = decision.adoptAvatar
  }
  return payload
}

function applyAdoptionSuggestionState(completion: {
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
}) {
  adoptionRequired.value = completion.adoption_required === true
  suggestedDisplayName.value = completion.suggested_display_name || ''
  suggestedAvatarUrl.value = completion.suggested_avatar_url || ''

  if (!suggestedDisplayName.value) {
    adoptDisplayName.value = false
  }
  if (!suggestedAvatarUrl.value) {
    adoptAvatar.value = false
  }
}

function hasSuggestedProfile(completion: {
  suggested_display_name?: string
  suggested_avatar_url?: string
}): boolean {
  return Boolean(completion.suggested_display_name || completion.suggested_avatar_url)
}

function normalizedPendingState(value: string | null | undefined): string {
  return value?.trim().toLowerCase() || ''
}

function extractPendingAccountEmail(completion: PendingOidcCompletion): string {
  return (
    completion.pending_email ||
    completion.existing_account_email ||
    completion.resolved_email ||
    completion.email ||
    ''
  ).trim()
}

function resolvePendingAccountAction(completion: PendingOidcCompletion): 'none' | 'create_account' | 'bind_login' {
  const raw = normalizedPendingState(completion.step || completion.error || completion.intent)
  if (raw === 'email_required' || raw === 'create_account_required' || raw === 'create_account') {
    return 'create_account'
  }
  if (
    raw === 'bind_login_required' ||
    raw === 'bind_login' ||
    raw === 'existing_account_binding_required' ||
    raw === 'existing_account_required' ||
    raw === 'adopt_existing_user_by_email'
  ) {
    return 'bind_login'
  }
  return 'none'
}

function applyPendingAccountAction(completion: PendingOidcCompletion) {
  const action = resolvePendingAccountAction(completion)
  pendingAccountAction.value = action
  accountActionError.value = ''
  needsTotpChallenge.value = false
  totpTempToken.value = ''
  totpCode.value = ''
  totpError.value = ''
  totpUserEmailMasked.value = ''

  const email = extractPendingAccountEmail(completion)
  if (action === 'create_account') {
    pendingAccountEmail.value = email
    canReturnToCreateAccount.value = true
    return
  }

  if (action === 'bind_login') {
    bindLoginEmail.value = email
    bindLoginPassword.value = ''
    canReturnToCreateAccount.value = false
    return
  }

  canReturnToCreateAccount.value = false
}

function applyTotpChallenge(completion: PendingOidcCompletion): boolean {
  if (completion.requires_2fa !== true || !completion.temp_token) {
    return false
  }

  pendingAccountAction.value = 'none'
  needsInvitation.value = false
  needsAdoptionConfirmation.value = false
  needsTotpChallenge.value = true
  totpTempToken.value = completion.temp_token
  totpCode.value = ''
  totpError.value = ''
  totpUserEmailMasked.value = completion.user_email_masked || ''
  isProcessing.value = false
  return true
}

function switchToBindLoginMode(nextEmail?: string) {
  pendingAccountAction.value = 'bind_login'
  bindLoginEmail.value = bindLoginEmail.value.trim() || nextEmail?.trim() || pendingAccountEmail.value.trim()
  bindLoginPassword.value = ''
  accountActionError.value = ''
  canReturnToCreateAccount.value = true
}

function switchToCreateAccountMode() {
  pendingAccountAction.value = 'create_account'
  pendingAccountEmail.value = pendingAccountEmail.value.trim() || bindLoginEmail.value.trim()
  accountActionError.value = ''
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
}

async function finalizeCompletion(completion: PendingOAuthExchangeResponse, redirect: string) {
  if (getOAuthCompletionKind(completion) === 'bind') {
    const bindRedirect = sanitizeRedirectPath(completion.redirect || '/profile')
    clearPendingAuthSession()
    appStore.showSuccess(bindSuccessMessage)
    await router.replace(bindRedirect)
    return
  }

  if (!isOAuthLoginCompletion(completion)) {
    throw new Error(t('auth.oidc.callbackMissingToken'))
  }

  persistOAuthTokenContext(completion)
  await authStore.setToken(completion.access_token)
  appStore.showSuccess(t('auth.loginSuccess'))
  await router.replace(redirect)
}

async function finalizePendingAccountResponse(completion: PendingOidcCompletion) {
  applyAdoptionSuggestionState(completion)
  const redirect = sanitizeRedirectPath(completion.redirect || redirectTo.value)

  if (completion.error === 'invitation_required') {
    pendingAccountAction.value = 'none'
    needsInvitation.value = true
    needsAdoptionConfirmation.value = false
    isProcessing.value = false
    persistPendingAuthSession(redirect)
    return
  }

  if (applyTotpChallenge(completion)) {
    persistPendingAuthSession(redirect)
    return
  }

  applyPendingAccountAction(completion)
  if (pendingAccountAction.value !== 'none') {
    needsInvitation.value = false
    needsAdoptionConfirmation.value = false
    isProcessing.value = false
    persistPendingAuthSession(redirect)
    return
  }

  await finalizeCompletion(completion, redirect)
}

async function handleSubmitInvitation() {
  invitationError.value = ''
  if (!invitationCode.value.trim()) return

  isSubmitting.value = true
  try {
    const tokenData = await completeOIDCOAuthRegistration(
      invitationCode.value.trim(),
      currentAdoptionDecision()
    )
    persistOAuthTokenContext(tokenData)
    await authStore.setToken(tokenData.access_token)
    appStore.showSuccess(t('auth.loginSuccess'))
    await router.replace(redirectTo.value)
  } catch (e: unknown) {
    const err = e as { message?: string; response?: { data?: { message?: string } } }
    invitationError.value =
      err.response?.data?.message || err.message || t('auth.oidc.completeRegistrationFailed')
  } finally {
    isSubmitting.value = false
  }
}

async function handleContinueLogin() {
  isSubmitting.value = true
  try {
    const completion = await exchangePendingOAuthCompletion(currentAdoptionDecision()) as PendingOidcCompletion
    await finalizePendingAccountResponse(completion)
  } catch (e: unknown) {
    errorMessage.value = getRequestErrorMessage(e, t('auth.loginFailed'))
    appStore.showError(errorMessage.value)
    needsAdoptionConfirmation.value = false
  } finally {
    isSubmitting.value = false
  }
}

async function handleCreateAccount(payload: PendingOAuthCreateAccountPayload) {
  accountActionError.value = ''
  if (!payload.email || !payload.password) return

  isSubmitting.value = true
  try {
    const { data } = await apiClient.post<PendingOidcCompletion>('/auth/oauth/pending/create-account', {
      email: payload.email,
      password: payload.password,
      verify_code: payload.verifyCode || undefined,
      ...serializeAdoptionDecision(currentAdoptionDecision())
    })
    await finalizePendingAccountResponse(data)
  } catch (e: unknown) {
    accountActionError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
  } finally {
    isSubmitting.value = false
  }
}

async function handleBindLogin() {
  accountActionError.value = ''
  const email = bindLoginEmail.value.trim()
  const password = bindLoginPassword.value
  if (!email || !password) return

  isSubmitting.value = true
  try {
    const { data } = await apiClient.post<PendingOidcCompletion>('/auth/oauth/pending/bind-login', {
      email,
      password,
      ...serializeAdoptionDecision(currentAdoptionDecision())
    })
    await finalizePendingAccountResponse(data)
  } catch (e: unknown) {
    accountActionError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
  } finally {
    isSubmitting.value = false
  }
}

async function handleSubmitTotpChallenge() {
  totpError.value = ''
  const code = totpCode.value.trim()
  if (!totpTempToken.value || code.length !== 6) return

  isSubmitting.value = true
  try {
    const completion = await login2FA({
      temp_token: totpTempToken.value,
      totp_code: code
    })
    await authStore.setToken(completion.access_token)
    appStore.showSuccess(t('auth.loginSuccess'))
    await router.replace(redirectTo.value)
  } catch (e: unknown) {
    totpError.value = getRequestErrorMessage(e, t('auth.loginFailed'))
  } finally {
    isSubmitting.value = false
  }
}

onMounted(async () => {
  void loadProviderName()

  const params = parseFragmentParams()
  const error = params.get('error')
  const errorDesc = params.get('error_description') || params.get('error_message') || ''

  if (error) {
    errorMessage.value = errorDesc || error
    appStore.showError(errorMessage.value)
    isProcessing.value = false
    return
  }

  try {
    const completion = await exchangePendingOAuthCompletion() as PendingOidcCompletion
    const redirect = sanitizeRedirectPath(
      completion.redirect || (route.query.redirect as string | undefined) || '/dashboard'
    )
    applyAdoptionSuggestionState(completion)
    redirectTo.value = redirect

    if (completion.error === 'invitation_required') {
      needsInvitation.value = true
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (applyTotpChallenge(completion)) {
      persistPendingAuthSession(redirect)
      return
    }

    applyPendingAccountAction(completion)
    if (pendingAccountAction.value !== 'none') {
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    if (adoptionRequired.value && hasSuggestedProfile(completion)) {
      needsAdoptionConfirmation.value = true
      isProcessing.value = false
      persistPendingAuthSession(redirect)
      return
    }

    await finalizeCompletion(completion, redirect)
  } catch (e: unknown) {
    clearPendingAuthSession()
    errorMessage.value = getRequestErrorMessage(e, t('auth.loginFailed'))
    appStore.showError(errorMessage.value)
    isProcessing.value = false
  }
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
