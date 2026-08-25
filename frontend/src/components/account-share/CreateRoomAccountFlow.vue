<template>
  <CreateAccountModal
    :show="show && stage === 'creating'"
    :title="t('accountShare.roomAccounts.createFlow.creatorTitle', { room: roomDisplayName })"
    :proxies="effectiveProxies"
    :groups="[]"
    account-scope="user"
    :allow-proxy="true"
    :allow-billing-rate="false"
    :initial-platform="roomPlatform"
    :lock-platform="true"
    :initial-account-level="roomAccountLevel"
    :lock-account-level="roomPlatform === 'openai'"
    :allow-multiple-o-auth="false"
    @created="handleAccountCreated"
    @close="requestClose"
    @proxy-scope-change="handleProxyScopeChange"
  />

  <BaseDialog
    :show="show && stage !== 'creating'"
    :title="t('accountShare.roomAccounts.createFlow.progressTitle')"
    width="normal"
    :close-disabled="busy"
    :close-on-escape="!busy"
    :close-on-click-outside="false"
    panel-class="create-room-account-flow-panel"
    @close="requestClose"
  >
    <div class="create-room-account-flow" data-testid="create-room-account-flow">
      <header class="flow-room-context">
        <span class="flow-room-mark" aria-hidden="true">
          <Icon name="database" size="sm" />
        </span>
        <div class="min-w-0">
          <span>{{ t('accountShare.roomAccounts.createFlow.targetRoom') }}</span>
          <strong>{{ roomDisplayName }}</strong>
          <small>{{ roomPlatformLabel }} · {{ roomAccountLevel }}</small>
        </div>
      </header>

      <div
        v-if="stage === 'preparing'"
        class="flow-preparing"
        role="status"
        data-testid="create-room-account-preparing"
      >
        <Icon name="refresh" size="sm" class="animate-spin" />
        <span>{{ t('accountShare.roomAccounts.createFlow.preparing') }}</span>
      </div>

      <ol v-else class="flow-steps" aria-live="polite">
        <li :class="stepClass('created')">
          <span class="flow-step-index">
            <Icon v-if="createdStepState === 'complete'" name="checkCircle" size="sm" />
            <Icon v-else-if="createdStepState === 'active'" name="refresh" size="sm" class="animate-spin" />
            <span v-else>1</span>
          </span>
          <div>
            <strong>{{ t('accountShare.roomAccounts.createFlow.created') }}</strong>
            <small v-if="createdAccount">
              {{ createdAccount.name }} · #{{ createdAccount.id }}
            </small>
            <small v-else>{{ t('accountShare.roomAccounts.createFlow.identifying') }}</small>
          </div>
        </li>
        <li :class="stepClass('converting')">
          <span class="flow-step-index">
            <Icon v-if="conversionStepState === 'complete'" name="checkCircle" size="sm" />
            <Icon v-else-if="conversionStepState === 'active'" name="refresh" size="sm" class="animate-spin" />
            <Icon v-else-if="conversionStepState === 'error'" name="exclamationCircle" size="sm" />
            <span v-else>2</span>
          </span>
          <div>
            <strong>{{ t('accountShare.roomAccounts.createFlow.converting') }}</strong>
            <small>{{ t('accountShare.roomAccounts.createFlow.convertingHint') }}</small>
          </div>
        </li>
        <li :class="stepClass('attaching')">
          <span class="flow-step-index">
            <Icon v-if="attachStepState === 'complete'" name="checkCircle" size="sm" />
            <Icon v-else-if="attachStepState === 'active'" name="refresh" size="sm" class="animate-spin" />
            <Icon v-else-if="attachStepState === 'error'" name="exclamationCircle" size="sm" />
            <span v-else>3</span>
          </span>
          <div>
            <strong>{{ t('accountShare.roomAccounts.createFlow.attaching') }}</strong>
            <small>{{ t('accountShare.roomAccounts.createFlow.attachingHint') }}</small>
          </div>
        </li>
      </ol>

      <div
        v-if="errorMessage"
        class="flow-result flow-result-error"
        role="alert"
        data-testid="create-room-account-error"
      >
        <Icon name="exclamationCircle" size="sm" />
        <div>
          <strong>{{ errorTitle }}</strong>
          <p>{{ errorMessage }}</p>
          <small v-if="createdAccount">
            {{ t('accountShare.roomAccounts.createFlow.accountPreserved', {
              name: createdAccount.name,
              id: createdAccount.id
            }) }}
          </small>
        </div>
      </div>

      <div
        v-if="stage === 'completed'"
        class="flow-result flow-result-success"
        role="status"
        data-testid="create-room-account-completed"
      >
        <Icon name="checkCircle" size="sm" />
        <div>
          <strong>{{ t('accountShare.roomAccounts.createFlow.completed') }}</strong>
          <p>{{ t('accountShare.roomAccounts.createFlow.completedHint', { room: roomDisplayName }) }}</p>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flow-footer">
        <button
          v-if="stage === 'resolve_failed'"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="busy"
          data-testid="retry-resolve-created-account"
          @click="retryResolveCreatedAccount"
        >
          <Icon name="refresh" size="sm" class="mr-2" />
          {{ t('accountShare.roomAccounts.createFlow.retryIdentify') }}
        </button>
        <button
          v-if="stage === 'conversion_failed'"
          type="button"
          class="btn btn-primary min-h-11"
          :disabled="busy"
          data-testid="retry-room-account-conversion"
          @click="retryConversion"
        >
          <Icon name="refresh" size="sm" class="mr-2" />
          {{ t('accountShare.roomAccounts.createFlow.retryConversion') }}
        </button>
        <button
          v-if="stage === 'attach_failed'"
          type="button"
          class="btn btn-primary min-h-11"
          :disabled="busy"
          data-testid="retry-room-account-attach"
          @click="retryAttach"
        >
          <Icon name="refresh" size="sm" class="mr-2" />
          {{ t('accountShare.roomAccounts.createFlow.retryAttach') }}
        </button>
        <button
          v-if="!busy"
          type="button"
          class="btn btn-secondary min-h-11"
          data-testid="close-create-room-account-flow"
          @click="requestClose"
        >
          {{ t('common.close') }}
        </button>
        <span v-else class="flow-busy-note">
          {{ t('accountShare.roomAccounts.createFlow.keepOpen') }}
        </span>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="discardConfirmationOpen"
    title="放弃新增账号？"
    width="narrow"
    :z-index="70"
    :close-on-click-outside="false"
    @close="cancelDiscard"
  >
    <div class="flow-discard-confirmation">
      <span class="flow-discard-icon" aria-hidden="true">
        <Icon name="exclamationCircle" size="sm" />
      </span>
      <div>
        <strong>当前账号信息尚未提交</strong>
        <p>关闭后，本次填写内容和 OAuth 授权进度不会保留；已经创建成功的账号不会被删除。</p>
      </div>
    </div>

    <template #footer>
      <div class="flow-footer">
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          data-testid="continue-create-room-account"
          @click="cancelDiscard"
        >
          继续填写
        </button>
        <button
          type="button"
          class="btn btn-danger min-h-11"
          data-testid="discard-create-room-account"
          @click="confirmDiscard"
        >
          放弃并关闭
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  accountShareAPI,
  loadAllPaginatedItems,
  type AccountShareListing
} from '@/api/accountShare'
import { accountsAPI } from '@/api/accounts'
import type { Account, AccountPlatform, Proxy } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'

type CreateRoomAccountStage =
  | 'preparing'
  | 'creating'
  | 'created'
  | 'converting'
  | 'attaching'
  | 'completed'
  | 'resolve_failed'
  | 'conversion_failed'
  | 'attach_failed'

type FlowStep = 'created' | 'converting' | 'attaching'
type FlowStepState = 'pending' | 'active' | 'complete' | 'error'

const props = defineProps<{
  show: boolean
  listing: AccountShareListing | null
  proxies: Proxy[]
}>()

const emit = defineEmits<{
  close: []
  completed: [{ accountID: number }]
}>()

const { t } = useI18n()
const stage = ref<CreateRoomAccountStage>('preparing')
const createdAccount = ref<Account | null>(null)
const accountIDsBeforeCreate = ref(new Set<number>())
const snapshotLoaded = ref(false)
const errorMessage = ref('')
const conversionIdempotencyKey = ref('')
const attachIdempotencyKey = ref('')
const discardConfirmationOpen = ref(false)
let activationVersion = 0
let completionEmitted = false
let accountSnapshotRequestController: AbortController | null = null

const roomDisplayName = computed(() => (
  props.listing?.room_name
  || props.listing?.account_name
  || (props.listing ? `#${props.listing.id}` : '')
))

const roomPlatform = computed<AccountPlatform>(() => {
  const platform = props.listing?.platform
  if (platform === 'anthropic') return 'anthropic'
  if (platform === 'opencode') return 'opencode'
  return 'openai'
})

const roomPlatformLabel = computed(() => {
  if (roomPlatform.value === 'anthropic') return 'Anthropic'
  if (roomPlatform.value === 'opencode') return 'Opencode'
  return 'OpenAI'
})

const roomAccountLevel = computed(() => (
  typeof props.listing?.account_level === 'string' && props.listing.account_level.trim()
    ? props.listing.account_level
    : 'unknown'
))

// 父级传下来的 proxies 是「创建房间」表单那次拉取的结果，范围未必等于本房间的
// 平台/等级；而这里的平台和等级是锁死的。所以按模态回报的范围自己取一次，
// 取到之前先用父级的列表兜底。
const scopedProxies = ref<Proxy[]>([])
const scopedProxiesLoaded = ref(false)
let scopedProxyScopeKey = ''
let scopedProxyRequestSeq = 0

const effectiveProxies = computed<Proxy[]>(() => (
  scopedProxiesLoaded.value ? scopedProxies.value : props.proxies
))

async function handleProxyScopeChange(scope: { platform: string; account_level: string }): Promise<void> {
  const scopeKey = `${scope.platform || ''}|${scope.account_level || ''}`
  if (scopeKey === scopedProxyScopeKey && scopedProxiesLoaded.value) {
    scopedProxyRequestSeq++
    return
  }
  const seq = ++scopedProxyRequestSeq
  try {
    const list = await accountShareAPI.listProxies({
      platform: scope.platform,
      account_level: scope.account_level
    })
    if (seq !== scopedProxyRequestSeq) return
    scopedProxies.value = list
    scopedProxyScopeKey = scopeKey
    scopedProxiesLoaded.value = true
  } catch (error) {
    if (seq !== scopedProxyRequestSeq) return
    // 取不到就继续用父级列表，至少不比修复前差。
    console.error('Failed to load room-scoped proxies:', error)
  }
}

const busy = computed(() => (
  stage.value === 'preparing'
  || stage.value === 'created'
  || stage.value === 'converting'
  || stage.value === 'attaching'
))

const createdStepState = computed<FlowStepState>(() => {
  if (stage.value === 'created' && !createdAccount.value) return 'active'
  if (createdAccount.value) return 'complete'
  return 'pending'
})

const conversionStepState = computed<FlowStepState>(() => {
  if (stage.value === 'converting') return 'active'
  if (stage.value === 'conversion_failed') return 'error'
  if (stage.value === 'attaching' || stage.value === 'attach_failed' || stage.value === 'completed') {
    return 'complete'
  }
  return 'pending'
})

const attachStepState = computed<FlowStepState>(() => {
  if (stage.value === 'attaching') return 'active'
  if (stage.value === 'attach_failed') return 'error'
  if (stage.value === 'completed') return 'complete'
  return 'pending'
})

const errorTitle = computed(() => {
  if (stage.value === 'resolve_failed') return t('accountShare.roomAccounts.createFlow.identifyFailed')
  if (stage.value === 'conversion_failed') return t('accountShare.roomAccounts.createFlow.conversionFailed')
  return t('accountShare.roomAccounts.createFlow.attachFailed')
})

watch(
  () => [props.show, props.listing?.id] as const,
  ([show]) => {
    const currentActivation = ++activationVersion
    accountSnapshotRequestController?.abort()
    accountSnapshotRequestController = null
    resetFlow()
    if (!show || !props.listing) return
    void prepareCreator(currentActivation)
  },
  { immediate: true }
)

function resetFlow(): void {
  stage.value = 'preparing'
  createdAccount.value = null
  accountIDsBeforeCreate.value = new Set()
  snapshotLoaded.value = false
  errorMessage.value = ''
  conversionIdempotencyKey.value = ''
  attachIdempotencyKey.value = ''
  discardConfirmationOpen.value = false
  completionEmitted = false
  // 换房间/重开流程后上一轮按范围取到的代理不再适用；序号自增顺带作废还在飞的旧请求，
  // 否则它回来会把上一个房间的列表标记成「已加载」。
  scopedProxies.value = []
  scopedProxiesLoaded.value = false
  scopedProxyScopeKey = ''
  scopedProxyRequestSeq++
}

async function prepareCreator(currentActivation: number): Promise<void> {
  try {
    const existingAccounts = await listAllPlatformAccounts(roomPlatform.value, currentActivation)
    if (currentActivation !== activationVersion || !props.show) return
    accountIDsBeforeCreate.value = new Set(existingAccounts.map((account) => account.id))
    snapshotLoaded.value = true
  } catch {
    if (currentActivation !== activationVersion || !props.show) return
    snapshotLoaded.value = false
  }
  if (currentActivation === activationVersion && props.show) {
    stage.value = 'creating'
  }
}

async function listAllPlatformAccounts(
  platform: AccountPlatform,
  currentActivation: number
): Promise<Account[]> {
  accountSnapshotRequestController?.abort()
  const controller = new AbortController()
  accountSnapshotRequestController = controller
  try {
    const loadedAccounts = await loadAllPaginatedItems(
      (page) => accountsAPI.list(page, 100, { platform }, { signal: controller.signal }),
      {
        signal: controller.signal,
        isCurrent: () => (
          currentActivation === activationVersion
          && props.show
          && roomPlatform.value === platform
        ),
        concurrency: 3
      }
    )
    const accountByID = new Map<number, Account>()
    for (const account of loadedAccounts) accountByID.set(account.id, account)
    return Array.from(accountByID.values())
  } finally {
    if (accountSnapshotRequestController === controller) {
      accountSnapshotRequestController = null
    }
  }
}

async function handleAccountCreated(accounts?: Account[]): Promise<void> {
  if (stage.value !== 'creating') return
  const currentActivation = activationVersion
  const listingID = props.listing?.id
  if (!listingID) return
  stage.value = 'created'
  errorMessage.value = ''
  const resolved = await resolveCreatedAccount(accounts, currentActivation, listingID)
  if (!resolved) return
  if (!isCurrentFlow(currentActivation, listingID)) return
  createdAccount.value = resolved
  await startConversion(currentActivation)
}

async function resolveCreatedAccount(
  accounts: Account[] | undefined,
  currentActivation: number,
  listingID: number
): Promise<Account | null> {
  const returnedAccounts = (accounts || []).filter((account) => (
    Number.isSafeInteger(account?.id)
    && account.id > 0
  ))
  if (returnedAccounts.length === 1) {
    return isCurrentFlow(currentActivation, listingID) ? returnedAccounts[0] : null
  }
  if (returnedAccounts.length > 1) {
    if (!isCurrentFlow(currentActivation, listingID)) return null
    stage.value = 'resolve_failed'
    errorMessage.value = t('accountShare.roomAccounts.createFlow.multipleCreated')
    return null
  }

  try {
    const currentAccounts = await listAllPlatformAccounts(roomPlatform.value, currentActivation)
    if (!isCurrentFlow(currentActivation, listingID) || stage.value !== 'created') return null
    const candidates = snapshotLoaded.value
      ? currentAccounts.filter((account) => !accountIDsBeforeCreate.value.has(account.id))
      : []
    if (candidates.length === 1) return candidates[0]
    stage.value = 'resolve_failed'
    errorMessage.value = snapshotLoaded.value
      ? t('accountShare.roomAccounts.createFlow.identifyAmbiguous', { count: candidates.length })
      : t('accountShare.roomAccounts.createFlow.identifyUnavailable')
    return null
  } catch (error) {
    if (!isCurrentFlow(currentActivation, listingID)) return null
    stage.value = 'resolve_failed'
    errorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.createFlow.identifyUnavailable')
    )
    return null
  }
}

async function retryResolveCreatedAccount(): Promise<void> {
  if (busy.value) return
  const currentActivation = activationVersion
  const listingID = props.listing?.id
  if (!listingID) return
  stage.value = 'created'
  errorMessage.value = ''
  const resolved = await resolveCreatedAccount(undefined, currentActivation, listingID)
  if (!resolved) return
  if (!isCurrentFlow(currentActivation, listingID)) return
  createdAccount.value = resolved
  await startConversion(currentActivation)
}

function isCurrentFlow(
  currentActivation: number,
  listingID: number,
  accountID?: number
): boolean {
  if (
    currentActivation !== activationVersion
    || !props.show
    || props.listing?.id !== listingID
  ) {
    return false
  }
  return accountID === undefined || createdAccount.value?.id === accountID
}

function createIdempotencyKey(
  scope: 'convert' | 'attach',
  listingID: number,
  accountID: number
): string {
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error(t('accountShare.roomAccounts.uuidUnavailable'))
  }
  return `room-account-${scope}-${listingID}-${accountID}-${requestID}`
}

function validateCreatedAccount(account: Account): string {
  const listing = props.listing
  if (!listing) return t('accountShare.roomAccounts.roomUnavailable')
  if (account.platform !== listing.platform) {
    return t('accountShare.roomAccounts.platformMismatch', { platform: listing.platform })
  }
  if (
    account.owner_user_id != null
    && Number(account.owner_user_id) !== Number(listing.owner_user_id)
  ) {
    return t('accountShare.roomAccounts.ownerMismatch')
  }
  const expectedLevel = roomAccountLevel.value.trim().toLocaleLowerCase()
  const actualLevel = String(account.account_level || '').trim().toLocaleLowerCase()
  if (expectedLevel && expectedLevel !== 'unknown' && actualLevel && actualLevel !== 'unknown' && expectedLevel !== actualLevel) {
    return t('accountShare.roomAccounts.levelMismatch', { level: roomAccountLevel.value })
  }
  return ''
}

async function startConversion(currentActivation = activationVersion): Promise<void> {
  const listing = props.listing
  const account = createdAccount.value
  if (!listing || !account) return
  const listingID = listing.id
  const accountID = account.id
  if (!isCurrentFlow(currentActivation, listingID, accountID)) return
  const compatibilityError = validateCreatedAccount(account)
  if (compatibilityError) {
    stage.value = 'conversion_failed'
    errorMessage.value = compatibilityError
    return
  }

  stage.value = 'converting'
  errorMessage.value = ''
  try {
    if (!conversionIdempotencyKey.value) {
      conversionIdempotencyKey.value = createIdempotencyKey('convert', listingID, accountID)
    }
    await accountShareAPI.convertAccountExternalPlacement(accountID, {
      target: 'room',
      idempotency_key: conversionIdempotencyKey.value
    })
    if (!isCurrentFlow(currentActivation, listingID, accountID)) return
    await startAttach(currentActivation)
  } catch (error) {
    if (!isCurrentFlow(currentActivation, listingID, accountID)) return
    stage.value = 'conversion_failed'
    errorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.createFlow.conversionRequestFailed')
    )
  }
}

async function retryConversion(): Promise<void> {
  if (busy.value || !createdAccount.value) return
  await startConversion(activationVersion)
}

async function startAttach(currentActivation = activationVersion): Promise<void> {
  const listing = props.listing
  const account = createdAccount.value
  if (!listing || !account) return
  const listingID = listing.id
  const accountID = account.id
  if (!isCurrentFlow(currentActivation, listingID, accountID)) return

  stage.value = 'attaching'
  errorMessage.value = ''
  try {
    if (!attachIdempotencyKey.value) {
      attachIdempotencyKey.value = createIdempotencyKey('attach', listingID, accountID)
    }
    const result = await accountShareAPI.attachRoomAccounts(listingID, {
      account_ids: [accountID],
      idempotency_key: attachIdempotencyKey.value
    })
    if (!isCurrentFlow(currentActivation, listingID, accountID)) return
    if ((result.success || 0) < 1) {
      const failure = result.results.find((item) => !item.success)
      throw new Error(failure?.error || t('accountShare.roomAccounts.createFlow.attachRequestFailed'))
    }
    stage.value = 'completed'
    if (!completionEmitted) {
      completionEmitted = true
      emit('completed', { accountID })
    }
  } catch (error) {
    if (!isCurrentFlow(currentActivation, listingID, accountID)) return
    stage.value = 'attach_failed'
    errorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.createFlow.attachRequestFailed')
    )
  }
}

async function retryAttach(): Promise<void> {
  if (busy.value || !createdAccount.value) return
  await startAttach(activationVersion)
}

function requestClose(): void {
  if (busy.value) return
  if (stage.value === 'creating') {
    discardConfirmationOpen.value = true
    return
  }
  emit('close')
}

function cancelDiscard(): void {
  discardConfirmationOpen.value = false
}

function confirmDiscard(): void {
  discardConfirmationOpen.value = false
  activationVersion += 1
  emit('close')
}

function stepClass(step: FlowStep): string[] {
  const state = step === 'created'
    ? createdStepState.value
    : step === 'converting'
      ? conversionStepState.value
      : attachStepState.value
  return ['flow-step', `flow-step-${state}`]
}
</script>

<style scoped>
.create-room-account-flow {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.flow-room-context {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.flow-room-mark {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  flex: 0 0 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: rgb(219 234 254);
  color: rgb(29 78 216);
}

.flow-room-context span,
.flow-room-context strong,
.flow-room-context small {
  display: block;
}

.flow-room-context > div > span,
.flow-room-context small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.flow-room-context strong {
  overflow: hidden;
  margin-top: 0.125rem;
  color: rgb(15 23 42);
  font-size: 0.9375rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.flow-room-context small {
  margin-top: 0.25rem;
}

.flow-preparing {
  display: flex;
  min-height: 8rem;
  align-items: center;
  justify-content: center;
  gap: 0.625rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
}

.flow-steps {
  display: grid;
  gap: 0.625rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.flow-step {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  padding: 0.875rem;
  transition: border-color 150ms ease, background-color 150ms ease;
}

.flow-step-index {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  flex: 0 0 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgb(241 245 249);
  color: rgb(100 116 139);
  font-size: 0.875rem;
  font-weight: 700;
}

.flow-step > div {
  min-width: 0;
  padding-top: 0.125rem;
}

.flow-step strong,
.flow-step small {
  display: block;
}

.flow-step strong {
  color: rgb(30 41 59);
  font-size: 0.875rem;
}

.flow-step small {
  margin-top: 0.25rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.25rem;
  overflow-wrap: anywhere;
}

.flow-step-active {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
}

.flow-step-active .flow-step-index {
  background: rgb(219 234 254);
  color: rgb(29 78 216);
}

.flow-step-complete {
  border-color: rgb(167 243 208);
}

.flow-step-complete .flow-step-index {
  background: rgb(209 250 229);
  color: rgb(5 150 105);
}

.flow-step-error {
  border-color: rgb(254 202 202);
  background: rgb(254 242 242);
}

.flow-step-error .flow-step-index {
  background: rgb(254 226 226);
  color: rgb(220 38 38);
}

.flow-result {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid;
  border-radius: 0.875rem;
  padding: 0.875rem;
  font-size: 0.875rem;
}

.flow-result svg {
  margin-top: 0.125rem;
  flex-shrink: 0;
}

.flow-result strong,
.flow-result p,
.flow-result small {
  display: block;
}

.flow-result p {
  margin-top: 0.25rem;
  line-height: 1.375rem;
}

.flow-result small {
  margin-top: 0.5rem;
  line-height: 1.25rem;
}

.flow-result-error {
  border-color: rgb(254 202 202);
  background: rgb(254 242 242);
  color: rgb(153 27 27);
}

.flow-result-success {
  border-color: rgb(167 243 208);
  background: rgb(236 253 245);
  color: rgb(6 95 70);
}

.flow-footer {
  display: flex;
  width: 100%;
  flex-direction: column;
  gap: 0.625rem;
}

.flow-discard-confirmation {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(254 202 202);
  border-radius: 0.875rem;
  background: rgb(254 242 242);
  padding: 0.875rem;
  color: rgb(153 27 27);
}

.flow-discard-icon {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  flex: 0 0 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: rgb(254 226 226);
}

.flow-discard-confirmation strong,
.flow-discard-confirmation p {
  display: block;
}

.flow-discard-confirmation strong {
  color: rgb(127 29 29);
  font-size: 0.875rem;
}

.flow-discard-confirmation p {
  margin-top: 0.25rem;
  font-size: 0.8125rem;
  line-height: 1.375rem;
}

.flow-busy-note {
  display: flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  color: rgb(100 116 139);
  font-size: 0.8125rem;
}

:global(.dark) .flow-room-context,
:global(.dark) .flow-step {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.55);
}

:global(.dark) .flow-room-mark,
:global(.dark) .flow-step-active .flow-step-index {
  background: rgb(30 64 175 / 0.3);
  color: rgb(147 197 253);
}

:global(.dark) .flow-room-context strong,
:global(.dark) .flow-step strong {
  color: rgb(244 244 245);
}

:global(.dark) .flow-room-context > div > span,
:global(.dark) .flow-room-context small,
:global(.dark) .flow-step small,
:global(.dark) .flow-busy-note {
  color: rgb(161 161 170);
}

:global(.dark) .flow-step-active {
  border-color: rgb(59 130 246 / 0.65);
  background: rgb(30 64 175 / 0.16);
}

:global(.dark) .flow-step-complete {
  border-color: rgb(16 185 129 / 0.55);
}

:global(.dark) .flow-step-complete .flow-step-index {
  background: rgb(6 78 59 / 0.45);
  color: rgb(110 231 183);
}

:global(.dark) .flow-step-error,
:global(.dark) .flow-result-error {
  border-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.28);
  color: rgb(252 165 165);
}

:global(.dark) .flow-result-success {
  border-color: rgb(6 95 70);
  background: rgb(6 78 59 / 0.25);
  color: rgb(167 243 208);
}

:global(.dark) .flow-discard-confirmation {
  border-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.28);
  color: rgb(252 165 165);
}

:global(.dark) .flow-discard-icon {
  background: rgb(127 29 29 / 0.4);
}

:global(.dark) .flow-discard-confirmation strong {
  color: rgb(254 202 202);
}

@media (min-width: 640px) {
  .flow-footer {
    flex-direction: row;
    justify-content: flex-end;
  }
}
</style>
