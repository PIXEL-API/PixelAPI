<template>
  <div class="card overflow-hidden">
    <div
      class="border-b border-gray-100 bg-gradient-to-r from-primary-500/10 to-primary-600/5 px-6 py-5 dark:border-dark-700 dark:from-primary-500/20 dark:to-primary-600/10"
    >
      <div class="flex items-center gap-4">
        <div
          class="flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl bg-gradient-to-br from-primary-500 to-primary-600 text-2xl font-bold text-white shadow-lg shadow-primary-500/20"
        >
          <img
            v-if="avatarUrl"
            :src="avatarUrl"
            :alt="displayName"
            class="h-full w-full object-cover"
          >
          <span v-else>{{ avatarInitial }}</span>
        </div>
        <div class="min-w-0 flex-1">
          <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">
            {{ user?.email }}
          </h2>
          <div class="mt-1 flex items-center gap-2">
            <span :class="['badge', user?.role === 'admin' ? 'badge-primary' : 'badge-gray']">
              {{ user?.role === 'admin' ? t('profile.administrator') : t('profile.user') }}
            </span>
            <span
              :class="['badge', user?.status === 'active' ? 'badge-success' : 'badge-danger']"
            >
              {{ user?.status }}
            </span>
          </div>
        </div>
      </div>
    </div>
    <div class="px-6 py-4">
      <div class="space-y-3">
        <div class="flex items-center gap-3 text-sm text-gray-600 dark:text-gray-400">
          <Icon name="mail" size="sm" class="text-gray-400 dark:text-gray-500" />
          <span class="truncate">{{ user?.email }}</span>
        </div>
        <div
          v-if="user?.username"
          class="flex items-center gap-3 text-sm text-gray-600 dark:text-gray-400"
        >
          <Icon name="user" size="sm" class="text-gray-400 dark:text-gray-500" />
          <span class="truncate">{{ user.username }}</span>
        </div>
      </div>

      <div
        v-if="sourceHints.length"
        class="mt-4 grid gap-2 rounded-2xl border border-gray-100 bg-gray-50/80 p-3 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-900/30 dark:text-gray-400"
      >
        <div
          v-for="hint in sourceHints"
          :key="hint.key"
          class="flex items-start gap-2"
        >
          <Icon name="link" size="sm" class="mt-0.5 text-gray-400 dark:text-gray-500" />
          <span>{{ hint.text }}</span>
        </div>
      </div>

      <ProfileIdentityBindingsSection
        class="mt-4"
        :user="user"
        :linuxdo-enabled="linuxdoEnabled"
        :oidc-enabled="oidcEnabled"
        :oidc-provider-name="oidcProviderName"
        :wechat-enabled="wechatEnabled"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ProfileIdentityBindingsSection from '@/components/user/profile/ProfileIdentityBindingsSection.vue'
import type { User, UserAuthProvider, UserProfileSourceContext } from '@/types'

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

const providerLabels = computed<Record<UserAuthProvider, string>>(() => ({
  email: t('profile.authBindings.providers.email'),
  linuxdo: t('profile.authBindings.providers.linuxdo'),
  oidc: t('profile.authBindings.providers.oidc', { providerName: props.oidcProviderName }),
  wechat: t('profile.authBindings.providers.wechat'),
}))

const avatarUrl = computed(() => props.user?.avatar_url?.trim() || '')
const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || 'User')
const avatarInitial = computed(() => displayName.value.charAt(0).toUpperCase() || 'U')

function normalizeProvider(value: string): UserAuthProvider | null {
  const normalized = value.trim().toLowerCase()
  if (normalized === 'email' || normalized === 'linuxdo' || normalized === 'wechat') {
    return normalized
  }
  if (normalized === 'oidc' || normalized.startsWith('oidc:') || normalized.startsWith('oidc/')) {
    return 'oidc'
  }
  return null
}

function readObjectString(source: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = source[key]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return ''
}

function resolveThirdPartySource(
  rawSource: string | UserProfileSourceContext | null | undefined
): { provider: UserAuthProvider; label: string } | null {
  if (!rawSource) {
    return null
  }

  if (typeof rawSource === 'string') {
    const provider = normalizeProvider(rawSource)
    if (!provider || provider === 'email') {
      return null
    }
    return {
      provider,
      label: providerLabels.value[provider],
    }
  }

  const sourceRecord = rawSource as Record<string, unknown>
  const provider = normalizeProvider(
    readObjectString(sourceRecord, 'provider', 'source', 'provider_type', 'auth_provider')
  )
  if (!provider || provider === 'email') {
    return null
  }

  const explicitLabel = readObjectString(
    sourceRecord,
    'provider_label',
    'label',
    'provider_name',
    'providerName'
  )

  return {
    provider,
    label: explicitLabel || providerLabels.value[provider],
  }
}

const sourceHints = computed(() => {
  const currentUser = props.user
  if (!currentUser) {
    return []
  }

  const hints: Array<{ key: string; text: string }> = []
  const avatarSource = resolveThirdPartySource(
    currentUser.profile_sources?.avatar ?? currentUser.avatar_source
  )
  const usernameSource = resolveThirdPartySource(
    currentUser.profile_sources?.username ??
      currentUser.profile_sources?.display_name ??
      currentUser.profile_sources?.nickname ??
      currentUser.display_name_source ??
      currentUser.username_source ??
      currentUser.nickname_source
  )

  if (avatarSource) {
    hints.push({
      key: 'avatar',
      text: t('profile.authBindings.source.avatar', { providerName: avatarSource.label }),
    })
  }

  if (usernameSource) {
    hints.push({
      key: 'username',
      text: t('profile.authBindings.source.username', { providerName: usernameSource.label }),
    })
  }

  return hints
})
</script>
