export const THEME_ATTRIBUTE = 'data-ui-theme' as const
export const THEME_ID_STORAGE_KEY = 'ui-theme' as const

export type ThemeId = 'glass-flow' | 'legacy'
export type ThemeMode = 'light' | 'dark' | 'system'

export interface ThemeDefinition {
  id: ThemeId
  label: string
  stylesheet?: string
  supportsReducedMotion?: boolean
}

export interface ThemeTokens {
  canvas: string
  surface: string
  elevated: string
  border: string
  text: string
  action: string
  focus: string
  radius: string
  shadow: string
  motion: string
  z: string
}

export const OVERLAY_Z_INDEX = {
  dialog: 100000000,
  menu: 100000030,
  select: 100000020,
  tooltip: 100000040,
} as const
