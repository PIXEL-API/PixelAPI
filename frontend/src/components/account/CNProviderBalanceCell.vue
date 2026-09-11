<template>
  <div v-if="visible" class="min-w-[130px] space-y-1">
    <span class="text-[10px] font-medium" :class="platformTextClass(account.platform)">{{ label }}</span>
    <button type="button" class="block text-[10px] text-blue-600 hover:underline disabled:opacity-50" :disabled="loading" @click="probe">
      {{ loading ? '查询中…' : '查询余额' }}
    </button>
    <span v-if="result && !result.success" class="block truncate text-[10px] text-red-500" :title="result.error">{{ result.error || '查询失败' }}</span>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Account } from '@/types'
import { adminAPI } from '@/api/admin'
import { queryUserBalance } from '@/api/admin/cnProviders'
import { platformTextClass } from '@/utils/platformColors'
import { cnBalanceCellVisible } from './credentialsBuilder'
import type { CNProviderBalanceProbeResult } from '@/api/admin/cnProviders'
const props = withDefaults(defineProps<{ account: Account; scope?: 'admin' | 'user' }>(), { scope: 'admin' })
const mode = computed(() => typeof props.account.credentials?.account_mode === 'string' ? props.account.credentials.account_mode : '')
const visible = computed(() => cnBalanceCellVisible(props.account.platform, mode.value))
const loading = ref(false)
const result = ref<CNProviderBalanceProbeResult | null>(null)
const entries = computed(() => result.value?.balances?.length ? result.value.balances : result.value?.success ? [{ currency: result.value.currency || '', balance: result.value.balance }] : [])
const label = computed(() => entries.value.length ? entries.value.map(item => `${item.currency || '¥'} ${item.balance >= 100 ? item.balance.toFixed(0) : item.balance.toFixed(2)}`).join(' · ') : '暂无余额数据')
async function probe() { if (loading.value) return; loading.value = true; try { result.value = props.scope === 'user' ? await queryUserBalance(props.account.id) : await adminAPI.cnProviders.queryBalance(props.account.id) } catch (error) { result.value = { provider: props.account.platform, success: false, balance: 0, available: false, fetched_at: Date.now(), persisted: false, error: error instanceof Error ? error.message : '查询失败' } } finally { loading.value = false } }
</script>
