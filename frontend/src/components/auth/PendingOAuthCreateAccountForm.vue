<template>
  <form class="space-y-3" @submit.prevent="handleSubmit">
    <input
      v-model="email"
      :data-testid="`${testIdPrefix}-create-account-email`"
      type="email"
      class="input w-full"
      placeholder="you@example.com"
      :disabled="isSubmitting || isSendingCode"
    />
    <input
      v-model="password"
      :data-testid="`${testIdPrefix}-create-account-password`"
      type="password"
      class="input w-full"
      placeholder="Password"
      :disabled="isSubmitting"
    />
    <div v-if="turnstileEnabled && turnstileSiteKey" class="space-y-2">
      <TurnstileWidget
        ref="turnstileRef"
        :site-key="turnstileSiteKey"
        @verify="onTurnstileVerify"
        @expire="onTurnstileExpire"
        @error="onTurnstileError"
      />
    </div>
    <div class="flex gap-3">
      <input
        v-model="verifyCode"
        :data-testid="`${testIdPrefix}-create-account-verify-code`"
        type="text"
        inputmode="numeric"
        maxlength="6"
        class="input min-w-0 flex-1"
        placeholder="123456"
        :disabled="isSubmitting"
      />
      <button
        :data-testid="`${testIdPrefix}-create-account-send-code`"
        type="button"
        class="btn btn-secondary shrink-0"
        :disabled="isSubmitting || isSendingCode || countdown > 0 || !email.trim() || (turnstileEnabled && !turnstileToken)"
        @click="handleSendCode"
      >
        {{
          isSendingCode
            ? t('auth.sendingCode')
            : countdown > 0
              ? t('auth.resendCountdown', { countdown })
              : t('auth.sendCode')
        }}
      </button>
    </div>
    <p v-if="sendCodeSuccess" class="text-sm text-green-600 dark:text-green-400">
      {{ t('auth.codeSentSuccess') }}
    </p>
    <p v-else class="text-xs text-gray-500 dark:text-dark-400">
      {{ t('auth.verificationCodeHint') }}
    </p>
    <button
      :data-testid="`${testIdPrefix}-create-account-submit`"
      type="button"
      class="btn btn-primary w-full"
      :disabled="isSubmitting || !email.trim() || password.length < 6"
      @click="handleSubmit"
    >
      {{ isSubmitting ? t('common.processing') : 'Create account' }}
    </button>
    <button
      type="button"
      class="btn btn-secondary w-full"
      :disabled="isSubmitting"
      @click="emitSwitchToBind"
    >
      I already have an account
    </button>
    <transition name="fade">
      <p v-if="sendCodeError" class="text-sm text-red-600 dark:text-red-400">
        {{ sendCodeError }}
      </p>
    </transition>
    <transition name="fade">
      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">
        {{ errorMessage }}
      </p>
    </transition>
  </form>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import { getPublicSettings, sendVerifyCode } from '@/api/auth'

export type PendingOAuthCreateAccountPayload = {
  email: string
  password: string
  verifyCode: string
}

const props = defineProps<{
  initialEmail: string
  testIdPrefix: string
  isSubmitting: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  submit: [payload: PendingOAuthCreateAccountPayload]
  switchToBind: [email: string]
}>()

const { t } = useI18n()

const email = ref('')
const password = ref('')
const verifyCode = ref('')
const isSendingCode = ref(false)
const sendCodeError = ref('')
const sendCodeSuccess = ref(false)
const countdown = ref(0)
const turnstileEnabled = ref(false)
const turnstileSiteKey = ref('')
const turnstileToken = ref('')
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)

let countdownTimer: ReturnType<typeof setInterval> | null = null

watch(
  () => props.initialEmail,
  value => {
    email.value = value || ''
  },
  { immediate: true }
)

function clearCountdown() {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

function startCountdown(seconds: number) {
  clearCountdown()
  countdown.value = Math.max(0, seconds)

  if (countdown.value <= 0) {
    return
  }

  countdownTimer = setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = 0
      clearCountdown()
      return
    }

    countdown.value -= 1
  }, 1000)
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
}

function resetTurnstile() {
  turnstileToken.value = ''
  turnstileRef.value?.reset()
}

function onTurnstileVerify(token: string) {
  turnstileToken.value = token
  sendCodeError.value = ''
}

function onTurnstileExpire() {
  turnstileToken.value = ''
  sendCodeError.value = t('auth.turnstileExpired')
}

function onTurnstileError() {
  turnstileToken.value = ''
  sendCodeError.value = t('auth.turnstileFailed')
}

async function handleSendCode() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail) {
    return
  }

  if (turnstileEnabled.value && !turnstileToken.value) {
    sendCodeError.value = t('auth.completeVerification')
    return
  }

  isSendingCode.value = true
  sendCodeError.value = ''
  sendCodeSuccess.value = false

  try {
    const response = await sendVerifyCode({
      email: trimmedEmail,
      turnstile_token: turnstileEnabled.value ? turnstileToken.value : undefined
    })
    sendCodeSuccess.value = true
    startCountdown(response.countdown)
    if (turnstileEnabled.value) {
      resetTurnstile()
    }
  } catch (error: unknown) {
    sendCodeError.value = getRequestErrorMessage(error, t('auth.sendCodeFailed'))
  } finally {
    isSendingCode.value = false
  }
}

function handleSubmit() {
  const trimmedEmail = email.value.trim()
  if (!trimmedEmail || password.value.length < 6) {
    return
  }

  emit('submit', {
    email: trimmedEmail,
    password: password.value,
    verifyCode: verifyCode.value.trim()
  })
}

function emitSwitchToBind() {
  emit('switchToBind', email.value.trim())
}

onMounted(async () => {
  try {
    const settings = await getPublicSettings()
    turnstileEnabled.value = settings.turnstile_enabled === true
    turnstileSiteKey.value = settings.turnstile_site_key || ''
  } catch {
    turnstileEnabled.value = false
    turnstileSiteKey.value = ''
  }
})

onUnmounted(() => {
  clearCountdown()
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
