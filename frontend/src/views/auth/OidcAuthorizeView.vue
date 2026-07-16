<template>
  <AuthLayout>
    <section class="space-y-6 text-center" aria-labelledby="oidc-authorize-title">
      <div
        class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-300"
        aria-hidden="true"
      >
        <svg
          v-if="status === 'loading'"
          class="h-7 w-7 animate-spin"
          viewBox="0 0 24 24"
          fill="none"
        >
          <circle class="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" stroke-width="3" />
          <path
            class="opacity-80"
            fill="currentColor"
            d="M21 12a9 9 0 0 0-9-9v3a6 6 0 0 1 6 6h3Z"
          />
        </svg>
        <Icon v-else name="exclamationCircle" size="lg" />
      </div>

      <div class="space-y-2" aria-live="polite">
        <h2 id="oidc-authorize-title" class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ status === 'loading' ? t('auth.oidcAuthorize.authorizing') : t('auth.oidcAuthorize.failed') }}
        </h2>
        <p class="mx-auto max-w-[36ch] text-base leading-6 text-gray-500 dark:text-dark-400">
          {{ status === 'loading' ? t('auth.oidcAuthorize.authorizingHint') : errorMessage }}
        </p>
      </div>

      <div v-if="status === 'error'" class="grid gap-3 sm:grid-cols-2">
        <button
          type="button"
          class="btn btn-primary min-h-11 w-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2"
          @click="completeAuthorization"
        >
          {{ t('auth.oidcAuthorize.retry') }}
        </button>
        <router-link
          to="/home"
          class="btn min-h-11 w-full border border-gray-200 bg-white text-gray-700 hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-100 dark:hover:bg-dark-700"
        >
          {{ t('auth.oidcAuthorize.backHome') }}
        </router-link>
      </div>
    </section>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

import { apiClient } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { AuthLayout } from '@/components/layout'
import { useAuthStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

type AuthorizationStatus = 'loading' | 'error'

interface OidcAuthorizationCompletion {
  redirect_url: string
}

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const { t } = useI18n()

const status = ref<AuthorizationStatus>('loading')
const errorMessage = ref('')

const requestId = computed(() => {
  const value = route.query.request_id
  return (Array.isArray(value) ? value[0] : value)?.trim() || ''
})

function validatedRedirectUrl(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) {
    throw new Error(t('auth.oidcAuthorize.invalidRedirect'))
  }

  const url = new URL(value)
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(t('auth.oidcAuthorize.invalidRedirect'))
  }

  return url.href
}

async function completeAuthorization(): Promise<void> {
  if (!authStore.isAuthenticated) {
    await router.replace({ path: '/login', query: { redirect: route.fullPath } })
    return
  }

  if (!requestId.value) {
    status.value = 'error'
    errorMessage.value = t('auth.oidcAuthorize.missingRequest')
    return
  }

  status.value = 'loading'
  errorMessage.value = ''

  try {
    const response = await apiClient.post<OidcAuthorizationCompletion>('/oidc/authorize/complete', {
      request_id: requestId.value,
    })
    window.location.replace(validatedRedirectUrl(response.data.redirect_url))
  } catch (error: unknown) {
    status.value = 'error'
    errorMessage.value = extractApiErrorMessage(error, t('auth.oidcAuthorize.failedHint'))
  }
}

onMounted(() => {
  void completeAuthorization()
})
</script>
