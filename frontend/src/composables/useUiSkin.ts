import { inject, provide, readonly, ref, type InjectionKey, type Ref } from 'vue'

export type UiSkin = 'legacy' | 'v2'

const uiSkinKey: InjectionKey<Readonly<Ref<UiSkin>>> = Symbol('ui-skin')
const legacyUiSkin = readonly(ref<UiSkin>('legacy'))

export const resolveUiSkin = (value: unknown): UiSkin => (value === 'v2' ? 'v2' : 'legacy')

export const provideUiSkin = (skin: Readonly<Ref<UiSkin>>): void => {
  provide(uiSkinKey, skin)
}

export const useUiSkin = (): Readonly<Ref<UiSkin>> => inject(uiSkinKey, legacyUiSkin)
