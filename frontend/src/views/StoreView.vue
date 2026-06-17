<template>
  <AppLayout>
    <div class="store-page mx-auto w-full max-w-6xl space-y-5">
      <section class="store-hero overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
        <div class="grid gap-5 p-5 md:grid-cols-[1.4fr_0.8fr] md:p-6">
          <div class="min-w-0">
            <p class="text-sm font-medium text-primary-600 dark:text-primary-400">{{ t('store.badge') }}</p>
            <h1 class="mt-2 text-2xl font-bold text-gray-900 dark:text-white md:text-3xl">
              {{ t('store.title') }}
            </h1>
            <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-300">
              {{ t('store.description') }}
            </p>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('store.productCount') }}</p>
              <p class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">{{ products.length }}</p>
            </div>
            <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('store.availableStock') }}</p>
              <p class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">{{ totalStock }}</p>
            </div>
          </div>
        </div>
      </section>

      <div v-if="loading" class="flex justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
      </div>

      <template v-else-if="paymentPhase === 'paying'">
        <PaymentStatusPanel
          :order-id="paymentState.orderId"
          :qr-code="paymentState.qrCode"
          :expires-at="paymentState.expiresAt"
          :payment-type="paymentState.paymentType"
          :pay-url="paymentState.payUrl"
          :order-type="paymentState.orderType"
          @done="resetPayment"
          @success="handlePaymentSuccess"
          @settled="handlePaymentSettled"
        />
      </template>

      <template v-else>
        <div class="flex gap-2 overflow-x-auto pb-1">
          <button
            type="button"
            class="store-filter"
            :class="{ 'store-filter-active': selectedCategoryId === 0 }"
            @click="selectedCategoryId = 0"
          >
            {{ t('common.all') }}
          </button>
          <button
            v-for="category in activeCategories"
            :key="category.id"
            type="button"
            class="store-filter"
            :class="{ 'store-filter-active': selectedCategoryId === category.id }"
            @click="selectedCategoryId = category.id"
          >
            {{ category.name }}
          </button>
        </div>

        <div v-if="filteredProducts.length === 0" class="rounded-lg border border-gray-200 bg-white p-12 text-center dark:border-dark-700 dark:bg-dark-900">
          <p class="text-gray-500 dark:text-dark-400">{{ t('store.empty') }}</p>
        </div>

        <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <article
            v-for="product in filteredProducts"
            :key="product.id"
            class="store-product-card"
          >
            <div class="flex min-w-0 flex-1 flex-col">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="truncate text-xs font-medium text-primary-600 dark:text-primary-400">
                    {{ product.category?.name || categoryName(product.category_id) }}
                  </p>
                  <h2 class="mt-1 line-clamp-2 text-lg font-semibold text-gray-900 dark:text-white">
                    {{ product.name }}
                  </h2>
                </div>
                <span class="shrink-0 rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-300">
                  {{ stockBadgeText(product) }}
                </span>
              </div>
              <div
                class="store-description"
                :class="{ 'store-description-interactive': productDescription(product) }"
                :tabindex="productDescription(product) ? 0 : undefined"
                :aria-describedby="productDescription(product) ? `store-product-description-${product.id}` : undefined"
              >
                <p class="line-clamp-3 min-h-[3.75rem] text-sm leading-5 text-gray-500 dark:text-dark-400">
                  {{ productDescriptionPreview(product) }}
                </p>
                <div
                  v-if="productDescription(product)"
                  :id="`store-product-description-${product.id}`"
                  class="store-description-popover"
                  role="tooltip"
                >
                  {{ productDescription(product) }}
                </div>
              </div>
              <div v-if="isDrawProduct(product)" class="mt-3 rounded-lg bg-gray-50 px-3 py-2 text-sm dark:bg-dark-800">
                <div class="flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.drawProgress') }}</span>
                  <span class="font-semibold text-gray-900 dark:text-white">{{ drawProgressText(product) }}</span>
                </div>
              </div>
              <div v-else-if="isLoadFactorCreditsProduct(product)" class="mt-3 rounded-lg bg-gray-50 px-3 py-2 text-sm dark:bg-dark-800">
                <div class="flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.loadFactorCreditsPerUnit') }}</span>
                  <span class="font-semibold text-emerald-600 dark:text-emerald-300">+{{ product.load_factor_credits_per_unit }}</span>
                </div>
              </div>
              <div class="mt-4 flex items-end justify-between gap-3">
                <div>
                  <span v-if="product.original_price" class="text-sm text-gray-400 line-through dark:text-dark-500">
                    ¥{{ product.original_price.toFixed(2) }}
                  </span>
                  <div class="text-2xl font-bold text-gray-900 dark:text-white">¥{{ product.price.toFixed(2) }}</div>
                </div>
                <button
                  type="button"
                  class="btn btn-primary min-h-[44px] px-4"
                  :disabled="!isProductPurchasable(product)"
                  @click="startCheckout(product)"
                >
                  {{ isProductPurchasable(product) ? t('store.buyNow') : t('store.soldOut') }}
                </button>
              </div>
            </div>
          </article>
        </div>
      </template>
    </div>

    <Teleport to="body">
      <Transition name="modal">
        <div v-if="checkoutProduct" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" @click.self="closeCheckout">
          <div class="w-full max-w-lg rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ checkoutProduct.name }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('store.checkoutTitle') }}</p>
              </div>
              <button type="button" class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 dark:hover:bg-dark-800" @click="closeCheckout">
                <span class="sr-only">{{ t('common.close') }}</span>
                x
              </button>
            </div>

            <div class="mt-5 space-y-4">
              <div>
                <label class="input-label">{{ t('store.quantity') }}</label>
                <input v-model.number="quantity" type="number" :min="checkoutMinQuantity" :max="checkoutMaxQuantity" class="input" />
              </div>

              <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
                <div class="flex justify-between">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.unitPrice') }}</span>
                  <span class="font-medium text-gray-900 dark:text-white">¥{{ checkoutProduct.price.toFixed(2) }}</span>
                </div>
                <div class="mt-2 flex justify-between">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.totalAmount') }}</span>
                  <span class="text-lg font-bold text-primary-600 dark:text-primary-400">¥{{ checkoutAmount.toFixed(2) }}</span>
                </div>
                <div v-if="isCheckoutDrawProduct" class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.drawRewardRange') }}</span>
                  <span class="text-right font-medium text-gray-900 dark:text-white">
                    {{ drawRewardRangeText }}
                  </span>
                </div>
                <div v-if="isCheckoutLoadFactorCreditsProduct" class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.loadFactorCreditsAwarded') }}</span>
                  <span class="text-right font-medium text-emerald-600 dark:text-emerald-300">
                    +{{ checkoutLoadFactorCreditsAmount }}
                  </span>
                </div>
                <div v-if="isCheckoutDrawProduct" class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.drawProgress') }}</span>
                  <span class="text-right font-medium text-gray-900 dark:text-white">
                    {{ drawProgressText(checkoutProduct) }}
                  </span>
                </div>
                <div v-if="authStore.isAuthenticated && checkoutProduct.allow_balance_payment !== false" class="mt-2 flex justify-between">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('payment.currentBalance') }}</span>
                  <span class="font-medium text-gray-900 dark:text-white">${{ currentBalance.toFixed(2) }}</span>
                </div>
                <div v-if="authStore.isAuthenticated && checkoutProduct.allow_points_payment" class="mt-2 flex justify-between">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.currentPoints') }}</span>
                  <span class="font-medium text-gray-900 dark:text-white">{{ currentPoints.toFixed(10).replace(/\.?0+$/, '') || '0' }}</span>
                </div>
              </div>

              <div>
                <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                  {{ t('store.payMethod') }}
                </label>
                <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
                  <button
                    v-if="isBalancePaymentAllowed"
                    type="button"
                    class="store-pay-option"
                    :class="{ 'store-pay-option-active': payMethod === 'balance' }"
                    :disabled="!canPayByBalance"
                    @click="payMethod = 'balance'"
                  >
                    <span class="font-semibold">{{ t('store.balancePay') }}</span>
                    <span class="text-xs text-gray-500 dark:text-dark-400">{{ balancePayHint }}</span>
                  </button>
                  <button
                    v-if="isPointsPaymentAllowed"
                    type="button"
                    class="store-pay-option"
                    :class="{ 'store-pay-option-active': payMethod === 'points' }"
                    :disabled="!canPayByPoints"
                    @click="payMethod = 'points'"
                  >
                    <span class="font-semibold">{{ t('store.pointsPay') }}</span>
                    <span class="text-xs text-gray-500 dark:text-dark-400">{{ pointsPayHint }}</span>
                  </button>
                  <button
                    v-if="isPlatformPaymentAllowed"
                    type="button"
                    class="store-pay-option"
                    :class="{ 'store-pay-option-active': payMethod === 'payment' }"
                    :disabled="methodOptions.length === 0"
                    @click="payMethod = 'payment'"
                  >
                    <span class="font-semibold">{{ t('store.gatewayPay') }}</span>
                    <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('store.gatewayPayHint') }}</span>
                  </button>
                </div>
              </div>

              <PaymentMethodSelector
                v-if="payMethod === 'payment' && methodOptions.length > 0"
                :methods="methodOptions"
                :selected="selectedMethod"
                @select="selectedMethod = $event"
              />

              <button
                type="button"
                class="btn btn-primary w-full min-h-[44px]"
                :disabled="!canSubmitCheckout || submitting"
                @click="submitCheckout"
              >
                {{ submitting ? t('common.processing') : t('store.confirmBuy') }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="modal">
        <div v-if="completedOrder" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" @click.self="closeCompletedOrder">
          <div class="w-full max-w-lg rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('store.purchaseSuccess') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                  {{ t('store.deliveryReady', { orderNo: completedOrder.order_no }) }}
                </p>
              </div>
              <button type="button" class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 dark:hover:bg-dark-800" @click="closeCompletedOrder">
                <span class="sr-only">{{ t('common.close') }}</span>
                x
              </button>
            </div>

            <div class="mt-5 space-y-4">
              <div class="rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
                <div class="flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.product') }}</span>
                  <span class="text-right font-medium text-gray-900 dark:text-white">{{ completedOrder.product_name }}</span>
                </div>
                <div class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.quantity') }}</span>
                  <span class="font-medium text-gray-900 dark:text-white">{{ completedOrder.quantity }}</span>
                </div>
                <div v-if="completedOrder.draw_reward_amount !== null && completedOrder.draw_reward_amount !== undefined" class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.drawReward') }}</span>
                  <span class="font-medium text-emerald-600 dark:text-emerald-300">
                    {{ formatDrawReward(completedOrder) }}
                  </span>
                </div>
                <div v-if="completedOrder.load_factor_credits_awarded > 0" class="mt-2 flex justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-400">{{ t('store.loadFactorCreditsAwarded') }}</span>
                  <span class="font-medium text-emerald-600 dark:text-emerald-300">
                    +{{ completedOrder.load_factor_credits_awarded }}
                  </span>
                </div>
              </div>

              <div v-if="shouldShowDeliveredCards(completedOrder)">
                <div class="mb-2 flex items-center justify-between gap-3">
                  <label class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('store.deliveredCards') }}</label>
                  <button
                    v-if="completedOrder.delivered_cards.length > 0"
                    type="button"
                    class="btn btn-secondary btn-sm"
                    @click="copyDeliveredCards"
                  >
                    {{ t('common.copy') }}
                  </button>
                </div>
                <div v-if="completedOrder.delivered_cards.length > 0" class="max-h-72 space-y-2 overflow-y-auto rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
                  <code
                    v-for="(card, index) in completedOrder.delivered_cards"
                    :key="index"
                    class="block break-all rounded-md bg-white px-3 py-2 font-mono text-xs text-gray-900 dark:bg-dark-900 dark:text-dark-100"
                  >
                    {{ card }}
                  </code>
                </div>
                <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                  {{ t('store.deliveryPending') }}
                </p>
              </div>

              <DeliveredFilesList
                v-if="completedOrder.delivered_files.length > 0"
                :order-id="completedOrder.id"
                :files="completedOrder.delivered_files"
              />

              <button type="button" class="btn btn-primary w-full min-h-[44px]" @click="closeCompletedOrder">
                {{ t('common.confirm') }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { paymentAPI } from '@/api/payment'
import { storeAPI } from '@/api/store'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { useClipboard } from '@/composables/useClipboard'
import { isMobileDevice } from '@/utils/device'
import AppLayout from '@/components/layout/AppLayout.vue'
import DeliveredFilesList from '@/components/store/DeliveredFilesList.vue'
import PaymentMethodSelector from '@/components/payment/PaymentMethodSelector.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import { METHOD_ORDER, getPaymentPopupFeatures } from '@/components/payment/providerConfig'
import { decidePaymentLaunch, getVisibleMethods, normalizeVisibleMethod, type PaymentRecoverySnapshot } from '@/components/payment/paymentFlow'
import type { CheckoutInfoResponse, CreateOrderResult } from '@/types/payment'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { formatStoreDrawReward } from '@/utils/storeRewards'
import type { StoreCategory, StoreDrawConfig, StoreOrder, StorePayMethod, StoreProduct } from '@/types/store'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(true)
const submitting = ref(false)
const categories = ref<StoreCategory[]>([])
const products = ref<StoreProduct[]>([])
const checkout = ref<CheckoutInfoResponse | null>(null)
const selectedCategoryId = ref(0)
const checkoutProduct = ref<StoreProduct | null>(null)
const completedOrder = ref<StoreOrder | null>(null)
const quantity = ref(1)
const payMethod = ref<StorePayMethod>('balance')
const selectedMethod = ref('')
const paymentPhase = ref<'select' | 'paying'>('select')
const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())
const activeShopOrderId = ref(0)
const checkoutIdempotencyKey = ref('')

const activeCategories = computed(() => categories.value.filter(category => category.enabled))
const filteredProducts = computed(() => products.value.filter((product) =>
  product.enabled && (selectedCategoryId.value === 0 || product.category_id === selectedCategoryId.value),
))
const totalStock = computed(() => products.value.reduce((sum, product) => sum + (product.stock_unlimited ? 0 : Math.max(0, product.stock)), 0))
const currentBalance = computed(() => Number(authStore.user?.balance || 0))
const currentPoints = computed(() => Number(authStore.user?.points_balance || 0))
const checkoutMinQuantity = computed(() => Math.max(1, checkoutProduct.value?.min_purchase || 1))
const checkoutMaxQuantity = computed(() => {
  const product = checkoutProduct.value
  if (!product) return 1
  if (isDrawProduct(product)) return 1
  const maxPurchase = product.max_purchase > 0 ? product.max_purchase : product.stock
  if (product.stock_unlimited) return Math.max(checkoutMinQuantity.value, maxPurchase || checkoutMinQuantity.value)
  return Math.max(checkoutMinQuantity.value, Math.min(product.stock, maxPurchase))
})
const checkoutAmount = computed(() => {
  const product = checkoutProduct.value
  if (!product) return 0
  return Math.round(product.price * Math.max(checkoutMinQuantity.value, quantity.value || checkoutMinQuantity.value) * 100) / 100
})
const visibleMethods = computed(() => getVisibleMethods(checkout.value?.methods || {}))
const isCheckoutDrawProduct = computed(() => isDrawProduct(checkoutProduct.value || null))
const isCheckoutLoadFactorCreditsProduct = computed(() => isLoadFactorCreditsProduct(checkoutProduct.value || null))
const checkoutLoadFactorCreditsAmount = computed(() => {
  const product = checkoutProduct.value
  if (!product || !isLoadFactorCreditsProduct(product)) return 0
  return Math.max(0, Number(product.load_factor_credits_per_unit || 0)) * Math.max(checkoutMinQuantity.value, quantity.value || checkoutMinQuantity.value)
})
const drawRewardRangeText = computed(() => {
  const config = checkoutProduct.value?.draw_config
  if (!config) return ''
  return formatDrawRewardRange(checkoutProduct.value, config)
})
const enabledMethods = computed(() => Object.keys(visibleMethods.value).sort((a, b) => {
  const ai = METHOD_ORDER.indexOf(a as typeof METHOD_ORDER[number])
  const bi = METHOD_ORDER.indexOf(b as typeof METHOD_ORDER[number])
  return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
}))
const methodOptions = computed<PaymentMethodOption[]>(() => enabledMethods.value.map((type) => ({
  type,
  fee_rate: visibleMethods.value[type]?.fee_rate ?? 0,
  available: visibleMethods.value[type]?.available !== false && amountFitsMethod(checkoutAmount.value, type),
})))
const isBalancePaymentAllowed = computed(() => checkoutProduct.value?.allow_balance_payment !== false)
const isPointsPaymentAllowed = computed(() => checkoutProduct.value?.allow_points_payment === true)
const isPlatformPaymentAllowed = computed(() => checkoutProduct.value?.allow_platform_payment !== false)
const canPayByBalance = computed(() => isBalancePaymentAllowed.value && currentBalance.value >= checkoutAmount.value && checkoutAmount.value > 0)
const canPayByPoints = computed(() => isPointsPaymentAllowed.value && currentPoints.value >= checkoutAmount.value && checkoutAmount.value > 0)
const balancePayHint = computed(() => {
  if (!isBalancePaymentAllowed.value) return t('store.paymentMethodUnavailable')
  return canPayByBalance.value ? t('store.balanceEnough') : t('store.balanceNotEnough')
})
const pointsPayHint = computed(() => canPayByPoints.value ? t('store.pointsEnough') : t('store.pointsNotEnough'))
const canSubmitCheckout = computed(() => {
  if (!checkoutProduct.value || quantity.value < checkoutMinQuantity.value || quantity.value > checkoutMaxQuantity.value) return false
  if (payMethod.value === 'balance') return canPayByBalance.value
  if (payMethod.value === 'points') return canPayByPoints.value
  return isPlatformPaymentAllowed.value && !!selectedMethod.value && amountFitsMethod(checkoutAmount.value, selectedMethod.value)
})

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    createdAt: 0,
  }
}

function categoryName(id?: number | null): string {
  if (!id) return t('store.uncategorized')
  return categories.value.find(category => category.id === id)?.name || t('store.uncategorized')
}

function amountFitsMethod(amount: number, method: string): boolean {
  const limit = visibleMethods.value[method]
  if (!limit || limit.available === false || amount <= 0) return false
  if (limit.single_min > 0 && amount < limit.single_min) return false
  if (limit.single_max > 0 && amount > limit.single_max) return false
  return true
}

function isProductPurchasable(product: StoreProduct): boolean {
  return isDrawProduct(product) || isLoadFactorCreditsProduct(product) || product.stock > 0
}

function stockBadgeText(product: StoreProduct): string {
  if (isLoadFactorCreditsProduct(product)) return t('store.loadFactorCreditsProductBadge')
  if (isDrawProduct(product)) return t('store.drawProductBadge')
  return t('store.stock', { count: product.stock })
}

function productDescription(product: StoreProduct): string {
  return product.description?.trim() || ''
}

function productDescriptionPreview(product: StoreProduct): string {
  return productDescription(product) || t('store.noDescription')
}

function isDrawProduct(product: StoreProduct | null): boolean {
  return product?.product_type === 'balance_draw' || product?.product_type === 'points_draw'
}

function isLoadFactorCreditsProduct(product: StoreProduct | null): boolean {
  return product?.product_type === 'load_factor_credits'
}

function shouldShowDeliveredCards(order: StoreOrder): boolean {
  return order.load_factor_credits_awarded <= 0 && (order.draw_reward_amount === null || order.draw_reward_amount === undefined)
}

function drawRewardUnit(productType?: string | null): string {
  return productType === 'points_draw' ? '' : '$'
}

function formatDrawReward(order: StoreOrder): string {
  return formatStoreDrawReward(order)
}

function formatDrawRewardRange(product: StoreProduct | null, config: StoreDrawConfig): string {
  if (product?.product_type === 'points_draw') {
    const min = config.min_amount.toFixed(10).replace(/\.?0+$/, '') || '0'
    const max = config.max_amount.toFixed(10).replace(/\.?0+$/, '') || '0'
    return `${min} - ${max}`
  }
  return `${drawRewardUnit(product?.product_type)}${config.min_amount.toFixed(2)} - ${drawRewardUnit(product?.product_type)}${config.max_amount.toFixed(2)}`
}

function drawProgressText(product: StoreProduct | null): string {
  if (!product?.draw_config) return '0/0'
  const progress = product.draw_progress
  const drawn = Math.max(0, progress?.drawn_count ?? 0)
  const total = Math.max(0, progress?.guarantee_count ?? product.draw_config.guarantee_count)
  return `${drawn}/${total}`
}

function createCheckoutIdempotencyKey(): string {
  const crypto = globalThis.crypto
  if (crypto?.randomUUID) {
    return crypto.randomUUID()
  }
  if (!crypto?.getRandomValues) {
    throw new Error('crypto.getRandomValues is required to create store orders')
  }

  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0'))
  return [
    hex.slice(0, 4).join(''),
    hex.slice(4, 6).join(''),
    hex.slice(6, 8).join(''),
    hex.slice(8, 10).join(''),
    hex.slice(10, 16).join(''),
  ].join('-')
}

async function startCheckout(product: StoreProduct) {
  if (!authStore.isAuthenticated) {
    router.push({ path: '/login', query: { redirect: '/store' } })
    return
  }
  try {
    await ensureCheckoutInfo()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'store.errors', t('common.error')))
    return
  }
  if (checkoutProduct.value?.id !== product.id || !checkoutIdempotencyKey.value) {
    checkoutIdempotencyKey.value = createCheckoutIdempotencyKey()
  }
  checkoutProduct.value = product
  quantity.value = Math.max(1, product.min_purchase || 1)
  if (canPayByBalance.value) {
    payMethod.value = 'balance'
  } else if (product.allow_points_payment && canPayByPoints.value) {
    payMethod.value = 'points'
  } else if (product.allow_platform_payment !== false) {
    payMethod.value = 'payment'
  } else if (product.allow_balance_payment !== false) {
    payMethod.value = 'balance'
  } else {
    payMethod.value = 'points'
  }
  if (!selectedMethod.value && enabledMethods.value.length > 0) {
    selectedMethod.value = enabledMethods.value[0]
  }
}

function closeCheckout() {
  if (submitting.value) return
  checkoutProduct.value = null
  checkoutIdempotencyKey.value = ''
}

function closeCompletedOrder() {
  completedOrder.value = null
}

function showCompletedOrder(order: StoreOrder) {
  completedOrder.value = order
}

async function copyDeliveredCards() {
  const cards = completedOrder.value?.delivered_cards || []
  if (cards.length === 0) return
  await copyToClipboard(cards.join('\n'), t('store.cardsCopied'))
}

async function submitCheckout() {
  if (!checkoutProduct.value || !canSubmitCheckout.value || submitting.value) return
  submitting.value = true
  try {
    const orderResponse = await storeAPI.createOrder({
      product_id: checkoutProduct.value.id,
      quantity: quantity.value,
      payment_method: payMethod.value === 'payment' ? selectedMethod.value : payMethod.value,
      return_url: `${window.location.origin}/payment/result`,
      payment_source: selectedMethod.value === 'wxpay' && /MicroMessenger/i.test(window.navigator.userAgent)
        ? 'wechat_in_app_resume'
        : 'hosted_redirect',
      is_mobile: isMobileDevice(),
    }, checkoutIdempotencyKey.value)
    const storeOrder = orderResponse.data
    if (payMethod.value === 'balance' || payMethod.value === 'points') {
      if (authStore.user) {
        const drawReward = Number(storeOrder.draw_reward_amount || 0)
        const rewardIsPoints = storeOrder.draw_reward_type === 'points' || storeOrder.product_type === 'points_draw'
        const loadFactorCreditsAwarded = Number(storeOrder.load_factor_credits_awarded || 0)
        if (payMethod.value === 'balance') {
          authStore.user.balance = Math.max(0, currentBalance.value - storeOrder.total_amount + (rewardIsPoints ? 0 : drawReward))
          if (rewardIsPoints && drawReward > 0) {
            authStore.user.points_balance = currentPoints.value + drawReward
          }
        } else {
          authStore.user.points_balance = Math.max(0, currentPoints.value - storeOrder.total_amount + (rewardIsPoints ? drawReward : 0))
          if (!rewardIsPoints && drawReward > 0) {
            authStore.user.balance = currentBalance.value + drawReward
          }
        }
        if (loadFactorCreditsAwarded > 0) {
          authStore.user.load_factor_credits_balance = Number(authStore.user.load_factor_credits_balance || 0) + loadFactorCreditsAwarded
        }
      }
      appStore.showSuccess(t('store.purchaseSuccess'))
      checkoutProduct.value = null
      checkoutIdempotencyKey.value = ''
      showCompletedOrder(storeOrder)
      await loadStore()
      return
    }

    launchPayment(storeOrder)
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'store.errors', t('common.error')))
  } finally {
    submitting.value = false
  }
}

function launchPayment(storeOrder: StoreOrder) {
  const result = storeOrder.payment as CreateOrderResult | null | undefined
  if (!result) {
    throw new Error(t('store.errors.UNHANDLED_PAYMENT_SCENARIO'))
  }
  activeShopOrderId.value = storeOrder.id
  const visibleMethod = normalizeVisibleMethod(result.payment_type || selectedMethod.value) || selectedMethod.value
  const stripeRouteUrl = result.client_secret
    ? router.resolve({
      path: '/payment/stripe',
      query: {
        order_id: String(result.order_id),
        client_secret: result.client_secret,
        method: visibleMethod === 'stripe' ? undefined : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay',
        resume_token: result.resume_token || undefined,
      },
    }).href
    : ''
  const decision = decidePaymentLaunch(result, {
    visibleMethod,
    orderType: 'shop',
    isMobile: isMobileDevice(),
    isWechatBrowser: /MicroMessenger/i.test(window.navigator.userAgent),
    stripePopupUrl: stripeRouteUrl,
    stripeRouteUrl,
  })
  if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
    window.location.href = decision.oauth.authorize_url
    return
  }
  if (decision.kind === 'unhandled') {
    throw new Error(t('store.errors.UNHANDLED_PAYMENT_SCENARIO'))
  }
  paymentState.value = decision.paymentState
  paymentPhase.value = 'paying'
  checkoutProduct.value = null
  checkoutIdempotencyKey.value = ''
  if (decision.kind === 'stripe_popup' || decision.kind === 'redirect_waiting') {
    const url = decision.paymentState.payUrl
    if (url) {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) window.location.href = url
    }
  }
  if (decision.kind === 'stripe_route') {
    window.location.href = decision.paymentState.payUrl
  }
}

function resetPayment() {
  paymentState.value = emptyPaymentState()
  paymentPhase.value = 'select'
  activeShopOrderId.value = 0
}

async function handlePaymentSuccess() {
  appStore.showSuccess(t('store.purchaseSuccess'))
  if (activeShopOrderId.value > 0) {
    try {
      const order = await waitForDeliveredOrder(activeShopOrderId.value)
      showCompletedOrder(order)
      if (order.load_factor_credits_awarded > 0) {
        await authStore.refreshUser()
      }
    } catch (err: unknown) {
      appStore.showError(extractI18nErrorMessage(err, t, 'store.errors', t('common.error')))
    }
  }
  await loadStore()
}

function handlePaymentSettled(outcome: string) {
  if (outcome !== 'success') {
    loadStore()
  }
}

async function loadStore() {
  const [categoriesResp, productsResp] = await Promise.all([storeAPI.getCategories(), storeAPI.getProducts()])
  categories.value = categoriesResp.data || []
  products.value = productsResp.data.items || []
  if (authStore.isAuthenticated) {
    await ensureCheckoutInfo()
    const { data } = await storeAPI.getDrawProgress()
    products.value = products.value.map(product => ({
      ...product,
      draw_progress: data?.[product.id] || product.draw_progress || null,
    }))
  }
  if (!selectedMethod.value && enabledMethods.value.length > 0) {
    selectedMethod.value = enabledMethods.value[0]
  }
}

async function ensureCheckoutInfo() {
  if (checkout.value) return
  const { data } = await paymentAPI.getCheckoutInfo()
  checkout.value = data
}

async function waitForDeliveredOrder(orderId: number): Promise<StoreOrder> {
  let latest: StoreOrder | null = null
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const { data } = await storeAPI.getOrder(orderId)
    latest = data
    if (data.status === 'completed' && (data.delivered_cards.length > 0 || data.delivered_files.length > 0 || data.load_factor_credits_awarded > 0 || data.draw_reward_amount !== null && data.draw_reward_amount !== undefined)) {
      return data
    }
    await new Promise(resolve => setTimeout(resolve, 700))
  }
  if (!latest) {
    throw new Error(t('store.errors.SHOP_ORDER_NOT_FOUND'))
  }
  return latest
}

onMounted(async () => {
  try {
    await loadStore()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'store.errors', t('common.error')))
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.store-filter {
  min-height: 2.75rem;
  flex: 0 0 auto;
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  background: white;
  padding: 0 1rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(75 85 99);
}

.dark .store-filter {
  border-color: rgb(55 65 81);
  background: rgb(17 24 39);
  color: rgb(209 213 219);
}

.store-filter-active {
  border-color: rgb(59 130 246);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.dark .store-filter-active {
  border-color: rgb(96 165 250);
  background: rgba(30, 64, 175, 0.35);
  color: rgb(147 197 253);
}

.store-product-card {
  display: flex;
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  background: white;
  padding: 1rem;
}

.dark .store-product-card {
  border-color: rgb(55 65 81);
  background: rgb(17 24 39);
}

.store-description {
  position: relative;
  margin-top: 0.75rem;
  outline: none;
}

.store-description-interactive {
  cursor: help;
}

.store-description-interactive:focus-visible {
  border-radius: 0.375rem;
  box-shadow: 0 0 0 2px rgb(59 130 246 / 0.45);
}

.store-description-popover {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 100%;
  z-index: 30;
  max-height: 14rem;
  overflow-y: auto;
  border-radius: 0.5rem;
  border: 1px solid rgb(209 213 219);
  background: rgb(17 24 39);
  padding: 0.75rem;
  color: white;
  font-size: 0.875rem;
  line-height: 1.5;
  opacity: 0;
  pointer-events: none;
  transform: translateY(0.25rem);
  visibility: hidden;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  box-shadow: 0 18px 35px rgb(15 23 42 / 0.22);
  transition: opacity 150ms ease, transform 150ms ease, visibility 150ms ease;
}

.dark .store-description-popover {
  border-color: rgb(75 85 99);
  background: rgb(31 41 55);
  box-shadow: 0 18px 35px rgb(0 0 0 / 0.4);
}

.store-description:hover .store-description-popover,
.store-description:focus .store-description-popover {
  opacity: 1;
  pointer-events: auto;
  transform: translateY(0);
  visibility: visible;
}

.store-pay-option {
  min-height: 4rem;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 0.25rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(209 213 219);
  background: white;
  padding: 0.75rem;
  text-align: left;
}

.dark .store-pay-option {
  border-color: rgb(75 85 99);
  background: rgb(31 41 55);
}

.store-pay-option:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.store-pay-option-active {
  border-color: rgb(59 130 246);
  background: rgb(239 246 255);
}

.dark .store-pay-option-active {
  border-color: rgb(96 165 250);
  background: rgba(30, 64, 175, 0.35);
}
</style>
