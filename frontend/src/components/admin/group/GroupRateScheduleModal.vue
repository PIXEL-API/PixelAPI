<template>
  <BaseDialog
    :show="show"
    :title="t('admin.groups.rateSchedulesTitle')"
    width="wide"
    @close="handleClose"
  >
    <div v-if="group" class="space-y-4">
      <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 px-4 py-2.5 text-sm dark:bg-dark-700">
        <span class="inline-flex items-center gap-1.5" :class="platformColorClass">
          <PlatformIcon :platform="group.platform" size="sm" />
          {{ t('admin.groups.platforms.' + group.platform) }}
        </span>
        <span class="text-gray-400">|</span>
        <span class="font-medium text-gray-900 dark:text-white">{{ group.name }}</span>
        <span class="text-gray-400">|</span>
        <span class="text-gray-600 dark:text-gray-400">
          {{ t('admin.groups.columns.rateMultiplier') }}: {{ group.rate_multiplier }}x
        </span>
      </div>

      <div class="flex items-center justify-between gap-3">
        <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.groups.rateSchedules') }} ({{ rows.length }})
        </h4>
        <button type="button" class="btn btn-secondary btn-sm" @click="addRow">
          <Icon name="plus" size="sm" class="mr-1" />
          {{ t('admin.groups.addRateSchedule') }}
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
      </div>

      <div v-else-if="rows.length === 0" class="py-8 text-center text-sm text-gray-400 dark:text-gray-500">
        {{ t('admin.groups.noRateSchedules') }}
      </div>

      <div v-else class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
        <div class="max-h-[460px] overflow-y-auto">
          <table class="w-full text-sm">
            <thead class="sticky top-0 z-[1]">
              <tr class="border-b border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-700">
                <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.groups.startTime') }}
                </th>
                <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.groups.endTime') }}
                </th>
                <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.groups.targetRate') }}
                </th>
                <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                  {{ t('admin.groups.enabled') }}
                </th>
                <th class="w-10 px-2 py-2"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-600">
              <tr
                v-for="row in rows"
                :key="row.key"
                class="hover:bg-gray-50 dark:hover:bg-dark-700/50"
              >
                <td class="px-3 py-2">
                  <input
                    v-model.trim="row.start_time"
                    type="text"
                    inputmode="numeric"
                    autocomplete="off"
                    placeholder="09:00"
                    class="input h-9 w-24"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.trim="row.end_time"
                    type="text"
                    inputmode="numeric"
                    autocomplete="off"
                    placeholder="18:00"
                    class="input h-9 w-24"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="row.rate_multiplier"
                    type="number"
                    step="0.001"
                    min="0.001"
                    autocomplete="off"
                    class="hide-spinner input h-9 w-28"
                  />
                </td>
                <td class="px-3 py-2">
                  <button
                    type="button"
                    class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                    :class="row.enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'"
                    @click="row.enabled = !row.enabled"
                  >
                    <span
                      class="inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform"
                      :class="row.enabled ? 'translate-x-6' : 'translate-x-1'"
                    />
                  </button>
                </td>
                <td class="px-2 py-2">
                  <button
                    type="button"
                    class="rounded p-1 text-gray-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                    @click="removeRow(row.key)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="validationError" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
        {{ validationError }}
      </div>

      <div class="flex items-center gap-3 border-t border-gray-200 pt-4 dark:border-dark-600">
        <template v-if="isDirty">
          <span class="text-xs text-amber-600 dark:text-amber-400">{{ t('admin.groups.unsavedChanges') }}</span>
          <button
            type="button"
            class="text-xs font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
            @click="resetRows"
          >
            {{ t('admin.groups.revertChanges') }}
          </button>
        </template>
        <div class="ml-auto flex items-center gap-3">
          <button type="button" class="btn btn-sm px-4 py-1.5" @click="handleClose">
            {{ t('common.close') }}
          </button>
          <button
            type="button"
            class="btn btn-primary btn-sm px-4 py-1.5"
            :disabled="saving || Boolean(validationError)"
            @click="handleSave"
          >
            <Icon v-if="saving" name="refresh" size="sm" class="mr-1 animate-spin" />
            {{ t('common.save') }}
          </button>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { GroupRateSchedule, GroupRateScheduleInput } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'

interface LocalRow {
  key: string
  start_time: string
  end_time: string
  rate_multiplier: number | null
  enabled: boolean
}

type ParsedRow =
  | { index: number; start: number; end: number; enabled: boolean }
  | { index: number; error: string }

const props = defineProps<{
  show: boolean
  group: AdminGroup | null
}>()

const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const rows = ref<LocalRow[]>([])
const serverRows = ref<LocalRow[]>([])
let rowSeq = 0

const platformColorClass = computed(() => {
  switch (props.group?.platform) {
    case 'anthropic': return 'text-orange-700 dark:text-orange-400'
    case 'openai': return 'text-emerald-700 dark:text-emerald-400'
    case 'antigravity': return 'text-purple-700 dark:text-purple-400'
    default: return 'text-blue-700 dark:text-blue-400'
  }
})

const serializeRows = (items: LocalRow[]) => {
  return JSON.stringify(items.map(row => ({
    start_time: row.start_time,
    end_time: row.end_time,
    rate_multiplier: row.rate_multiplier,
    enabled: row.enabled
  })))
}

const isDirty = computed(() => serializeRows(rows.value) !== serializeRows(serverRows.value))

const validationError = computed(() => {
  const parsed: ParsedRow[] = rows.value.map((row, index): ParsedRow => {
    const start = parseMinute(row.start_time, false)
    const end = parseMinute(row.end_time, true)
    if (start == null || end == null) {
      return { index, error: t('admin.groups.invalidScheduleTime') }
    }
    if (end <= start) {
      return { index, error: t('admin.groups.invalidTimeRange') }
    }
    if (row.rate_multiplier == null || row.rate_multiplier <= 0 || !Number.isFinite(row.rate_multiplier)) {
      return { index, error: t('admin.groups.invalidScheduleRate') }
    }
    return { index, start, end, enabled: row.enabled }
  })

  const invalid = parsed.find(item => 'error' in item)
  if (invalid && 'error' in invalid) {
    return `${t('admin.groups.rowLabel', { index: invalid.index + 1 })}: ${invalid.error}`
  }

  const ordered = parsed
    .filter((item): item is Extract<ParsedRow, { start: number }> => 'start' in item)
    .sort((a, b) => a.start - b.start || a.end - b.end)
  for (let i = 1; i < ordered.length; i++) {
    if (ordered[i].start < ordered[i - 1].end) {
      return t('admin.groups.overlapTimeRange')
    }
  }
  return ''
})

watch(() => props.show, (val) => {
  if (val && props.group) {
    loadSchedules()
  }
})

const loadSchedules = async () => {
  if (!props.group) return
  loading.value = true
  try {
    const data = await adminAPI.groups.getGroupRateSchedules(props.group.id)
    serverRows.value = data.map(scheduleToRow)
    rows.value = cloneRows(serverRows.value)
  } catch (error) {
    appStore.showError(t('admin.groups.failedToLoadSchedules'))
    console.error('Error loading group rate schedules:', error)
  } finally {
    loading.value = false
  }
}

const scheduleToRow = (schedule: GroupRateSchedule): LocalRow => ({
  key: nextRowKey(),
  start_time: minuteToText(schedule.start_minute),
  end_time: minuteToText(schedule.end_minute),
  rate_multiplier: schedule.rate_multiplier,
  enabled: schedule.enabled
})

const cloneRows = (items: LocalRow[]) => items.map(row => ({ ...row, key: nextRowKey() }))

const addRow = () => {
  rows.value.push({
    key: nextRowKey(),
    start_time: '09:00',
    end_time: '18:00',
    rate_multiplier: props.group?.rate_multiplier ?? 1,
    enabled: true
  })
}

const removeRow = (key: string) => {
  rows.value = rows.value.filter(row => row.key !== key)
}

const resetRows = () => {
  rows.value = cloneRows(serverRows.value)
}

const handleSave = async () => {
  if (!props.group || validationError.value) return
  const entries = rows.value.map(row => ({
    start_minute: parseMinute(row.start_time, false) as number,
    end_minute: parseMinute(row.end_time, true) as number,
    rate_multiplier: row.rate_multiplier as number,
    enabled: row.enabled
  } satisfies GroupRateScheduleInput))

  saving.value = true
  try {
    const saved = await adminAPI.groups.replaceGroupRateSchedules(props.group.id, entries)
    serverRows.value = saved.map(scheduleToRow)
    rows.value = cloneRows(serverRows.value)
    appStore.showSuccess(t('admin.groups.rateSchedulesSaved'))
    emit('success')
    emit('close')
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.groups.failedToSaveSchedules'))
    console.error('Error saving group rate schedules:', error)
  } finally {
    saving.value = false
  }
}

const handleClose = () => {
  if (isDirty.value) {
    resetRows()
  }
  emit('close')
}

const parseMinute = (value: string, allowEndOfDay: boolean): number | null => {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value.trim())
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) return null
  if (minute < 0 || minute > 59) return null
  if (allowEndOfDay && hour === 24 && minute === 0) return 1440
  if (hour < 0 || hour > 23) return null
  return hour * 60 + minute
}

const minuteToText = (minute: number): string => {
  if (minute === 1440) return '24:00'
  const hour = Math.floor(minute / 60)
  const min = minute % 60
  return `${String(hour).padStart(2, '0')}:${String(min).padStart(2, '0')}`
}

const nextRowKey = () => `rate-schedule-${++rowSeq}`
</script>

<style scoped>
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
