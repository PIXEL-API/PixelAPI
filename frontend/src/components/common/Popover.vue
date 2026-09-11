<template>
  <Teleport to="body">
    <div v-if="show && position" class="ui-popover-root" data-ui-overlay="popover">
      <div
        v-if="closeOnBackdrop"
        class="ui-popover-backdrop ui-action-menu-backdrop fixed inset-0 z-[9998]"
        aria-hidden="true"
        @click="emit('close')"
      />
      <div
        class="ui-popover-panel fixed z-[9999]"
        :class="panelClass"
        :style="{ top: `${position.top}px`, left: `${position.left}px` }"
        :role="role"
        @click.stop
      >
        <slot />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onUnmounted, watch } from 'vue'

const props = withDefaults(defineProps<{
  show: boolean
  position: { top: number; left: number } | null
  panelClass?: string
  closeOnBackdrop?: boolean
  closeOnEscape?: boolean
  role?: string
}>(), {
  closeOnBackdrop: false,
  closeOnEscape: true,
  role: undefined
})

const emit = defineEmits<{
  (event: 'close'): void
}>()

function handleKeydown(event: KeyboardEvent): void {
  if (props.closeOnEscape && event.key === 'Escape') emit('close')
}

watch(
  () => props.show,
  (visible) => {
    if (visible && props.closeOnEscape) window.addEventListener('keydown', handleKeydown)
    else window.removeEventListener('keydown', handleKeydown)
  },
  { immediate: true }
)

onUnmounted(() => window.removeEventListener('keydown', handleKeydown))
</script>
