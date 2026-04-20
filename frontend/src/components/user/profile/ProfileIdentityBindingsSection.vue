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
        class="flex items-center justify-between gap-3 rounded-xl bg-white/80 px-3 py-2.5 dark:bg-dark-800/70"
      >
        <div class="min-w-0">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ item.label }}
          </div>
        </div>

        <div class="flex shrink-0 items-center gap-2">
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
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { startOAuthBinding } from '@/api/user'
import type { User, UserAuthBindingStatus, UserAuthProvider } from '@/types'

const props = withDefaults(
  defineProps<{
    user: User | null
    linuxdoEnabled?: boolean
    oidcEnabled?: boolean
    oidcProviderName?: string
    wechatEnabled?: boolean
  }>(),
  {
    linuxdoEnabled: false,
    oidcEnabled: false,
    oidcProviderName: 'OIDC',
    wechatEnabled: false,
  }
)

const { t } = useI18n()
const route = useRoute()

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
  const currentUser = props.user

  if (provider === 'email') {
    return typeof currentUser?.email_bound === 'boolean'
      ? currentUser.email_bound
      : Boolean(currentUser?.email)
  }

  const directFlag = currentUser?.[`${provider}_bound` as keyof User]
  if (typeof directFlag === 'boolean') {
    return directFlag
  }

  const nested = currentUser?.auth_bindings?.[provider] ?? currentUser?.identity_bindings?.[provider]
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
    canBind: props.wechatEnabled && !getBindingStatus('wechat'),
  },
])

function startBinding(provider: UserAuthProvider): void {
  if (provider === 'email') {
    return
  }
  startOAuthBinding(provider, {
    redirectTo: route.fullPath || '/profile',
  })
}
</script>
