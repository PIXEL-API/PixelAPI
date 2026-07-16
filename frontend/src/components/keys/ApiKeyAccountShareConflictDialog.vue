<template>
  <BaseDialog
    :show="show"
    :title="dialogTitle"
    width="normal"
    :close-on-escape="!navigating"
    @close="handleClose"
  >
    <div class="space-y-5">
      <div
        class="flex gap-3 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-amber-950 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-100"
      >
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-100 text-amber-700 dark:bg-amber-900/60 dark:text-amber-300">
          <Icon name="exclamationTriangle" size="lg" :stroke-width="2" />
        </div>
        <div class="min-w-0">
          <p class="break-words text-base font-semibold leading-6">
            {{ t('keys.accountShareConflict.blockedKey', { name: keyName }) }}
          </p>
          <p class="mt-1 text-sm leading-6 text-amber-800 dark:text-amber-200">
            {{ t('keys.accountShareConflict.description') }}
          </p>
        </div>
      </div>

      <div v-if="hasCounts" class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div class="rounded-2xl border border-red-200 bg-red-50 p-4 dark:border-red-900/70 dark:bg-red-950/25">
          <div class="flex items-center gap-2 text-red-700 dark:text-red-300">
            <Icon name="bolt" size="sm" :stroke-width="2" />
            <span class="text-sm font-medium">{{ t('keys.accountShareConflict.activeLabel') }}</span>
          </div>
          <p class="mt-2 text-2xl font-bold text-red-900 dark:text-red-100">{{ activeCount }}</p>
        </div>
        <div class="rounded-2xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/70 dark:bg-amber-950/25">
          <div class="flex items-center gap-2 text-amber-700 dark:text-amber-300">
            <Icon name="clock" size="sm" :stroke-width="2" />
            <span class="text-sm font-medium">{{ t('keys.accountShareConflict.queuedLabel') }}</span>
          </div>
          <p class="mt-2 text-2xl font-bold text-amber-900 dark:text-amber-100">{{ queuedCount }}</p>
        </div>
      </div>

      <div v-else class="rounded-xl border border-gray-200 bg-gray-50 px-4 py-3 text-sm leading-6 text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200">
        {{ t('keys.accountShareConflict.detailsUnavailable') }}
      </div>

      <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-600">
        <p class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('keys.accountShareConflict.stepsTitle') }}
        </p>
        <ol class="mt-3 space-y-3 text-sm leading-6 text-gray-600 dark:text-dark-300">
          <li v-for="(step, index) in steps" :key="step" class="flex gap-3">
            <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
              {{ index + 1 }}
            </span>
            <span>{{ step }}</span>
          </li>
        </ol>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button
          type="button"
          class="min-h-11 w-full rounded-xl border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-700 dark:focus:ring-offset-dark-900 sm:w-auto"
          :disabled="navigating"
          @click="handleClose"
        >
          {{ t('keys.accountShareConflict.later') }}
        </button>
        <button
          type="button"
          class="inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-amber-600 px-4 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-amber-700 focus:outline-none focus:ring-2 focus:ring-amber-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:focus:ring-offset-dark-900 sm:w-auto"
          :disabled="navigating"
          @click="emit('resolve')"
        >
          <span>{{ t('keys.accountShareConflict.resolve') }}</span>
          <Icon name="arrowRight" size="sm" :stroke-width="2" />
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

type BlockedAction = 'delete' | 'change_group'

const props = defineProps<{
  show: boolean
  action: BlockedAction
  keyName: string
  activeCount: number | null
  queuedCount: number | null
  navigating?: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'resolve'): void
}>()

const { t } = useI18n()

const dialogTitle = computed(() => (
  props.action === 'delete'
    ? t('keys.accountShareConflict.deleteTitle')
    : t('keys.accountShareConflict.changeGroupTitle')
))

const hasCounts = computed(() => props.activeCount !== null && props.queuedCount !== null)

const steps = computed(() => [
  t('keys.accountShareConflict.stepOpen'),
  t('keys.accountShareConflict.stepResolve'),
  t('keys.accountShareConflict.stepRetry')
])

function handleClose(): void {
  if (!props.navigating) emit('close')
}
</script>
