import { computed, reactive } from 'vue'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { QUOTA_THRESHOLD_TYPE_FIXED, type QuotaThresholdType } from '@/constants/account'

export const QUOTA_NOTIFY_DIMS = ['daily', 'weekly', 'total'] as const
export type QuotaNotifyDim = (typeof QUOTA_NOTIFY_DIMS)[number]

interface DimState {
  enabled: boolean | null
  threshold: number | null
  thresholdType: QuotaThresholdType | null
}

export function useQuotaNotifyState() {
  const adminSettingsStore = useAdminSettingsStore()

  // 只读派生自 store：本 composable 的两个消费方（CreateAccountModal /
  // EditAccountModal）对它只有 prop 下传，没有任何写入。
  const globalEnabled = computed(() => adminSettingsStore.accountQuotaNotifyEnabled)

  const state = reactive<Record<QuotaNotifyDim, DimState>>({
    daily: { enabled: null, threshold: null, thresholdType: null },
    weekly: { enabled: null, threshold: null, thresholdType: null },
    total: { enabled: null, threshold: null, thresholdType: null },
  })

  /**
   * 确保全局开关已加载。
   *
   * 此前这里直连 /admin/settings 拉 211KB，只为读一个布尔字段；而调用方是账号页里
   * 常驻挂载的弹窗，在 setup 顶层就执行——用户没打开任何弹窗就已经多打了两发。
   * 现在改为触发 store 的 fetch，并发调用会被 store 合并成一次请求；
   * 若 store 已加载则完全不产生网络请求。
   */
  function loadGlobalState() {
    void adminSettingsStore.fetch()
  }

  function loadFromExtra(extra: Record<string, unknown> | null | undefined) {
    for (const d of QUOTA_NOTIFY_DIMS) {
      state[d].enabled = (extra?.[`quota_notify_${d}_enabled`] as boolean) ?? null
      state[d].threshold = (extra?.[`quota_notify_${d}_threshold`] as number) ?? null
      state[d].thresholdType = (extra?.[`quota_notify_${d}_threshold_type`] as QuotaThresholdType) ?? null
    }
  }

  function writeToExtra(extra: Record<string, unknown>, mode: 'create' | 'update') {
    for (const d of QUOTA_NOTIFY_DIMS) {
      const s = state[d]
      if (s.enabled) {
        extra[`quota_notify_${d}_enabled`] = true
        if (s.threshold != null) {
          extra[`quota_notify_${d}_threshold`] = s.threshold
        } else if (mode === 'update') {
          delete extra[`quota_notify_${d}_threshold`]
        }
        extra[`quota_notify_${d}_threshold_type`] = s.thresholdType || QUOTA_THRESHOLD_TYPE_FIXED
      } else if (mode === 'update') {
        delete extra[`quota_notify_${d}_enabled`]
        delete extra[`quota_notify_${d}_threshold`]
        delete extra[`quota_notify_${d}_threshold_type`]
      }
    }
  }

  function reset() {
    for (const d of QUOTA_NOTIFY_DIMS) {
      state[d].enabled = null
      state[d].threshold = null
      state[d].thresholdType = null
    }
  }

  return { globalEnabled, state, loadGlobalState, loadFromExtra, writeToExtra, reset }
}
