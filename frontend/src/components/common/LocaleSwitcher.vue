<template>
  <div class="relative flex-none" ref="dropdownRef">
    <button
      ref="triggerRef"
      @click="toggleMenu()"
      @keydown="onTriggerKeydown"
      :disabled="switching"
      class="flex min-h-11 min-w-11 flex-none items-center justify-center gap-1.5 rounded-lg px-2 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
      :title="currentLocale?.name"
      :aria-label="t('common.language')"
      aria-haspopup="menu"
      :aria-expanded="isOpen"
      :aria-controls="isOpen ? MENU_ID : undefined"
    >
      <span class="flex h-5 w-6 items-center justify-center rounded border border-gray-300 text-[10px] font-bold leading-none text-gray-600 dark:border-dark-600 dark:text-gray-300" aria-hidden="true">
        {{ currentLocale?.short }}
      </span>
      <span class="hidden sm:inline">{{ currentLocale?.name }}</span>
      <Icon
        name="chevronDown"
        size="xs"
        class="text-gray-400 transition-transform duration-200"
        :class="{ 'rotate-180': isOpen }"
      />
    </button>

    <transition name="dropdown">
      <div
        v-if="isOpen"
        :id="MENU_ID"
        class="absolute right-0 z-[var(--ui-z-menu)] mt-1 w-36 overflow-hidden rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
        role="menu"
        :aria-label="t('common.language')"
        @keydown="onMenuKeydown"
        @focusout="onMenuFocusout"
      >
        <button
          v-for="locale in availableLocales"
          :key="locale.code"
          :disabled="switching"
          @click="selectLocale(locale.code)"
          class="flex w-full items-center gap-2 px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
          :class="{
            'bg-primary-50 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400':
              locale.code === currentLocaleCode
          }"
          role="menuitemradio"
          :aria-checked="locale.code === currentLocaleCode"
          tabindex="-1"
          data-menu-item
        >
          <span class="flex h-5 w-6 items-center justify-center rounded border border-gray-300 text-[10px] font-bold leading-none dark:border-dark-600" aria-hidden="true">
            {{ locale.short }}
          </span>
          <span>{{ locale.name }}</span>
          <Icon v-if="locale.code === currentLocaleCode" name="check" size="sm" class="ml-auto text-primary-500" />
        </button>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { setLocale, availableLocales } from '@/i18n'
import { useDropdownMenu } from '@/composables/useDropdownMenu'

const { t, locale } = useI18n()

const MENU_ID = 'locale-switcher-menu'
const isOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const switching = ref(false)

const currentLocaleCode = computed(() => locale.value)
const currentLocale = computed(() => availableLocales.find((l) => l.code === locale.value))

const { toggleMenu, closeMenu, onTriggerKeydown, onMenuKeydown, onMenuFocusout } =
  useDropdownMenu({
    open: isOpen,
    container: dropdownRef,
    trigger: triggerRef
  })

async function selectLocale(code: string) {
  if (switching.value || code === currentLocaleCode.value) {
    closeMenu(true)
    return
  }
  switching.value = true
  try {
    await setLocale(code)
    closeMenu(true)
  } finally {
    switching.value = false
  }
}
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.15s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
