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
  width: min(60rem, calc(100vw - 1rem));
  max-height: calc(100dvh - 1rem);
}

.create-room-dialog-body {
  padding: 0;
  overscroll-behavior: contain;
}

.create-room-dialog-shell {
  min-height: calc(100dvh - 8rem);
  background: rgb(248 250 252);
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

@media (min-width: 640px) {
  .create-room-dialog-panel {
    max-height: 90dvh;
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
</style>
