<template>
  <div v-if="showInviteAction" class="header-invite">
    <button
      type="button"
      class="header-invite-button"
      :class="{ 'header-invite-button-copied': copied }"
      :aria-label="buttonAriaLabel"
      aria-describedby="header-invite-tooltip"
      @click="copyInviteLink"
    >
      <Icon :name="copied ? 'check' : 'gift'" size="sm" aria-hidden="true" />
      <span class="hidden whitespace-nowrap xl:inline" aria-live="polite">
        {{ buttonText }}
      </span>
    </button>

    <div id="header-invite-tooltip" class="header-invite-tooltip" role="tooltip">
      <div class="header-invite-tooltip-title">
        <Icon name="trendingUp" size="sm" aria-hidden="true" />
        <span>{{ t('affiliate.header.tooltipTitle') }}</span>
      </div>
      <p>
        {{ t('affiliate.header.tooltipBody', { rate: formattedRebateRate }) }}
      </p>
      <div class="header-invite-tooltip-hint">
        <Icon name="copy" size="xs" aria-hidden="true" />
        <span>{{ t('affiliate.header.copyHint') }}</span>
      </div>
      <span class="header-invite-tooltip-arrow" aria-hidden="true"></span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import userAPI from '@/api/user'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'
import { buildAffiliateInviteLink } from '@/utils/oauthAffiliate'

const { locale, t } = useI18n()
const { copied, copyToClipboard } = useClipboard()

const inviteCode = ref('')
const rebateRate = ref(0)
let isUnmounted = false

const inviteLink = computed(() => buildAffiliateInviteLink(inviteCode.value))
const showInviteAction = computed(() => Boolean(inviteLink.value) && rebateRate.value > 0)
const formattedRebateRate = computed(() => {
  return new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: 2,
  }).format(rebateRate.value)
})
const buttonText = computed(() => {
  if (copied.value) {
    return t('affiliate.header.copied')
  }
  return t('affiliate.header.cta', { rate: formattedRebateRate.value })
})
const buttonAriaLabel = computed(() => {
  if (copied.value) {
    return t('affiliate.linkCopied')
  }
  return t('affiliate.header.copyAriaLabel', { rate: formattedRebateRate.value })
})

async function loadInviteLink(): Promise<void> {
  try {
    const summary = await userAPI.getAffiliateShareSummary()
    if (isUnmounted) {
      return
    }
    if (!summary.enabled) {
      return
    }
    inviteCode.value = summary.aff_code ?? ''
    rebateRate.value = summary.effective_rebate_rate_percent
  } catch (error) {
    console.error('Failed to load header invite link:', error)
  }
}

async function copyInviteLink(): Promise<void> {
  if (!inviteLink.value) {
    return
  }
  await copyToClipboard(inviteLink.value, t('affiliate.linkCopied'))
}

onMounted(() => {
  void loadInviteLink()
})

onBeforeUnmount(() => {
  isUnmounted = true
})
</script>

<style scoped>
.header-invite {
  position: relative;
  display: inline-flex;
}

.header-invite-button {
  display: inline-flex;
  min-width: 2.5rem;
  min-height: 2.5rem;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  border: 1px solid rgb(var(--ui-brand) / 0.2);
  border-radius: 0.625rem;
  background:
    linear-gradient(135deg, rgb(var(--ui-brand-soft)), rgb(var(--ui-surface)));
  padding: 0.5rem 0.625rem;
  color: rgb(var(--ui-brand));
  font-size: 0.875rem;
  font-weight: 700;
  cursor: pointer;
  transition:
    border-color 180ms ease,
    background-color 180ms ease,
    color 180ms ease,
    box-shadow 180ms ease;
}

.header-invite-button:hover {
  border-color: rgb(var(--ui-brand) / 0.4);
  background: rgb(var(--ui-brand-soft));
  box-shadow: 0 0.375rem 1rem rgb(var(--ui-brand) / 0.12);
}

.header-invite-button:focus-visible {
  outline: 2px solid rgb(var(--ui-brand));
  outline-offset: 2px;
}

.header-invite-button-copied {
  border-color: rgb(16 185 129 / 0.35);
  background: rgb(236 253 245);
  color: rgb(5 150 105);
}

.header-invite-tooltip {
  position: absolute;
  z-index: 50;
  top: calc(100% + 0.75rem);
  right: 0;
  display: none;
  width: min(18rem, calc(100vw - 2rem));
  border: 1px solid rgb(51 65 85);
  border-radius: 0.875rem;
  background: rgb(15 23 42);
  padding: 0.875rem;
  color: rgb(226 232 240);
  box-shadow: 0 1rem 2.5rem rgb(15 23 42 / 0.22);
  opacity: 0;
  pointer-events: none;
  transform: translateY(-0.25rem);
  transition:
    opacity 180ms ease,
    transform 180ms ease;
}

.header-invite-tooltip-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: white;
  font-size: 0.875rem;
  font-weight: 700;
}

.header-invite-tooltip p {
  margin-top: 0.5rem;
  color: rgb(203 213 225);
  font-size: 0.8125rem;
  line-height: 1.55;
}

.header-invite-tooltip-hint {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  margin-top: 0.625rem;
  color: rgb(165 180 252);
  font-size: 0.75rem;
  font-weight: 600;
}

.header-invite-tooltip-arrow {
  position: absolute;
  top: -0.3125rem;
  right: 1rem;
  width: 0.625rem;
  height: 0.625rem;
  border-top: 1px solid rgb(51 65 85);
  border-left: 1px solid rgb(51 65 85);
  background: rgb(15 23 42);
  transform: rotate(45deg);
}

@media (min-width: 768px) {
  .header-invite-tooltip {
    display: block;
  }

  .header-invite:hover .header-invite-tooltip,
  .header-invite:focus-within .header-invite-tooltip {
    opacity: 1;
    transform: translateY(0);
  }
}

.dark .header-invite-button-copied {
  border-color: rgb(52 211 153 / 0.35);
  background: rgb(6 78 59 / 0.45);
  color: rgb(110 231 183);
}

@media (pointer: coarse), (any-pointer: coarse) {
  .header-invite-button {
    min-width: 2.75rem;
    min-height: 2.75rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .header-invite-button,
  .header-invite-tooltip {
    transition: none;
  }
}
</style>
