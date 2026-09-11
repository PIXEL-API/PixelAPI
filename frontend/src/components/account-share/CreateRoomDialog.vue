<template>
  <BaseDialog
    :show="show"
    title="创建房间"
    width="full"
    :close-disabled="busy || closeDisabled"
    :close-on-click-outside="true"
    panel-class="create-room-dialog-panel"
    body-class="create-room-dialog-body"
    @close="emit('close')"
  >
    <div class="create-room-dialog-shell">
      <div class="create-room-dialog-intro">
        <div class="min-w-0">
          <p class="text-sm leading-6 text-gray-500 dark:text-dark-300">
            优先选择已经登录的自有账号创建房间，无需删除账号或重新 OAuth。
          </p>
          <p
            v-if="busy"
            class="mt-1 text-xs font-medium text-primary-600 dark:text-primary-300"
            role="status"
            aria-live="polite"
          >
            正在处理，请勿关闭窗口。
          </p>
        </div>
        <button
        class="btn btn-secondary min-h-11 w-full shrink-0 sm:w-auto"
          type="button"
          :disabled="busy || closeDisabled"
          @click="emit('reset')"
        >
          <Icon name="refresh" size="sm" class="mr-2" />
          重置
        </button>
      </div>

      <slot />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

interface Props {
  show: boolean
  busy?: boolean
  closeDisabled?: boolean
}

interface Emits {
  (event: 'close'): void
  (event: 'reset'): void
}

withDefaults(defineProps<Props>(), {
  busy: false,
  closeDisabled: false,
})

const emit = defineEmits<Emits>()
</script>

<style>
.create-room-dialog-panel {
  width: min(84rem, calc(100vw - 2rem));
  max-width: min(84rem, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
}

.create-room-dialog-body {
  display: flex;
  min-height: 0;
  padding: 0;
  overflow: hidden;
  overscroll-behavior: contain;
}

.create-room-dialog-shell {
  display: flex;
  min-height: 0;
  max-height: 100%;
  flex: 1 1 auto;
  flex-direction: column;
  background: rgb(248 250 252);
  overflow-y: auto;
}

.create-room-dialog-intro {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  padding: 1rem;
}

.dark .create-room-dialog-shell {
  background: rgb(24 24 27);
}

.dark .create-room-dialog-intro {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

@media (max-width: 1023px) {
  .create-room-dialog-shell .create-room-submit-stage {
    position: sticky;
    bottom: 0;
    z-index: 10;
    box-shadow: 0 -0.5rem 1.25rem rgb(15 23 42 / 0.08);
  }
}

@media (min-width: 640px) {
  .create-room-dialog-panel {
    max-height: 94dvh;
  }

  .create-room-dialog-shell {
    min-height: 0;
  }

  .create-room-dialog-intro {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.5rem;
  }
}

/* Keep the primary action in view on wide screens while the form scrolls. */
@media (min-width: 1024px) {
  .create-room-dialog-shell .create-room-workspace {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 23rem);
    align-items: start;
    gap: 1rem;
    padding: 1.25rem 1.5rem;
  }

  .create-room-dialog-shell .create-room-form-flow,
  .create-room-dialog-shell .create-room-submit-stage {
    padding: 0;
  }

  .create-room-dialog-shell .create-room-submit-stage {
    position: sticky;
    top: 0;
    border-top: 0;
    border-left: 1px solid rgb(226 232 240);
    background: transparent;
    padding-left: 1rem;
  }

  .create-room-dialog-shell .create-room-submit-content {
    position: sticky;
    top: 0;
    grid-template-columns: minmax(0, 1fr);
    border: 1px solid rgb(191 219 254);
    border-radius: 0.875rem;
    background: rgb(239 246 255 / 0.72);
    padding: 1rem;
  }

  .create-room-dialog-shell .create-room-submit-content > :not(.create-room-stage-heading):not(.create-room-submit-button) {
    grid-column: auto;
  }

  .create-room-dialog-shell .create-room-submit-button {
    width: 100%;
  }

  .dark .create-room-dialog-shell .create-room-submit-stage {
    border-left-color: rgb(63 63 70);
  }

  .dark .create-room-dialog-shell .create-room-submit-content {
    border-color: rgb(30 64 175);
    background: rgb(30 64 175 / 0.14);
  }
}
</style>
