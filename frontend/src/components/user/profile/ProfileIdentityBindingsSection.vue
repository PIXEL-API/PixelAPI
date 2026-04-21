<template>
  <div class="rounded-2xl border border-gray-100 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-900/30">
    <div>
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('profile.authBindings.title') }}
      </h3>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('profile.authBindings.description') }}
      </p>
    </div>

    <div class="mt-4 space-y-2">
      <div
        v-for="item in providerItems"
        :key="item.provider"
        class="rounded-xl bg-white/80 px-3 py-3 dark:bg-dark-800/70"
      >
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <div class="text-sm font-medium text-gray-900 dark:text-white">
                {{ item.label }}
              </div>
              <span
                :data-testid="`profile-binding-${item.provider}-status`"
                :class="['badge', item.bound ? 'badge-success' : 'badge-gray']"
              >
                {{
                  item.bound
                    ? t('profile.authBindings.status.bound')
                    : t('profile.authBindings.status.notBound')
                }}
              </span>
            </div>

            <div
              v-if="item.provider === 'email' && !item.bound"
              class="mt-3 grid gap-2 sm:grid-cols-[minmax(0,1.4fr)_auto]"
            >
              <input
                v-model.trim="emailBindingForm.email"
                data-testid="profile-binding-email-input"
                type="email"
                class="input"
                :placeholder="t('profile.authBindings.emailPlaceholder')"
                :disabled="isSendingEmailCode || isBindingEmail"
              />
              <button
                data-testid="profile-binding-email-send-code"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="isSendingEmailCode || isBindingEmail"
                @click="sendEmailCode"
              >
                {{
                  isSendingEmailCode
                    ? t('common.loading')
                    : t('profile.authBindings.sendCodeAction')
                }}
              </button>
              <input
                v-model.trim="emailBindingForm.verifyCode"
                data-testid="profile-binding-email-code-input"
                type="text"
                inputmode="numeric"
                maxlength="6"
                class="input"
                :placeholder="t('profile.authBindings.codePlaceholder')"
                :disabled="isBindingEmail"
              />
              <input
                v-model="emailBindingForm.password"
                data-testid="profile-binding-email-password-input"
                type="password"
                class="input"
                :placeholder="t('profile.authBindings.passwordPlaceholder')"
                :disabled="isBindingEmail"
              />
              <button
                data-testid="profile-binding-email-submit"
                type="button"
                class="btn btn-primary btn-sm sm:col-span-2"
                :disabled="isBindingEmail"
                @click="bindEmail"
              >
                {{
                  isBindingEmail
                    ? t('common.loading')
                    : t('profile.authBindings.confirmEmailBindAction')
                }}
              </button>
            </div>
          </div>

          <div class="flex shrink-0 items-center gap-2">
            <button
              v-if="item.canBind"
              :data-testid="`profile-binding-${item.provider}-action`"
              type="button"
              class="btn btn-secondary btn-sm"
              @click="startBinding(item.provider)"
            >
              {{ t('profile.authBindings.bindAction', { providerName: item.label }) }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import {
  hasExplicitWeChatOAuthCapabilities,
  resolveWeChatOAuthStartStrict,
  type WeChatOAuthPublicSettings,
} from '@/api/auth'
import { bindEmailIdentity, sendEmailBindingCode, startOAuthBinding } from '@/api/user'
import { useAppStore, useAuthStore } from '@/stores'
import type { User, UserAuthBindingStatus, UserAuthProvider } from '@/types'

const props = withDefaults(
  defineProps<{
    user: User | null
    linuxdoEnabled?: boolean
    oidcEnabled?: boolean
    oidcProviderName?: string
    wechatEnabled?: boolean
    wechatOpenEnabled?: boolean
    wechatMpEnabled?: boolean
  }>(),
  {
    linuxdoEnabled: false,
    oidcEnabled: false,
    oidcProviderName: 'OIDC',
    wechatEnabled: false,
    wechatOpenEnabled: undefined,
    wechatMpEnabled: undefined,
  }
)

const { t } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

const localUser = ref<User | null>(null)
const isSendingEmailCode = ref(false)
const isBindingEmail = ref(false)
const emailBindingForm = reactive({
  email: '',
  verifyCode: '',
  password: '',
})

watch(
  () => props.user,
  (user) => {
    localUser.value = null
    if (!user || getBindingStatusForUser(user, 'email')) {
      return
    }
    if (typeof user.email === 'string' && !user.email.endsWith('.invalid')) {
      emailBindingForm.email = user.email
    }
  },
  { immediate: true }
)

const currentUser = computed(() => localUser.value ?? props.user)

const wechatOAuthSettings = computed<WeChatOAuthPublicSettings | null>(() => {
  if (hasExplicitWeChatOAuthCapabilities(appStore.cachedPublicSettings)) {
    return appStore.cachedPublicSettings
  }

  if (typeof props.wechatOpenEnabled === 'boolean' && typeof props.wechatMpEnabled === 'boolean') {
    return {
      wechat_oauth_enabled: props.wechatEnabled,
      wechat_oauth_open_enabled: props.wechatOpenEnabled,
      wechat_oauth_mp_enabled: props.wechatMpEnabled,
    }
  }

  return null
})

const resolvedWeChatBinding = computed(() => resolveWeChatOAuthStartStrict(wechatOAuthSettings.value))

function normalizeBindingStatus(binding: boolean | UserAuthBindingStatus | undefined): boolean | null {
  if (typeof binding === 'boolean') {
    return binding
  }
  if (!binding) {
    return null
  }
  if (typeof binding.bound === 'boolean') {
    return binding.bound
  }
  return Boolean(binding.provider_subject || binding.issuer || binding.provider_key)
}

function getBindingStatus(provider: UserAuthProvider): boolean {
  return getBindingStatusForUser(currentUser.value, provider)
}

function getBindingStatusForUser(user: User | null | undefined, provider: UserAuthProvider): boolean {
  if (provider === 'email') {
    return typeof user?.email_bound === 'boolean' ? user.email_bound : Boolean(user?.email)
  }

  const directFlag = user?.[`${provider}_bound` as keyof User]
  if (typeof directFlag === 'boolean') {
    return directFlag
  }

  const nested = user?.auth_bindings?.[provider] ?? user?.identity_bindings?.[provider]
  const normalized = normalizeBindingStatus(nested)
  return normalized ?? false
}

const providerItems = computed(() => [
  {
    provider: 'email' as const,
    label: t('profile.authBindings.providers.email'),
    bound: getBindingStatus('email'),
    canBind: false,
  },
  {
    provider: 'linuxdo' as const,
    label: t('profile.authBindings.providers.linuxdo'),
    bound: getBindingStatus('linuxdo'),
    canBind: props.linuxdoEnabled && !getBindingStatus('linuxdo'),
  },
  {
    provider: 'oidc' as const,
    label: t('profile.authBindings.providers.oidc', { providerName: props.oidcProviderName }),
    bound: getBindingStatus('oidc'),
    canBind: props.oidcEnabled && !getBindingStatus('oidc'),
  },
  {
    provider: 'wechat' as const,
    label: t('profile.authBindings.providers.wechat'),
    bound: getBindingStatus('wechat'),
    canBind: resolvedWeChatBinding.value.mode !== null && !getBindingStatus('wechat'),
  },
])

function startBinding(provider: UserAuthProvider): void {
  if (provider === 'email') {
    return
  }
  startOAuthBinding(provider, {
    redirectTo: route.fullPath || '/profile',
    wechatOAuthSettings: provider === 'wechat' ? wechatOAuthSettings.value : null,
  })
}

function applyUpdatedUser(user: User): void {
  localUser.value = user
  authStore.user = user
}

function validateEmailBindingForm(requireCode: boolean): boolean {
  if (!emailBindingForm.email) {
    appStore.showError(t('auth.emailRequired'))
    return false
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailBindingForm.email)) {
    appStore.showError(t('auth.invalidEmail'))
    return false
  }
  if (requireCode && !emailBindingForm.verifyCode) {
    appStore.showError(t('auth.codeRequired'))
    return false
  }
  if (requireCode && !emailBindingForm.password) {
    appStore.showError(t('auth.passwordRequired'))
    return false
  }
  if (requireCode && emailBindingForm.password.length < 6) {
    appStore.showError(t('auth.passwordMinLength'))
    return false
  }
  return true
}

async function sendEmailCode(): Promise<void> {
  if (!validateEmailBindingForm(false)) {
    return
  }

  isSendingEmailCode.value = true
  try {
    await sendEmailBindingCode(emailBindingForm.email)
    appStore.showSuccess(t('profile.authBindings.codeSentTo', { email: emailBindingForm.email }))
  } catch (error) {
    appStore.showError((error as { message?: string }).message || t('auth.sendCodeFailed'))
  } finally {
    isSendingEmailCode.value = false
  }
}

async function bindEmail(): Promise<void> {
  if (!validateEmailBindingForm(true)) {
    return
  }

  isBindingEmail.value = true
  try {
    const user = await bindEmailIdentity({
      email: emailBindingForm.email,
      verify_code: emailBindingForm.verifyCode,
      password: emailBindingForm.password,
    })
    applyUpdatedUser(user)
    emailBindingForm.verifyCode = ''
    emailBindingForm.password = ''
    appStore.showSuccess(t('profile.authBindings.bindSuccess'))
  } catch (error) {
    appStore.showError((error as { message?: string }).message || t('common.tryAgain'))
  } finally {
    isBindingEmail.value = false
  }
}
</script>
