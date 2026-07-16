<template>
  <div class="min-h-screen bg-gray-50 px-4 py-6 text-gray-900 dark:bg-dark-950 dark:text-white">
    <main class="mx-auto flex min-h-[calc(100vh-3rem)] w-full max-w-md flex-col justify-center">
      <section class="rounded-lg border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="flex items-center gap-3">
          <span
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-lg font-bold text-blue-600 dark:bg-blue-900/30 dark:text-blue-300"
          >
            支
          </span>
          <div class="min-w-0">
            <h1 class="text-base font-semibold leading-6">支付宝支付</h1>
            <p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">{{ statusText }}</p>
          </div>
        </div>

        <div class="mt-5">
          <div v-if="phase !== 'failed'" class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
            <div class="h-full rounded-full bg-blue-500 transition-all duration-500" :style="{ width: progressWidth }"></div>
          </div>
          <div v-else class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm leading-6 text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-200">
            {{ errorMessage }}
          </div>
        </div>

        <p class="mt-4 rounded-lg bg-blue-50 px-3 py-2 text-sm leading-6 text-blue-700 dark:bg-blue-950/30 dark:text-blue-200">
          请在支付宝内置浏览器完成支付。
        </p>

        <div class="mt-5 grid gap-2">
          <button
            v-if="phase === 'failed'"
            type="button"
            class="btn btn-primary min-h-[44px] w-full justify-center"
            @click="restart"
          >
            重试
          </button>
          <button
            v-if="canGoResult"
            type="button"
            class="btn btn-secondary min-h-[44px] w-full justify-center"
            @click="goResult"
          >
            查看结果
          </button>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { paymentAPI } from '@/api/payment'
import type { CreateOrderResult } from '@/types/payment'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  type PaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'

type AlipayPhase = 'checking' | 'authorizing' | 'creating' | 'paying' | 'settled' | 'failed'

interface AlipayJSBridgeLike {
  call(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

interface AlipayH5JSAPILike {
  getAuthCode(
    options: {
      appId: string
      scopes: string[]
      showErrorTip?: boolean
    },
    callback: (result: Record<string, unknown>) => void,
  ): void
  tradePay(
    options: {
      tradeNO: string
    },
    callback: (result: Record<string, unknown>) => void,
  ): void
}

const route = useRoute()
const router = useRouter()
const phase = ref<AlipayPhase>('checking')
const errorMessage = ref('')
const lastResult = ref<CreateOrderResult | null>(null)
const alipayH5JSAPISrc = 'https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/alipayjsapi.min.js'
let alipayH5JSAPILoading: Promise<AlipayH5JSAPILike | null> | null = null

const resumeToken = computed(() => typeof route.query.resume_token === 'string' ? route.query.resume_token.trim() : '')
const outTradeNo = computed(() => typeof route.query.out_trade_no === 'string' ? route.query.out_trade_no.trim() : '')
const routeAppId = computed(() => typeof route.query.app_id === 'string' ? route.query.app_id.trim() : '')
const canGoResult = computed(() => phase.value === 'settled' || (!!resumeToken.value && phase.value === 'failed'))

const statusText = computed(() => {
  switch (phase.value) {
    case 'checking':
      return '正在检查支付环境'
    case 'authorizing':
      return '正在获取支付宝授权'
    case 'creating':
      return '正在创建支付交易'
    case 'paying':
      return '正在打开支付宝收银台'
    case 'settled':
      return '支付请求已提交'
    case 'failed':
      return '支付未能继续'
    default:
      return ''
  }
})

const progressWidth = computed(() => {
  switch (phase.value) {
    case 'checking':
      return '18%'
    case 'authorizing':
      return '42%'
    case 'creating':
      return '66%'
    case 'paying':
      return '86%'
    case 'settled':
      return '100%'
    default:
      return '0%'
  }
})

function getAlipayJSBridge(): AlipayJSBridgeLike | undefined {
  return (window as Window & { AlipayJSBridge?: AlipayJSBridgeLike }).AlipayJSBridge
}

function getAlipayH5JSAPI(): AlipayH5JSAPILike | undefined {
  return (window as Window & { ap?: AlipayH5JSAPILike }).ap
}

function isAlipayEnvironment(): boolean {
  if (typeof window === 'undefined') return false
  return /AlipayClient/i.test(window.navigator.userAgent) || !!getAlipayH5JSAPI() || !!getAlipayJSBridge()
}

function ensureAlipayH5JSAPI(timeoutMs = 5000): Promise<AlipayH5JSAPILike | null> {
  const existing = getAlipayH5JSAPI()
  if (existing) return Promise.resolve(existing)
  if (typeof document === 'undefined') return Promise.resolve(null)
  if (alipayH5JSAPILoading) return alipayH5JSAPILoading

  const loading = new Promise<AlipayH5JSAPILike | null>((resolve) => {
    let settled = false
    let script = document.querySelector<HTMLScriptElement>(`script[src="${alipayH5JSAPISrc}"]`)
    const finish = (api: AlipayH5JSAPILike | null) => {
      if (settled) return
      settled = true
      window.clearTimeout(timer)
      script?.removeEventListener('load', handleLoad)
      script?.removeEventListener('error', handleError)
      resolve(api)
    }
    const handleLoad = () => finish(getAlipayH5JSAPI() ?? null)
    const handleError = () => finish(null)
    const timer = window.setTimeout(() => finish(getAlipayH5JSAPI() ?? null), timeoutMs)

    if (!script) {
      script = document.createElement('script')
      script.src = alipayH5JSAPISrc
      script.async = true
      document.head.appendChild(script)
    }
    script.addEventListener('load', handleLoad, { once: true })
    script.addEventListener('error', handleError, { once: true })
  }).finally(() => {
    alipayH5JSAPILoading = null
  })

  alipayH5JSAPILoading = loading
  return loading
}

function waitForAlipayJSBridge(timeoutMs = 5000): Promise<AlipayJSBridgeLike | null> {
  const existing = getAlipayJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: AlipayJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('AlipayJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getAlipayJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getAlipayJSBridge() ?? null), timeoutMs)
    document.addEventListener('AlipayJSBridgeReady', handleReady, false)
  })
}

async function requestAuthCode(appId: string): Promise<string> {
  if (!appId) {
    throw new Error('缺少支付宝 AppID')
  }
  const authPayload = { appId, scopes: ['auth_base'], showErrorTip: false }
  const h5JSAPI = await ensureAlipayH5JSAPI()
  if (h5JSAPI?.getAuthCode) {
    return new Promise((resolve, reject) => {
      h5JSAPI.getAuthCode(authPayload, (result) => {
        const code = extractAuthCode(result)
        code ? resolve(code) : reject(new Error(alipayJSAPIErrorMessage(result, 'ALIPAY_AUTH_CODE_EMPTY', appId)))
      })
    })
  }

  const bridge = await waitForAlipayJSBridge()
  if (!bridge) {
    throw new Error('ALIPAY_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve, reject) => {
    bridge.call('getAuthCode', authPayload, (result) => {
      const code = extractAuthCode(result)
      code ? resolve(code) : reject(new Error(alipayJSAPIErrorMessage(result, 'ALIPAY_AUTH_CODE_EMPTY', appId)))
    })
  })
}

function extractAuthCode(result: Record<string, unknown> | undefined): string {
  if (!result) return ''
  const code = result.authCode ?? result.auth_code ?? result.code
  return typeof code === 'string' ? code.trim() : ''
}

function alipayJSAPIErrorMessage(result: Record<string, unknown> | undefined, fallback: string, appId = ''): string {
  const callbackURL = `${window.location.origin}/payment/alipay-jsapi`
  if (!result) {
    return `${fallback}：支付宝未返回授权码，请确认应用 ${appId || 'AppID'} 已配置授权回调地址 ${callbackURL}`
  }

  const errorCode = stringValue(result.error ?? result.errorCode ?? result.error_code)
  const description = stringValue(result.errorDesc ?? result.errorMessage ?? result.message)
  if (errorCode === '15') {
    return `支付宝授权回调地址不合法：请在支付宝开放平台应用 ${appId || 'AppID'} 中配置授权回调地址 ${callbackURL}`
  }
  if (errorCode === '11') {
    return '你已取消支付宝授权，请重新发起支付'
  }
  if (description) {
    return errorCode ? `支付宝授权失败（${errorCode}）：${description}` : description
  }
  return errorCode ? `支付宝授权失败（${errorCode}）` : `${fallback}：支付宝未返回授权码`
}

function stringValue(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

async function invokeTradePay(tradeNO: string): Promise<Record<string, unknown>> {
  const h5JSAPI = await ensureAlipayH5JSAPI()
  if (h5JSAPI?.tradePay) {
    return new Promise((resolve) => {
      h5JSAPI.tradePay({ tradeNO }, (result) => resolve(result || {}))
    })
  }

  const bridge = await waitForAlipayJSBridge()
  if (!bridge) {
    throw new Error('ALIPAY_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.call('tradePay', { tradeNO }, (result) => resolve(result || {}))
  })
}

async function startPayment() {
  errorMessage.value = ''
  lastResult.value = null
  phase.value = 'checking'

  try {
    if (!resumeToken.value) {
      throw new Error('缺少支付恢复令牌')
    }
    if (!isAlipayEnvironment()) {
      throw new Error('请在支付宝 App 内继续支付')
    }

    phase.value = 'authorizing'
    const authCode = await requestAuthCode(routeAppId.value)

    phase.value = 'creating'
    const response = await paymentAPI.startAlipayJSAPI({
      resume_token: resumeToken.value,
      auth_code: authCode,
    })
    const result = response.data
    lastResult.value = result
    persistSnapshot(result)

    const tradeNO = result.alipay_jsapi?.tradeNO?.trim()
    if (!tradeNO) {
      throw new Error('支付宝交易号为空')
    }

    phase.value = 'paying'
    const payResult = await invokeTradePay(tradeNO)
    handleTradePayResult(payResult)
  } catch (err: unknown) {
    phase.value = 'failed'
    errorMessage.value = extractApiErrorMessage(err, err instanceof Error ? err.message : '支付宝支付启动失败')
  }
}

function handleTradePayResult(result: Record<string, unknown>) {
  const resultCode = String(result.resultCode || result.result_code || '').toLowerCase()
  const memo = String(result.memo || result.err_msg || '')
  if (resultCode === '6001' || memo.toLowerCase().includes('cancel')) {
    phase.value = 'failed'
    errorMessage.value = '你已取消支付宝支付'
    return
  }
  if (resultCode && resultCode !== '9000' && resultCode !== '8000') {
    phase.value = 'failed'
    errorMessage.value = memo || `支付宝支付返回异常：${resultCode}`
    return
  }
  if (!resultCode) {
    phase.value = 'checking'
    goResult()
    return
  }
  phase.value = 'settled'
  goResult()
}

function persistSnapshot(result: CreateOrderResult) {
  if (typeof window === 'undefined') return
  const snapshot: PaymentRecoverySnapshot = {
    orderId: result.order_id,
    amount: result.amount,
    qrCode: '',
    expiresAt: result.expires_at || '',
    paymentType: 'alipay',
    payUrl: '',
    outTradeNo: result.out_trade_no || outTradeNo.value,
    clientSecret: '',
    intentId: '',
    currency: result.currency || '',
    countryCode: '',
    paymentEnv: '',
    payAmount: result.pay_amount,
    orderType: '',
    paymentMode: 'jsapi',
    resumeToken: resumeToken.value,
    createdAt: Date.now(),
  }
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function goResult() {
  router.replace({
    path: '/payment/result',
    query: {
      resume_token: resumeToken.value || undefined,
      out_trade_no: outTradeNo.value || lastResult.value?.out_trade_no || undefined,
    },
  })
}

function restart() {
  startPayment()
}

onMounted(() => {
  startPayment()
})
</script>
