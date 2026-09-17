<template>
  <div class="space-y-3 rounded-lg border border-amber-300 p-3 dark:border-amber-700">
    <div class="flex items-center gap-2">
      <span class="inline-flex items-center rounded-md bg-amber-100 px-2 py-0.5 text-xs font-bold text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">APIKEY</span>
      <p class="text-xs text-amber-700 dark:text-amber-400">
        {{ t('admin.accounts.apiAggregation.unverifiedHint') }}
      </p>
    </div>
    <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.cnApiProtocol') }}
      <select v-model="local.protocol" class="input mt-1">
        <option value="adaptive">Adaptive</option>
        <option value="chat_completions">Chat Completions</option>
        <option value="responses">Responses</option>
        <option value="anthropic">Anthropic Messages</option>
      </select>
    </label>
    <div class="space-y-1">
      <span class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.accounts.apiAggregation.baseUrl') }}</span>
      <input v-model="local.base_url" class="input mt-1" type="url" required placeholder="https://api.example.com" />
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.apiAggregation.baseUrlHint') }}</p>
    </div>
    <template v-if="local.protocol === 'adaptive'">
      <div v-for="item in adaptiveItems" :key="item.key" class="space-y-1">
        <span class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ item.label }}</span>
        <input v-model="local.api_base_urls[item.key]" class="input mt-1" type="url" :placeholder="local.base_url || 'https://api.example.com'" />
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.apiAggregation.adaptiveHint') }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export interface APIAggregationConfig {
  protocol: 'adaptive' | 'chat_completions' | 'responses' | 'anthropic'
  base_url: string
  api_base_urls: Record<string, string>
}

const { t } = useI18n()
const props = defineProps<{ modelValue: APIAggregationConfig }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: APIAggregationConfig): void }>()

const local = reactive<APIAggregationConfig>({
  protocol: props.modelValue.protocol,
  base_url: props.modelValue.base_url,
  api_base_urls: { ...props.modelValue.api_base_urls }
})

const adaptiveItems = [
  { key: 'chat_completions', label: 'Chat Completions URL' },
  { key: 'responses', label: 'Responses URL' },
  { key: 'anthropic', label: 'Anthropic Messages URL' }
]

const emitConfig = () => {
  const apiBaseUrls: Record<string, string> = {}
  if (local.protocol === 'adaptive') {
    for (const item of adaptiveItems) {
      const url = local.api_base_urls[item.key]?.trim()
      if (url) apiBaseUrls[item.key] = url
    }
  }
  emit('update:modelValue', {
    protocol: local.protocol,
    base_url: local.base_url,
    api_base_urls: apiBaseUrls
  })
}

watch(local, emitConfig, { deep: true })
watch(() => props.modelValue, (value) => {
  if (local.protocol !== value.protocol) local.protocol = value.protocol
  if (local.base_url !== value.base_url) local.base_url = value.base_url
  if (JSON.stringify(local.api_base_urls) !== JSON.stringify(value.api_base_urls)) {
    local.api_base_urls = { ...value.api_base_urls }
  }
}, { deep: true })
</script>
