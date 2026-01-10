<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.linuxdo.callbackTitle') }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ isProcessing ? t('auth.linuxdo.callbackProcessing') : t('auth.linuxdo.callbackHint') }}
        </p>
      </div>

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
                {{ t('auth.linuxdo.backToLogin') }}
              </router-link>
            </div>
          </div>
        </div>
      </transition>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import Icon from '@/components/icons/Icon.vue'
import { useAuthStore, useAppStore } from '@/stores'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const isProcessing = ref(true)
const errorMessage = ref('')

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

onMounted(async () => {
  const params = parseFragmentParams()

  const token = params.get('access_token') || ''
  const redirect = sanitizeRedirectPath(
    params.get('redirect') || (route.query.redirect as string | undefined) || '/dashboard'
  )
  const error = params.get('error')
  const errorDesc = params.get('error_description') || params.get('error_message') || ''

  if (error) {
    errorMessage.value = errorDesc || error
    appStore.showError(errorMessage.value)
    isProcessing.value = false
    return
  }

  if (!token) {
    errorMessage.value = t('auth.linuxdo.callbackMissingToken')
    appStore.showError(errorMessage.value)
    isProcessing.value = false
    return
  }

  try {
    await authStore.setToken(token)
    appStore.showSuccess(t('auth.loginSuccess'))
    await router.replace(redirect)
  } catch (e: unknown) {
    const err = e as { message?: string; response?: { data?: { detail?: string } } }
    errorMessage.value = err.response?.data?.detail || err.message || t('auth.loginFailed')
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

