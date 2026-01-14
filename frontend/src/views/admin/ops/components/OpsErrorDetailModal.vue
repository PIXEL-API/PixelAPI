<template>
  <BaseDialog :show="show" :title="title" width="full" :close-on-click-outside="true" @close="close">
    <div v-if="loading" class="flex items-center justify-center py-16">
      <div class="flex flex-col items-center gap-3">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
        <div class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorDetail.loading') }}</div>
      </div>
    </div>

    <div v-else-if="!detail" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ emptyText }}
    </div>

    <div v-else class="space-y-6 p-6">
      <!-- Header actions -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-xs">
          <span class="font-semibold text-gray-600 dark:text-gray-300">{{ t('admin.ops.errorDetail.resolution') }}</span>
          <span :class="(detail as any).resolved ? 'text-green-700 dark:text-green-400' : 'text-amber-700 dark:text-amber-300'">
            {{ (detail as any).resolved ? t('admin.ops.errorDetails.resolved') : t('admin.ops.errorDetails.unresolved') }}
          </span>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            v-if="!(detail as any).resolved"
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            @click="markResolved(true)"
          >
            {{ t('admin.ops.errorDetail.markResolved') }}
          </button>
          <button
            v-else
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            @click="markResolved(false)"
          >
            {{ t('admin.ops.errorDetail.markUnresolved') }}
          </button>
        </div>
      </div>

      <!-- Tabs -->
      <div class="flex flex-wrap gap-2 border-b border-gray-200 pb-3 dark:border-dark-700">
        <button type="button" class="btn btn-secondary btn-sm" :class="activeTab==='overview' ? 'opacity-100' : 'opacity-70'" @click="activeTab='overview'">{{ t('admin.ops.errorDetail.tabOverview') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :class="activeTab==='retries' ? 'opacity-100' : 'opacity-70'" @click="activeTab='retries'">{{ t('admin.ops.errorDetail.tabRetries') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :class="activeTab==='request' ? 'opacity-100' : 'opacity-70'" @click="activeTab='request'">{{ t('admin.ops.errorDetail.tabRequest') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :class="activeTab==='response' ? 'opacity-100' : 'opacity-70'" @click="activeTab='response'">{{ t('admin.ops.errorDetail.tabResponse') }}</button>
      </div>

      <div v-if="activeTab==='overview'">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-4">
        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
          <div class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.errorDetail.requestId') }}</div>
          <div class="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">
            {{ detail.request_id || detail.client_request_id || '—' }}
          </div>
        </div>

        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
          <div class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.errorDetail.time') }}</div>
          <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">
            {{ formatDateTime(detail.created_at) }}
          </div>
        </div>

        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
          <div class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.errorDetail.phase') }}</div>
          <div class="mt-1 text-sm font-bold uppercase text-gray-900 dark:text-white">
            {{ detail.phase || '—' }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ detail.type || '—' }}
          </div>
        </div>

        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
          <div class="text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.errorDetail.status') }}</div>
          <div class="mt-1 flex flex-wrap items-center gap-2">
            <span :class="['inline-flex items-center rounded-lg px-2 py-1 text-xs font-black ring-1 ring-inset shadow-sm', statusClass]">
              {{ detail.status_code }}
            </span>
            <span
              v-if="detail.severity"
              :class="['rounded-md px-2 py-0.5 text-[10px] font-black shadow-sm', severityClass]"
            >
              {{ detail.severity }}
            </span>
          </div>
        </div>
      </div>

      <!-- Message -->
      <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
        <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.message') }}</h3>
        <div class="text-sm font-medium text-gray-800 dark:text-gray-200 break-words">
          {{ detail.message || '—' }}
        </div>
      </div>

      <!-- Suggestion -->
      <div v-if="handlingSuggestion" class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
        <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.suggestion') }}</h3>
        <div class="text-sm font-medium text-gray-800 dark:text-gray-200 break-words">
          {{ handlingSuggestion }}
        </div>
      </div>

        <!-- Classification -->
        <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
          <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.classification') }}</h3>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.phase') }}</div>
              <div class="mt-1 text-sm font-bold uppercase text-gray-900 dark:text-white">{{ detail.phase || '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.owner') }}</div>
              <div class="mt-1 text-sm font-bold uppercase text-gray-900 dark:text-white">{{ (detail as any).error_owner || '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.source') }}</div>
              <div class="mt-1 text-sm font-bold uppercase text-gray-900 dark:text-white">{{ (detail as any).error_source || '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.retryable') }}</div>
              <div class="mt-1 text-sm font-bold text-gray-900 dark:text-white">{{ (detail as any).is_retryable ? t('common.yes') : t('common.no') }}</div>
            </div>
          </div>

          <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.resolvedAt') }}</div>
              <div class="mt-1 font-mono text-xs text-gray-700 dark:text-gray-200">{{ (detail as any).resolved_at || '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.resolvedBy') }}</div>
              <div class="mt-1 font-mono text-xs text-gray-700 dark:text-gray-200">{{ (detail as any).resolved_by_user_id ?? '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.resolvedRetryId') }}</div>
              <div class="mt-1 font-mono text-xs text-gray-700 dark:text-gray-200">{{ (detail as any).resolved_retry_id ?? '—' }}</div>
            </div>
            <div>
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.classificationKeys.retryCount') }}</div>
              <div class="mt-1 font-mono text-xs text-gray-700 dark:text-gray-200">{{ (detail as any).retry_count ?? '—' }}</div>
            </div>
          </div>
        </div>

        <!-- Basic Info -->
        <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
          <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.basicInfo') }}</h3>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.platform') }}</div>
            <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ detail.platform || '—' }}</div>
          </div>
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.model') }}</div>
            <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ detail.model || '—' }}</div>
          </div>
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.group') }}</div>
            <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">
              <el-tooltip v-if="detail.group_id" :content="t('admin.ops.errorLog.id') + ' ' + detail.group_id" placement="top">
                <span>{{ detail.group_name || detail.group_id }}</span>
              </el-tooltip>
              <span v-else>—</span>
            </div>
          </div>
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.account') }}</div>
            <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">
              <el-tooltip v-if="detail.account_id" :content="t('admin.ops.errorLog.id') + ' ' + detail.account_id" placement="top">
                <span>{{ detail.account_name || detail.account_id }}</span>
              </el-tooltip>
              <span v-else>—</span>
            </div>
          </div>
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.ttft') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.time_to_first_token_ms != null ? `${detail.time_to_first_token_ms}ms` : '—' }}
            </div>
          </div>
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.businessLimited') }}</div>
            <div class="mt-1 text-sm font-medium text-gray-900 dark:text-white">
              {{ detail.is_business_limited ? 'true' : 'false' }}
            </div>
          </div>
          <div class="lg:col-span-2">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.requestPath') }}</div>
            <div class="mt-1 font-mono text-xs text-gray-700 dark:text-gray-200 break-all">
              {{ detail.request_path || '—' }}
            </div>
          </div>
        </div>
      </div>

      <!-- Timings (best-effort fields) -->
      <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
        <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.timings') }}</h3>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div class="rounded-lg bg-white p-4 shadow-sm dark:bg-dark-800">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.auth') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.auth_latency_ms != null ? `${detail.auth_latency_ms}ms` : '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-white p-4 shadow-sm dark:bg-dark-800">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.routing') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.routing_latency_ms != null ? `${detail.routing_latency_ms}ms` : '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-white p-4 shadow-sm dark:bg-dark-800">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.upstream') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.upstream_latency_ms != null ? `${detail.upstream_latency_ms}ms` : '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-white p-4 shadow-sm dark:bg-dark-800">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.response') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.response_latency_ms != null ? `${detail.response_latency_ms}ms` : '—' }}
            </div>
          </div>
        </div>
      </div>

       <!-- Retry -->
       <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
         <div class="flex flex-col justify-between gap-4 md:flex-row md:items-start">
           <div class="space-y-1">
             <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.retry') }}</h3>
             <div class="text-xs text-gray-500 dark:text-gray-400">
               {{ t('admin.ops.errorDetail.retryNote1') }}
             </div>
           </div>
           <div class="flex flex-wrap gap-2">
             <template v-if="(detail as any).is_retryable">
               <button type="button" class="btn btn-secondary btn-sm" :disabled="retrying" @click="openRetryConfirm('client')">
                 {{ t('admin.ops.errorDetail.retryClient') }}
               </button>
               <button
                 v-if="props.errorType === 'upstream'"
                 type="button"
                 class="btn btn-secondary btn-sm"
                 :disabled="retrying"
                 @click="openRetryConfirm('upstream')"
               >
                 {{ t('admin.ops.errorDetail.retryUpstream') }}
               </button>
             </template>
             <template v-else>
               <span class="text-xs font-semibold text-amber-700 dark:text-amber-300">{{ t('admin.ops.errorDetail.notRetryable') }}</span>
             </template>
           </div>
         </div>


        <div v-if="props.errorType === 'upstream'" class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-3">
          <div class="md:col-span-1">
            <label class="mb-1 block text-xs font-bold uppercase tracking-wider text-gray-400">{{ t('admin.ops.errorDetail.pinnedAccountId') }}</label>
            <input v-model="pinnedAccountIdInput" type="text" class="input font-mono text-sm" disabled />
            <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
               {{ t('admin.ops.errorDetail.pinnedToOriginalAccountId') }}
            </div>
          </div>
          <div class="md:col-span-2">
            <div class="rounded-lg bg-white p-4 shadow-sm dark:bg-dark-800">
              <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.retryNotes') }}</div>
              <ul class="mt-2 list-disc space-y-1 pl-5 text-xs text-gray-600 dark:text-gray-300">
                <li>{{ t('admin.ops.errorDetail.retryNote3') }}</li>
                <li>{{ t('admin.ops.errorDetail.retryNote4') }}</li>
              </ul>
            </div>
          </div>
        </div>
      </div>

      <!-- Upstream errors -->
      <div
        v-if="detail.upstream_status_code || detail.upstream_error_message || detail.upstream_error_detail || detail.upstream_errors"
        class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900"
      >
        <h3 class="mb-4 text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">
          {{ t('admin.ops.errorDetails.upstreamErrors') }}
        </h3>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div>
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.upstreamKeys.status') }}</div>
            <div class="mt-1 font-mono text-sm font-bold text-gray-900 dark:text-white">
              {{ detail.upstream_status_code != null ? detail.upstream_status_code : '—' }}
            </div>
          </div>
          <div class="sm:col-span-2">
            <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.upstreamKeys.message') }}</div>
            <div class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white">
              {{ detail.upstream_error_message || '—' }}
            </div>
          </div>
        </div>

        <div v-if="detail.upstream_error_detail" class="mt-4">
          <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.upstreamKeys.detail') }}</div>
          <pre
            class="mt-2 max-h-[240px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"
          ><code>{{ prettyJSON(detail.upstream_error_detail) }}</code></pre>
        </div>

        <div v-if="detail.upstream_errors" class="mt-5">
          <div class="mb-2 text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.upstreamKeys.upstreamErrors') }}</div>

          <div v-if="upstreamErrors.length" class="space-y-3">
            <div
              v-for="(ev, idx) in upstreamErrors"
              :key="idx"
              class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="text-xs font-black text-gray-800 dark:text-gray-100">
                  #{{ idx + 1 }} <span v-if="ev.kind" class="font-mono">{{ ev.kind }}</span>
                </div>
                <div class="flex items-center gap-2">
                  <button
                    v-if="props.errorType !== 'upstream'"
                    type="button"
                    class="rounded-md bg-gray-100 px-2 py-1 text-[10px] font-bold text-gray-700 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-200 dark:hover:bg-dark-600"
                    :disabled="retrying || !ev.upstream_request_body"
                    :title="ev.upstream_request_body ? '' : t('admin.ops.errorDetail.missingUpstreamRequestBody')"
                    @click.stop="retryUpstreamEvent(idx)"
                  >
                    {{ t('admin.ops.errorDetail.retryUpstream') }} #{{ idx + 1 }}
                  </button>
                  <div class="font-mono text-xs text-gray-500 dark:text-gray-400">
                    {{ ev.at_unix_ms ? formatDateTime(new Date(ev.at_unix_ms)) : '' }}
                  </div>
                </div>
              </div>

              <div class="mt-2 grid grid-cols-1 gap-2 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2">
                <div>
                  <span class="text-gray-400">{{ t('admin.ops.errorDetail.upstreamEvent.account') }}:</span>
                  <el-tooltip v-if="ev.account_id" :content="t('admin.ops.errorLog.id') + ' ' + ev.account_id" placement="top">
                    <span class="font-medium text-gray-900 dark:text-white ml-1">{{ ev.account_name || ev.account_id }}</span>
                  </el-tooltip>
                  <span v-else class="ml-1">—</span>
                </div>
                <div><span class="text-gray-400">{{ t('admin.ops.errorDetail.upstreamEvent.status') }}:</span> <span class="font-mono ml-1">{{ ev.upstream_status_code ?? '—' }}</span></div>
                <div class="sm:col-span-2 break-all">
                  <span class="text-gray-400">{{ t('admin.ops.errorDetail.upstreamEvent.requestId') }}:</span> <span class="font-mono ml-1">{{ ev.upstream_request_id || '—' }}</span>
                </div>
              </div>

              <div v-if="ev.message" class="mt-2 break-words text-sm font-medium text-gray-900 dark:text-white">
                {{ ev.message }}
              </div>

              <pre
                v-if="ev.detail"
                class="mt-3 max-h-[240px] overflow-auto rounded-xl border border-gray-200 bg-gray-50 p-3 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-100"
              ><code>{{ prettyJSON(ev.detail) }}</code></pre>
            </div>
          </div>

          <pre
            v-else
            class="max-h-[420px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"
          ><code>{{ prettyJSON(detail.upstream_errors) }}</code></pre>
        </div>
      </div>

      <!-- Request body -->
      <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.requestBody') }}</h3>
          <div
            v-if="detail.request_body_truncated"
            class="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
          >
            {{ t('admin.ops.errorDetail.trimmed') }}
          </div>
        </div>
        <pre
          class="mt-4 max-h-[420px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"
        ><code>{{ prettyJSON(detail.request_body) }}</code></pre>
      </div>

       <!-- Error body -->
       <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
         <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.errorBody') }}</h3>
         <pre
           class="mt-4 max-h-[420px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"
         ><code>{{ prettyJSON(detail.error_body) }}</code></pre>
       </div>
      </div>

      <div v-else-if="activeTab==='retries'">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorDetail.retryHistory') }}</div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary btn-sm" @click="loadRetryHistory">{{ t('common.refresh') }}</button>
          </div>
        </div>

        <div class="mt-4">
          <div v-if="retryHistoryLoading" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</div>
          <div v-else-if="!retryHistory.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.noData') }}</div>
          <div v-else>
            <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
              <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
                <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.compareA') }}</div>
                <select v-model.number="compareA" class="input mt-2 w-full font-mono text-xs">
                  <option :value="null">—</option>
                  <option v-for="a in retryHistory" :key="a.id" :value="a.id">#{{ a.id }} · {{ a.mode }} · {{ a.status }}</option>
                </select>
              </div>
              <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900">
                <div class="text-xs font-bold uppercase text-gray-400">{{ t('admin.ops.errorDetail.compareB') }}</div>
                <select v-model.number="compareB" class="input mt-2 w-full font-mono text-xs">
                  <option :value="null">—</option>
                  <option v-for="b in retryHistory" :key="b.id" :value="b.id">#{{ b.id }} · {{ b.mode }} · {{ b.status }}</option>
                </select>
              </div>
            </div>

            <div v-if="selectedA || selectedB" class="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
                <div class="text-xs font-black text-gray-900 dark:text-white">{{ selectedA ? `#${selectedA.id} · ${selectedA.mode} · ${selectedA.status}` : '—' }}</div>
                <div class="mt-2 text-xs text-gray-600 dark:text-gray-300">
                  {{ t('admin.ops.errorDetail.retryMeta.http') }}: <span class="font-mono">{{ selectedA?.http_status_code ?? '—' }}</span> ·
                  {{ t('admin.ops.errorDetail.retryMeta.used') }}: <span class="font-mono">
                    <el-tooltip v-if="selectedA?.used_account_id" :content="'ID: ' + selectedA.used_account_id" placement="top">
                      <span class="font-medium">{{ selectedA.used_account_name || selectedA.used_account_id }}</span>
                    </el-tooltip>
                    <span v-else>—</span>
                  </span>
                </div>
                <pre class="mt-3 max-h-[320px] overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-900 dark:text-gray-100"><code>{{ selectedA?.response_preview || '' }}</code></pre>
                <div v-if="selectedA?.error_message" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ selectedA.error_message }}</div>
              </div>
              <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
                <div class="text-xs font-black text-gray-900 dark:text-white">{{ selectedB ? `#${selectedB.id} · ${selectedB.mode} · ${selectedB.status}` : '—' }}</div>
                <div class="mt-2 text-xs text-gray-600 dark:text-gray-300">
                  {{ t('admin.ops.errorDetail.retryMeta.http') }}: <span class="font-mono">{{ selectedB?.http_status_code ?? '—' }}</span> ·
                  {{ t('admin.ops.errorDetail.retryMeta.used') }}: <span class="font-mono">
                    <el-tooltip v-if="selectedB?.used_account_id" :content="'ID: ' + selectedB.used_account_id" placement="top">
                      <span class="font-medium">{{ selectedB.used_account_name || selectedB.used_account_id }}</span>
                    </el-tooltip>
                    <span v-else>—</span>
                  </span>
                </div>
                <pre class="mt-3 max-h-[320px] overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-900 dark:text-gray-100"><code>{{ selectedB?.response_preview || '' }}</code></pre>
                <div v-if="selectedB?.error_message" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ selectedB.error_message }}</div>
              </div>
            </div>

            <div v-else class="space-y-3">
              <div v-for="a in retryHistory" :key="a.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="text-xs font-black text-gray-900 dark:text-white">#{{ a.id }} · {{ a.mode }} · {{ a.status }}</div>
                  <div class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ a.created_at }}</div>
                </div>
                <div class="mt-2 grid grid-cols-1 gap-2 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-4">
                  <div><span class="text-gray-400">{{ t('admin.ops.errorDetail.retryMeta.success') }}:</span> <span class="font-mono">{{ a.success ?? '—' }}</span></div>
                  <div><span class="text-gray-400">{{ t('admin.ops.errorDetail.retryMeta.http') }}:</span> <span class="font-mono">{{ a.http_status_code ?? '—' }}</span></div>
                  <div>
                    <span class="text-gray-400">{{ t('admin.ops.errorDetail.retryMeta.pinned') }}:</span>
                    <el-tooltip v-if="a.pinned_account_id" :content="'ID: ' + a.pinned_account_id" placement="top">
                      <span class="font-mono ml-1">{{ a.pinned_account_name || a.pinned_account_id }}</span>
                    </el-tooltip>
                    <span v-else class="font-mono ml-1">—</span>
                  </div>
                  <div>
                    <span class="text-gray-400">{{ t('admin.ops.errorDetail.retryMeta.used') }}:</span>
                    <el-tooltip v-if="a.used_account_id" :content="'ID: ' + a.used_account_id" placement="top">
                      <span class="font-mono ml-1">{{ a.used_account_name || a.used_account_id }}</span>
                    </el-tooltip>
                    <span v-else class="font-mono ml-1">—</span>
                  </div>
                </div>
                <pre v-if="a.response_preview" class="mt-3 max-h-[240px] overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-900 dark:text-gray-100"><code>{{ a.response_preview }}</code></pre>
                <div v-if="a.error_message" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ a.error_message }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div v-else-if="activeTab==='request'">
        <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
          <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.requestBody') }}</h3>
          <pre class="mt-4 max-h-[520px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"><code>{{ prettyJSON(detail.request_body) }}</code></pre>
        </div>
      </div>

      <div v-else-if="activeTab==='response'">
        <div class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900">
          <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.responseBody') }}</h3>
          <div class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            {{ responseTabHint }}
          </div>
          <pre class="mt-4 max-h-[520px] overflow-auto rounded-xl border border-gray-200 bg-white p-4 text-xs text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100"><code>{{ prettyJSON(responseTabBody) }}</code></pre>
        </div>
      </div>
     </div>
   </BaseDialog>


  <ConfirmDialog
    :show="showRetryConfirm"
    :title="t('admin.ops.errorDetail.confirmRetry')"
    :message="retryConfirmMessage"
    @confirm="runConfirmedRetry"
    @cancel="cancelRetry"
  />

  <div v-if="showRetryConfirm && !(detail as any)?.is_retryable" class="fixed inset-0 z-[60] flex items-end justify-center p-4 pointer-events-none">
    <div class="pointer-events-auto w-full max-w-xl rounded-2xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-200">
      <label class="flex items-center gap-2">
        <input v-model="forceRetryAck" type="checkbox" class="h-4 w-4" />
        <span>{{ t('admin.ops.errorDetail.forceRetry') }}</span>
      </label>
    </div>
  </div>

</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores'
import { opsAPI, type OpsErrorDetail, type OpsRetryAttempt } from '@/api/admin/ops'
import { formatDateTime } from '@/utils/format'
import { getSeverityClass } from '../utils/opsFormatters'

interface Props {
  show: boolean
  errorId: number | null
  errorType?: 'request' | 'upstream'
}

interface Emits {
  (e: 'update:show', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const detail = ref<OpsErrorDetail | null>(null)

const activeTab = ref<'overview' | 'retries' | 'request' | 'response'>('overview')

const retrying = ref(false)
const showRetryConfirm = ref(false)
const pendingRetryMode = ref<'client' | 'upstream' | 'upstream_event'>('client')

const forceRetryAck = ref(false)
const retryHistory = ref<OpsRetryAttempt[]>([])
const retryHistoryLoading = ref(false)
const showRetryHistory = ref(false)

const compareA = ref<number | null>(null)
const compareB = ref<number | null>(null)

const pinnedAccountIdInput = ref('')

const title = computed(() => {
  if (!props.errorId) return t('admin.ops.errorDetail.title')
  return t('admin.ops.errorDetail.titleWithId', { id: String(props.errorId) })
})

const emptyText = computed(() => t('admin.ops.errorDetail.noErrorSelected'))

type UpstreamErrorEvent = {
  at_unix_ms?: number
  platform?: string
  account_id?: number
  account_name?: string
  upstream_status_code?: number
  upstream_request_id?: string
  upstream_request_body?: string
  kind?: string
  message?: string
  detail?: string
}

const upstreamErrors = computed<UpstreamErrorEvent[]>(() => {
  const raw = detail.value?.upstream_errors
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as UpstreamErrorEvent[]) : []
  } catch {
    return []
  }
})

function close() {
  emit('update:show', false)
}

function prettyJSON(raw?: string): string {
  if (!raw) return t('admin.ops.errorDetail.na')
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

const handlingSuggestion = computed(() => {
  const d: any = detail.value
  if (!d) return ''

  const owner = String(d.error_owner || '').toLowerCase()
  const phase = String(d.phase || '').toLowerCase()

  if (owner === 'provider' && phase === 'upstream') {
    if (retryHistory.value.some((r) => r.success === true) && d.resolved) {
      return t('admin.ops.errorDetail.suggestUpstreamResolved')
    }
    return t('admin.ops.errorDetail.suggestUpstream')
  }
  if (owner === 'client' && phase === 'request') {
    return t('admin.ops.errorDetail.suggestRequest')
  }
  if (owner === 'client' && phase === 'auth') {
    return t('admin.ops.errorDetail.suggestAuth')
  }
  if (owner === 'platform') {
    return t('admin.ops.errorDetail.suggestPlatform')
  }
  return t('admin.ops.errorDetail.suggestGeneric')
})

async function fetchDetail(id: number) {
  loading.value = true
  try {
    const kind = props.errorType || (detail.value?.phase === 'upstream' ? 'upstream' : 'request')
    const d = kind === 'upstream' ? await opsAPI.getUpstreamErrorDetail(id) : await opsAPI.getRequestErrorDetail(id)
    detail.value = d

    // Keep showing original account_id (read-only hint for upstream retries).
    pinnedAccountIdInput.value = d.account_id && d.account_id > 0 ? String(d.account_id) : ''
  } catch (err: any) {
    detail.value = null
    appStore.showError(err?.message || t('admin.ops.failedToLoadErrorDetail'))
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.show, props.errorId] as const,
  ([show, id]) => {
    if (!show) {
      detail.value = null
      retryHistory.value = []
      retryHistoryLoading.value = false
      showRetryHistory.value = false
      activeTab.value = 'overview'
      return
    }
    if (typeof id === 'number' && id > 0) {
      activeTab.value = 'overview'
      fetchDetail(id).then(() => {
        loadRetryHistory()
      })
    }
  },
  { immediate: true }
)

function openRetryConfirm(mode: 'client' | 'upstream' | 'upstream_event') {
  pendingRetryMode.value = mode
  // Force-ack required only when backend says not retryable.
  forceRetryAck.value = false
  showRetryConfirm.value = true
}

async function loadRetryHistory() {
  if (!props.errorId) return
  retryHistoryLoading.value = true
  try {
    const items = await opsAPI.listRetryAttempts(props.errorId, 50)
    retryHistory.value = items || []

    // Default compare selections: newest succeeded vs newest failed.
    if (retryHistory.value.length) {
      const succeeded = retryHistory.value.find((a) => a.success === true)
      const failed = retryHistory.value.find((a) => a.success === false)
      compareA.value = succeeded?.id ?? retryHistory.value[0].id
      compareB.value = failed?.id ?? (retryHistory.value[1]?.id ?? null)
    }
  } catch (err: any) {
    retryHistory.value = []
    compareA.value = null
    compareB.value = null
    appStore.showError(err?.message || t('admin.ops.errorDetail.failedToLoadRetryHistory'))
  } finally {
    retryHistoryLoading.value = false
  }
}

const selectedA = computed(() => retryHistory.value.find((a) => a.id === compareA.value) || null)
const selectedB = computed(() => retryHistory.value.find((a) => a.id === compareB.value) || null)

const bestSucceededAttempt = computed(() => retryHistory.value.find((a) => a.success === true) || null)

const responseTabBody = computed(() => {
  // Prefer any succeeded attempt preview; fall back to stored error body.
  const succeeded = bestSucceededAttempt.value
  if (succeeded?.response_preview) return succeeded.response_preview
  return detail.value?.error_body || ''
})

const responseTabHint = computed(() => {
  const succeeded = bestSucceededAttempt.value
  if (succeeded?.response_preview) {
    return t('admin.ops.errorDetail.responseHintSucceeded', { id: String(succeeded.id) }) || `Showing succeeded retry response_preview (#${succeeded.id})`
  }
  return t('admin.ops.errorDetail.responseHintFallback')
})

async function markResolved(resolved: boolean) {
  if (!props.errorId) return
  try {
    const kind = props.errorType || (detail.value?.phase === 'upstream' ? 'upstream' : 'request')
    if (kind === 'upstream') {
      await opsAPI.updateUpstreamErrorResolved(props.errorId, resolved)
    } else {
      await opsAPI.updateRequestErrorResolved(props.errorId, resolved)
    }
    await fetchDetail(props.errorId)
    appStore.showSuccess(resolved ? t('admin.ops.errorDetails.resolved') : t('admin.ops.errorDetails.unresolved'))
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.ops.errorDetail.failedToUpdateResolvedStatus'))
  }
}

const retryConfirmMessage = computed(() => {
  const mode = pendingRetryMode.value
  const retryable = !!(detail.value as any)?.is_retryable
  if (!retryable) {
    return t('admin.ops.errorDetail.forceRetryHint')
  }
  if (mode === 'upstream') {
    return t('admin.ops.errorDetail.confirmRetryMessage')
  }
  return t('admin.ops.errorDetail.confirmRetryHint')
})

const severityClass = computed(() => {
  if (!detail.value?.severity) return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
  return getSeverityClass(detail.value.severity)
})

const statusClass = computed(() => {
  const code = detail.value?.status_code ?? 0
  if (code >= 500) return 'bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-900/30 dark:text-red-400 dark:ring-red-500/30'
  if (code === 429) return 'bg-purple-50 text-purple-700 ring-purple-600/20 dark:bg-purple-900/30 dark:text-purple-400 dark:ring-purple-500/30'
  if (code >= 400) return 'bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-900/30 dark:text-amber-400 dark:ring-amber-500/30'
  return 'bg-gray-50 text-gray-700 ring-gray-600/20 dark:bg-gray-900/30 dark:text-gray-400 dark:ring-gray-500/30'
})

async function runConfirmedRetry() {
  if (!props.errorId) return
  const mode = pendingRetryMode.value
  const retryable = !!(detail.value as any)?.is_retryable
  if (!retryable && !forceRetryAck.value) {
    appStore.showError(t('admin.ops.errorDetail.forceRetryNeedAck'))
    return
  }

  showRetryConfirm.value = false

  retrying.value = true
  try {
    const kind = props.errorType || (detail.value?.phase === 'upstream' ? 'upstream' : 'request')

    let res
    if (kind === 'upstream') {
      // Upstream error retries always pin the original account_id.
      res = await opsAPI.retryUpstreamError(props.errorId)
      } else {
        if (mode === 'client') {
          res = await opsAPI.retryRequestErrorClient(props.errorId)
        } else {
          throw new Error(t('admin.ops.errorDetail.unsupportedRetryMode'))
        }
      }

    const summary = res.status === 'succeeded' ? t('admin.ops.errorDetail.retrySuccess') : t('admin.ops.errorDetail.retryFailed')
    appStore.showSuccess(summary)

    // Refresh detail + history so resolved reflects auto resolution
    await fetchDetail(props.errorId)
    await loadRetryHistory()
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.ops.retryFailed'))
  } finally {
    retrying.value = false
  }
}

async function retryUpstreamEvent(idx: number) {
  if (!props.errorId) return
  try {
    retrying.value = true
    const res = await opsAPI.retryRequestErrorUpstreamEvent(props.errorId, idx)
    const summary = res.status === 'succeeded' ? t('admin.ops.errorDetail.retrySuccess') : t('admin.ops.errorDetail.retryFailed')
    appStore.showSuccess(summary)
    await fetchDetail(props.errorId)
    await loadRetryHistory()
  } catch (err: any) {
    appStore.showError(err?.message || t('admin.ops.retryFailed'))
  } finally {
    retrying.value = false
  }
}

function cancelRetry() {
  showRetryConfirm.value = false
}
</script>