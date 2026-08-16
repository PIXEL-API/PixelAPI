<template>
  <div class="rounded border p-2"
       :class="isEmpty ? 'border-red-400 bg-red-50 dark:border-red-500 dark:bg-red-950/20' : 'border-gray-200 bg-white dark:border-dark-500 dark:bg-dark-700'">
    <!-- 时间段 + 删除 -->
    <div class="flex items-start gap-2">
      <div class="w-24">
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.startTime', '开始') }}</label>
        <input :value="range.start_time" @input="emitField('start_time', ($event.target as HTMLInputElement).value)"
          type="text" inputmode="numeric" autocomplete="off" placeholder="09:00" class="input mt-0.5 text-xs" />
      </div>
      <div class="w-24">
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.endTime', '结束') }}</label>
        <input :value="range.end_time" @input="emitField('end_time', ($event.target as HTMLInputElement).value)"
          type="text" inputmode="numeric" autocomplete="off" placeholder="18:00" class="input mt-0.5 text-xs" />
      </div>
      <div class="mt-5 text-xs text-gray-400">[start, end)</div>
      <button type="button" @click="emit('remove')" class="ml-auto mt-4 rounded p-0.5 text-gray-400 hover:text-red-500">
        <Icon name="x" size="sm" />
      </button>
    </div>

    <!-- Token 模式：完整价格字段 -->
    <div v-if="mode === 'token'" class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.inputPrice', '输入') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="text-gray-300">$/M</span></label>
        <input :value="range.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.outputPrice', '输出') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheWritePrice', '缓存W') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.cacheReadPrice', '缓存R') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageInputPrice', '图片输入') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.image_input_price" @input="emitField('image_input_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageCacheReadPrice', '图片缓存') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.image_cache_read_price" @input="emitField('image_cache_read_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
      <div>
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.imageTokenPrice', '图片输出') }} <span class="text-gray-300">$/M</span></label>
        <input :value="range.image_output_price" @input="emitField('image_output_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
    </div>

    <!-- Per-request / Image 模式：单次价格 -->
    <div v-else class="mt-2 flex items-center gap-2">
      <div class="w-40">
        <label class="text-xs text-gray-400">{{ t('admin.channels.form.perRequestPrice', '单次价格') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="text-gray-300">$</span></label>
        <input :value="range.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
          type="number" step="any" min="0" class="input mt-0.5 text-xs" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { TimeRangeFormEntry } from './types'
import type { BillingMode } from '@/api/admin/channels'

const { t } = useI18n()

const props = defineProps<{
  range: TimeRangeFormEntry
  mode: BillingMode
}>()

const emit = defineEmits<{
  update: [range: TimeRangeFormEntry]
  remove: []
}>()

const isEmpty = computed(() => {
  const r = props.range
  return [r.input_price, r.output_price, r.cache_write_price, r.cache_read_price,
    r.image_input_price, r.image_cache_read_price, r.image_output_price, r.per_request_price]
    .every(v => v == null || v === '')
})

function emitField(field: keyof TimeRangeFormEntry, value: string) {
  emit('update', { ...props.range, [field]: value === '' ? null : value })
}
</script>
