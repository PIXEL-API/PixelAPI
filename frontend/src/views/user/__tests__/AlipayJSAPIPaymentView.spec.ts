import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerReplace = vi.hoisted(() => vi.fn())
const startAlipayJSAPI = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({
      replace: routerReplace,
    }),
  }
})

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    startAlipayJSAPI,
  },
}))

import AlipayJSAPIPaymentView from '../AlipayJSAPIPaymentView.vue'

describe('AlipayJSAPIPaymentView', () => {
  beforeEach(() => {
    routeState.query = {
      resume_token: 'resume-token',
      out_trade_no: 'sub2_alipay_jsapi',
      app_id: '202100merchantappid',
    }
    routerReplace.mockReset()
    startAlipayJSAPI.mockReset().mockResolvedValue({
      data: {
        order_id: 88,
        amount: 18,
        pay_amount: 18,
        out_trade_no: 'sub2_alipay_jsapi',
        expires_at: '2099-01-01T00:10:00.000Z',
        currency: 'CNY',
        alipay_jsapi: {
          tradeNO: '2026070122001400000000000001',
          appId: '202100merchantappid',
        },
      },
    })
    Object.defineProperty(window.navigator, 'userAgent', {
      value: 'Mozilla/5.0 AlipayClient',
      configurable: true,
    })
    window.localStorage.clear()
  })

  afterEach(() => {
    Reflect.deleteProperty(window, 'ap')
    Reflect.deleteProperty(window, 'my')
    window.localStorage.clear()
  })

  it('uses Alipay H5 JSAPI instead of mini-program getAuthCode inside Alipay app H5', async () => {
    const h5GetAuthCode = vi.fn((_options, callback) => callback({ authCode: 'auth-code-123' }))
    const h5TradePay = vi.fn((_options, callback) => callback({ resultCode: '9000' }))
    const miniProgramGetAuthCode = vi.fn()

    Object.defineProperty(window, 'ap', {
      value: {
        getAuthCode: h5GetAuthCode,
        tradePay: h5TradePay,
      },
      configurable: true,
    })
    Object.defineProperty(window, 'my', {
      value: {
        getAuthCode: miniProgramGetAuthCode,
      },
      configurable: true,
    })

    mount(AlipayJSAPIPaymentView)
    await flushPromises()
    await flushPromises()

    expect(h5GetAuthCode).toHaveBeenCalledWith(
      {
        appId: '202100merchantappid',
        scopes: ['auth_base'],
        showErrorTip: false,
      },
      expect.any(Function),
    )
    expect(miniProgramGetAuthCode).not.toHaveBeenCalled()
    expect(startAlipayJSAPI).toHaveBeenCalledWith({
      resume_token: 'resume-token',
      auth_code: 'auth-code-123',
    })
    expect(h5TradePay).toHaveBeenCalledWith(
      { tradeNO: '2026070122001400000000000001' },
      expect.any(Function),
    )
    expect(routerReplace).toHaveBeenCalledWith({
      path: '/payment/result',
      query: {
        resume_token: 'resume-token',
        out_trade_no: 'sub2_alipay_jsapi',
      },
    })
  })

  it('shows the alipay callback configuration error when getAuthCode returns numeric error 15', async () => {
    const h5GetAuthCode = vi.fn((_options, callback) => callback({ error: 15 }))

    Object.defineProperty(window, 'ap', {
      value: {
        getAuthCode: h5GetAuthCode,
      },
      configurable: true,
    })

    const wrapper = mount(AlipayJSAPIPaymentView)
    await flushPromises()
    await flushPromises()

    expect(startAlipayJSAPI).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('支付宝授权回调地址不合法')
    expect(wrapper.text()).toContain(`${window.location.origin}/payment/alipay-jsapi`)
  })
})
