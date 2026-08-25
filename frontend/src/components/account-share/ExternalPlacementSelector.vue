<template>
  <div class="space-y-4">
    <fieldset :disabled="disabled" class="space-y-3">
      <legend class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
        {{ legend || t('userAccounts.externalPlacement.destination') }}
      </legend>

      <label
        v-for="option in targetOptions"
        :key="option.value"
        :class="[
          'flex min-h-11 cursor-pointer gap-3 rounded-2xl border p-4 transition-colors',
          modelValue === option.value
            ? 'border-primary-500 bg-primary-50 ring-1 ring-primary-500/20 dark:border-primary-500 dark:bg-primary-950/25'
            : 'border-gray-200 bg-white hover:border-gray-300 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500 dark:hover:bg-dark-700',
          option.disabled ? 'cursor-not-allowed opacity-55' : ''
        ]"
      >
        <input
          :checked="modelValue === option.value"
          class="mt-1 h-5 w-5 shrink-0 border-gray-300 text-primary-600 focus:ring-primary-500"
          type="radio"
          :name="inputName"
          :value="option.value"
          :disabled="disabled || option.disabled"
          :data-testid="`placement-target-${option.value}`"
          @change="selectTarget(option.value)"
        />
        <span class="min-w-0">
          <span class="block text-sm font-semibold text-gray-900 dark:text-white">
            {{ option.label }}
          </span>
          <span class="mt-1 block text-sm leading-6 text-gray-500 dark:text-dark-300">
            {{ option.description }}
          </span>
          <span
            v-if="option.disabled && option.disabledReason"
            class="mt-1 block text-xs leading-5 text-amber-700 dark:text-amber-300"
            :data-testid="`placement-disabled-reason-${option.value}`"
          >
            {{ option.disabledReason }}
          </span>
        </span>
      </label>
    </fieldset>

    <div
      v-if="showPreservationHint"
      class="flex gap-3 rounded-2xl border border-sky-200 bg-sky-50 p-4 text-sm leading-6 text-sky-900 dark:border-sky-900/70 dark:bg-sky-950/25 dark:text-sky-200"
    >
      <Icon class="mt-0.5 shrink-0" name="infoCircle" size="md" :stroke-width="2" />
      <p>{{ t('userAccounts.externalPlacement.credentialsPreserved') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type {
  AccountExternalPlacementTarget,
  AccountPlatform
} from '@/types'

interface TargetOption {
  value: AccountExternalPlacementTarget
  label: string
  description: string
  disabled: boolean
  disabledReason: string
}

const props = withDefaults(defineProps<{
  modelValue: AccountExternalPlacementTarget
  platform: AccountPlatform | ''
  disabled?: boolean
  inputName?: string
  legend?: string
  platformModeDisabledReason?: string
  // 每个目标各自的禁用原因；有原因即视为禁用。用于提前拦掉后端一定会拒绝的切换，
  // 避免「基础设置已保存、模式切换失败」这种半成功。
  targetDisabledReasons?: Partial<Record<AccountExternalPlacementTarget, string>>
  showPreservationHint?: boolean
}>(), {
  disabled: false,
  inputName: 'external-placement-target',
  legend: '',
  platformModeDisabledReason: '',
  targetDisabledReasons: () => ({}),
  showPreservationHint: true
})

const emit = defineEmits<{
  (event: 'update:modelValue', target: AccountExternalPlacementTarget): void
}>()

const { t } = useI18n()

const platformDisplayName = computed(() => {
  if (props.platform === 'openai') return 'OpenAI'
  if (props.platform === 'anthropic') return 'Anthropic'
  if (props.platform === 'opencode') return 'Opencode'
  return props.platform ? String(props.platform) : ''
})

const supportsPlatformMode = computed(() => (
  props.platform === 'openai' || props.platform === 'anthropic' || props.platform === 'opencode'
))

function explicitDisabledReason(target: AccountExternalPlacementTarget): string {
  // 当前已经生效的模式永远不禁用，否则选择器会显示成「没有任何选项被选中」。
  if (target === props.modelValue) return ''
  return props.targetDisabledReasons?.[target] || ''
}

const targetOptions = computed<TargetOption[]>(() => {
  const roomReason = supportsPlatformMode.value
    ? explicitDisabledReason('room')
    : (props.platformModeDisabledReason || t('userAccounts.externalPlacement.unsupportedPlatform'))
  return [
    {
      value: 'private',
      label: t('userAccounts.externalPlacement.privateTitle'),
      description: t('userAccounts.externalPlacement.privateDescription'),
      disabled: Boolean(explicitDisabledReason('private')),
      disabledReason: explicitDisabledReason('private')
    },
    {
      value: 'public_pool',
      label: t('userAccounts.externalPlacement.publicPoolTitle'),
      description: t('userAccounts.externalPlacement.publicPoolDescription'),
      disabled: Boolean(explicitDisabledReason('public_pool')),
      disabledReason: explicitDisabledReason('public_pool')
    },
    {
      value: 'room',
      label: t('userAccounts.externalPlacement.platformModeTitle', {
        platform: platformDisplayName.value
      }),
      description: t('userAccounts.externalPlacement.platformModeDescription', {
        platform: platformDisplayName.value
      }),
      disabled: !supportsPlatformMode.value || Boolean(roomReason),
      disabledReason: roomReason
    }
  ]
})

function selectTarget(target: AccountExternalPlacementTarget): void {
  if (target === props.modelValue) return
  emit('update:modelValue', target)
}
</script>
