<template>
  <Teleport to="body">
    <div v-if="show && position">
      <div class="fixed inset-0 z-[9998]" @click="emit('close')"></div>
      <div
        class="fixed z-[9999] w-52 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 dark:bg-dark-800"
        :style="{ top: position.top + 'px', left: position.left + 'px' }"
        @click.stop
      >
        <div v-if="account" class="py-1">
          <button class="menu-item" @click="emitAction('test')">
            <Icon name="play" size="sm" class="text-green-500" :stroke-width="2" />
            {{ t('admin.accounts.testConnection') }}
          </button>
          <button class="menu-item" @click="emitAction('stats')">
            <Icon name="chart" size="sm" class="text-indigo-500" />
            {{ t('admin.accounts.viewStats') }}
          </button>
          <template v-if="supportsCredentialMaintenance">
            <button
              class="menu-item text-blue-600"
              :title="reAuthAttachedToRoom ? t('userAccounts.reAuthRoomAttachedHint') : undefined"
              @click="emitAction('reauth')"
            >
              <Icon name="link" size="sm" />
              {{ t('admin.accounts.reAuthorize') }}
            </button>
            <button class="menu-item text-purple-600" @click="emitAction('refresh-token')">
              <Icon name="refresh" size="sm" />
              {{ t('admin.accounts.refreshToken') }}
            </button>
          </template>
          <p
            v-if="supportsCredentialMaintenance && reAuthAttachedToRoom"
            class="px-3 pb-2 text-xs leading-snug text-gray-500 dark:text-gray-400"
          >
            {{ t('userAccounts.reAuthRoomAttachedHint') }}
          </p>
          <button v-if="supportsPrivacy" class="menu-item text-emerald-600" @click="emitAction('set-privacy')">
            <Icon name="shield" size="sm" />
            {{ t('admin.accounts.setPrivacy') }}
          </button>
          <button class="menu-item text-sky-600" @click="emitAction('moderation')">
            <Icon name="shield" size="sm" />
            {{ t('userAccounts.moderationSettings') }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { Account } from '@/types'

const props = defineProps<{
  show: boolean
  account: Account | null
  position: { top: number; left: number } | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'test', account: Account): void
  (e: 'stats', account: Account): void
  (e: 'reauth', account: Account): void
  (e: 'refresh-token', account: Account): void
  (e: 'set-privacy', account: Account): void
  (e: 'moderation', account: Account): void
}>()

const { t } = useI18n()

const isOpenAIAgentIdentity = computed(() => {
  const authMode = props.account?.credentials?.auth_mode
  return (
    props.account?.platform === 'openai' &&
    props.account.type === 'oauth' &&
    typeof authMode === 'string' &&
    authMode.trim().toLowerCase().replace(/[\s_-]+/g, '') === 'agentidentity'
  )
})

const supportsCredentialMaintenance = computed(() => {
  const account = props.account
  return Boolean(account && (account.type === 'oauth' || account.type === 'setup-token') && !isOpenAIAgentIdentity.value)
})

// 账号还挂在广场房间时，重新授权很可能被守卫拦下（ACCOUNT_MUTATION_BLOCKED_BY_ROOM），
// 用户会白走一套 OAuth、烧掉一次性 authorization code。这里给出事前提示。
//
// 刻意只提示、不置灰：后端的放行条件是「房间已暂停 + 无阻塞项 + 无未闭合绑定」
// （account_repo.go 的 owner intent 分支），而 account_share_mode_listing_id 只按
// room_account.state IN ('active','draining') 投影，**不看房间生命周期状态** ——
// 房间下架变成已暂停后它仍然 > 0。拿它当禁用条件会把后端明明允许的那条路一起堵死，
// 也和提示里「下架房间等它变成已暂停」的指引自相矛盾。
// 真被拦下时，ReAuthAccountModal 现在会原样回显后端给出的具体原因。
//
// 不能用 external_placement.target === 'room'：退房不会把它写回 private，
// 那样已经退房的账号会被永久标记。
const reAuthAttachedToRoom = computed(() => Number(props.account?.account_share_mode_listing_id || 0) > 0)

const supportsPrivacy = computed(() => {
  return (
    !isOpenAIAgentIdentity.value &&
    props.account?.type === 'oauth' &&
    (props.account.platform === 'openai' || props.account.platform === 'antigravity')
  )
})

function emitAction(event: 'test' | 'stats' | 'reauth' | 'refresh-token' | 'set-privacy' | 'moderation'): void {
  if (!props.account) return
  switch (event) {
    case 'test':
      emit('test', props.account)
      break
    case 'stats':
      emit('stats', props.account)
      break
    case 'reauth':
      emit('reauth', props.account)
      break
    case 'refresh-token':
      emit('refresh-token', props.account)
      break
    case 'set-privacy':
      emit('set-privacy', props.account)
      break
    case 'moderation':
      emit('moderation', props.account)
      break
  }
  emit('close')
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') emit('close')
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      window.addEventListener('keydown', handleKeydown)
    } else {
      window.removeEventListener('keydown', handleKeydown)
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.menu-item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 1rem;
  text-align: left;
  font-size: 0.875rem;
  transition: background-color 0.15s ease;
}

.menu-item:hover {
  background: rgb(243 244 246);
}

:global(.dark) .menu-item:hover {
  background: rgb(55 65 81);
}
</style>
