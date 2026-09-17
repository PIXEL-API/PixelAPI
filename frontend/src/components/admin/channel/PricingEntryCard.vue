<template>
  <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800">
    <!-- Collapsed summary header (clickable) -->
    <div
      class="flex cursor-pointer select-none items-center gap-2"
      @click="collapsed = !collapsed"
    >
      <Icon
        :name="collapsed ? 'chevronRight' : 'chevronDown'"
        size="sm"
        :stroke-width="2"
        class="flex-shrink-0 text-gray-400 transition-transform duration-200"
      />

      <!-- Summary: model tags + billing badge -->
      <div v-if="collapsed" class="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
        <!-- Compact model tags (show first 3) -->
        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1">
          <span
            v-for="(m, i) in entry.models.slice(0, 3)"
            :key="i"
            class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-xs"
            :class="getPlatformTagClass(props.platform || '')"
          >
            {{ m }}
          </span>
          <span
            v-if="entry.models.length > 3"
            class="whitespace-nowrap text-xs text-gray-400"
          >
            +{{ entry.models.length - 3 }}
          </span>
          <span
            v-if="entry.models.length === 0"
            class="text-xs italic text-gray-400"
          >
            {{ t('admin.channels.form.noModels') }}
          </span>
        </div>

        <!-- Billing mode badge -->
        <span
          class="flex-shrink-0 rounded-full bg-primary-100 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
        >
          {{ billingModeLabel }}
        </span>
      </div>

      <!-- Expanded: show the label "Pricing Entry" or similar -->
      <div v-else class="flex-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        {{ t('admin.channels.form.pricingEntry') }}
      </div>

      <!-- Remove button (always visible, stop propagation) -->
      <button
        type="button"
        @click.stop="emit('remove')"
        class="flex-shrink-0 rounded p-1 text-gray-400 hover:text-red-500"
      >
        <Icon name="trash" size="sm" />
      </button>
    </div>

    <!-- Expandable content with transition -->
    <div
      class="collapsible-content"
      :class="{ 'collapsible-content--collapsed': collapsed }"
    >
      <div class="collapsible-inner">
        <!-- Header: Models + Billing Mode -->
        <div class="mt-3 flex items-start gap-2">
          <div class="flex-1">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.models') }} <span class="text-red-500">*</span>
            </label>
            <ModelTagInput
              :models="entry.models"
              :platform="props.platform"
              @update:models="onModelsUpdate($event)"
              :placeholder="t('admin.channels.form.modelsPlaceholder')"
              class="mt-1"
            />
          </div>
          <div class="w-40">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.billingMode') }}
            </label>
            <Select
              :modelValue="entry.billing_mode"
              @update:modelValue="onBillingModeUpdate($event as BillingMode)"
              :options="billingModeOptions"
              class="mt-1"
            />
          </div>
        </div>

        <!-- Token mode -->
        <div v-if="entry.billing_mode === 'token'">
          <div
            v-if="showLongContextControls"
            data-testid="long-context-pricing"
            class="mt-3 rounded-lg border border-emerald-200 bg-white p-3 dark:border-emerald-900/60 dark:bg-dark-700"
          >
            <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <div class="min-w-0">
                <div class="text-sm font-medium text-gray-700 dark:text-gray-200">
                  {{ t('admin.channels.form.longContextPricingTitle') }}
                </div>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  {{ t('admin.channels.form.longContextPricingDescription') }}
                </p>
                <p class="mt-1 text-xs" :class="longContextStatusClass">
                  {{ longContextStatusLabel }}
                </p>
              </div>

              <div class="flex min-h-11 shrink-0 items-center justify-between gap-3 sm:justify-end">
                <span class="text-sm text-gray-600 dark:text-gray-300">
                  {{ longContextToggleValue
                    ? t('admin.channels.form.longContextPricingOn')
                    : t('admin.channels.form.longContextPricingOff')
                  }}
                </span>
                <Toggle
                  :model-value="longContextToggleValue"
                  :disabled="longContextToggleDisabled"
                  :aria-disabled="longContextToggleDisabled"
                  :aria-label="t('admin.channels.form.longContextPricingTitle')"
                  data-testid="long-context-toggle"
                  class="disabled:cursor-not-allowed disabled:opacity-50"
                  @update:model-value="onLongContextToggle"
                />
              </div>
            </div>

            <div class="mt-3 grid grid-cols-1 gap-1 sm:max-w-sm">
              <label class="text-xs font-medium text-gray-600 dark:text-gray-300">
                {{ t('admin.channels.form.longContextThreshold') }}
              </label>
              <input
                :value="entry.long_context_input_token_threshold"
                type="number"
                inputmode="numeric"
                step="1"
                min="1"
                :max="MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD"
                :disabled="hasTokenIntervals"
                data-testid="long-context-threshold"
                class="input min-h-11 w-full text-sm disabled:cursor-not-allowed disabled:opacity-50"
                :placeholder="t('admin.channels.form.longContextThresholdPlaceholder')"
                @input="onLongContextThresholdInput(($event.target as HTMLInputElement).value)"
              />
              <p class="text-xs leading-5 text-gray-400">
                {{ t('admin.channels.form.longContextThresholdHint') }}
              </p>
            </div>
          </div>

          <!-- Default prices (fallback when no interval matches) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPrices') }}
            <span class="ml-1 font-normal text-gray-400">$/MTok</span>
          </label>
          <div class="mt-1 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.inputPrice') }}</label>
              <input :value="entry.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.outputPrice') }}</label>
              <input :value="entry.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheWritePrice') }}</label>
              <input :value="entry.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheReadPrice') }}</label>
              <input :value="entry.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageInputPrice') }}</label>
              <input :value="entry.image_input_price" @input="emitField('image_input_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageCacheReadPrice') }}</label>
              <input :value="entry.image_cache_read_price" @input="emitField('image_cache_read_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
            <div>
              <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageTokenPrice') }}</label>
              <input :value="entry.image_output_price" @input="emitField('image_output_price', ($event.target as HTMLInputElement).value)"
                type="number" step="any" min="0" class="input mt-0.5 text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
            </div>
          </div>

          <!-- Token intervals -->
          <div class="mt-3">
            <div class="flex items-center justify-between">
              <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('admin.channels.form.intervals') }}
                <span class="ml-1 font-normal text-gray-400">(min, max]</span>
              </label>
              <button
                type="button"
                :disabled="longContextIntervalsBlocked"
                data-testid="add-token-interval"
                class="min-h-11 rounded px-2 text-xs text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:text-gray-400"
                @click="addInterval"
              >
                + {{ t('admin.channels.form.addInterval') }}
              </button>
            </div>
            <p v-if="longContextIntervalsBlocked" class="mt-1 text-xs leading-5 text-amber-600 dark:text-amber-400">
              {{ t('admin.channels.form.longContextAddIntervalBlocked') }}
            </p>
            <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
              <IntervalRow
                v-for="(iv, idx) in entry.intervals"
                :key="idx"
                :interval="iv"
                :mode="entry.billing_mode"
                @update="updateInterval(idx, $event)"
                @remove="removeInterval(idx)"
              />
            </div>
          </div>
        </div>

        <!-- Per-request mode -->
        <div v-else-if="entry.billing_mode === 'per_request'">
          <!-- Default per-request price -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultPerRequestPrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.requestTiers') }}
            </label>
            <button type="button" @click="addInterval" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
          <div v-else class="mt-2 rounded border border-dashed border-gray-300 p-3 text-center text-xs text-gray-400 dark:border-dark-500">
            {{ t('admin.channels.form.noTiersYet') }}
          </div>
        </div>

        <!-- Image mode -->
        <div v-else-if="entry.billing_mode === 'image'">
          <!-- Default image price (per-request, same as per_request mode) -->
          <label class="mt-3 block text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t('admin.channels.form.defaultImagePrice') }}
            <span class="ml-1 font-normal text-gray-400">$</span>
          </label>
          <div class="mt-1 w-48">
            <input :value="entry.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
              type="number" step="any" min="0" class="input text-sm" :placeholder="t('admin.channels.form.pricePlaceholder')" />
          </div>

          <!-- Image tiers -->
          <div class="mt-3 flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.imageTiers') }}
            </label>
            <button type="button" @click="addImageTier" class="text-xs text-primary-600 hover:text-primary-700">
              + {{ t('admin.channels.form.addTier') }}
            </button>
          </div>
          <div v-if="entry.intervals && entry.intervals.length > 0" class="mt-2 space-y-2">
            <IntervalRow
              v-for="(iv, idx) in entry.intervals"
              :key="idx"
              :interval="iv"
              :mode="entry.billing_mode"
              @update="updateInterval(idx, $event)"
              @remove="removeInterval(idx)"
            />
          </div>
        </div>

        <!-- 时间段价格（峰谷价） -->
        <div class="mt-3 border-t border-gray-100 pt-3 dark:border-dark-600">
          <div class="flex items-center justify-between">
            <label class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.channels.form.timeRanges') }}
            </label>
            <button
              type="button"
              data-testid="add-time-range"
              class="min-h-11 rounded px-2 text-xs text-primary-600 hover:text-primary-700"
              @click="addTimeRange"
            >
              + {{ t('admin.channels.form.addTimeRange') }}
            </button>
          </div>
          <p class="mt-1 text-xs leading-5 text-gray-400">
            {{ t('admin.channels.form.timeRangesHint') }}
          </p>
          <div v-if="entry.time_ranges && entry.time_ranges.length > 0" class="mt-2 space-y-2">
            <TimeRangeRow
              v-for="(tr, idx) in entry.time_ranges"
              :key="idx"
              :range="tr"
              :mode="entry.billing_mode"
              @update="updateTimeRange(idx, $event)"
              @remove="removeTimeRange(idx)"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import IntervalRow from './IntervalRow.vue'
import TimeRangeRow from './TimeRangeRow.vue'
import ModelTagInput from './ModelTagInput.vue'
import type { PricingFormEntry, IntervalFormEntry, TimeRangeFormEntry } from './types'
import {
  MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD,
  getPlatformTagClass,
  perTokenToMTok,
} from './types'
import type { BillingMode } from '@/api/admin/channels'
import channelsAPI from '@/api/admin/channels'

const { t } = useI18n()

const props = defineProps<{
  entry: PricingFormEntry
  platform?: string
  showLongContextPricing?: boolean
}>()

const emit = defineEmits<{
  update: [entry: PricingFormEntry]
  remove: []
}>()

// Collapse state: entries with existing models default to collapsed
const collapsed = ref(props.entry.models.length > 0)

const billingModeOptions = computed(() => [
  { value: 'token', label: 'Token' },
  { value: 'per_request', label: t('admin.channels.billingMode.perRequest') },
  { value: 'image', label: t('admin.channels.billingMode.image') }
])

const billingModeLabel = computed(() => {
  const opt = billingModeOptions.value.find(o => o.value === props.entry.billing_mode)
  return opt ? opt.label : props.entry.billing_mode
})

const showLongContextControls = computed(() => (
  props.showLongContextPricing === true &&
  props.platform === 'openai' &&
  props.entry.billing_mode === 'token'
))

const hasTokenIntervals = computed(() => (props.entry.intervals?.length || 0) > 0)

const longContextToggleValue = computed(() => {
  return props.entry.long_context_pricing_enabled === true
})

const longContextIntervalsBlocked = computed(() => (
  showLongContextControls.value && props.entry.long_context_pricing_enabled === true
))

const longContextToggleDisabled = computed(() => (
  hasTokenIntervals.value && props.entry.long_context_pricing_enabled !== true
))

const longContextStatusLabel = computed(() => {
  if (hasTokenIntervals.value) {
    return props.entry.long_context_pricing_enabled === true
      ? t('admin.channels.form.longContextConflictStatus')
      : t('admin.channels.form.longContextCustomIntervals')
  }
  if (props.entry.long_context_pricing_enabled === null) {
    return t('admin.channels.form.longContextInherited')
  }
  return props.entry.long_context_pricing_enabled
    ? t('admin.channels.form.longContextExplicitlyEnabled')
    : t('admin.channels.form.longContextExplicitlyDisabled')
})

const longContextStatusClass = computed(() => {
  if (hasTokenIntervals.value && props.entry.long_context_pricing_enabled === true) {
    return 'text-red-600 dark:text-red-400'
  }
  if (hasTokenIntervals.value) return 'text-amber-600 dark:text-amber-400'
  if (props.entry.long_context_pricing_enabled === true) return 'text-emerald-600 dark:text-emerald-400'
  return 'text-gray-500 dark:text-gray-400'
})

function emitField(field: keyof PricingFormEntry, value: string) {
  emit('update', { ...props.entry, [field]: value === '' ? null : value })
}

function onBillingModeUpdate(billingMode: BillingMode) {
  const resetLongContextPricing = billingMode !== 'token'
  emit('update', {
    ...props.entry,
    billing_mode: billingMode,
    intervals: [],
    time_ranges: [],
    long_context_pricing_enabled: resetLongContextPricing
      ? null
      : props.entry.long_context_pricing_enabled,
    long_context_input_token_threshold: resetLongContextPricing
      ? null
      : props.entry.long_context_input_token_threshold,
  })
}

function onLongContextToggle(enabled: boolean) {
  if (enabled && hasTokenIntervals.value) return
  emit('update', {
    ...props.entry,
    long_context_pricing_enabled: enabled,
    long_context_input_token_threshold: enabled
      ? props.entry.long_context_input_token_threshold
      : null,
  })
}

function onLongContextThresholdInput(value: string) {
  emit('update', {
    ...props.entry,
    long_context_pricing_enabled: true,
    long_context_input_token_threshold: value === '' ? null : value,
  })
}

function addInterval() {
  if (longContextIntervalsBlocked.value) return
  const intervals = [...(props.entry.intervals || [])]
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_read_price: null, per_request_price: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function addImageTier() {
  const intervals = [...(props.entry.intervals || [])]
  const labels = ['1K', '2K', '4K', 'HD']
  intervals.push({
    min_tokens: 0, max_tokens: null, tier_label: labels[intervals.length] || '',
    input_price: null, output_price: null, cache_write_price: null,
    cache_read_price: null, per_request_price: null,
    sort_order: intervals.length
  })
  emit('update', { ...props.entry, intervals })
}

function updateInterval(idx: number, updated: IntervalFormEntry) {
  const intervals = [...(props.entry.intervals || [])]
  intervals[idx] = updated
  emit('update', { ...props.entry, intervals })
}

function removeInterval(idx: number) {
  const intervals = [...(props.entry.intervals || [])]
  intervals.splice(idx, 1)
  emit('update', { ...props.entry, intervals })
}

function addTimeRange() {
  const timeRanges = [...(props.entry.time_ranges || [])]
  timeRanges.push({
    start_time: '09:00', end_time: '12:00',
    input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
    image_input_price: null, image_cache_read_price: null, image_output_price: null,
    per_request_price: null,
    sort_order: timeRanges.length
  })
  emit('update', { ...props.entry, time_ranges: timeRanges })
}

function updateTimeRange(idx: number, updated: TimeRangeFormEntry) {
  const timeRanges = [...(props.entry.time_ranges || [])]
  timeRanges[idx] = updated
  emit('update', { ...props.entry, time_ranges: timeRanges })
}

function removeTimeRange(idx: number) {
  const timeRanges = [...(props.entry.time_ranges || [])]
  timeRanges.splice(idx, 1)
  emit('update', { ...props.entry, time_ranges: timeRanges })
}

async function onModelsUpdate(newModels: string[]) {
  const oldModels = props.entry.models
  emit('update', { ...props.entry, models: newModels })

  // 只在新增模型且当前无价格时自动填充
  const addedModels = newModels.filter(m => !oldModels.includes(m))
  if (addedModels.length === 0) return

  // 检查是否所有价格字段都为空
  const e = props.entry
  const hasPrice = e.input_price != null || e.output_price != null ||
                   e.cache_write_price != null || e.cache_read_price != null ||
                   e.image_input_price != null || e.image_cache_read_price != null ||
                   e.image_output_price != null
  if (hasPrice) return

  // 查询第一个新增模型的默认价格
  try {
    const result = await channelsAPI.getModelDefaultPricing(addedModels[0])
    if (result.found) {
      emit('update', {
        ...props.entry,
        models: newModels,
        input_price: perTokenToMTok(result.input_price ?? null),
        output_price: perTokenToMTok(result.output_price ?? null),
        cache_write_price: perTokenToMTok(result.cache_write_price ?? null),
        cache_read_price: perTokenToMTok(result.cache_read_price ?? null),
        image_input_price: perTokenToMTok(result.image_input_price ?? null),
        image_cache_read_price: perTokenToMTok(result.image_cache_read_price ?? null),
        image_output_price: perTokenToMTok(result.image_output_price ?? null),
      })
    }
  } catch {
    // 查询失败不影响用户操作
  }
}
</script>

<style scoped>
.collapsible-content {
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.25s ease;
}

.collapsible-content--collapsed {
  grid-template-rows: 0fr;
}

.collapsible-inner {
  overflow: hidden;
}
</style>
