import { nextTick, onBeforeUnmount, onMounted, watch, type Ref } from 'vue'

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

/** 生成唯一的对话框 aria-labelledby id，避免多弹窗并存时冲突 */
export function nextDialogId(prefix = 'modal-title'): string {
  return `${prefix}-${++dialogIdCounter}`
}

export interface UseModalDialogOptions {
  /** 弹窗是否打开 */
  show: () => boolean
  /** 面板元素（焦点陷阱与焦点恢复的边界） */
  panel: Ref<HTMLElement | null>
  /** 弹窗层级，用于判定顶层弹窗 */
  zIndex: () => number
  /** 是否允许 Esc 关闭，默认允许 */
  closeOnEscape?: () => boolean
  /** 是否禁止关闭（如提交中），默认不禁止 */
  closeDisabled?: () => boolean
  /** 请求关闭时回调（通常 emit('close')） */
  onRequestClose: () => void
}

/**
 * 模态框共享行为：焦点陷阱、Esc 关闭、弹窗栈、焦点恢复、body 滚动锁定。
 * BaseDialog 与自定义样式的 ModalShell 共用此实现。
 */
export function useModalDialog(options: UseModalDialogOptions) {
  const show = options.show
  const panel = options.panel
  const closeOnEscape = options.closeOnEscape ?? (() => true)
  const closeDisabled = options.closeDisabled ?? (() => false)

  let hasBodyScrollLock = false
  let isDialogRegistered = false
  let focusRequestVersion = 0

  const getFocusableElements = (): HTMLElement[] => {
    if (!panel.value) return []
    return Array.from(panel.value.querySelectorAll<HTMLElement>(focusableSelector)).filter(
      isAvailableFocusTarget
    )
  }

  const focusInitialElement = () => {
    const target = panel.value
    if (!target) return
    const firstFocusable = getFocusableElements()[0]
    const focusTarget = firstFocusable ?? target
    focusTarget.focus()
  }

  const lockBodyScroll = (): void => {
    if (hasBodyScrollLock) return
    hasBodyScrollLock = true
    bodyScrollLockCount += 1
    document.body.classList.add('modal-open')
  }

  const unlockBodyScroll = (): void => {
    if (!hasBodyScrollLock) return
    hasBodyScrollLock = false
    bodyScrollLockCount = Math.max(0, bodyScrollLockCount - 1)
    if (bodyScrollLockCount === 0) {
      document.body.classList.remove('modal-open')
    }
  }

  const dialogEntry: DialogStackEntry = {
    id: Symbol('modal-dialog'),
    getPanel: () => panel.value,
    getZIndex: () => options.zIndex(),
    focusInitial: focusInitialElement,
    restoreTarget: null,
    activationOrder: 0
  }

  const isTopDialog = () => getTopActiveDialog()?.id === dialogEntry.id

  const getOwnedFocusPortalTrigger = (
    eventTarget: EventTarget | null,
    panelEl: HTMLElement
  ): HTMLElement | null => {
    if (!(eventTarget instanceof Element)) return null

    const portal = eventTarget.closest<HTMLElement>('[data-dialog-focus-owner-id]')
    const ownerId = portal?.dataset.dialogFocusOwnerId
    if (!ownerId) return null

    const owner = document.getElementById(ownerId)
    if (!(owner instanceof HTMLElement) || !panelEl.contains(owner)) return null
    return isAvailableFocusTarget(owner) ? owner : null
  }

  const trapFocus = (event: KeyboardEvent) => {
    const panelEl = panel.value
    if (!panelEl) return

    const focusableElements = getFocusableElements()
    if (focusableElements.length === 0) {
      event.preventDefault()
      panelEl.focus()
      return
    }

    const firstFocusable = focusableElements[0]
    const lastFocusable = focusableElements[focusableElements.length - 1]
    const portalOwner = getOwnedFocusPortalTrigger(event.target, panelEl)
    if (portalOwner) {
      const ownerIndex = focusableElements.indexOf(portalOwner)
      if (ownerIndex >= 0) {
        event.preventDefault()
        const nextIndex = event.shiftKey
          ? (ownerIndex - 1 + focusableElements.length) % focusableElements.length
          : (ownerIndex + 1) % focusableElements.length
        const focusTarget = focusableElements[nextIndex]
        void nextTick(() => {
          if (show() && isTopDialog() && isAvailableFocusTarget(focusTarget)) {
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

    if (!activeElement || !panelEl.contains(activeElement) || activeElement === panelEl || activeIndex < 0) {
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
    if (!show() || !isTopDialog() || event.defaultPrevented) return

    if (event.key === 'Escape' && closeOnEscape() && !closeDisabled()) {
      event.preventDefault()
      event.stopPropagation()
      options.onRequestClose()
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
    if (requestVersion === focusRequestVersion && show() && isTopDialog()) {
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

  watch(
    show,
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
}
