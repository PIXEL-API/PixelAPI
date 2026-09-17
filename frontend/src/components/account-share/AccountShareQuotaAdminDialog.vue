<template>
  <BaseDialog
    :show="show"
    :title="t('accountShare.quotaAdmin.title')"
    width="extra-wide"
    :close-disabled="submitting"
    :close-on-escape="!submitting"
    :close-on-click-outside="false"
    panel-class="account-share-quota-admin-panel"
    @close="requestClose"
  >
    <div class="quota-admin-shell" data-testid="account-share-quota-dialog">
      <header class="quota-admin-summary">
        <span class="quota-admin-summary-icon" aria-hidden="true">
          <Icon name="cog" size="md" />
        </span>
        <div>
          <span>{{ t('accountShare.quotaAdmin.adminOnly') }}</span>
          <strong>{{ t('accountShare.quotaAdmin.subtitle') }}</strong>
          <p>{{ t('accountShare.quotaAdmin.auditNote') }}</p>
        </div>
      </header>

      <div class="quota-admin-tabs" role="tablist" :aria-label="t('accountShare.quotaAdmin.scopeLabel')">
        <button
          type="button"
          role="tab"
          :aria-selected="activeScope === 'global'"
          :class="{ 'quota-admin-tab-active': activeScope === 'global' }"
          data-testid="quota-global-tab"
          @click="setScope('global')"
        >
          {{ t('accountShare.quotaAdmin.scopeGlobal') }}
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="activeScope === 'owner'"
          :class="{ 'quota-admin-tab-active': activeScope === 'owner' }"
          data-testid="quota-owner-tab"
          @click="setScope('owner')"
        >
          {{ t('accountShare.quotaAdmin.scopeOwner') }}
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="activeScope === 'batch'"
          :class="{ 'quota-admin-tab-active': activeScope === 'batch' }"
          data-testid="quota-batch-tab"
          @click="setScope('batch')"
        >
          {{ t('accountShare.quotaAdmin.scopeBatch') }}
        </button>
      </div>

      <div v-if="loadError" class="quota-alert quota-alert-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ loadError }}</span>
      </div>

      <section v-if="activeScope === 'global'" class="quota-admin-workspace" :aria-label="t('accountShare.quotaAdmin.globalSectionLabel')">
        <div class="quota-admin-editor">
          <div class="quota-section-heading">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.globalTitle') }}</strong>
              <span v-if="globalPolicy">{{ t('accountShare.quotaAdmin.globalVersion', { version: globalPolicy.version, effectiveAt: formatDateTime(globalPolicy.effective_at) }) }}</span>
              <span v-else>{{ t('accountShare.quotaAdmin.globalReadHint') }}</span>
            </div>
            <button
              type="button"
              class="quota-icon-button"
              :disabled="loadingGlobal || submitting"
              :aria-label="t('accountShare.quotaAdmin.refreshGlobal')"
              @click="loadGlobal"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingGlobal }" />
            </button>
          </div>

          <div v-if="loadingGlobal && !globalPolicy" class="quota-empty" role="status">
            {{ t('accountShare.quotaAdmin.globalLoading') }}
          </div>
          <template v-else-if="globalPolicy">
            <QuotaLimitsEditor v-model="globalLimits" :disabled="submitting" prefix="global" />
            <label class="quota-field">
              <span>{{ t('accountShare.quotaAdmin.reasonLabel') }}</span>
              <textarea
                v-model="globalReason"
                class="input min-h-24"
                maxlength="1000"
                :disabled="submitting"
                :placeholder="t('accountShare.quotaAdmin.reasonPlaceholder')"
                data-testid="global-quota-reason"
              ></textarea>
              <small>{{ t('accountShare.quotaAdmin.reasonCounter', { length: globalReason.trim().length }) }}</small>
            </label>
            <button
              type="button"
              class="btn btn-primary min-h-11"
              :disabled="submitting || !globalReason.trim()"
              data-testid="prepare-global-quota-update"
              @click="prepareGlobalUpdate"
            >
              {{ t('accountShare.quotaAdmin.reviewGlobal') }}
            </button>
          </template>
        </div>

        <QuotaAuditPanel
          :items="auditItems"
          :loading="loadingAudit"
          :page="auditPage"
          :pages="auditPages"
          @refresh="loadAudit(auditPage)"
          @page="loadAudit"
        />
      </section>

      <section v-else-if="activeScope === 'owner'" class="quota-admin-workspace" :aria-label="t('accountShare.quotaAdmin.dialogAria')">
        <div class="quota-admin-editor">
          <div class="quota-section-heading">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.pickOwner') }}</strong>
              <span>{{ t('accountShare.quotaAdmin.pickOwnerDesc') }}</span>
            </div>
          </div>

          <form class="quota-owner-search" @submit.prevent="loadOwner">
            <label class="quota-field">
              <span>{{ t('accountShare.quotaAdmin.ownerUserId') }}</span>
              <input
                v-model.trim="ownerIDInput"
                class="input min-h-11"
                inputmode="numeric"
                pattern="[0-9]*"
                :disabled="loadingOwner || submitting"
                :placeholder="t('accountShare.quotaAdmin.ownerIdPlaceholder')"
                data-testid="quota-owner-id"
              />
            </label>
            <button
              type="submit"
              class="btn btn-secondary min-h-11"
              :disabled="loadingOwner || submitting || !validOwnerID"
              data-testid="load-owner-quota"
            >
              <Icon name="search" size="sm" class="mr-2" :class="{ 'animate-pulse': loadingOwner }" />
              {{ loadingOwner ? t('accountShare.quotaAdmin.reading') : t('accountShare.quotaAdmin.readQuota') }}
            </button>
          </form>

          <template v-if="ownerState">
            <div class="quota-owner-state">
              <div>
                <span>{{ t('accountShare.quotaAdmin.currentSource') }}</span>
                <strong>{{ quotaSourceLabel(ownerState.effective_quota) }}</strong>
              </div>
              <div>
                <span>{{ t('accountShare.quotaAdmin.effectiveVersion') }}</span>
                <strong>v{{ ownerState.effective_quota.policy_version }}</strong>
              </div>
              <div>
                <span>{{ t('accountShare.quotaAdmin.liveRooms') }}</span>
                <strong>{{ ownerState.usage.live_rooms }}/{{ ownerState.effective_quota.limits.max_live_rooms }}</strong>
              </div>
              <div>
                <span>{{ t('accountShare.quotaAdmin.creates24h') }}</span>
                <strong>{{ ownerState.usage.room_creates_24_hours }}/{{ ownerState.effective_quota.limits.max_room_creates_24_hours }}</strong>
              </div>
              <div>
                <span>{{ t('accountShare.quotaAdmin.totalRoomAccounts') }}</span>
                <strong>{{ ownerState.usage.owner_room_accounts }}/{{ ownerState.effective_quota.limits.max_room_accounts_per_owner }}</strong>
              </div>
              <div>
                <span>{{ t('accountShare.quotaAdmin.maxAccountsPerRoom') }}</span>
                <strong>{{ ownerState.usage.largest_room_accounts }}/{{ ownerState.effective_quota.limits.max_accounts_per_room }}</strong>
              </div>
            </div>

            <div
              v-if="ownerState.effective_quota.growth_blocked"
              class="quota-alert quota-alert-warning"
              role="status"
              data-testid="quota-growth-blocked"
            >
              <Icon name="exclamationTriangle" size="sm" />
              <span>{{ t('accountShare.quotaAdmin.holdModeBanner') }}</span>
            </div>

            <QuotaLimitsEditor v-model="ownerLimits" :disabled="submitting" prefix="owner" />

            <label class="quota-field">
              <span>{{ t('accountShare.quotaAdmin.overrideExpiry') }}</span>
              <input
                v-model="ownerExpiresAt"
                class="input min-h-11"
                type="datetime-local"
                :min="minimumExpiryInput"
                :disabled="submitting"
                data-testid="owner-quota-expiry"
              />
              <small>{{ t('accountShare.quotaAdmin.overrideExpiryHint') }}</small>
            </label>

            <label class="quota-field">
              <span>{{ t('accountShare.quotaAdmin.reason') }}</span>
              <textarea
                v-model="ownerReason"
                class="input min-h-24"
                maxlength="1000"
                :disabled="submitting"
                :placeholder="t('accountShare.quotaAdmin.reasonPlaceholder')"
                data-testid="owner-quota-reason"
              ></textarea>
              <small>{{ t('accountShare.quotaAdmin.reasonCounter', { length: ownerReason.trim().length }) }}</small>
            </label>

            <div class="quota-owner-actions">
              <button
                type="button"
                class="btn btn-primary min-h-11"
                :disabled="submitting || !ownerReason.trim() || !ownerExpiresAt"
                data-testid="prepare-owner-quota-update"
                @click="prepareOwnerUpdate"
              >
                {{ t('accountShare.quotaAdmin.tempOverride') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary min-h-11"
                :disabled="submitting || !ownerReason.trim() || !ownerExpiresAt"
                data-testid="prepare-owner-grandfather"
                @click="prepareOwnerGrandfather"
              >
                {{ t('accountShare.quotaAdmin.setHold') }}
              </button>
              <button
                type="button"
                class="btn btn-danger min-h-11"
                :disabled="submitting || !ownerReason.trim() || !activeOwnerOverride"
                data-testid="prepare-owner-quota-revoke"
                @click="prepareOwnerRevoke"
              >
                {{ t('accountShare.quotaAdmin.revokeOverride') }}
              </button>
            </div>
          </template>

          <div v-else class="quota-empty">
            <Icon name="user" size="md" />
            <strong>{{ t('accountShare.quotaAdmin.noOwner') }}</strong>
            <span>{{ t('accountShare.quotaAdmin.noOwnerDesc') }}</span>
          </div>
        </div>

        <QuotaAuditPanel
          :items="auditItems"
          :loading="loadingAudit"
          :page="auditPage"
          :pages="auditPages"
          @refresh="loadAudit(auditPage)"
          @page="loadAudit"
        />
      </section>

      <section v-else class="quota-batch-workspace" :aria-label="t('accountShare.quotaAdmin.batchAria')">
        <div class="quota-batch-candidates">
          <div class="quota-section-heading">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.candidatesTitle') }}</strong>
              <span>{{ t('accountShare.quotaAdmin.candidatesDesc') }}</span>
            </div>
            <button
              type="button"
              class="quota-icon-button"
              :disabled="loadingCandidates || submitting"
              :aria-label="t('accountShare.quotaAdmin.refreshCandidates')"
              @click="refreshCandidates"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingCandidates }" />
            </button>
          </div>

          <div class="quota-alert quota-alert-warning" role="status">
            <Icon name="exclamationTriangle" size="sm" />
            <span>{{ t('accountShare.quotaAdmin.holdExplainer') }}</span>
          </div>

          <div v-if="candidateError" class="quota-alert quota-alert-error" role="alert">
            <Icon name="exclamationCircle" size="sm" />
            <span>{{ candidateError }}</span>
          </div>

          <div class="quota-batch-toolbar">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.selectedCount', { selectedCandidateCount }) }}</strong>
              <span>{{ t('accountShare.quotaAdmin.totalCandidates', { candidateTotal }) }}</span>
            </div>
            <div>
              <button
                type="button"
                class="btn btn-secondary min-h-11"
                :disabled="loadingCandidates || batchCandidates.length === 0 || submitting"
                data-testid="toggle-page-candidates"
                @click="toggleCurrentPageCandidates"
              >
                {{ allPageCandidatesSelected ? t('accountShare.quotaAdmin.deselectPage') : t('accountShare.quotaAdmin.selectPage') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary min-h-11"
                :disabled="selectedCandidateCount === 0 || submitting"
                data-testid="clear-batch-candidates"
                @click="clearBatchSelection"
              >
                {{ t('accountShare.quotaAdmin.clearSelection') }}
              </button>
            </div>
          </div>

          <div v-if="loadingCandidates && batchCandidates.length === 0" class="quota-empty" role="status">
            {{ t('accountShare.quotaAdmin.candidatesLoading') }}
          </div>
          <div v-else-if="batchCandidates.length === 0" class="quota-empty">
            <Icon name="checkCircle" size="md" />
            <strong>{{ t('accountShare.quotaAdmin.noCandidates') }}</strong>
            <span>{{ t('accountShare.quotaAdmin.noCandidatesDesc') }}</span>
          </div>
          <div v-else class="quota-candidate-list" data-testid="quota-candidate-list">
            <article
              v-for="candidate in batchCandidates"
              :key="candidate.owner_user_id"
              class="quota-candidate-card"
              :class="{ 'quota-candidate-card-selected': isCandidateSelected(candidate.owner_user_id) }"
            >
              <label class="quota-candidate-select">
                <input
                  type="checkbox"
                  :checked="isCandidateSelected(candidate.owner_user_id)"
                  :disabled="!canSelectCandidate(candidate.owner_user_id) || submitting"
                  :aria-:label="t('accountShare.quotaAdmin.selectOwnerAria')"
                  :data-testid="`candidate-${candidate.owner_user_id}`"
                  @change="toggleCandidate(candidate)"
                />
                <span>
                  <strong>{{ t('accountShare.quotaAdmin.ownerRef', { ownerUserId: candidate.owner_user_id }) }}</strong>
                  <small>{{ t('accountShare.quotaAdmin.snapshotInfo', { asOf: formatDateTime(candidate.as_of), latestOwnerVersion: candidate.latest_owner_version }) }}</small>
                </span>
              </label>

              <div class="quota-candidate-dimensions" :aria-label="t('accountShare.quotaAdmin.dimensionAria')">
                <span v-for="dimension in candidate.exceeded_dimensions" :key="dimension">
                  {{ quotaDimensionLabel(dimension) }}
                </span>
              </div>

              <dl class="quota-candidate-metrics">
                <div>
                  <dt>{{ t('accountShare.quotaAdmin.liveRooms') }}</dt>
                  <dd>{{ candidate.usage.live_rooms }} / {{ candidate.effective_quota.limits.max_live_rooms }}</dd>
                  <small>{{ t('accountShare.quotaAdmin.keepLiveRooms', { maxLiveRooms: candidate.suggested_limits.max_live_rooms }) }}</small>
                </div>
                <div>
                  <dt>{{ t('accountShare.quotaAdmin.creates24h') }}</dt>
                  <dd>{{ candidate.usage.room_creates_24_hours }} / {{ candidate.effective_quota.limits.max_room_creates_24_hours }}</dd>
                  <small>{{ t('accountShare.quotaAdmin.keepCreates24h', { maxRoomCreates_24Hours: candidate.suggested_limits.max_room_creates_24_hours }) }}</small>
                </div>
                <div>
                  <dt>{{ t('accountShare.quotaAdmin.totalRoomAccounts') }}</dt>
                  <dd>{{ candidate.usage.owner_room_accounts }} / {{ candidate.effective_quota.limits.max_room_accounts_per_owner }}</dd>
                  <small>{{ t('accountShare.quotaAdmin.keepRoomAccounts', { maxRoomAccountsPerOwner: candidate.suggested_limits.max_room_accounts_per_owner }) }}</small>
                </div>
                <div>
                  <dt>{{ t('accountShare.quotaAdmin.maxAccountsPerRoom') }}</dt>
                  <dd>{{ candidate.usage.largest_room_accounts }} / {{ candidate.effective_quota.limits.max_accounts_per_room }}</dd>
                  <small>{{ t('accountShare.quotaAdmin.keepAccountsPerRoom', { maxAccountsPerRoom: candidate.suggested_limits.max_accounts_per_room }) }}</small>
                </div>
              </dl>
            </article>
          </div>

          <div v-if="candidatePages > 1" class="quota-audit-pagination">
            <button
              type="button"
              class="btn btn-secondary min-h-11"
              :disabled="loadingCandidates || candidatePage <= 1 || submitting"
              @click="loadCandidates(candidatePage - 1)"
            >
              {{ t('pagination.prev') }}
            </button>
            <span>{{ t('accountShare.quotaAdmin.pageInfo', { candidatePage, candidatePages }) }}</span>
            <button
              type="button"
              class="btn btn-secondary min-h-11"
              :disabled="loadingCandidates || candidatePage >= candidatePages || submitting"
              @click="loadCandidates(candidatePage + 1)"
            >
              {{ t('pagination.next') }}
            </button>
          </div>
        </div>

        <aside class="quota-batch-control" :aria-label="t('accountShare.quotaAdmin.batchSettings')">
          <div class="quota-section-heading">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.batchSettings') }}</strong>
              <span>{{ t('accountShare.quotaAdmin.batchSettingsDesc') }}</span>
            </div>
          </div>

          <div class="quota-batch-selection-summary">
            <span>{{ t('accountShare.quotaAdmin.pendingOwners') }}</span>
            <strong>{{ t('accountShare.quotaAdmin.ownerCount', { selectedCandidateCount }) }}</strong>
            <small v-if="selectedCandidateCount > 0">{{ selectedOwnerSummary }}</small>
            <small v-else>{{ t('accountShare.quotaAdmin.selectFirst') }}</small>
          </div>

          <label class="quota-field">
            <span>{{ t('accountShare.quotaAdmin.holdExpiry') }}</span>
            <input
              v-model="batchExpiresAt"
              class="input min-h-11"
              type="datetime-local"
              :min="minimumExpiryInput"
              :disabled="submitting"
              data-testid="batch-quota-expiry"
            />
            <small>{{ t('accountShare.quotaAdmin.holdExpiryHint') }}</small>
          </label>

          <label class="quota-field">
            <span>{{ t('accountShare.quotaAdmin.batchReason') }}</span>
            <textarea
              v-model="batchReason"
              class="input min-h-24"
              maxlength="1000"
              :disabled="submitting"
              :placeholder="t('accountShare.quotaAdmin.batchReasonPlaceholder')"
              data-testid="batch-quota-reason"
            ></textarea>
            <small>{{ t('accountShare.quotaAdmin.batchReasonCounter', { length: batchReason.trim().length }) }}</small>
          </label>

          <button
            type="button"
            class="btn btn-primary min-h-11"
            :disabled="!batchCanPrepare || submitting"
            data-testid="prepare-batch-grandfather"
            @click="prepareBatchGrandfather"
          >
            {{ t('accountShare.quotaAdmin.reviewAndRun') }}
          </button>
        </aside>

        <section
          v-if="batchResults.length > 0"
          class="quota-batch-results"
          :aria-label="t('accountShare.quotaAdmin.batchResultAria')"
          aria-live="polite"
          data-testid="batch-grandfather-results"
        >
          <div class="quota-section-heading">
            <div>
              <strong>{{ t('accountShare.quotaAdmin.lastResult') }}</strong>
              <span>{{ batchResultSummary }}</span>
            </div>
            <button type="button" class="btn btn-secondary min-h-11" @click="batchResults = []">
              {{ t('accountShare.quotaAdmin.clearResult') }}
            </button>
          </div>

          <div class="quota-result-list">
            <article
              v-for="result in batchResults"
              :key="result.owner_user_id"
              class="quota-result-card"
            >
              <div>
                <strong>{{ t('accountShare.quotaAdmin.ownerRef', { ownerUserId: result.owner_user_id }) }}</strong>
                <span class="quota-result-status" :class="batchResultStatusClass(result.status)">
                  {{ batchResultStatusLabel(result.status) }}
                </span>
              </div>
              <p>{{ batchResultMessage(result) }}</p>
              <small v-if="result.policy_version">
                {{ t('accountShare.quotaAdmin.policyVersion', { policyVersion: result.policy_version }) }}
                <template v-if="result.expires_at"> {{ t('accountShare.quotaAdmin.expiresAt', { expiresAt: formatDateTime(result.expires_at) }) }}</template>
              </small>
              <button
                type="button"
                class="btn btn-secondary min-h-11"
                :disabled="loadingOwner || submitting"
                :data-testid="`view-owner-${result.owner_user_id}`"
                @click="openBatchResultOwner(result.owner_user_id)"
              >
                {{ t('accountShare.quotaAdmin.viewOwnerStatus') }}
              </button>
            </article>
          </div>
        </section>
      </section>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="requestClose">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="pendingMutation !== null"
    :title="t('accountShare.quotaAdmin.finalConfirmTitle')"
    width="narrow"
    :z-index="75"
    :close-disabled="submitting"
    :close-on-escape="!submitting"
    :close-on-click-outside="false"
    @close="cancelMutation"
  >
    <div v-if="pendingMutation" class="quota-confirmation" data-testid="quota-mutation-confirmation">
      <div
        class="quota-alert"
        :class="pendingMutation.kind === 'revoke' ? 'quota-alert-error' : 'quota-alert-warning'"
      >
        <Icon name="exclamationTriangle" size="sm" />
        <span>{{ mutationWarning(pendingMutation) }}</span>
      </div>

      <dl>
        <div>
          <dt>{{ t('common.actions') }}</dt>
          <dd>{{ mutationKindLabel(pendingMutation.kind) }}</dd>
        </div>
        <div v-if="pendingMutation.ownerID">
          <dt>{{ t('accountShare.quotaAdmin.owner') }}</dt>
          <dd>#{{ pendingMutation.ownerID }}</dd>
        </div>
        <div v-if="pendingMutation.expectedVersion !== undefined">
          <dt>{{ t('accountShare.quotaAdmin.expectedVersion') }}</dt>
          <dd>v{{ pendingMutation.expectedVersion }}</dd>
        </div>
        <div v-if="pendingMutation.items">
          <dt>{{ t('accountShare.quotaAdmin.itemCount') }}</dt>
          <dd>{{ t('accountShare.quotaAdmin.ownerCountSuffix', { length: pendingMutation.items.length }) }}</dd>
        </div>
        <div v-if="pendingMutation.expiresAt">
          <dt>{{ t('accountShare.quotaAdmin.expiresAtLabel') }}</dt>
          <dd>{{ formatDateTime(pendingMutation.expiresAt) }}</dd>
        </div>
        <div class="quota-confirmation-wide">
          <dt>{{ t('accountShare.quotaAdmin.reasonLabel') }}</dt>
          <dd>{{ pendingMutation.reason }}</dd>
        </div>
      </dl>

      <label class="quota-confirm-check">
        <input
          v-model="mutationConfirmed"
          type="checkbox"
          :disabled="submitting"
          data-testid="quota-mutation-confirmed"
        />
        <span>
          <strong>{{ t('accountShare.quotaAdmin.confirmCheck') }}</strong>
          <small>{{ t('accountShare.quotaAdmin.confirmCheckDesc') }}</small>
        </span>
      </label>

      <div v-if="mutationError" class="quota-alert quota-alert-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ mutationError }}</span>
      </div>
    </div>

    <template #footer>
      <div class="quota-confirm-footer">
        <button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="cancelMutation">
          {{ t('accountShare.quotaAdmin.backToCheck') }}
        </button>
        <button
          type="button"
          class="btn"
          :class="pendingMutation?.kind === 'revoke' ? 'btn-danger min-h-11' : 'btn-primary min-h-11'"
          :disabled="!mutationConfirmed || submitting"
          data-testid="confirm-quota-mutation"
          @click="submitMutation"
        >
          <Icon
            :name="submitting ? 'refresh' : 'checkCircle'"
            size="sm"
            class="mr-2"
            :class="{ 'animate-spin': submitting }"
          />
          {{ submitting ? t('common.submitting') : t('accountShare.quotaAdmin.confirmExecute') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="discardDraftRequested"
    :title="t('accountShare.quotaAdmin.abandonTitle')"
    width="narrow"
    :z-index="85"
    :close-on-click-outside="false"
    @close="discardDraftRequested = false"
  >
    <div class="quota-confirmation">
      <div class="quota-alert quota-alert-warning" role="alert">
        <Icon name="exclamationTriangle" size="sm" />
        <span>{{ t('accountShare.quotaAdmin.abandonDesc') }}</span>
      </div>
    </div>
    <template #footer>
      <div class="quota-confirm-footer">
        <button type="button" class="btn btn-secondary min-h-11" @click="discardDraftRequested = false">
          {{ t('accountShare.quotaAdmin.continueEdit') }}
        </button>
        <button
          type="button"
          class="btn btn-danger min-h-11"
          data-testid="discard-quota-draft"
          @click="confirmDiscardAndClose"
        >
          {{ t('accountShare.quotaAdmin.abandonConfirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, ref, watch } from 'vue'
import accountShareQuotaAdminAPI, {
  type AccountShareGrandfatherBatchItemResult,
  type AccountShareGrandfatherCandidate,
  type AccountShareGrandfatherCandidateItem,
  type AccountShareQuotaAdminState,
  type AccountShareQuotaLimits,
  type AccountShareQuotaPolicy,
  type AccountShareQuotaScope,
  type AccountShareResolvedQuota
} from '@/api/admin/accountShareQuota'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

type QuotaAdminView = AccountShareQuotaScope | 'batch'
type QuotaMutationKind = 'global' | 'owner' | 'grandfather' | 'revoke' | 'batch-grandfather'

interface PendingQuotaMutation {
  kind: QuotaMutationKind
  ownerID?: number
  expectedVersion?: number
  reason: string
  limits?: AccountShareQuotaLimits
  expiresAt?: string
  items?: AccountShareGrandfatherCandidateItem[]
}

const QuotaLimitsEditor = defineComponent({
  name: 'QuotaLimitsEditor',
  props: {
    modelValue: {
      type: Object as () => AccountShareQuotaLimits,
      required: true
    },
    disabled: Boolean,
    prefix: {
      type: String,
      required: true
    }
  },
  emits: ['update:modelValue'],
  setup(componentProps, { emit }) {
    const fields: Array<{ key: keyof AccountShareQuotaLimits; label: string; help: string }> = [
      { key: 'max_live_rooms', label: t('accountShare.quotaAdmin.fieldMaxLiveRooms'), help: t('accountShare.quotaAdmin.fieldMaxLiveRoomsHelp') },
      { key: 'max_room_creates_24_hours', label: t('accountShare.quotaAdmin.fieldMaxCreates'), help: t('accountShare.quotaAdmin.fieldMaxCreatesHelp') },
      { key: 'max_accounts_per_room', label: t('accountShare.quotaAdmin.fieldMaxPerRoom'), help: t('accountShare.quotaAdmin.fieldMaxPerRoomHelp') },
      { key: 'max_room_accounts_per_owner', label: t('accountShare.quotaAdmin.fieldMaxOwnerTotal'), help: t('accountShare.quotaAdmin.fieldMaxOwnerTotalHelp') }
    ]
    const update = (key: keyof AccountShareQuotaLimits, event: Event) => {
      const target = event.target as HTMLInputElement
      emit('update:modelValue', {
        ...componentProps.modelValue,
        [key]: Number(target.value)
      })
    }
    return () => h('div', { class: 'quota-limits-grid' }, fields.map(field =>
      h('label', { class: 'quota-field', for: `${componentProps.prefix}-${field.key}` }, [
        h('span', field.label),
        h('input', {
          id: `${componentProps.prefix}-${field.key}`,
          class: 'input min-h-11',
          type: 'number',
          min: 1,
          max: 1_000_000,
          step: 1,
          disabled: componentProps.disabled,
          value: componentProps.modelValue[field.key],
          'data-testid': `${componentProps.prefix}-${field.key}`,
          onInput: (event: Event) => update(field.key, event)
        }),
        h('small', field.help)
      ])
    ))
  }
})

const QuotaAuditPanel = defineComponent({
  name: 'QuotaAuditPanel',
  props: {
    items: {
      type: Array as () => AccountShareQuotaPolicy[],
      required: true
    },
    loading: Boolean,
    page: {
      type: Number,
      required: true
    },
    pages: {
      type: Number,
      required: true
    }
  },
  emits: ['refresh', 'page'],
  setup(componentProps, { emit }) {
    return () => h('aside', { class: 'quota-audit-panel', 'aria-label': t('accountShare.quotaAdmin.auditLabel') }, [
      h('div', { class: 'quota-section-heading' }, [
        h('div', [h('strong', t('accountShare.quotaAdmin.auditTitle')), h('span', t('accountShare.quotaAdmin.auditSub'))]),
        h('button', {
          type: 'button',
          class: 'quota-icon-button',
          disabled: componentProps.loading,
          'aria-label': t('accountShare.quotaAdmin.auditRefresh'),
          onClick: () => emit('refresh')
        }, [h(Icon, { name: 'refresh', size: 'sm', class: componentProps.loading ? 'animate-spin' : '' })])
      ]),
      componentProps.loading && componentProps.items.length === 0
        ? h('div', { class: 'quota-empty' }, t('accountShare.quotaAdmin.auditLoading'))
        : componentProps.items.length === 0
          ? h('div', { class: 'quota-empty' }, t('accountShare.quotaAdmin.auditEmpty'))
          : h('div', { class: 'quota-audit-list' }, componentProps.items.map(item =>
              h('article', { class: 'quota-audit-card', key: item.id }, [
                h('div', [h('strong', `v${item.version} · ${policyKindLabel(item.override_kind)}`), h('span', policyStatusLabel(item.status))]),
                h('p', item.reason || t('accountShare.quotaAdmin.noReason')),
                h('small', t('accountShare.quotaAdmin.auditMeta', { time: formatDateTime(item.created_at), actor: item.actor_user_id_snapshot || item.actor_user_id || '—' }))
              ])
            )),
      componentProps.pages > 1
        ? h('div', { class: 'quota-audit-pagination' }, [
            h('button', {
              type: 'button',
              class: 'btn btn-secondary min-h-11',
              disabled: componentProps.loading || componentProps.page <= 1,
              onClick: () => emit('page', componentProps.page - 1)
            }, t('accountShare.quotaAdmin.prevPage')),
            h('span', t('accountShare.quotaAdmin.pageInfo', { page: componentProps.page, pages: componentProps.pages })),
            h('button', {
              type: 'button',
              class: 'btn btn-secondary min-h-11',
              disabled: componentProps.loading || componentProps.page >= componentProps.pages,
              onClick: () => emit('page', componentProps.page + 1)
            }, t('accountShare.quotaAdmin.nextPage'))
          ])
        : null
    ])
  }
})

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  updated: [AccountShareQuotaPolicy | AccountShareGrandfatherBatchItemResult[]]
}>()

const QUOTA_PAGE_SIZE = 12
const QUOTA_ERROR_MESSAGES: Record<string, string> = {
  ACCOUNT_SHARE_QUOTA_ADMIN_REQUIRED: t('accountShare.quotaAdmin.errors.authExpired'),
  ACCOUNT_SHARE_QUOTA_INVALID: t('accountShare.quotaAdmin.errors.invalidConfig'),
  ACCOUNT_SHARE_QUOTA_REASON_REQUIRED: t('accountShare.quotaAdmin.errors.reasonRequired'),
  ACCOUNT_SHARE_QUOTA_CONFIRMATION_REQUIRED: t('accountShare.quotaAdmin.errors.confirmRequired'),
  ACCOUNT_SHARE_QUOTA_EXPECTED_VERSION_REQUIRED: t('accountShare.quotaAdmin.errors.staleVersion'),
  ACCOUNT_SHARE_QUOTA_VERSION_CONFLICT: t('accountShare.quotaAdmin.errors.conflict'),
  ACCOUNT_SHARE_QUOTA_CONFIGURATION_UNAVAILABLE: t('accountShare.quotaAdmin.errors.unavailable'),
  ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_FOUND: t('accountShare.quotaAdmin.errors.noOverride'),
  ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_ACTIVE: t('accountShare.quotaAdmin.errors.overrideGone'),
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED: t('accountShare.quotaAdmin.errors.grandfatherShrinkOnly'),
  ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED: t('accountShare.quotaAdmin.errors.overLimit'),
  ACCOUNT_SHARE_QUOTA_NOT_A_CANDIDATE: t('accountShare.quotaAdmin.errors.notOverLimit'),
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_ALREADY_ACTIVE: t('accountShare.quotaAdmin.errors.alreadyGrandfathered'),
  ACCOUNT_SHARE_QUOTA_CANDIDATE_STALE: t('accountShare.quotaAdmin.errors.snapshotStale'),
  ACCOUNT_SHARE_QUOTA_APPLY_FAILED: t('accountShare.quotaAdmin.errors.grandfatherFailed'),
  OWNER_NOT_FOUND: t('accountShare.quotaAdmin.errors.ownerGone')
}

const appStore = useAppStore()
const activeScope = ref<QuotaAdminView>('global')
const globalPolicy = ref<AccountShareQuotaPolicy | null>(null)
const ownerState = ref<AccountShareQuotaAdminState | null>(null)
const ownerIDInput = ref('')
const loadedOwnerID = ref(0)
const globalLimits = ref<AccountShareQuotaLimits>(emptyLimits())
const ownerLimits = ref<AccountShareQuotaLimits>(emptyLimits())
const globalLimitsBaseline = ref<AccountShareQuotaLimits | null>(null)
const ownerLimitsBaseline = ref<AccountShareQuotaLimits | null>(null)
const globalReason = ref('')
const ownerReason = ref('')
const ownerExpiresAt = ref('')
const ownerExpiresAtBaseline = ref<string | null>(null)
const batchReason = ref('')
const batchExpiresAt = ref('')
const batchExpiresAtBaseline = ref<string | null>(null)
const loadingGlobal = ref(false)
const loadingOwner = ref(false)
const loadingAudit = ref(false)
const loadingCandidates = ref(false)
const loadError = ref('')
const candidateError = ref('')
const auditItems = ref<AccountShareQuotaPolicy[]>([])
const auditPage = ref(1)
const auditPages = ref(1)
const batchCandidates = ref<AccountShareGrandfatherCandidate[]>([])
const selectedCandidates = ref<Map<number, AccountShareGrandfatherCandidate>>(new Map())
const candidatePage = ref(1)
const candidatePages = ref(1)
const candidateTotal = ref(0)
const candidatesLoaded = ref(false)
const batchResults = ref<AccountShareGrandfatherBatchItemResult[]>([])
const pendingMutation = ref<PendingQuotaMutation | null>(null)
const mutationConfirmed = ref(false)
const mutationError = ref('')
const submitting = ref(false)
const discardDraftRequested = ref(false)
let globalRequestVersion = 0
let ownerRequestVersion = 0
let auditRequestVersion = 0
let candidateRequestVersion = 0
let globalController: AbortController | null = null
let ownerController: AbortController | null = null
let auditController: AbortController | null = null
let candidateController: AbortController | null = null
let mutationSignature = ''
let mutationIdempotencyKey = ''

const validOwnerID = computed(() => {
  const ownerID = Number(ownerIDInput.value)
  return Number.isSafeInteger(ownerID) && ownerID > 0
})
const activeOwnerOverride = computed(() => Boolean(
  ownerState.value?.owner_policy
  && ownerState.value.owner_policy.status === 'active'
))
const minimumExpiryInput = computed(() => toDateTimeInput(new Date(Date.now() + 60_000)))
const selectedCandidateCount = computed(() => selectedCandidates.value.size)
const allPageCandidatesSelected = computed(() => (
  batchCandidates.value.length > 0
  && batchCandidates.value.every(candidate => selectedCandidates.value.has(candidate.owner_user_id))
))
const batchCanPrepare = computed(() => (
  selectedCandidateCount.value > 0
  && selectedCandidateCount.value <= 100
  && batchReason.value.trim().length > 0
  && Boolean(normalizeDateTimeInput(batchExpiresAt.value))
))
const selectedOwnerSummary = computed(() => {
  const ownerIDs = [...selectedCandidates.value.keys()].sort((left, right) => left - right)
  const visible = ownerIDs.slice(0, 6).map(ownerID => `#${ownerID}`).join('、')
  return ownerIDs.length > 6 ? t('accountShare.quotaAdmin.ownerCount', { visible, count: ownerIDs.length }) : visible
})
const batchResultSummary = computed(() => {
  const counts = batchResults.value.reduce<Record<string, number>>((result, item) => {
    result[item.status] = (result[item.status] || 0) + 1
    return result
  }, {})
  return [
    t('accountShare.quotaAdmin.countApplied', { count: counts.applied || 0 }),
    t('accountShare.quotaAdmin.countSkipped', { count: counts.skipped || 0 }),
    t('accountShare.quotaAdmin.countConflict', { count: counts.conflict || 0 }),
    t('accountShare.quotaAdmin.countFailed', { count: counts.failed || 0 })
  ].join(' · ')
})
const hasUnsavedDraft = computed(() => Boolean(
  globalReason.value.trim()
  || ownerReason.value.trim()
  || batchReason.value.trim()
  || selectedCandidateCount.value > 0
  || pendingMutation.value
  || (
    globalLimitsBaseline.value !== null
    && !limitsEqual(globalLimits.value, globalLimitsBaseline.value)
  )
  || (
    ownerLimitsBaseline.value !== null
    && !limitsEqual(ownerLimits.value, ownerLimitsBaseline.value)
  )
  || (
    ownerExpiresAtBaseline.value !== null
    && ownerExpiresAt.value !== ownerExpiresAtBaseline.value
  )
  || (
    batchExpiresAtBaseline.value !== null
    && batchExpiresAt.value !== batchExpiresAtBaseline.value
  )
))

watch(
  () => props.show,
  (show) => {
    if (!show) {
      resetState()
      return
    }
    void initialize()
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  invalidateRequests()
})

function emptyLimits(): AccountShareQuotaLimits {
  return {
    max_live_rooms: 1,
    max_room_creates_24_hours: 1,
    max_accounts_per_room: 1,
    max_room_accounts_per_owner: 1
  }
}

function cloneLimits(limits: AccountShareQuotaLimits): AccountShareQuotaLimits {
  return { ...limits }
}

function limitsEqual(
  current: AccountShareQuotaLimits,
  baseline: AccountShareQuotaLimits
): boolean {
  return current.max_live_rooms === baseline.max_live_rooms
    && current.max_room_creates_24_hours === baseline.max_room_creates_24_hours
    && current.max_accounts_per_room === baseline.max_accounts_per_room
    && current.max_room_accounts_per_owner === baseline.max_room_accounts_per_owner
}

function quotaErrorMessage(error: unknown, fallback: string): string {
  return extractApiErrorMessage(error, fallback, QUOTA_ERROR_MESSAGES)
}

function isCanceledRequest(error: unknown): boolean {
  if (error instanceof DOMException && error.name === 'AbortError') return true
  if (!error || typeof error !== 'object') return false
  const candidate = error as { code?: unknown; name?: unknown }
  return candidate.code === 'ERR_CANCELED' || candidate.name === 'CanceledError'
}

function invalidateRequests(): void {
  globalRequestVersion += 1
  ownerRequestVersion += 1
  auditRequestVersion += 1
  candidateRequestVersion += 1
  globalController?.abort()
  ownerController?.abort()
  auditController?.abort()
  candidateController?.abort()
  globalController = null
  ownerController = null
  auditController = null
  candidateController = null
}

function resetState(): void {
  invalidateRequests()
  activeScope.value = 'global'
  globalPolicy.value = null
  ownerState.value = null
  ownerIDInput.value = ''
  loadedOwnerID.value = 0
  globalLimits.value = emptyLimits()
  ownerLimits.value = emptyLimits()
  globalLimitsBaseline.value = null
  ownerLimitsBaseline.value = null
  globalReason.value = ''
  ownerReason.value = ''
  ownerExpiresAt.value = ''
  ownerExpiresAtBaseline.value = null
  batchReason.value = ''
  batchExpiresAt.value = ''
  batchExpiresAtBaseline.value = null
  loadingGlobal.value = false
  loadingOwner.value = false
  loadingAudit.value = false
  loadingCandidates.value = false
  loadError.value = ''
  candidateError.value = ''
  auditItems.value = []
  auditPage.value = 1
  auditPages.value = 1
  batchCandidates.value = []
  selectedCandidates.value = new Map()
  candidatePage.value = 1
  candidatePages.value = 1
  candidateTotal.value = 0
  candidatesLoaded.value = false
  batchResults.value = []
  discardDraftRequested.value = false
  clearMutation()
}

async function initialize(): Promise<void> {
  await Promise.all([loadGlobal(), loadAudit(1)])
}

function setScope(scope: QuotaAdminView): void {
  if (submitting.value || activeScope.value === scope) return
  activeScope.value = scope
  loadError.value = ''
  candidateError.value = ''
  if (scope === 'global') {
    auditItems.value = []
    auditPage.value = 1
    auditPages.value = 1
    void Promise.all([loadGlobal(), loadAudit(1)])
  } else if (scope === 'owner') {
    auditItems.value = []
    auditPage.value = 1
    auditPages.value = 1
    if (loadedOwnerID.value > 0) {
      void loadAudit(1)
    }
  } else {
    if (!batchExpiresAt.value) {
      batchExpiresAt.value = toDateTimeInput(new Date(Date.now() + 30 * 24 * 60 * 60 * 1000))
    }
    if (batchExpiresAtBaseline.value === null) {
      batchExpiresAtBaseline.value = batchExpiresAt.value
    }
    if (!candidatesLoaded.value) {
      void loadCandidates(1)
    }
  }
}

async function loadGlobal(): Promise<void> {
  if (!props.show) return
  globalController?.abort()
  const controller = new AbortController()
  globalController = controller
  const version = ++globalRequestVersion
  loadingGlobal.value = true
  loadError.value = ''
  try {
    const policy = await accountShareQuotaAdminAPI.getGlobal({ signal: controller.signal })
    if (controller.signal.aborted || version !== globalRequestVersion || !props.show) return
    globalPolicy.value = policy
    globalLimits.value = cloneLimits(policy.limits)
    globalLimitsBaseline.value = cloneLimits(policy.limits)
  } catch (error: unknown) {
    if (controller.signal.aborted || version !== globalRequestVersion || isCanceledRequest(error)) return
    loadError.value = quotaErrorMessage(error, t('accountShare.quotaAdmin.errLoadGlobal'))
  } finally {
    if (version === globalRequestVersion) {
      loadingGlobal.value = false
      if (globalController === controller) globalController = null
    }
  }
}

async function loadOwner(): Promise<void> {
  if (!props.show || !validOwnerID.value || submitting.value) return
  const ownerID = Number(ownerIDInput.value)
  ownerController?.abort()
  const controller = new AbortController()
  ownerController = controller
  const version = ++ownerRequestVersion
  loadingOwner.value = true
  loadError.value = ''
  try {
    const state = await accountShareQuotaAdminAPI.getOwner(ownerID, { signal: controller.signal })
    if (controller.signal.aborted || version !== ownerRequestVersion || !props.show) return
    ownerState.value = state
    loadedOwnerID.value = ownerID
    ownerLimits.value = cloneLimits(state.effective_quota.limits)
    ownerLimitsBaseline.value = cloneLimits(state.effective_quota.limits)
    ownerReason.value = ''
    ownerExpiresAt.value = state.owner_policy?.expires_at
      ? toDateTimeInput(new Date(state.owner_policy.expires_at))
      : toDateTimeInput(new Date(Date.now() + 30 * 24 * 60 * 60 * 1000))
    ownerExpiresAtBaseline.value = ownerExpiresAt.value
    await loadAudit(1)
  } catch (error: unknown) {
    if (controller.signal.aborted || version !== ownerRequestVersion || isCanceledRequest(error)) return
    ownerState.value = null
    loadedOwnerID.value = 0
    ownerLimitsBaseline.value = null
    ownerExpiresAtBaseline.value = null
    auditItems.value = []
    loadError.value = quotaErrorMessage(error, t('accountShare.quotaAdmin.errLoadOwner'))
  } finally {
    if (version === ownerRequestVersion) {
      loadingOwner.value = false
      if (ownerController === controller) ownerController = null
    }
  }
}

async function loadAudit(targetPage = auditPage.value): Promise<void> {
  if (
    !props.show
    || activeScope.value === 'batch'
    || (activeScope.value === 'owner' && loadedOwnerID.value <= 0)
  ) return
  auditController?.abort()
  const controller = new AbortController()
  auditController = controller
  const version = ++auditRequestVersion
  const scope: AccountShareQuotaScope = activeScope.value === 'owner' ? 'owner' : 'global'
  loadingAudit.value = true
  try {
    const result = await accountShareQuotaAdminAPI.listAudit(
      scope,
      Math.max(1, targetPage),
      QUOTA_PAGE_SIZE,
      scope === 'owner' ? loadedOwnerID.value : undefined,
      { signal: controller.signal }
    )
    if (controller.signal.aborted || version !== auditRequestVersion || !props.show) return
    auditItems.value = result.items || []
    auditPage.value = Math.max(1, Number(result.page || targetPage || 1))
    auditPages.value = Math.max(1, Number(result.pages || 1))
  } catch (error: unknown) {
    if (controller.signal.aborted || version !== auditRequestVersion || isCanceledRequest(error)) return
    loadError.value = quotaErrorMessage(error, t('accountShare.quotaAdmin.errLoadAudit'))
  } finally {
    if (version === auditRequestVersion) {
      loadingAudit.value = false
      if (auditController === controller) auditController = null
    }
  }
}

async function loadCandidates(targetPage = candidatePage.value): Promise<void> {
  if (!props.show) return
  candidateController?.abort()
  const controller = new AbortController()
  candidateController = controller
  const version = ++candidateRequestVersion
  loadingCandidates.value = true
  candidateError.value = ''
  try {
    const result = await accountShareQuotaAdminAPI.listGrandfatherCandidates(
      Math.max(1, targetPage),
      QUOTA_PAGE_SIZE,
      { signal: controller.signal }
    )
    if (controller.signal.aborted || version !== candidateRequestVersion || !props.show) return
    batchCandidates.value = result.items || []
    candidatePage.value = Math.max(1, Number(result.page || targetPage || 1))
    candidatePages.value = Math.max(1, Number(result.pages || 1))
    candidateTotal.value = Math.max(0, Number(result.total || 0))
    candidatesLoaded.value = true
  } catch (error: unknown) {
    if (controller.signal.aborted || version !== candidateRequestVersion || isCanceledRequest(error)) return
    candidateError.value = quotaErrorMessage(error, t('accountShare.quotaAdmin.errCandidates'))
  } finally {
    if (version === candidateRequestVersion) {
      loadingCandidates.value = false
      if (candidateController === controller) candidateController = null
    }
  }
}

function refreshCandidates(): void {
  if (loadingCandidates.value || submitting.value) return
  clearBatchSelection()
  void loadCandidates(1)
}

function validateLimits(limits: AccountShareQuotaLimits): string {
  const values = Object.values(limits)
  if (values.some(value => !Number.isSafeInteger(value) || value <= 0 || value > 1_000_000)) {
    return t('accountShare.quotaAdmin.errRange')
  }
  if (limits.max_room_accounts_per_owner < limits.max_accounts_per_room) {
    return t('accountShare.quotaAdmin.errTotalLtRoom')
  }
  return ''
}

function normalizedExpiry(): string {
  return normalizeDateTimeInput(ownerExpiresAt.value)
}

function normalizeDateTimeInput(rawValue: string): string {
  const value = rawValue.trim()
  if (!value) return ''
  const date = new Date(value)
  if (!Number.isFinite(date.getTime()) || date.getTime() <= Date.now()) return ''
  return date.toISOString()
}

function isCandidateSelected(ownerUserID: number): boolean {
  return selectedCandidates.value.has(ownerUserID)
}

function canSelectCandidate(ownerUserID: number): boolean {
  return isCandidateSelected(ownerUserID) || selectedCandidateCount.value < 100
}

function toggleCandidate(candidate: AccountShareGrandfatherCandidate): void {
  const next = new Map(selectedCandidates.value)
  if (next.has(candidate.owner_user_id)) {
    next.delete(candidate.owner_user_id)
  } else if (next.size < 100) {
    next.set(candidate.owner_user_id, candidate)
  } else {
    appStore.showWarning(t('accountShare.quotaAdmin.errMaxOwners'))
  }
  selectedCandidates.value = next
}

function toggleCurrentPageCandidates(): void {
  const next = new Map(selectedCandidates.value)
  if (allPageCandidatesSelected.value) {
    for (const candidate of batchCandidates.value) {
      next.delete(candidate.owner_user_id)
    }
  } else {
    let reachedLimit = false
    for (const candidate of batchCandidates.value) {
      if (next.has(candidate.owner_user_id)) continue
      if (next.size >= 100) {
        reachedLimit = true
        break
      }
      next.set(candidate.owner_user_id, candidate)
    }
    if (reachedLimit) {
      appStore.showWarning(t('accountShare.quotaAdmin.errMaxOwnersReached'))
    }
  }
  selectedCandidates.value = next
}

function clearBatchSelection(): void {
  selectedCandidates.value = new Map()
}

function candidateToBatchItem(
  candidate: AccountShareGrandfatherCandidate
): AccountShareGrandfatherCandidateItem {
  return {
    owner_user_id: candidate.owner_user_id,
    expected_version: candidate.latest_owner_version,
    preview_usage: { ...candidate.usage },
    preview_fingerprint: candidate.preview_fingerprint
  }
}

function prepareMutation(mutation: PendingQuotaMutation): void {
  mutationError.value = ''
  mutationConfirmed.value = false
  pendingMutation.value = mutation
}

function prepareGlobalUpdate(): void {
  const policy = globalPolicy.value
  const reason = globalReason.value.trim()
  const limitsError = validateLimits(globalLimits.value)
  if (!policy || policy.version <= 0) {
    loadError.value = t('accountShare.quotaAdmin.errStaleGlobalVersion')
    return
  }
  if (limitsError || !reason) {
    loadError.value = limitsError || t('accountShare.quotaAdmin.errGlobalReason')
    return
  }
  prepareMutation({
    kind: 'global',
    expectedVersion: policy.version,
    reason,
    limits: cloneLimits(globalLimits.value)
  })
}

function prepareOwnerUpdate(): void {
  const reason = ownerReason.value.trim()
  const limitsError = validateLimits(ownerLimits.value)
  const expiresAt = normalizedExpiry()
  if (limitsError || !reason || !expiresAt || loadedOwnerID.value <= 0 || !ownerState.value) {
    loadError.value = limitsError || (!expiresAt ? t('accountShare.quotaAdmin.errOverrideExpiry') : t('accountShare.quotaAdmin.errOverrideFields'))
    return
  }
  prepareMutation({
    kind: 'owner',
    ownerID: loadedOwnerID.value,
    expectedVersion: ownerState.value.owner_policy?.version || 0,
    reason,
    limits: cloneLimits(ownerLimits.value),
    expiresAt
  })
}

function prepareOwnerGrandfather(): void {
  const reason = ownerReason.value.trim()
  const expiresAt = normalizedExpiry()
  if (!reason || !expiresAt || loadedOwnerID.value <= 0 || !ownerState.value) {
    loadError.value = !expiresAt ? t('accountShare.quotaAdmin.errGrandfatherExpiry') : t('accountShare.quotaAdmin.errGrandfatherReason')
    return
  }
  prepareMutation({
    kind: 'grandfather',
    ownerID: loadedOwnerID.value,
    expectedVersion: ownerState.value.owner_policy?.version || 0,
    reason,
    expiresAt
  })
}

function prepareOwnerRevoke(): void {
  const policy = ownerState.value?.owner_policy
  const reason = ownerReason.value.trim()
  if (!policy || policy.status !== 'active' || policy.version <= 0 || !reason || loadedOwnerID.value <= 0) {
    loadError.value = t('accountShare.quotaAdmin.errRevokeMissing')
    return
  }
  prepareMutation({
    kind: 'revoke',
    ownerID: loadedOwnerID.value,
    expectedVersion: policy.version,
    reason
  })
}

function prepareBatchGrandfather(): void {
  const reason = batchReason.value.trim()
  const expiresAt = normalizeDateTimeInput(batchExpiresAt.value)
  if (selectedCandidateCount.value <= 0 || selectedCandidateCount.value > 100) {
    candidateError.value = t('accountShare.quotaAdmin.errSelectOwners')
    return
  }
  if (!reason || !expiresAt) {
    candidateError.value = !expiresAt
      ? t('accountShare.quotaAdmin.errBatchExpiry')
      : t('accountShare.quotaAdmin.errBatchReason')
    return
  }
  candidateError.value = ''
  const items = [...selectedCandidates.value.values()]
    .sort((left, right) => left.owner_user_id - right.owner_user_id)
    .map(candidateToBatchItem)
  prepareMutation({
    kind: 'batch-grandfather',
    reason,
    expiresAt,
    items
  })
}

function cancelMutation(): void {
  if (submitting.value) return
  clearMutation()
}

function clearMutation(): void {
  pendingMutation.value = null
  mutationConfirmed.value = false
  mutationError.value = ''
  mutationSignature = ''
  mutationIdempotencyKey = ''
}

function mutationIdempotency(mutation: PendingQuotaMutation): string {
  const signature = JSON.stringify(mutation)
  if (mutationSignature === signature && mutationIdempotencyKey) return mutationIdempotencyKey
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) throw new Error(t('accountShare.quotaAdmin.errNoCrypto'))
  mutationSignature = signature
  mutationIdempotencyKey = `account-share-quota-${mutation.kind}-${requestID}`
  return mutationIdempotencyKey
}

async function submitMutation(): Promise<void> {
  const mutation = pendingMutation.value
  if (!mutation || !mutationConfirmed.value || submitting.value) return
  submitting.value = true
  mutationError.value = ''
  try {
    const idempotencyKey = mutationIdempotency(mutation)
    if (mutation.kind === 'batch-grandfather') {
      if (!mutation.items?.length || !mutation.expiresAt) {
        throw new Error(t('accountShare.quotaAdmin.errMissingSnapshot'))
      }
      const results = await accountShareQuotaAdminAPI.batchGrandfather({
        items: mutation.items,
        expires_at: mutation.expiresAt,
        reason: mutation.reason,
        confirmed: true
      }, idempotencyKey)
      batchResults.value = results
      clearMutation()
      clearBatchSelection()
      batchReason.value = ''
      batchExpiresAtBaseline.value = batchExpiresAt.value
      emit('updated', results)

      const applied = results.filter(result => result.status === 'applied').length
      const unresolved = results.length - applied
      if (applied > 0 && unresolved === 0) {
        appStore.showSuccess(t('accountShare.quotaAdmin.batchApplied', { applied }))
      } else if (applied > 0) {
        appStore.showWarning(t('accountShare.quotaAdmin.batchPartial', { applied, unresolved }))
      } else {
        appStore.showWarning(t('accountShare.quotaAdmin.batchNone'))
      }
      await loadCandidates(1)
      return
    }

    let policy: AccountShareQuotaPolicy
    if (mutation.kind === 'global') {
      policy = await accountShareQuotaAdminAPI.updateGlobal({
        limits: cloneLimits(mutation.limits!),
        expected_version: mutation.expectedVersion!,
        reason: mutation.reason,
        confirmed: true
      }, idempotencyKey)
    } else if (mutation.kind === 'owner') {
      policy = await accountShareQuotaAdminAPI.upsertOwner(mutation.ownerID!, {
        limits: cloneLimits(mutation.limits!),
        expires_at: mutation.expiresAt!,
        expected_version: mutation.expectedVersion!,
        reason: mutation.reason,
        confirmed: true
      }, idempotencyKey)
    } else if (mutation.kind === 'grandfather') {
      policy = await accountShareQuotaAdminAPI.grandfatherOwner(mutation.ownerID!, {
        expires_at: mutation.expiresAt!,
        expected_version: mutation.expectedVersion!,
        reason: mutation.reason,
        confirmed: true
      }, idempotencyKey)
    } else {
      policy = await accountShareQuotaAdminAPI.revokeOwner(mutation.ownerID!, {
        expected_version: mutation.expectedVersion!,
        reason: mutation.reason,
        confirmed: true
      }, idempotencyKey)
    }

    const scope = mutation.kind
    clearMutation()
    emit('updated', policy)
    appStore.showSuccess(t('accountShare.quotaAdmin.mutationApplied', { kind: mutationKindLabel(scope) }))
    if (scope === 'global') {
      globalReason.value = ''
      await Promise.all([loadGlobal(), loadAudit(1)])
    } else {
      ownerReason.value = ''
      await loadOwner()
    }
  } catch (error: unknown) {
    mutationError.value = quotaErrorMessage(error, t('accountShare.quotaAdmin.errMutation'))
    const code = extractApiErrorCode(error)
    if (
      code === 'ACCOUNT_SHARE_QUOTA_VERSION_CONFLICT'
      || code === 'ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_FOUND'
      || code === 'ACCOUNT_SHARE_QUOTA_OVERRIDE_NOT_ACTIVE'
    ) {
      const message = mutationError.value
      pendingMutation.value = null
      mutationConfirmed.value = false
      mutationSignature = ''
      mutationIdempotencyKey = ''
      loadError.value = message
      if (activeScope.value === 'global') {
        await Promise.all([loadGlobal(), loadAudit(1)])
      } else if (activeScope.value === 'owner') {
        await loadOwner()
      } else {
        clearBatchSelection()
        await loadCandidates(1)
      }
    }
  } finally {
    submitting.value = false
  }
}

function requestClose(): void {
  if (submitting.value) return
  if (hasUnsavedDraft.value) {
    discardDraftRequested.value = true
    return
  }
  resetState()
  emit('close')
}

function confirmDiscardAndClose(): void {
  if (submitting.value) return
  resetState()
  emit('close')
}

async function openBatchResultOwner(ownerUserID: number): Promise<void> {
  if (!Number.isSafeInteger(ownerUserID) || ownerUserID <= 0 || submitting.value) return
  ownerIDInput.value = String(ownerUserID)
  activeScope.value = 'owner'
  candidateError.value = ''
  loadError.value = ''
  auditItems.value = []
  auditPage.value = 1
  auditPages.value = 1
  await loadOwner()
}

function toDateTimeInput(date: Date): string {
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function mutationKindLabel(kind: QuotaMutationKind): string {
  if (kind === 'global') return t('accountShare.quotaAdmin.kindGlobal')
  if (kind === 'owner') return t('accountShare.quotaAdmin.kindOwner')
  if (kind === 'grandfather') return t('accountShare.quotaAdmin.kindGrandfather')
  if (kind === 'batch-grandfather') return t('accountShare.quotaAdmin.kindBatchGrandfather')
  return t('accountShare.quotaAdmin.kindRevoke')
}

function mutationWarning(mutation: PendingQuotaMutation): string {
  if (mutation.kind === 'grandfather' || mutation.kind === 'batch-grandfather') {
    return t('accountShare.quotaAdmin.warnGrandfather')
  }
  if (mutation.kind === 'revoke') {
    return t('accountShare.quotaAdmin.warnRevoke')
  }
  return t('accountShare.quotaAdmin.warnNew')
}

function quotaSourceLabel(quota: AccountShareResolvedQuota): string {
  if (quota.override_kind === 'grandfather') return t('accountShare.quotaAdmin.sourceGrandfather')
  if (quota.override_kind === 'manual') return t('accountShare.quotaAdmin.sourceOverride')
  return t('accountShare.quotaAdmin.scopeGlobal')
}

function policyKindLabel(kind: string): string {
  if (kind === 'grandfather') return t('accountShare.quotaAdmin.sourceGrandfather')
  if (kind === 'manual') return t('accountShare.quotaAdmin.scopeOwner')
  return t('accountShare.quotaAdmin.scopeGlobal')
}

function policyStatusLabel(status: string): string {
  if (status === 'active') return t('accountShare.quotaAdmin.statusActive')
  if (status === 'revoked') return t('accountShare.quotaAdmin.statusRevoked')
  return status || t('accountShare.quotaAdmin.statusUnknown')
}

function quotaDimensionLabel(dimension: string): string {
  const labels: Record<string, string> = {
    max_live_rooms: t('accountShare.quotaAdmin.dimLiveRooms'),
    max_room_creates_24_hours: t('accountShare.quotaAdmin.dimCreates24h'),
    max_accounts_per_room: t('accountShare.quotaAdmin.dimAccountsPerRoom'),
    max_room_accounts_per_owner: t('accountShare.quotaAdmin.dimOwnerAccounts')
  }
  return labels[dimension] || dimension
}

function batchResultStatusLabel(
  status: AccountShareGrandfatherBatchItemResult['status']
): string {
  if (status === 'applied') return t('accountShare.quotaAdmin.resultApplied')
  if (status === 'skipped') return t('accountShare.quotaAdmin.resultSkipped')
  if (status === 'conflict') return t('accountShare.quotaAdmin.resultConflict')
  return t('accountShare.quotaAdmin.resultFailed')
}

function batchResultStatusClass(
  status: AccountShareGrandfatherBatchItemResult['status']
): string {
  return `quota-result-status-${status}`
}

function batchResultMessage(result: AccountShareGrandfatherBatchItemResult): string {
  if (result.result_code && QUOTA_ERROR_MESSAGES[result.result_code]) {
    return QUOTA_ERROR_MESSAGES[result.result_code]
  }
  if (result.status === 'applied') {
    return result.policy_version
      ? t('accountShare.quotaAdmin.appliedVersion', { version: result.policy_version })
      : t('accountShare.quotaAdmin.appliedGeneric')
  }
  return result.message?.trim() || t('accountShare.quotaAdmin.noDetailReason')
}
</script>

<style scoped>
.quota-admin-shell,
.quota-admin-workspace,
.quota-admin-editor,
.quota-audit-panel,
.quota-batch-workspace,
.quota-batch-candidates,
.quota-batch-control,
.quota-batch-results {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.quota-admin-summary {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 0.75rem;
  border: 1px solid rgb(221 214 254);
  border-radius: 1rem;
  background: linear-gradient(135deg, rgb(245 243 255), rgb(248 250 252));
  padding: 1rem;
}

.quota-admin-summary-icon {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
  background: rgb(237 233 254);
  color: rgb(109 40 217);
}

.quota-admin-summary span,
.quota-admin-summary strong,
.quota-admin-summary p {
  display: block;
}

.quota-admin-summary > div > span {
  color: rgb(109 40 217);
  font-size: 0.75rem;
  font-weight: 700;
}

.quota-admin-summary strong {
  margin-top: 0.125rem;
  color: rgb(15 23 42);
}

.quota-admin-summary p {
  margin-top: 0.375rem;
  max-width: 70ch;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  line-height: 1.35rem;
}

.quota-admin-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.375rem;
  border-radius: 0.875rem;
  background: rgb(241 245 249);
  padding: 0.25rem;
}

.quota-admin-tabs button {
  min-height: 2.75rem;
  cursor: pointer;
  border-radius: 0.6875rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  font-weight: 700;
}

.quota-admin-tabs .quota-admin-tab-active {
  background: white;
  color: rgb(109 40 217);
  box-shadow: 0 1px 3px rgb(15 23 42 / 0.12);
}

.quota-admin-editor,
.quota-audit-panel,
.quota-batch-candidates,
.quota-batch-control,
.quota-batch-results {
  align-content: start;
  border: 1px solid rgb(226 232 240);
  border-radius: 1rem;
  background: white;
  padding: 1rem;
}

.quota-batch-toolbar,
.quota-batch-toolbar > div,
.quota-result-card > div {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.625rem;
}

.quota-batch-toolbar {
  justify-content: space-between;
}

.quota-batch-toolbar > div:first-child {
  display: grid;
  gap: 0.125rem;
}

.quota-batch-toolbar strong,
.quota-batch-toolbar span {
  display: block;
}

.quota-batch-toolbar strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.quota-batch-toolbar span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-candidate-list,
.quota-result-list {
  display: grid;
  min-width: 0;
  gap: 0.75rem;
}

.quota-candidate-card {
  display: grid;
  min-width: 0;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  background: rgb(248 250 252);
  padding: 0.875rem;
  transition: border-color 180ms ease, background-color 180ms ease, box-shadow 180ms ease;
}

.quota-candidate-card-selected {
  border-color: rgb(139 92 246);
  background: rgb(245 243 255);
  box-shadow: 0 0 0 1px rgb(139 92 246 / 0.16);
}

.quota-candidate-select {
  display: flex;
  min-height: 2.75rem;
  min-width: 0;
  cursor: pointer;
  align-items: flex-start;
  gap: 0.75rem;
}

.quota-candidate-select input {
  width: 1.125rem;
  height: 1.125rem;
  flex: 0 0 auto;
  margin-top: 0.1875rem;
}

.quota-candidate-select span,
.quota-candidate-select strong,
.quota-candidate-select small {
  display: block;
  min-width: 0;
}

.quota-candidate-select strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.quota-candidate-select small {
  margin-top: 0.25rem;
  overflow-wrap: anywhere;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-candidate-dimensions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.quota-candidate-dimensions span {
  border-radius: 9999px;
  background: rgb(254 226 226);
  padding: 0.25rem 0.5rem;
  color: rgb(185 28 28);
  font-size: 0.6875rem;
  font-weight: 700;
}

.quota-candidate-metrics {
  display: grid;
  min-width: 0;
  gap: 0.5rem;
}

.quota-candidate-metrics > div {
  min-width: 0;
  border-radius: 0.75rem;
  background: white;
  padding: 0.625rem;
}

.quota-candidate-metrics dt,
.quota-candidate-metrics dd,
.quota-candidate-metrics small {
  display: block;
  overflow-wrap: anywhere;
}

.quota-candidate-metrics dt,
.quota-candidate-metrics small {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.quota-candidate-metrics dd {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 700;
}

.quota-candidate-metrics small {
  margin-top: 0.1875rem;
}

.quota-batch-selection-summary {
  display: grid;
  gap: 0.25rem;
  border-radius: 0.875rem;
  background: rgb(245 243 255);
  padding: 0.875rem;
}

.quota-batch-selection-summary span,
.quota-batch-selection-summary strong,
.quota-batch-selection-summary small {
  display: block;
  overflow-wrap: anywhere;
}

.quota-batch-selection-summary span,
.quota-batch-selection-summary small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-batch-selection-summary strong {
  color: rgb(91 33 182);
  font-size: 1.125rem;
}

.quota-result-card {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: 0.625rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.quota-result-card > div {
  justify-content: space-between;
}

.quota-result-card strong,
.quota-result-card p,
.quota-result-card small {
  overflow-wrap: anywhere;
}

.quota-result-card strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.quota-result-card p {
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  line-height: 1.35rem;
}

.quota-result-card small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-result-status {
  border-radius: 9999px;
  padding: 0.25rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 700;
}

.quota-result-status-applied {
  background: rgb(220 252 231);
  color: rgb(21 128 61);
}

.quota-result-status-skipped {
  background: rgb(241 245 249);
  color: rgb(71 85 105);
}

.quota-result-status-conflict {
  background: rgb(254 243 199);
  color: rgb(146 64 14);
}

.quota-result-status-failed {
  background: rgb(254 226 226);
  color: rgb(185 28 28);
}

.quota-section-heading {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.quota-section-heading strong,
.quota-section-heading span {
  display: block;
}

.quota-section-heading strong {
  color: rgb(15 23 42);
  font-size: 0.9375rem;
}

.quota-section-heading span {
  margin-top: 0.25rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-icon-button {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  cursor: pointer;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  color: rgb(71 85 105);
}

.quota-icon-button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

:deep(.quota-limits-grid) {
  display: grid;
  min-width: 0;
  gap: 0.75rem;
}

.quota-field {
  display: grid;
  min-width: 0;
  gap: 0.375rem;
}

.quota-field > span {
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  font-weight: 700;
}

.quota-field small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.2rem;
}

.quota-owner-search,
.quota-owner-actions,
.quota-confirm-footer {
  display: grid;
  gap: 0.75rem;
}

.quota-owner-state {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.quota-owner-state > div {
  min-width: 0;
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.quota-owner-state span,
.quota-owner-state strong {
  display: block;
  overflow-wrap: anywhere;
}

.quota-owner-state span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-owner-state strong {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.quota-alert {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.625rem;
  border: 1px solid rgb(253 230 138);
  border-radius: 0.875rem;
  background: rgb(255 251 235);
  padding: 0.875rem;
  color: rgb(146 64 14);
  font-size: 0.8125rem;
  line-height: 1.35rem;
}

.quota-alert svg {
  flex: 0 0 auto;
  margin-top: 0.125rem;
}

.quota-alert-error {
  border-color: rgb(254 202 202);
  background: rgb(254 242 242);
  color: rgb(185 28 28);
}

.quota-empty {
  display: flex;
  min-height: 8rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  border: 1px dashed rgb(203 213 225);
  border-radius: 0.875rem;
  padding: 1.25rem;
  color: rgb(100 116 139);
  font-size: 0.8125rem;
  text-align: center;
}

.quota-audit-list {
  display: grid;
  gap: 0.625rem;
}

.quota-audit-card {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.quota-audit-card > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
}

.quota-audit-card strong,
.quota-audit-card span,
.quota-audit-card p,
.quota-audit-card small {
  overflow-wrap: anywhere;
}

.quota-audit-card strong {
  color: rgb(15 23 42);
  font-size: 0.8125rem;
}

.quota-audit-card span,
.quota-audit-card small {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.quota-audit-card p {
  margin-top: 0.5rem;
  color: rgb(51 65 85);
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.quota-audit-card small {
  display: block;
  margin-top: 0.5rem;
}

.quota-audit-pagination {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
}

.quota-audit-pagination span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-confirmation {
  display: grid;
  gap: 1rem;
}

.quota-confirmation dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.quota-confirmation dl > div {
  min-width: 0;
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.quota-confirmation .quota-confirmation-wide {
  grid-column: 1 / -1;
}

.quota-confirmation dt {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.quota-confirmation dd {
  margin-top: 0.25rem;
  overflow-wrap: anywhere;
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 700;
}

.quota-confirm-check {
  display: flex;
  min-height: 2.75rem;
  cursor: pointer;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(221 214 254);
  border-radius: 0.875rem;
  padding: 0.875rem;
}

.quota-confirm-check input {
  width: 1.125rem;
  height: 1.125rem;
  flex: 0 0 auto;
  margin-top: 0.125rem;
}

.quota-confirm-check strong,
.quota-confirm-check small {
  display: block;
}

.quota-confirm-check strong {
  color: rgb(15 23 42);
  font-size: 0.8125rem;
}

.quota-confirm-check small {
  margin-top: 0.25rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.2rem;
}

@media (min-width: 640px) {
  :deep(.quota-limits-grid) {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .quota-owner-search {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: end;
  }

  .quota-owner-actions,
  .quota-confirm-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .quota-candidate-metrics,
  .quota-result-list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .quota-admin-workspace {
    grid-template-columns: minmax(0, 1.35fr) minmax(18rem, 0.65fr);
    align-items: start;
  }

  .quota-owner-state {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .quota-batch-workspace {
    grid-template-columns: minmax(0, 1.45fr) minmax(18rem, 0.55fr);
    align-items: start;
  }

  .quota-batch-control {
    position: sticky;
    top: 0;
  }

  .quota-batch-results {
    grid-column: 1 / -1;
  }
}

:global(.dark) .quota-admin-summary {
  border-color: rgb(109 40 217 / 0.45);
  background: linear-gradient(135deg, rgb(76 29 149 / 0.24), rgb(24 24 27));
}

:global(.dark) .quota-admin-summary-icon {
  background: rgb(76 29 149 / 0.45);
  color: rgb(221 214 254);
}

:global(.dark) .quota-admin-summary strong,
:global(.dark) .quota-section-heading strong,
:global(.dark) .quota-owner-state strong,
:global(.dark) .quota-audit-card strong,
:global(.dark) .quota-batch-toolbar strong,
:global(.dark) .quota-candidate-select strong,
:global(.dark) .quota-candidate-metrics dd,
:global(.dark) .quota-result-card strong,
:global(.dark) .quota-confirmation dd,
:global(.dark) .quota-confirm-check strong {
  color: rgb(248 250 252);
}

:global(.dark) .quota-admin-summary p,
:global(.dark) .quota-section-heading span,
:global(.dark) .quota-field small,
:global(.dark) .quota-owner-state span,
:global(.dark) .quota-audit-card span,
:global(.dark) .quota-audit-card small,
:global(.dark) .quota-batch-toolbar span,
:global(.dark) .quota-candidate-select small,
:global(.dark) .quota-candidate-metrics dt,
:global(.dark) .quota-candidate-metrics small,
:global(.dark) .quota-result-card small,
:global(.dark) .quota-confirmation dt,
:global(.dark) .quota-confirm-check small {
  color: rgb(161 161 170);
}

:global(.dark) .quota-admin-tabs {
  background: rgb(24 24 27);
}

:global(.dark) .quota-admin-tabs button {
  color: rgb(161 161 170);
}

:global(.dark) .quota-admin-tabs .quota-admin-tab-active {
  background: rgb(63 63 70);
  color: rgb(221 214 254);
}

:global(.dark) .quota-admin-editor,
:global(.dark) .quota-audit-panel,
:global(.dark) .quota-batch-candidates,
:global(.dark) .quota-batch-control,
:global(.dark) .quota-batch-results,
:global(.dark) .quota-icon-button,
:global(.dark) .quota-audit-card,
:global(.dark) .quota-confirm-check {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

:global(.dark) .quota-owner-state > div,
:global(.dark) .quota-candidate-metrics > div,
:global(.dark) .quota-confirmation dl > div {
  background: rgb(39 39 42);
}

:global(.dark) .quota-field > span,
:global(.dark) .quota-audit-card p,
:global(.dark) .quota-result-card p {
  color: rgb(212 212 216);
}

:global(.dark) .quota-candidate-card,
:global(.dark) .quota-result-card {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

:global(.dark) .quota-candidate-card-selected {
  border-color: rgb(139 92 246);
  background: rgb(76 29 149 / 0.22);
}

:global(.dark) .quota-batch-selection-summary {
  background: rgb(76 29 149 / 0.24);
}

:global(.dark) .quota-batch-selection-summary strong {
  color: rgb(221 214 254);
}

:global(.dark) .quota-alert-warning {
  border-color: rgb(146 64 14);
  background: rgb(120 53 15 / 0.24);
  color: rgb(253 230 138);
}

:global(.dark) .quota-alert-error {
  border-color: rgb(153 27 27);
  background: rgb(127 29 29 / 0.22);
  color: rgb(254 202 202);
}

:global(.dark) .quota-empty {
  border-color: rgb(82 82 91);
  color: rgb(161 161 170);
}
</style>
