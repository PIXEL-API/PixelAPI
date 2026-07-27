<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        class="modal-overlay"
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
          :class="['modal-content', widthClasses, panelClass]"
          tabindex="-1"
          @click.stop
        >
          <!-- Header -->
          <div class="modal-header">
            <div class="flex min-w-0 flex-1 items-center gap-3">
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
          <div :class="['modal-body', bodyClass]">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let bodyScrollLockCount = 0
let dialogIdCounter = 0

type DialogStackEntry = {
  id: symbol
  getPanel: () => HTMLElement | null
  getZIndex: () => number
  focusInitial: () => void
  restoreTarget: HTMLElement | null
  activationOrder: number
}

const activeDialogStack: DialogStackEntry[] = []
let dialogActivationCounter = 0

function registerActiveDialog(entry: DialogStackEntry): void {
  const existingIndex = activeDialogStack.findIndex((item) => item.id === entry.id)
  if (existingIndex >= 0) {
    activeDialogStack.splice(existingIndex, 1)
  }
  entry.activationOrder = ++dialogActivationCounter
  activeDialogStack.push(entry)
}

function unregisterActiveDialog(entry: DialogStackEntry): HTMLElement | null {
  const index = activeDialogStack.findIndex((item) => item.id === entry.id)
  if (index < 0) return entry.restoreTarget

  const panel = entry.getPanel()
  for (let childIndex = index + 1; childIndex < activeDialogStack.length; childIndex += 1) {
    const childEntry = activeDialogStack[childIndex]
    if (panel?.contains(childEntry.restoreTarget)) {
      childEntry.restoreTarget = entry.restoreTarget
    }
  }

  activeDialogStack.splice(index, 1)
  return entry.restoreTarget
}

function getTopActiveDialog(): DialogStackEntry | undefined {
  let topDialog: DialogStackEntry | undefined
  for (const entry of activeDialogStack) {
    if (
      !topDialog ||
      entry.getZIndex() > topDialog.getZIndex() ||
      (entry.getZIndex() === topDialog.getZIndex() &&
        entry.activationOrder > topDialog.activationOrder)
    ) {
      topDialog = entry
    }
  }
  return topDialog
}
</script>

<script setup lang="ts">
import { computed, watch, onMounted, onBeforeUnmount, ref, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useUiSkin } from '@/composables/useUiSkin'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`
const { t } = useI18n()
const uiSkin = useUiSkin()

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
let hasBodyScrollLock = false
let isDialogRegistered = false
let focusRequestVersion = 0

const focusableSelector = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  'object',
  'embed',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

const isAvailableFocusTarget = (element: HTMLElement | null): element is HTMLElement => {
  if (!element || !element.isConnected) return false
  if (element.tabIndex < 0) return false
  if (element.matches(':disabled') || element.closest('[hidden], [inert], [aria-hidden="true"]')) {
    return false
  }
  let current: HTMLElement | null = element
  while (current) {
    const style = window.getComputedStyle(current)
    if (style.display === 'none' || style.visibility === 'hidden') return false
    current = current.parentElement
  }
  return true
}

const getFocusableElements = (): HTMLElement[] => {
  if (!dialogRef.value) return []
  return Array.from(dialogRef.value.querySelectorAll<HTMLElement>(focusableSelector)).filter(
    isAvailableFocusTarget
  )
}

const focusInitialElement = () => {
  const panel = dialogRef.value
  if (!panel) return
  const firstFocusable = getFocusableElements()[0]
  const focusTarget = firstFocusable ?? panel
  focusTarget.focus()
}

function lockBodyScroll(): void {
  if (hasBodyScrollLock) return
  hasBodyScrollLock = true
  bodyScrollLockCount += 1
  document.body.classList.add('modal-open')
}

function unlockBodyScroll(): void {
  if (!hasBodyScrollLock) return
  hasBodyScrollLock = false
  bodyScrollLockCount = Math.max(0, bodyScrollLockCount - 1)
  if (bodyScrollLockCount === 0) {
    document.body.classList.remove('modal-open')
  }
}

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
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
  closeOnEscape: true,
  closeOnClickOutside: false,
  closeDisabled: false,
  panelClass: '',
  bodyClass: '',
  zIndex: 50
})

const emit = defineEmits<Emits>()

const dialogEntry: DialogStackEntry = {
  id: Symbol(dialogId),
  getPanel: () => dialogRef.value,
  getZIndex: () => props.zIndex,
  focusInitial: focusInitialElement,
  restoreTarget: null,
  activationOrder: 0
}

const isTopDialog = () => getTopActiveDialog()?.id === dialogEntry.id

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
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

const getOwnedFocusPortalTrigger = (
  eventTarget: EventTarget | null,
  panel: HTMLElement
): HTMLElement | null => {
  if (!(eventTarget instanceof Element)) return null

  const portal = eventTarget.closest<HTMLElement>('[data-dialog-focus-owner-id]')
  const ownerId = portal?.dataset.dialogFocusOwnerId
  if (!ownerId) return null

  const owner = document.getElementById(ownerId)
  if (!(owner instanceof HTMLElement) || !panel.contains(owner)) return null
  return isAvailableFocusTarget(owner) ? owner : null
}

const trapFocus = (event: KeyboardEvent) => {
  const panel = dialogRef.value
  if (!panel) return

  const focusableElements = getFocusableElements()
  if (focusableElements.length === 0) {
    event.preventDefault()
    panel.focus()
    return
  }

  const firstFocusable = focusableElements[0]
  const lastFocusable = focusableElements[focusableElements.length - 1]
  const portalOwner = getOwnedFocusPortalTrigger(event.target, panel)
  if (portalOwner) {
    const ownerIndex = focusableElements.indexOf(portalOwner)
    if (ownerIndex >= 0) {
      event.preventDefault()
      const nextIndex = event.shiftKey
        ? (ownerIndex - 1 + focusableElements.length) % focusableElements.length
        : (ownerIndex + 1) % focusableElements.length
      const focusTarget = focusableElements[nextIndex]
      void nextTick(() => {
        if (props.show && isTopDialog() && isAvailableFocusTarget(focusTarget)) {
          focusTarget.focus()
        }
      })
      return
    }
  }

  const activeElement = document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null
  const activeIndex = activeElement ? focusableElements.indexOf(activeElement) : -1

  if (!activeElement || !panel.contains(activeElement) || activeElement === panel || activeIndex < 0) {
    event.preventDefault()
    const focusTarget = event.shiftKey ? lastFocusable : firstFocusable
    focusTarget.focus()
    return
  }

  if (event.shiftKey && activeElement === firstFocusable) {
    event.preventDefault()
    lastFocusable.focus()
  } else if (!event.shiftKey && activeElement === lastFocusable) {
    event.preventDefault()
    firstFocusable.focus()
  }
}

const handleDocumentKeydown = (event: KeyboardEvent) => {
  if (!props.show || !isTopDialog() || event.defaultPrevented) return

  if (event.key === 'Escape' && props.closeOnEscape && !props.closeDisabled) {
    event.preventDefault()
    event.stopPropagation()
    requestClose()
    return
  }

  if (event.key === 'Tab') {
    trapFocus(event)
  }
}

const restoreFocusAfterClose = (target: HTMLElement | null) => {
  void nextTick(() => {
    const topDialog = getTopActiveDialog()
    if (topDialog) {
      const topPanel = topDialog.getPanel()
      if (target && topPanel?.contains(target) && isAvailableFocusTarget(target)) {
        target.focus()
      } else if (!topPanel?.contains(document.activeElement)) {
        topDialog.focusInitial()
      }
      return
    }

    if (isAvailableFocusTarget(target)) target.focus()
  })
}

const activateDialog = async () => {
  const requestVersion = ++focusRequestVersion
  dialogEntry.restoreTarget = document.activeElement instanceof HTMLElement
    ? document.activeElement
    : null
  registerActiveDialog(dialogEntry)
  isDialogRegistered = true
  lockBodyScroll()

  await nextTick()
  if (requestVersion === focusRequestVersion && props.show && isTopDialog()) {
    focusInitialElement()
  }
}

const deactivateDialog = (restoreFocus: boolean) => {
  if (!isDialogRegistered && !hasBodyScrollLock && !dialogEntry.restoreTarget) return
  focusRequestVersion += 1
  const restoreTarget = isDialogRegistered
    ? unregisterActiveDialog(dialogEntry)
    : dialogEntry.restoreTarget
  isDialogRegistered = false
  dialogEntry.restoreTarget = null
  unlockBodyScroll()
  if (restoreFocus) restoreFocusAfterClose(restoreTarget)
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  (isOpen) => {
    if (isOpen) {
      void activateDialog()
    } else {
      deactivateDialog(true)
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleDocumentKeydown)
  deactivateDialog(true)
})
</script>
