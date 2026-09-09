<template>
  <nav class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 p-3 dark:border-dark-700" :aria-label="label">
    <span class="text-sm text-gray-500 dark:text-dark-300">第 {{ page }} 页 · 至少 {{ total }} 条</span>
    <div class="flex gap-2">
      <button type="button" class="btn-secondary min-h-11" :disabled="loading || page <= 1" @click="emit('update:page', page - 1)">上一页</button>
      <button type="button" class="btn-secondary min-h-11" :disabled="loading || !hasMore" @click="emit('update:page', page + 1)">下一页</button>
    </div>
  </nav>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  page: number
  total: number
  hasMore: boolean
  loading?: boolean
  label?: string
}>(), { loading: false, label: '列表分页' })

const emit = defineEmits<{ 'update:page': [page: number] }>()
</script>
