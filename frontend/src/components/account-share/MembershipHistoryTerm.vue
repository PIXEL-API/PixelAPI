<template>
  <div class="min-w-0">
    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ label }}</span>
    <strong class="mt-1 block break-words text-sm font-medium leading-6 text-slate-900 dark:text-white">
      {{ value }}
    </strong>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label: string
  value: string
}>()
</script>
