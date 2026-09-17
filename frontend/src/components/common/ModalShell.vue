<template>
  <Teleport to="#dialog-root">
    <Transition :name="placement === 'right' ? 'modal-drawer' : 'modal'">
      <div
        v-if="show"
        class="modal-overlay ui-overlay"
        :class="[{ 'modal-overlay-right': placement === 'right' }, overlayClass]"
        :data-ui-skin="uiSkin"
        :style="zIndexStyle"
        @click.self="handleClose"
      >
        <div
          ref="dialogRef"
          class="modal-shell-panel"
          :class="panelClass"
          :role="role"
          aria-modal="true"
          :aria-labelledby="labelledby || undefined"
          :aria-label="labelledby ? undefined : title"
          tabindex="-1"
          @click.stop
        >
          <slot></slot>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useUiSkin } from '@/composables/useUiSkin'
import { useModalDialog } from '@/composables/useModalDialog'

/**
 * 无默认外观的模态框外壳：与 BaseDialog 共享焦点陷阱、Esc 关闭、
 * 弹窗栈、焦点恢复和 body 滚动锁定，但不渲染 header/footer，
 * 供视觉完全自定义的弹窗（确认卡、图片预览等）使用。
 */
interface Props {
  show: boolean
  /** 面板无可见标题元素时的无障碍名称 */
  title?: string
  /** 面板内标题元素 id，优先于 title */
  labelledby?: string
  role?: 'dialog' | 'alertdialog'
  placement?: 'center' | 'right'
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  closeDisabled?: boolean
  overlayClass?: string
  panelClass?: string
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  title: undefined,
  labelledby: undefined,
  role: 'dialog',
  placement: 'center',
  closeOnEscape: true,
  closeOnClickOutside: false,
  closeDisabled: false,
  overlayClass: '',
  panelClass: '',
  zIndex: 50
})

const emit = defineEmits<Emits>()

const uiSkin = useUiSkin()
const dialogRef = ref<HTMLElement | null>(null)

const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const requestClose = () => {
  if (props.closeDisabled) return
  emit('close')
}

const handleClose = () => {
  if (!props.closeOnClickOutside) return
  requestClose()
}

useModalDialog({
  show: () => props.show,
  panel: dialogRef,
  zIndex: () => props.zIndex,
  closeOnEscape: () => props.closeOnEscape,
  closeDisabled: () => props.closeDisabled,
  onRequestClose: requestClose
})
</script>

<style scoped>
.modal-shell-panel {
  @apply flex w-full justify-center outline-none;
}

.modal-overlay.modal-overlay-right {
  align-items: stretch;
  justify-content: flex-end;
  overflow: hidden;
  padding: 0;
}

.modal-overlay-right .modal-shell-panel {
  height: 100%;
  justify-content: flex-end;
}

.modal-drawer-enter-active {
  transition: opacity 250ms ease-out;
}

.modal-drawer-leave-active {
  transition: opacity 200ms ease-in;
}

.modal-drawer-enter-from,
.modal-drawer-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .modal-drawer-enter-active,
  .modal-drawer-leave-active {
    transition-duration: 1ms;
  }
}
</style>
