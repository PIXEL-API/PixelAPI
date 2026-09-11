<template>
  <Popover
    :show="show"
    :position="position"
    :close-on-backdrop="true"
    panel-class="ui-menu action-menu-content w-52 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 dark:bg-dark-800"
    role="menu"
    @close="emit('close')"
  >
    <slot />
  </Popover>
</template>

<script setup lang="ts">
import Popover from './Popover.vue'

defineProps<{
  show: boolean
  position: { top: number; left: number } | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()
</script>
