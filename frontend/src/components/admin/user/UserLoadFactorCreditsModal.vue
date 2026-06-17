<template>
  <BaseDialog
    :show="show"
    :title="operation === 'add' ? t('admin.users.addLoadFactorCredits') : t('admin.users.deductLoadFactorCredits')"
    width="narrow"
    @close="$emit('close')"
  >
    <form v-if="user" id="load-factor-credits-form" @submit.prevent="handleSubmit" class="space-y-5">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100">
          <span class="text-lg font-medium text-primary-700">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="flex-1">
          <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-300">
            {{ t('admin.users.currentLoadFactorCredits') }}: {{ currentCredits }}
          </p>
        </div>
      </div>

      <div>
        <label class="input-label">
          {{ operation === 'add' ? t('admin.users.addLoadFactorCreditsAmount') : t('admin.users.deductLoadFactorCreditsAmount') }}
        </label>
        <div class="relative flex gap-2">
          <input v-model.number="form.amount" type="number" step="1" min="1" required class="input flex-1" />
          <button
            v-if="operation === 'subtract'"
            type="button"
            @click="fillAllCredits"
            class="btn btn-secondary whitespace-nowrap"
          >
            {{ t('admin.users.deductAllLoadFactorCredits') }}
          </button>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.users.notes') }}</label>
        <textarea v-model="form.notes" rows="3" class="input"></textarea>
      </div>

      <div v-if="form.amount > 0" class="rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950">
        <div class="flex items-center justify-between text-sm">
          <span class="text-gray-700 dark:text-gray-300">{{ t('admin.users.newLoadFactorCredits') }}:</span>
          <span class="font-bold text-gray-900 dark:text-gray-100">{{ calculateNewCredits() }}</span>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button
          type="submit"
          form="load-factor-credits-form"
          :disabled="submitting || !form.amount"
          class="btn"
          :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'"
        >
          {{ submitting ? t('common.saving') : t('common.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean, user: AdminUser | null, operation: 'add' | 'subtract' }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const submitting = ref(false)
const form = reactive({ amount: 0, notes: '' })
const currentCredits = computed(() => Math.max(0, Math.trunc(Number(props.user?.load_factor_credits_balance || 0))))

watch(() => props.show, (visible) => {
  if (visible) {
    form.amount = 0
    form.notes = ''
  }
})

function fillAllCredits() {
  form.amount = currentCredits.value
}

function calculateNewCredits() {
  const amount = Math.trunc(Number(form.amount || 0))
  return props.operation === 'add'
    ? currentCredits.value + amount
    : Math.max(0, currentCredits.value - amount)
}

async function handleSubmit() {
  if (!props.user) return
  const amount = Math.trunc(Number(form.amount))
  if (!Number.isFinite(amount) || amount <= 0) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }
  if (props.operation === 'subtract' && amount > currentCredits.value) {
    appStore.showError(t('admin.users.insufficientLoadFactorCredits'))
    return
  }
  submitting.value = true
  try {
    await adminAPI.users.updateLoadFactorCredits(props.user.id, amount, props.operation, form.notes)
    appStore.showSuccess(t('common.success'))
    emit('success')
    emit('close')
  } catch (e: any) {
    console.error('Failed to update load factor credits:', e)
    appStore.showError(e.response?.data?.detail || t('common.error'))
  } finally {
    submitting.value = false
  }
}
</script>
