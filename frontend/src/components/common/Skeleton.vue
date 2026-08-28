<template>
  <div
    :class="[
      'animate-pulse bg-gray-200 dark:bg-dark-700',
      variant === 'circle' ? 'rounded-full' : 'rounded-lg',
      customClass
    ]"
    :style="style"
  ></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  variant?: 'rect' | 'circle' | 'text'
  width?: string | number
  height?: string | number
  class?: string
}

const props = withDefaults(defineProps<Props>(), {
  variant: 'rect',
  width: '100%'
})

const customClass = computed(() => props.class || '')

function normalizeSize(v: string | number | undefined): string | undefined {
  if (v === undefined || v === null) return undefined
  if (typeof v === 'number') return `${v}px`
  const str = String(v).trim()
  return /^\d+(\.\d+)?$/.test(str) ? `${str}px` : str
}

const style = computed(() => {
  const s: Record<string, string> = {}

  if (props.width) {
    const w = normalizeSize(props.width)
    if (w) s.width = w
  }

  if (props.height) {
    const h = normalizeSize(props.height)
    if (h) s.height = h
  } else if (props.variant === 'text') {
    s.height = '1em'
    s.marginTop = '0.25em'
    s.marginBottom = '0.25em'
  }
  
  return s
})
</script>
