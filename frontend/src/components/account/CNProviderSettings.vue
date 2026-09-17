<template>
  <div v-if="isCN" class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.cnAccountMode') }}
        <select v-model="local.mode" class="input mt-1">
          <option value="payg">API key</option>
          <option value="coding">Coding Plan</option>
        </select>
      </label>
      <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.cnApiProtocol') }}
        <select v-model="local.protocol" class="input mt-1">
          <option value="chat_completions">Chat Completions</option>
          <option value="anthropic">Anthropic Messages</option>
          <option v-if="supportsResponses" value="responses">Responses</option>
          <option value="adaptive">Adaptive</option>
        </select>
      </label>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.cnEndpointAutoLock') }}
    </p>
    <template v-if="local.protocol === 'adaptive'">
      <div v-for="item in adaptiveItems" :key="item.key" class="space-y-1">
        <span class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.cnOfficialEndpoint', { label: item.label }) }}</span>
        <code v-if="isReadOnly" class="block rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-600 break-all dark:bg-dark-700 dark:text-gray-300">{{ item.url }}</code>
        <input v-else v-model="local.api_base_urls[item.key]" class="input mt-1" type="url" :placeholder="item.url" />
      </div>
    </template>
    <div v-else class="space-y-1">
      <span class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.cnOfficialEndpointLabel') }}</span>
      <code v-if="isReadOnly" class="block rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-600 break-all dark:bg-dark-700 dark:text-gray-300">{{ defaultBaseUrl }}</code>
      <input v-else v-model="local.base_url" class="input mt-1" type="url" :placeholder="defaultBaseUrl" />
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { defaultCNAdaptiveBaseUrls, defaultCNBaseUrl, cnSupportsNativeResponses, type CnAccountMode, type CnApiProtocol, type CnProviderPlatform } from './credentialsBuilder'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = withDefaults(defineProps<{ platform: string; modelValue: { mode: CnAccountMode; protocol: CnApiProtocol; base_url: string; api_base_urls: Record<string, string> }; allowCustomBaseUrl?: boolean }>(), {
  allowCustomBaseUrl: false
})
const emit = defineEmits<{ (e: 'update:modelValue', value: typeof props.modelValue): void }>()
const isCN = computed(() => ['kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'].includes(props.platform))
const isReadOnly = computed(() => props.allowCustomBaseUrl === false)
const canCustomizeBaseUrl = computed(() => !isReadOnly.value)
const supportsResponses = computed(() => cnSupportsNativeResponses(props.platform))
const local = reactive({ mode: props.modelValue.mode, protocol: props.modelValue.protocol, base_url: props.modelValue.base_url, api_base_urls: { ...props.modelValue.api_base_urls } })
const defaultBaseUrl = computed(() => isCN.value ? defaultCNBaseUrl(props.platform, local.mode, local.protocol) : '')
const adaptiveItems = computed(() => Object.entries(defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, local.mode))
  .filter(([key]) => key !== 'responses' || supportsResponses.value)
  .map(([key, url]) => ({
    key,
    label: key === 'chat_completions' ? 'Chat Completions' : key === 'anthropic' ? 'Anthropic Messages' : 'Responses',
    url
  })))

const sameConfig = (left: typeof props.modelValue, right: typeof props.modelValue) =>
  left.mode === right.mode &&
  left.protocol === right.protocol &&
  left.base_url === right.base_url &&
  JSON.stringify(left.api_base_urls) === JSON.stringify(right.api_base_urls)

const emitConfig = () => {
  const defaults = defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, local.mode)
  const isAdaptive = local.protocol === 'adaptive'
  const officialBaseUrl = isAdaptive ? defaults.chat_completions : defaultCNBaseUrl(props.platform, local.mode, local.protocol)
  const baseUrl = canCustomizeBaseUrl.value ? (local.base_url.trim() || officialBaseUrl) : officialBaseUrl
  const apiBaseUrls = isAdaptive
    ? Object.fromEntries(Object.entries(defaults).map(([key, url]) => [key, canCustomizeBaseUrl.value ? (local.api_base_urls[key]?.trim() || url) : url]))
    : {}
  const nextConfig = {
    mode: local.mode,
    protocol: local.protocol,
    base_url: baseUrl,
    api_base_urls: apiBaseUrls
  }
  if (local.base_url !== nextConfig.base_url) local.base_url = nextConfig.base_url
  if (JSON.stringify(local.api_base_urls) !== JSON.stringify(nextConfig.api_base_urls)) {
    local.api_base_urls = nextConfig.api_base_urls
  }
  if (!sameConfig(props.modelValue, nextConfig)) emit('update:modelValue', nextConfig)
}

watch(() => [local.mode, local.protocol, props.platform] as const, ([mode, protocol], previous) => {
  if (!cnSupportsNativeResponses(props.platform) && local.protocol === 'responses') {
    local.protocol = 'chat_completions'
    return
  }
  if (canCustomizeBaseUrl.value && previous) {
    const previousDefault = defaultCNBaseUrl(previous[2], previous[0], previous[1])
    if (!local.base_url.trim() || local.base_url === previousDefault) {
      local.base_url = defaultCNBaseUrl(props.platform, mode, protocol)
    }
    const previousURLs = defaultCNAdaptiveBaseUrls(previous[2] as CnProviderPlatform, previous[0])
    const nextURLs = defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, mode)
    for (const key of Object.keys(nextURLs) as Array<keyof typeof nextURLs>) {
      if (!local.api_base_urls[key] || local.api_base_urls[key] === previousURLs[key]) {
        local.api_base_urls[key] = nextURLs[key]
      }
    }
  }
  emitConfig()
}, { immediate: true })

watch(local, () => emitConfig(), { deep: true })

watch(() => props.modelValue, (value) => {
  if (local.mode !== value.mode) local.mode = value.mode
  const protocol = !cnSupportsNativeResponses(props.platform) && value.protocol === 'responses'
    ? 'chat_completions'
    : value.protocol
  if (local.protocol !== protocol) local.protocol = protocol
  if (canCustomizeBaseUrl.value) {
    if (local.base_url !== value.base_url) local.base_url = value.base_url
    if (JSON.stringify(local.api_base_urls) !== JSON.stringify(value.api_base_urls)) local.api_base_urls = { ...value.api_base_urls }
  }
  if (local.protocol === protocol && local.mode === value.mode) emitConfig()
}, { deep: true })
</script>
