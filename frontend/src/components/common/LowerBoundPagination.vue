<template>
  <nav class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 p-3 dark:border-dark-700" :aria-label="resolvedLabel">
    <span class="text-sm text-gray-500 dark:text-dark-300">{{ t('common.pageIndicatorAtLeast', { page, total }) }}</span>
    <div class="flex gap-2">
      <button type="button" class="btn-secondary min-h-11" :disabled="loading || page <= 1" @click="emit('update:page', page - 1)">{{ t('common.prevPage') }}</button>
      <button type="button" class="btn-secondary min-h-11" :disabled="loading || !hasMore" @click="emit('update:page', page + 1)">{{ t('common.nextPage') }}</button>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = withDefaults(defineProps<{
  page: number
  total: number
  hasMore: boolean
  loading?: boolean
  label?: string
}>(), { loading: false })

const emit = defineEmits<{ 'update:page': [page: number] }>()
const resolvedLabel = computed(() => props.label || t('common.listPagination'))
</script>
