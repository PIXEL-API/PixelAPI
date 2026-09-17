<template>
  <Teleport to="#dialog-root">
    <Transition :name="placement === 'right' ? 'modal-drawer' : 'modal'">
      <div
        v-if="show"
        class="modal-overlay ui-overlay"
        :class="{ 'modal-overlay-right': placement === 'right' }"
        :data-ui-skin="uiSkin"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div
          ref="dialogRef"
          :class="['modal-content', 'ui-dialog-panel', { 'modal-content-right': placement === 'right' }, widthClasses, panelClass]"
          tabindex="-1"
          @click.stop
        >
          <!-- Header -->
          <div class="modal-header ui-dialog-header">
            <div class="flex min-w-0 flex-1 items-center gap-3">
              <slot name="title-prefix"></slot>
              <h3 :id="dialogId" class="modal-title">
                {{ title }}
              </h3>
              <slot name="title-extra"></slot>
            </div>
            <button
              type="button"
              :disabled="closeDisabled"
              :aria-disabled="closeDisabled"
              @click="requestClose"
              class="-mr-2 ml-3 inline-flex min-h-11 min-w-11 cursor-pointer items-center justify-center rounded-xl text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300"
              :class="closeDisabled && 'cursor-not-allowed opacity-50 hover:bg-transparent hover:text-gray-400 dark:hover:bg-transparent dark:hover:text-dark-500'"
              :aria-label="t('common.close')"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div :class="['modal-body', 'ui-dialog-body', bodyClass]">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer ui-dialog-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useUiSkin } from '@/composables/useUiSkin'
import { nextDialogId, useModalDialog } from '@/composables/useModalDialog'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = nextDialogId()
const { t } = useI18n()
const uiSkin = useUiSkin()

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)

type DialogWidth = 'narrow' | 'normal' | 'medium' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  placement?: 'center' | 'right'
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  closeDisabled?: boolean
  panelClass?: string
  bodyClass?: string
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  placement: 'center',
  closeOnEscape: true,
  closeOnClickOutside: false,
  closeDisabled: false,
  panelClass: '',
  bodyClass: '',
  zIndex: 50
})

const emit = defineEmits<Emits>()

// Custom z-index style (overrides the default var(--ui-z-modal) = 50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  if (props.placement === 'right') return ''

  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    medium: 'w-full sm:max-w-xl md:max-w-2xl',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
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
.modal-overlay.modal-overlay-right {
  align-items: stretch;
  justify-content: flex-end;
  overflow: hidden;
  padding: 0;
}

.modal-content.modal-content-right {
  width: 100%;
  max-width: 100%;
  height: 100dvh;
  max-height: 100dvh;
  border-top: 0;
  border-right: 0;
  border-bottom: 0;
  border-radius: 0;
}

.modal-content-right > .modal-body {
  min-height: 0;
  overscroll-behavior-y: contain;
}

@media (min-width: 640px) {
  .modal-content.modal-content-right {
    /* Set this property through panelClass to customize a drawer's desktop width. */
    max-width: min(100vw, var(--dialog-drawer-width, 48rem));
  }
}

.modal-drawer-enter-active {
  transition: opacity 250ms ease-out;
}

.modal-drawer-leave-active {
  transition: opacity 200ms ease-in;
}

.modal-drawer-enter-active .modal-content-right {
  transition: transform 250ms ease-out;
}

.modal-drawer-leave-active .modal-content-right {
  transition: transform 200ms ease-in;
}

.modal-drawer-enter-from,
.modal-drawer-leave-to {
  opacity: 0;
}

.modal-drawer-enter-from .modal-content-right,
.modal-drawer-leave-to .modal-content-right {
  transform: translateX(100%);
}

@media (prefers-reduced-motion: reduce) {
  .modal-drawer-enter-active,
  .modal-drawer-leave-active,
  .modal-drawer-enter-active .modal-content-right,
  .modal-drawer-leave-active .modal-content-right {
    transition-duration: 1ms;
  }

  .modal-drawer-enter-from .modal-content-right,
  .modal-drawer-leave-to .modal-content-right {
    transform: none;
  }
}
</style>
