import { readonly, ref, type Ref } from 'vue'
import { toggleThemeMode } from '@/theme-kit'

/**
 * Reactive dark-mode state.
 *
 * The `.dark` class on <html> is the single source of truth (written by
 * theme-kit's runtime, main.ts bootstrap, and theme toggles). A shared
 * MutationObserver keeps this ref in sync no matter which entry point
 * flips the class, so charts and other non-CSS consumers re-render on
 * toggle instead of caching the first read forever.
 */
const isDark = ref(document.documentElement.classList.contains('dark'))

const observer = new MutationObserver(() => {
  isDark.value = document.documentElement.classList.contains('dark')
})
observer.observe(document.documentElement, {
  attributes: true,
  attributeFilter: ['class']
})

export function useDarkMode(): Readonly<Ref<boolean>> {
  return readonly(isDark)
}

/** Toggle light/dark and persist the choice (delegates to theme-kit). */
export function toggleDarkMode(): void {
  toggleThemeMode()
}
