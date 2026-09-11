import {
  THEME_ATTRIBUTE,
  THEME_ID_STORAGE_KEY,
  type ThemeDefinition,
  type ThemeId,
  type ThemeMode,
} from './contracts'

const themeDefinitions: Record<ThemeId, ThemeDefinition> = {
  'glass-flow': {
    id: 'glass-flow',
    label: 'Glass Flow',
    stylesheet: './themes/glass-flow.css',
    supportsReducedMotion: true,
  },
  legacy: {
    id: 'legacy',
    label: 'Legacy',
    supportsReducedMotion: true,
  },
}

const resolveDarkMode = (mode: ThemeMode): boolean => {
  if (mode === 'dark') return true
  if (mode === 'light') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export const resolveTheme = (theme: string | null | undefined): ThemeDefinition => {
  if (!theme) {
    return themeDefinitions['glass-flow']
  }
  if (!Object.prototype.hasOwnProperty.call(themeDefinitions, theme)) {
    throw new Error(`Unknown UI theme: ${theme}`)
  }
  return themeDefinitions[theme as ThemeId]
}

export const activateTheme = (theme: ThemeId, mode: ThemeMode = 'system'): void => {
  const definition = resolveTheme(theme)
  document.documentElement.setAttribute(THEME_ATTRIBUTE, definition.id)
  document.documentElement.classList.toggle('dark', resolveDarkMode(mode))
}

// Compatibility entry point for callers that already use the theme-kit runtime.
export const applyTheme = activateTheme

export const readThemeId = (): ThemeId => {
  return resolveTheme(localStorage.getItem(THEME_ID_STORAGE_KEY)).id
}

export const persistThemeId = (theme: ThemeId): void => {
  const definition = resolveTheme(theme)
  localStorage.setItem(THEME_ID_STORAGE_KEY, definition.id)
}

export const readThemeMode = (): ThemeMode => {
  // `theme` is the existing light/dark preference. Keep accepting it while
  // the new theme id uses its own storage key.
  const stored = localStorage.getItem('theme')
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system'
}

export const persistThemeMode = (mode: ThemeMode): void => {
  localStorage.setItem('theme', mode)
}

export const listThemes = (): ThemeDefinition[] => Object.values(themeDefinitions)
