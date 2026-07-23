<template>
  <div class="dashboard-section-card h-full">
    <div class="dashboard-section-header">
      <h2 class="dashboard-card-title">{{ t('dashboard.quickActions') }}</h2>
    </div>
    <div class="space-y-3 p-4 sm:p-5">
      <button type="button" @click="router.push('/keys')" class="group dashboard-action-row">
        <div class="dashboard-action-icon">
          <Icon name="key" size="md" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-sm font-medium text-content">{{ t('dashboard.createApiKey') }}</p>
          <p class="text-xs text-content-subtle">{{ t('dashboard.generateNewKey') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
          class="text-content-subtle transition-colors group-hover:text-brand"
        />
      </button>

      <button type="button" @click="router.push('/usage')" class="group dashboard-action-row">
        <div class="dashboard-action-icon">
          <Icon name="chart" size="md" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-sm font-medium text-content">{{ t('dashboard.viewUsage') }}</p>
          <p class="text-xs text-content-subtle">{{ t('dashboard.checkDetailedLogs') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
          class="text-content-subtle transition-colors group-hover:text-brand"
        />
      </button>

      <button type="button" @click="router.push('/redeem')" class="group dashboard-action-row">
        <div class="dashboard-action-icon">
          <Icon name="gift" size="md" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-sm font-medium text-content">{{ t('dashboard.redeemCode') }}</p>
          <p class="text-xs text-content-subtle">{{ t('dashboard.addBalanceWithCode') }}</p>
        </div>
        <Icon
          name="chevronRight"
          size="md"
          class="text-content-subtle transition-colors group-hover:text-brand"
        />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
const router = useRouter()
const { t } = useI18n()
</script>
