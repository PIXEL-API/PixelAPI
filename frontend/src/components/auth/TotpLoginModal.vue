<template>
  <BaseDialog
    :show="true"
    :title="t('profile.totp.loginTitle')"
    width="narrow"
    :close-on-escape="false"
    :close-on-click-outside="false"
    :close-disabled="verifying"
    panel-class="totp-login-panel"
    @close="emit('cancel')"
  >
    <template #title-prefix>
      <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
        <svg class="h-5 w-5 text-primary-600 dark:text-primary-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
        </svg>
      </span>
    </template>

    <div class="mb-6 text-center">
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('profile.totp.loginHint') }}
      </p>
      <p v-if="userEmailMasked" class="mt-1 text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ userEmailMasked }}
      </p>
    </div>

    <!-- Code Input -->
    <div class="mb-2">
      <div class="flex justify-center gap-2">
        <input
          v-for="(_, index) in 6"
          :key="index"
          :ref="(el) => setInputRef(el, index)"
          type="text"
          maxlength="1"
          inputmode="numeric"
          pattern="[0-9]"
          class="h-12 w-10 rounded-lg border border-gray-300 text-center text-lg font-semibold focus:border-primary-500 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
          :disabled="verifying"
          @input="handleCodeInput($event, index)"
          @keydown="handleKeydown($event, index)"
          @paste="handlePaste"
        />
      </div>
      <!-- Loading indicator -->
      <div v-if="verifying" class="mt-3 flex items-center justify-center gap-2 text-sm text-gray-500">
        <div class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-500"></div>
        {{ t('common.verifying') }}
      </div>
    </div>

    <template #footer>
      <button
        type="button"
        class="btn btn-secondary w-full"
        :disabled="verifying"
        @click="emit('cancel')"
      >
        {{ t('common.cancel') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import BaseDialog from '@/components/common/BaseDialog.vue'

defineProps<{
  tempToken: string
  userEmailMasked?: string
}>()

const emit = defineEmits<{
  verify: [code: string]
  cancel: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const verifying = ref(false)
const code = ref<string[]>(['', '', '', '', '', ''])
const inputRefs = ref<(HTMLInputElement | null)[]>([])

// Watch for code changes and auto-submit when 6 digits are entered
watch(
  () => code.value.join(''),
  (newCode) => {
    if (newCode.length === 6 && !verifying.value) {
      emit('verify', newCode)
    }
  }
)

defineExpose({
  setVerifying: (value: boolean) => { verifying.value = value },
  setError: (message: string) => {
    if (message) {
      appStore.showError(message)
    }
    code.value = ['', '', '', '', '', '']
    // Clear input DOM values
    inputRefs.value.forEach(input => {
      if (input) input.value = ''
    })
    nextTick(() => {
      inputRefs.value[0]?.focus()
    })
  }
})

const setInputRef = (el: any, index: number) => {
  inputRefs.value[index] = el as HTMLInputElement | null
}

const handleCodeInput = (event: Event, index: number) => {
  const input = event.target as HTMLInputElement
  const value = input.value.replace(/[^0-9]/g, '')
  code.value[index] = value

  if (value && index < 5) {
    nextTick(() => {
      inputRefs.value[index + 1]?.focus()
    })
  }
}

const handleKeydown = (event: KeyboardEvent, index: number) => {
  if (event.key === 'Backspace') {
    const input = event.target as HTMLInputElement
    // If current cell is empty and not the first, move to previous cell
    if (!input.value && index > 0) {
      event.preventDefault()
      inputRefs.value[index - 1]?.focus()
    }
    // Otherwise, let the browser handle the backspace naturally
    // The input event will sync code.value via handleCodeInput
  }
}

const handlePaste = (event: ClipboardEvent) => {
  event.preventDefault()
  const pastedData = event.clipboardData?.getData('text') || ''
  const digits = pastedData.replace(/[^0-9]/g, '').slice(0, 6).split('')

  // Update both the ref and the input elements
  digits.forEach((digit, index) => {
    code.value[index] = digit
    if (inputRefs.value[index]) {
      inputRefs.value[index]!.value = digit
    }
  })

  // Clear remaining inputs if pasted less than 6 digits
  for (let i = digits.length; i < 6; i++) {
    code.value[i] = ''
    if (inputRefs.value[i]) {
      inputRefs.value[i]!.value = ''
    }
  }

  const focusIndex = Math.min(digits.length, 5)
  nextTick(() => {
    inputRefs.value[focusIndex]?.focus()
  })
}

onMounted(() => {
  nextTick(() => {
    inputRefs.value[0]?.focus()
  })
})
</script>

<style scoped>
.totp-login-panel :deep(.modal-header) {
  align-items: center;
}

.totp-login-panel :deep(.modal-body) {
  padding-top: 0.25rem;
}
</style>
