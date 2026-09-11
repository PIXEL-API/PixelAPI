<template>
  <div v-if="isCN" class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-sm font-medium text-gray-700 dark:text-gray-300">账号模式
        <select v-model="local.mode" class="input mt-1"><option value="payg">Pay-as-you-go</option><option value="coding">Coding Plan</option></select>
      </label>
      <label class="text-sm font-medium text-gray-700 dark:text-gray-300">API 协议
        <select v-model="local.protocol" class="input mt-1"><option value="chat_completions">Chat Completions</option><option value="anthropic">Anthropic Messages</option><option v-if="supportsResponses" value="responses">Responses</option><option value="adaptive">Adaptive</option></select>
      </label>
    </div>
    <template v-if="local.protocol === 'adaptive'">
      <label v-for="item in adaptiveItems" :key="item.key" class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ item.label }} base URL
        <input v-model="local.api_base_urls[item.key]" class="input mt-1" type="url" :placeholder="item.placeholder" />
      </label>
    </template>
    <label v-else class="block text-sm font-medium text-gray-700 dark:text-gray-300">Base URL
      <input v-model="local.base_url" class="input mt-1" type="url" :placeholder="defaultBaseUrl" />
    </label>
  </div>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { defaultCNAdaptiveBaseUrls, defaultCNBaseUrl, cnSupportsNativeResponses, type CnAccountMode, type CnApiProtocol, type CnProviderPlatform } from './credentialsBuilder'
const props = defineProps<{ platform: string; modelValue: { mode: CnAccountMode; protocol: CnApiProtocol; base_url: string; api_base_urls: Record<string, string> } }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: typeof props.modelValue): void }>()
const isCN = computed(() => ['kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'].includes(props.platform))
const supportsResponses = computed(() => cnSupportsNativeResponses(props.platform))
const local = reactive({ mode: props.modelValue.mode, protocol: props.modelValue.protocol, base_url: props.modelValue.base_url, api_base_urls: { ...props.modelValue.api_base_urls } })
const defaultBaseUrl = computed(() => isCN.value ? defaultCNBaseUrl(props.platform, local.mode, local.protocol) : '')
const adaptiveItems = computed(() => Object.entries(defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, local.mode)).filter(([key]) => key !== 'responses' || supportsResponses.value).map(([key, placeholder]) => ({ key, label: key === 'chat_completions' ? 'Chat Completions' : key[0].toUpperCase() + key.slice(1), placeholder })))
watch(() => [local.mode, local.protocol] as const, ([mode, protocol], [previousMode, previousProtocol]) => {
  const previousDefault = defaultCNBaseUrl(props.platform, previousMode, previousProtocol)
  if (!local.base_url.trim() || local.base_url === previousDefault) {
    local.base_url = defaultCNBaseUrl(props.platform, mode, protocol)
  }
  const previousURLs = defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, previousMode)
  const nextURLs = defaultCNAdaptiveBaseUrls(props.platform as CnProviderPlatform, mode)
  for (const key of Object.keys(nextURLs) as Array<keyof typeof nextURLs>) {
    if (!local.api_base_urls[key] || local.api_base_urls[key] === previousURLs[key]) {
      local.api_base_urls[key] = nextURLs[key]
    }
  }
})
watch(local, () => emit('update:modelValue', { mode: local.mode, protocol: local.protocol, base_url: local.base_url, api_base_urls: { ...local.api_base_urls } }), { deep: true })
const sameApiBaseUrls = (left: Record<string, string>, right: Record<string, string>) => {
  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  if (leftKeys.length !== rightKeys.length) return false
  return leftKeys.every((key) => left[key] === right[key])
}
watch(() => [props.platform, props.modelValue], () => {
  const protocol = !cnSupportsNativeResponses(props.platform) && props.modelValue.protocol === 'responses'
    ? 'chat_completions'
    : props.modelValue.protocol
  if (local.mode !== props.modelValue.mode) local.mode = props.modelValue.mode
  if (local.protocol !== protocol) local.protocol = protocol
  if (local.base_url !== props.modelValue.base_url) local.base_url = props.modelValue.base_url
  if (!sameApiBaseUrls(local.api_base_urls, props.modelValue.api_base_urls)) {
    local.api_base_urls = { ...props.modelValue.api_base_urls }
  }
}, { deep: true, immediate: true })
</script>
