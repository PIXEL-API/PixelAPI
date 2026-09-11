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
        <div class="create-room-intro-copy">
          <span class="create-room-intro-kicker">房间配置向导</span>
          <p>
            用已有自有账号快速创建共享房间，凭证、代理和运行时能力都会沿用账号配置。
          </p>
          <p
            v-if="busy"
            class="create-room-intro-status"
            role="status"
            aria-live="polite"
          >
            正在处理，请勿关闭窗口。
          </p>
        </div>
        <div class="create-room-intro-actions">
          <span class="create-room-intro-progress">4 个步骤 · 约 1 分钟</span>
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
      </div>

      <div class="create-room-stepper" aria-label="创建房间步骤">
        <div v-for="(step, index) in steps" :key="step.title" class="create-room-step" :class="{ 'create-room-step-active': index === 0 }">
          <span class="create-room-step-index">{{ index + 1 }}</span>
          <span>
            <strong>{{ step.title }}</strong>
            <small>{{ step.description }}</small>
          </span>
        </div>
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

const steps = [
  { title: '选择账号', description: '账号来源' },
  { title: '设置规则', description: '计费与并发' },
  { title: '保护模型', description: '白名单与额度' },
  { title: '确认创建', description: '检查并提交' },
]
</script>

<style>
.create-room-dialog-panel {
  width: min(88rem, calc(100vw - 2rem));
  max-width: min(88rem, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  overflow: hidden;
  border-color: rgb(203 213 225);
  box-shadow: 0 2rem 5rem rgb(15 23 42 / 0.2), 0 0.5rem 1.5rem rgb(15 23 42 / 0.08);
}

.create-room-dialog-panel > .modal-header {
  position: relative;
  min-height: 4.5rem;
  border-bottom-color: rgb(226 232 240);
  background:
    radial-gradient(circle at 0% 0%, rgb(219 234 254 / 0.72), transparent 36%),
    linear-gradient(120deg, rgb(255 255 255), rgb(248 250 252));
  padding: 1rem 1.5rem;
}

.create-room-dialog-panel > .modal-header::after {
  position: absolute;
  right: 1.5rem;
  bottom: -1px;
  left: 1.5rem;
  height: 2px;
  background: linear-gradient(90deg, rgb(59 130 246 / 0.65), rgb(56 189 248 / 0.12), transparent);
  content: '';
}

.create-room-dialog-panel > .modal-header .modal-title {
  color: rgb(15 23 42);
  font-size: 1.125rem;
  letter-spacing: -0.015em;
}

.create-room-dialog-panel > .modal-header button {
  border: 1px solid transparent;
}

.create-room-dialog-panel > .modal-header button:hover {
  border-color: rgb(191 219 254);
  background: rgb(239 246 255);
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
  background:
    radial-gradient(circle at 100% 0%, rgb(224 242 254 / 0.42), transparent 24rem),
    rgb(246 249 252);
  overflow-y: auto;
  scrollbar-gutter: stable;
}

.create-room-dialog-intro {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.82);
  padding: 1.125rem 1.5rem;
  backdrop-filter: blur(0.75rem);
}

.create-room-intro-copy > p:not(.create-room-intro-status) {
  max-width: 54rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.55;
}

.create-room-intro-copy,
.create-room-intro-actions {
  min-width: 0;
}

.create-room-intro-copy {
  display: grid;
  gap: 0.25rem;
}

.create-room-intro-kicker {
  color: rgb(37 99 235);
  font-size: 0.6875rem;
  font-weight: 800;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

.create-room-intro-copy p {
  margin: 0;
}

.create-room-intro-status {
  color: rgb(37 99 235) !important;
  font-size: 0.75rem !important;
  font-weight: 700;
}

.create-room-intro-actions {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.create-room-intro-progress {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
}

.create-room-stepper {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.75rem;
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.72);
  padding: 0.875rem 1.5rem 1rem;
}

.create-room-step {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
  color: rgb(100 116 139);
}

.create-room-step-index {
  display: inline-flex;
  height: 1.75rem;
  width: 1.75rem;
  flex: 0 0 1.75rem;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(203 213 225);
  border-radius: 9999px;
  background: rgb(248 250 252);
  font-size: 0.75rem;
  font-weight: 800;
}

.create-room-step > span:last-child {
  display: grid;
  min-width: 0;
  gap: 0.125rem;
}

.create-room-step strong,
.create-room-step small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.create-room-step strong {
  color: rgb(51 65 85);
  font-size: 0.75rem;
  font-weight: 750;
}

.create-room-step small {
  font-size: 0.6875rem;
}

.create-room-step-active {
  color: rgb(37 99 235);
}

.create-room-step-active .create-room-step-index {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
  color: rgb(37 99 235);
  box-shadow: 0 0 0 3px rgb(219 234 254 / 0.7);
}

.create-room-step-active strong {
  color: rgb(30 64 175);
}

.create-room-dialog-intro button {
  min-width: 5.5rem;
  border-radius: 0.75rem;
}

.create-room-dialog-shell .create-capability-summary {
  margin: 1rem 1.5rem 0;
  border: 1px solid rgb(186 230 253);
  border-radius: 0.875rem;
  background: linear-gradient(105deg, rgb(239 246 255 / 0.95), rgb(240 249 255 / 0.72));
  padding: 0.75rem 1rem;
  box-shadow: 0 0.5rem 1.25rem rgb(14 116 144 / 0.05);
}

.create-room-dialog-shell .create-room-source-stage {
  margin: 1rem 1.5rem 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 1rem;
  background: rgb(255 255 255 / 0.96);
  padding: 1.25rem;
  box-shadow: 0 0.75rem 1.75rem rgb(15 23 42 / 0.045);
}

.create-room-dialog-shell .create-room-source-stage > .create-room-stage-heading {
  align-items: center;
}

.create-room-dialog-shell .create-room-source-stage > .btn-secondary {
  margin-top: 1rem;
  border-radius: 0.75rem;
}

.create-room-dialog-shell .create-room-account-picker {
  margin-top: 1rem;
  border-radius: 0.875rem;
  background: rgb(248 250 252 / 0.9);
  padding: 1rem;
}

.dark .create-room-dialog-shell {
  background: rgb(24 24 27);
}

.dark .create-room-dialog-intro {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .create-room-dialog-panel {
  border-color: rgb(63 63 70);
  box-shadow: 0 2rem 5rem rgb(0 0 0 / 0.42), 0 0.5rem 1.5rem rgb(0 0 0 / 0.2);
}

.dark .create-room-dialog-panel > .modal-header {
  border-bottom-color: rgb(63 63 70);
  background:
    radial-gradient(circle at 0% 0%, rgb(30 64 175 / 0.24), transparent 36%),
    linear-gradient(120deg, rgb(39 39 42), rgb(24 24 27));
}

.dark .create-room-dialog-panel > .modal-header .modal-title {
  color: rgb(244 244 245);
}

.dark .create-room-dialog-panel > .modal-header button:hover {
  border-color: rgb(30 64 175);
  background: rgb(30 64 175 / 0.2);
}

.dark .create-room-intro-copy > p:not(.create-room-intro-status) {
  color: rgb(161 161 170);
}

.dark .create-room-intro-progress,
.dark .create-room-step {
  color: rgb(161 161 170);
}

.dark .create-room-stepper {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.82);
}

.dark .create-room-step-index {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
}

.dark .create-room-step strong {
  color: rgb(212 212 216);
}

.dark .create-room-step-active strong {
  color: rgb(147 197 253);
}

.dark .create-room-dialog-shell .create-room-source-stage {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.92);
  box-shadow: 0 0.75rem 1.75rem rgb(0 0 0 / 0.16);
}

.dark .create-room-dialog-shell .create-room-account-picker {
  background: rgb(24 24 27 / 0.82);
}

@media (max-width: 1023px) {
  .create-room-dialog-shell .create-room-submit-stage {
    position: sticky;
    bottom: 0;
    z-index: 10;
    border-top: 1px solid rgb(226 232 240);
    box-shadow: 0 -0.75rem 1.75rem rgb(15 23 42 / 0.1);
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
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 20rem);
    align-items: start;
    gap: 1.25rem;
    padding: 1rem 1.5rem 1.5rem;
  }

  .create-room-dialog-shell .create-room-form-flow,
  .create-room-dialog-shell .create-room-submit-stage {
    padding: 0;
  }

  .create-room-dialog-shell .create-room-submit-stage {
    position: sticky;
    top: 0;
    border-top: 0;
    min-width: 0;
    border-left: 0;
    background: transparent;
    padding-left: 0;
  }

  .create-room-dialog-shell .create-room-submit-content {
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    width: 100%;
    border: 1px solid rgb(191 219 254);
    min-width: 0;
    border-radius: 1rem;
    background: linear-gradient(160deg, rgb(239 246 255 / 0.95), rgb(248 250 252 / 0.9));
    padding: 1.125rem;
    box-shadow: 0 0.75rem 1.5rem rgb(37 99 235 / 0.07);
  }

  .create-room-dialog-shell .create-room-submit-content .create-room-stage-heading > div {
    min-width: 0;
    flex: 1 1 auto;
    writing-mode: horizontal-tb;
  }

  .create-room-dialog-shell .create-room-submit-content .create-room-stage-heading small {
    white-space: normal;
    word-break: normal;
    overflow-wrap: anywhere;
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

@media (max-width: 639px) {
  .create-room-dialog-panel {
    width: calc(100vw - 1rem);
    max-width: calc(100vw - 1rem);
    max-height: calc(100dvh - 1rem);
  }

  .create-room-dialog-panel > .modal-header {
    padding: 0.875rem 1rem;
  }

  .create-room-dialog-intro,
  .create-room-dialog-shell .create-room-source-stage {
    padding: 1rem;
  }

  .create-room-dialog-shell .create-capability-summary,
  .create-room-dialog-shell .create-room-source-stage {
    margin-right: 1rem;
    margin-left: 1rem;
  }

  .create-room-stepper {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    padding-inline: 1rem;
  }

  .create-room-intro-actions {
    justify-content: space-between;
  }
}
</style>
