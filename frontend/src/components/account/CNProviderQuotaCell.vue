<template>
  <div v-if="visible" class="min-w-[150px] space-y-1">
    <template v-if="isQwen">
      <p class="text-[10px] leading-tight text-gray-500 dark:text-gray-400">
        套餐剩余次数请在<a class="text-blue-600 hover:underline" href="https://bailian.console.aliyun.com/cn-beijing/?tab=plan" target="_blank" rel="noopener noreferrer">官方控制台</a>查看；下方为最近限额记录。
      </p>
      <span v-if="result && !result.success" class="block truncate text-[10px] text-red-500" :title="result.error">本次刷新失败：{{ result.error || '未知错误' }}</span>
      <div v-if="qwenObservations.length" class="space-y-1">
        <div v-for="observation in qwenObservations" :key="observation.window" class="flex items-center gap-1 text-[10px]">
          <span class="w-12 rounded bg-cyan-100 px-1 text-center text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300">{{ observation.window }}</span>
          <span class="text-gray-600 dark:text-gray-300">{{ formatObservedAt(observation.observed_at) }}</span>
          <span v-if="observation === latestQwenObservation" class="rounded bg-amber-100 px-1 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">最近记录</span>
          <span v-if="observation.retry_at" class="text-gray-400">{{ formatRetry(observation.retry_at) }}</span>
        </div>
      </div>
      <span v-else class="text-[10px] text-gray-400">暂无上游限额记录</span>
    </template>
    <div v-else-if="result?.success && result.tiers?.length" class="space-y-1">
      <div v-for="tier in result.tiers" :key="tier.window" class="flex items-center gap-1 text-[10px]">
        <span class="w-12 rounded bg-indigo-100 px-1 text-center text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300">{{ tier.window }}</span>
        <span class="text-gray-600 dark:text-gray-300">{{ Number.isFinite(tier.used_percent) ? `${Math.round(tier.used_percent)}%` : '—' }}</span>
        <span v-if="tier.reset_at" class="text-gray-400">{{ formatReset(tier.reset_at) }}</span>
      </div>
    </div>
    <span v-else-if="!isQwen" class="text-[10px] text-gray-400">{{ result?.error || '暂无额度数据' }}</span>
    <button type="button" class="text-[10px] text-blue-600 hover:underline disabled:opacity-50" :disabled="loading" @click="probe">
      {{ loading ? '刷新中…' : isQwen ? '刷新状态' : '查询额度' }}
    </button>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Account } from '@/types'
import { adminAPI } from '@/api/admin'
import { queryUserQuota } from '@/api/admin/cnProviders'
import { cnQuotaCellVisible, readQwenCodingLimitObservations, type QwenCodingLimitObservation } from './credentialsBuilder'
import type { CNProviderQuotaProbeResult } from '@/api/admin/cnProviders'
const props = withDefaults(defineProps<{ account: Account; scope?: 'admin' | 'user' }>(), { scope: 'admin' })
const mode = computed(() => typeof props.account.credentials?.account_mode === 'string' ? props.account.credentials.account_mode : '')
const visible = computed(() => cnQuotaCellVisible(props.account.platform, mode.value))
const loading = ref(false)
const result = ref<CNProviderQuotaProbeResult | null>(null)
const isQwen = computed(() => props.account.platform === 'qwen')
const qwenObservations = computed(() => {
  if (!isQwen.value) return []
  if (result.value?.observed_limits) return result.value.observed_limits
  return readQwenCodingLimitObservations(props.account.extra)
})
const latestQwenObservation = computed(() => qwenObservations.value.reduce<QwenCodingLimitObservation | null>((latest, current) => {
  if (!latest || Date.parse(current.observed_at) > Date.parse(latest.observed_at)) return current
  return latest
}, null))
const formatReset = (value: string) => { const date = new Date(value); return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString() }
const formatObservedAt = (value: string) => { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString() }
const formatRetry = (value: string) => { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : `可重试 ${date.toLocaleString()}` }
async function probe() { if (loading.value) return; loading.value = true; try { result.value = props.scope === 'user' ? await queryUserQuota(props.account.id) : await adminAPI.cnProviders.queryQuota(props.account.id) } catch (error) { result.value = { provider: props.account.platform, success: false, credential_valid: null, fetched_at: Date.now(), persisted: false, error: error instanceof Error ? error.message : '查询失败' } } finally { loading.value = false } }
</script>
