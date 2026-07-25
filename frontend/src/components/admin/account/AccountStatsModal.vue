<template>
  <AccountStatsModal
    :show="show"
    :account="account"
    :stats-loader="adminAPI.accounts.getStats"
    :usage-loader="adminAPI.accounts.getUsage"
    @close="emit('close')"
  />
</template>

<script setup lang="ts">
import AccountStatsModal from '@/components/account/AccountStatsModal.vue'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'

defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()
</script>
