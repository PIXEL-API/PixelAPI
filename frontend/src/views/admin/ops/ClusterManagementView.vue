<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <header
        class="overflow-hidden rounded-2xl border border-line bg-surface shadow-panel"
      >
        <div class="h-1 bg-gradient-to-r from-primary-600 via-accent-500 to-emerald-500"></div>
        <div class="flex flex-col gap-4 p-5 sm:p-6 lg:flex-row lg:items-center lg:justify-between">
          <div class="min-w-0">
            <div class="mb-2 flex flex-wrap items-center gap-2">
              <span class="text-xs font-semibold uppercase tracking-[0.18em] text-primary-600 dark:text-primary-400">
                {{ t('admin.cluster.eyebrow') }}
              </span>
              <span
                v-if="summary"
                class="rounded-full px-2.5 py-1 text-xs font-semibold"
                :class="summary.enabled
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                  : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
              >
                {{ summary.enabled ? t('admin.cluster.enabled') : t('admin.cluster.disabled') }}
              </span>
            </div>
            <h1 class="text-2xl font-bold tracking-tight text-content sm:text-3xl">
              {{ t('admin.cluster.title') }}
            </h1>
            <p class="mt-2 max-w-3xl text-sm leading-6 text-content-muted">
              {{ t('admin.cluster.description') }}
            </p>
          </div>

          <div class="flex flex-col items-stretch gap-2 sm:flex-row sm:items-center">
            <div class="text-left text-xs text-content-subtle sm:text-right">
              <div>{{ t('admin.cluster.lastUpdated') }}</div>
              <div class="mt-0.5 font-mono text-content-muted">{{ formatDateTime(summary?.refreshed_at) }}</div>
            </div>
            <button
              type="button"
              class="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-line bg-surface-elevated px-4 text-sm font-semibold text-content transition hover:border-primary-300 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:border-primary-700 dark:hover:text-primary-300"
              :disabled="loading"
              @click="refresh"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
              {{ t('admin.cluster.refresh') }}
            </button>
          </div>
        </div>
      </header>

      <div
        v-if="errorMessage"
        role="alert"
        class="flex items-start gap-3 rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300"
      >
        <Icon name="exclamationTriangle" class="mt-0.5 flex-none" />
        <div class="min-w-0 flex-1">
          <div class="font-semibold">{{ t('admin.cluster.loadFailed') }}</div>
          <div class="mt-1 break-words">{{ errorMessage }}</div>
        </div>
      </div>

      <div
        v-if="summary && !summary.enabled"
        class="rounded-2xl border border-amber-200 bg-amber-50 p-5 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200"
      >
        <div class="flex items-start gap-3">
          <Icon name="infoCircle" class="mt-0.5 flex-none" />
          <div>
            <div class="font-semibold">{{ t('admin.cluster.disabledTitle') }}</div>
            <p class="mt-1 leading-6">{{ t('admin.cluster.disabledDescription') }}</p>
          </div>
        </div>
      </div>

      <section aria-labelledby="cluster-summary-title">
        <h2 id="cluster-summary-title" class="sr-only">{{ t('admin.cluster.summary.title') }}</h2>
        <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          <article
            v-for="card in summaryCards"
            :key="card.key"
            class="relative overflow-hidden rounded-2xl border border-line bg-surface p-4 shadow-card"
          >
            <div class="absolute inset-y-0 left-0 w-1" :class="card.railClass"></div>
            <div class="pl-1">
              <div class="text-xs font-semibold uppercase tracking-wide text-content-subtle">
                {{ card.label }}
              </div>
              <div class="mt-2 text-2xl font-bold tabular-nums text-content">{{ card.value }}</div>
              <div class="mt-1 truncate text-xs text-content-muted">{{ card.hint }}</div>
            </div>
          </article>
        </div>

        <div class="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-3">
          <article class="rounded-2xl border border-line bg-surface p-4 shadow-card">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-sm font-semibold text-content">{{ t('admin.cluster.summary.resilience') }}</div>
                <div class="mt-1 text-xs text-content-muted">{{ t('admin.cluster.summary.resilienceHint') }}</div>
              </div>
              <span class="status-pill" :class="summary?.n_minus_one_ready ? statusClass('ready') : statusClass('unhealthy')">
                {{ summary?.n_minus_one_ready ? t('admin.cluster.summary.nMinusOneReady') : t('admin.cluster.summary.nMinusOneRisk') }}
              </span>
            </div>
          </article>
          <article class="rounded-2xl border border-line bg-surface p-4 shadow-card">
            <div class="flex items-center justify-between gap-3">
              <div class="min-w-0">
                <div class="text-sm font-semibold text-content">{{ t('admin.cluster.summary.version') }}</div>
                <div class="mt-1 truncate font-mono text-xs text-content-muted">
                  {{ summary?.versions?.join(', ') || '—' }}
                </div>
              </div>
              <span class="status-pill" :class="summary?.version_consistent ? statusClass('ready') : statusClass('unhealthy')">
                {{ summary?.version_consistent ? t('admin.cluster.summary.consistent') : t('admin.cluster.summary.inconsistent') }}
              </span>
            </div>
          </article>
          <article class="rounded-2xl border border-line bg-surface p-4 shadow-card">
            <div class="flex items-center justify-between gap-3">
              <div>
                <div class="text-sm font-semibold text-content">{{ t('admin.cluster.summary.cacheSync') }}</div>
                <div class="mt-1 text-xs text-content-muted">{{ t('admin.cluster.summary.cacheSyncHint') }}</div>
              </div>
              <span class="status-pill" :class="(summary?.cache_lagging_nodes ?? 0) === 0 ? statusClass('ready') : statusClass('draining')">
                {{ t('admin.cluster.summary.laggingNodes', { count: summary?.cache_lagging_nodes ?? 0 }) }}
              </span>
            </div>
          </article>
        </div>
      </section>

      <section class="rounded-2xl border border-line bg-surface shadow-panel" aria-labelledby="cluster-nodes-title">
        <div class="flex flex-col gap-3 border-b border-line p-5 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 id="cluster-nodes-title" class="text-lg font-bold text-content">{{ t('admin.cluster.nodes.title') }}</h2>
            <p class="mt-1 text-sm text-content-muted">{{ t('admin.cluster.nodes.description') }}</p>
          </div>
          <button
            type="button"
            class="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-primary-600 px-4 text-sm font-semibold text-white shadow-sm transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="!summary?.enabled || instances.length === 0"
            @click="openCacheDialog"
          >
            <Icon name="sync" size="sm" />
            {{ t('admin.cluster.actions.refreshCache') }}
          </button>
        </div>

        <div v-if="loading && !hasLoaded" class="grid grid-cols-1 gap-3 p-5 lg:grid-cols-3">
          <div v-for="index in 3" :key="index" class="h-48 animate-pulse rounded-2xl bg-surface-subtle"></div>
        </div>

        <div v-else-if="instances.length === 0" class="p-10 text-center text-sm text-content-muted">
          <Icon name="server" size="xl" class="mx-auto mb-3 opacity-50" />
          {{ t('admin.cluster.nodes.empty') }}
        </div>

        <div v-else>
          <div class="grid grid-cols-1 gap-3 p-4 lg:hidden">
            <article
              v-for="node in instances"
              :key="node.node_id"
              class="rounded-2xl border border-line bg-surface-elevated p-4"
            >
              <div class="flex items-start justify-between gap-3">
                <button type="button" class="min-w-0 text-left" @click="openNodeDetails(node)">
                  <div class="truncate font-mono text-sm font-bold text-content">{{ node.node_id }}</div>
                  <div class="mt-1 truncate text-xs text-content-muted">{{ node.hostname }} · {{ node.version || '—' }}</div>
                </button>
                <span class="status-pill flex-none" :class="statusClass(node.status)">
                  {{ statusLabel(node.status) }}
                </span>
              </div>

              <div class="mt-4 grid grid-cols-2 gap-3 text-sm">
                <MetricCell :label="t('admin.cluster.nodes.cpu')" :value="formatPercent(node.cpu_usage_percent)" />
                <MetricCell :label="t('admin.cluster.nodes.memory')" :value="formatMemoryUsage(node)" />
                <MetricCell :label="t('admin.cluster.nodes.connections')" :value="String(node.active_http + node.active_sse + node.active_ws)" />
                <MetricCell :label="t('admin.cluster.nodes.lastSeen')" :value="formatRelativeTime(node.last_seen_at)" />
              </div>

              <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-line pt-4">
                <DependencyBadge :ok="node.db_ok" label="PostgreSQL" />
                <DependencyBadge :ok="node.redis_ok" label="Redis" />
                <div class="ml-auto flex gap-2">
                  <button
                    v-if="node.desired_state !== 'draining'"
                    type="button"
                    class="node-action node-action-danger"
                    :disabled="!canDrain(node)"
                    :title="!canDrain(node) ? t('admin.cluster.actions.drainUnavailable') : undefined"
                    @click="openNodeAction('drain', node)"
                  >
                    {{ t('admin.cluster.actions.drain') }}
                  </button>
                  <button
                    v-else
                    type="button"
                    class="node-action node-action-primary"
                    :disabled="!canResume(node)"
                    @click="openNodeAction('resume', node)"
                  >
                    {{ t('admin.cluster.actions.resume') }}
                  </button>
                </div>
              </div>
            </article>
          </div>

          <div class="hidden overflow-x-auto lg:block">
            <table class="min-w-full divide-y divide-line">
              <thead class="bg-surface-subtle">
                <tr class="text-left text-xs font-semibold uppercase tracking-wide text-content-subtle">
                  <th class="px-5 py-3">{{ t('admin.cluster.nodes.node') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cluster.nodes.status') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cluster.nodes.resources') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cluster.nodes.connections') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cluster.nodes.dependencies') }}</th>
                  <th class="px-4 py-3">{{ t('admin.cluster.nodes.lastSeen') }}</th>
                  <th class="px-5 py-3 text-right">{{ t('admin.cluster.nodes.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-line">
                <tr v-for="node in instances" :key="node.node_id" class="transition hover:bg-surface-hover">
                  <td class="px-5 py-4">
                    <button type="button" class="max-w-60 text-left" @click="openNodeDetails(node)">
                      <div class="truncate font-mono text-sm font-bold text-content hover:text-primary-600">{{ node.node_id }}</div>
                      <div class="mt-1 truncate text-xs text-content-muted">{{ node.hostname }} · {{ node.version || '—' }}</div>
                    </button>
                  </td>
                  <td class="px-4 py-4">
                    <span class="status-pill" :class="statusClass(node.status)">{{ statusLabel(node.status) }}</span>
                    <div v-if="node.readiness_message" class="mt-1 max-w-48 truncate text-xs text-content-subtle" :title="node.readiness_message">
                      {{ node.readiness_message }}
                    </div>
                  </td>
                  <td class="px-4 py-4 text-sm text-content-muted">
                    <div>CPU {{ formatPercent(node.cpu_usage_percent) }}</div>
                    <div class="mt-1">{{ formatMemoryUsage(node) }}</div>
                  </td>
                  <td class="px-4 py-4 font-mono text-xs text-content-muted">
                    HTTP {{ node.active_http }} · SSE {{ node.active_sse }} · WS {{ node.active_ws }}
                  </td>
                  <td class="px-4 py-4">
                    <div class="flex flex-wrap gap-2">
                      <DependencyBadge :ok="node.db_ok" label="PG" />
                      <DependencyBadge :ok="node.redis_ok" label="Redis" />
                    </div>
                  </td>
                  <td class="px-4 py-4 text-sm text-content-muted">
                    <div>{{ formatRelativeTime(node.last_seen_at) }}</div>
                    <div class="mt-1 font-mono text-xs text-content-subtle">{{ formatDateTime(node.last_seen_at) }}</div>
                  </td>
                  <td class="px-5 py-4">
                    <div class="flex justify-end gap-2">
                      <button
                        v-if="node.desired_state !== 'draining'"
                        type="button"
                        class="node-action node-action-danger"
                        :disabled="!canDrain(node)"
                        :title="!canDrain(node) ? t('admin.cluster.actions.drainUnavailable') : undefined"
                        @click="openNodeAction('drain', node)"
                      >
                        {{ t('admin.cluster.actions.drain') }}
                      </button>
                      <button
                        v-else
                        type="button"
                        class="node-action node-action-primary"
                        :disabled="!canResume(node)"
                        @click="openNodeAction('resume', node)"
                      >
                        {{ t('admin.cluster.actions.resume') }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <section class="rounded-2xl border border-line bg-surface shadow-panel" aria-labelledby="cluster-tasks-title">
          <div class="border-b border-line p-5">
            <h2 id="cluster-tasks-title" class="text-lg font-bold text-content">{{ t('admin.cluster.tasks.title') }}</h2>
            <p class="mt-1 text-sm text-content-muted">{{ t('admin.cluster.tasks.description') }}</p>
          </div>
          <div v-if="tasks.length === 0" class="p-8 text-center text-sm text-content-muted">
            {{ t('admin.cluster.tasks.empty') }}
          </div>
          <div v-else class="divide-y divide-line">
            <article v-for="task in tasks" :key="task.task_name" class="p-4 sm:p-5">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="truncate font-mono text-sm font-semibold text-content">{{ task.task_name }}</div>
                  <div class="mt-1 truncate text-xs text-content-muted">
                    {{ task.owner_node_id || t('admin.cluster.tasks.unowned') }}
                  </div>
                </div>
                <span class="rounded-lg bg-surface-subtle px-2 py-1 font-mono text-xs text-content-muted">
                  #{{ task.fencing_token }}
                </span>
              </div>
              <div class="mt-3 grid grid-cols-2 gap-3 text-xs sm:grid-cols-3">
                <MetricCell :label="t('admin.cluster.tasks.expires')" :value="formatDateTime(task.lease_expires_at)" />
                <MetricCell :label="t('admin.cluster.tasks.lastSuccess')" :value="formatRelativeTime(task.last_success_at)" />
                <MetricCell :label="t('admin.cluster.tasks.duration')" :value="formatDuration(task.last_duration_ms)" />
              </div>
              <div
                v-if="task.last_error"
                class="mt-3 rounded-xl bg-red-50 px-3 py-2 text-xs leading-5 text-red-700 dark:bg-red-950/30 dark:text-red-300"
              >
                {{ task.last_error }}
              </div>
            </article>
          </div>
        </section>

        <section class="rounded-2xl border border-line bg-surface shadow-panel" aria-labelledby="cluster-cache-title">
          <div class="flex items-center justify-between gap-3 border-b border-line p-5">
            <div>
              <h2 id="cluster-cache-title" class="text-lg font-bold text-content">{{ t('admin.cluster.cache.title') }}</h2>
              <p class="mt-1 text-sm text-content-muted">{{ t('admin.cluster.cache.description') }}</p>
            </div>
            <span class="status-pill" :class="(summary?.cache_lagging_nodes ?? 0) === 0 ? statusClass('ready') : statusClass('draining')">
              {{ t('admin.cluster.cache.lagging', { count: summary?.cache_lagging_nodes ?? 0 }) }}
            </span>
          </div>
          <div v-if="instances.length === 0" class="p-8 text-center text-sm text-content-muted">
            {{ t('admin.cluster.cache.empty') }}
          </div>
          <div v-else class="divide-y divide-line">
            <article v-for="node in instances" :key="`cache-${node.node_id}`" class="p-4 sm:p-5">
              <div class="flex items-center justify-between gap-3">
                <div class="truncate font-mono text-sm font-semibold text-content">{{ node.node_id }}</div>
                <span class="status-pill" :class="node.ready ? statusClass('ready') : statusClass(node.status)">
                  {{ node.ready ? t('admin.cluster.status.ready') : statusLabel(node.status) }}
                </span>
              </div>
              <div v-if="Object.keys(node.cache_versions || {}).length" class="mt-3 flex flex-wrap gap-2">
                <span
                  v-for="[scope, version] in Object.entries(node.cache_versions)"
                  :key="scope"
                  class="rounded-lg border border-line bg-surface-subtle px-2.5 py-1.5 font-mono text-xs text-content-muted"
                >
                  {{ cacheScopeLabel(scope) }} · v{{ version }}
                </span>
              </div>
              <div v-else class="mt-3 text-xs text-content-subtle">{{ t('admin.cluster.cache.noVersions') }}</div>
            </article>
          </div>
        </section>
      </div>

      <section class="rounded-2xl border border-line bg-surface shadow-panel" aria-labelledby="cluster-operations-title">
        <div class="border-b border-line p-5">
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 id="cluster-operations-title" class="text-lg font-bold text-content">{{ t('admin.cluster.operations.title') }}</h2>
              <p class="mt-1 text-sm text-content-muted">{{ t('admin.cluster.operations.description') }}</p>
            </div>
            <span v-if="hasActiveOperations" class="inline-flex items-center gap-2 text-xs font-semibold text-primary-600 dark:text-primary-300">
              <span class="h-2 w-2 animate-pulse rounded-full bg-primary-500"></span>
              {{ t('admin.cluster.operations.fastRefresh') }}
            </span>
          </div>
        </div>
        <div v-if="operations.length === 0" class="p-10 text-center text-sm text-content-muted">
          {{ t('admin.cluster.operations.empty') }}
        </div>
        <div v-else class="overflow-x-auto">
          <table class="min-w-[900px] w-full divide-y divide-line">
            <thead class="bg-surface-subtle">
              <tr class="text-left text-xs font-semibold uppercase tracking-wide text-content-subtle">
                <th class="px-5 py-3">{{ t('admin.cluster.operations.operation') }}</th>
                <th class="px-4 py-3">{{ t('admin.cluster.operations.target') }}</th>
                <th class="px-4 py-3">{{ t('admin.cluster.operations.status') }}</th>
                <th class="px-4 py-3">{{ t('admin.cluster.operations.reason') }}</th>
                <th class="px-4 py-3">{{ t('admin.cluster.operations.requestedBy') }}</th>
                <th class="px-5 py-3">{{ t('admin.cluster.operations.requestedAt') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-line">
              <tr v-for="operation in operations" :key="operation.id" class="align-top">
                <td class="px-5 py-4">
                  <div class="text-sm font-semibold text-content">{{ operationKindLabel(operation.kind) }}</div>
                  <div class="mt-1 max-w-48 truncate font-mono text-xs text-content-subtle" :title="operation.id">{{ operation.id }}</div>
                </td>
                <td class="px-4 py-4 font-mono text-sm text-content-muted">{{ operation.target_node_id || t('admin.cluster.operations.allNodes') }}</td>
                <td class="px-4 py-4">
                  <span class="status-pill" :class="operationStatusClass(operation.status)">
                    {{ operationStatusLabel(operation.status) }}
                  </span>
                  <div v-if="operation.error" class="mt-2 max-w-56 text-xs leading-5 text-red-600 dark:text-red-300">{{ operation.error }}</div>
                </td>
                <td class="max-w-72 px-4 py-4 text-sm leading-5 text-content-muted">{{ operation.reason }}</td>
                <td class="px-4 py-4 text-sm text-content-muted">{{ operation.requested_by }}</td>
                <td class="px-5 py-4 text-sm text-content-muted">{{ formatDateTime(operation.requested_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <BaseDialog
      :show="nodeActionDialog.show"
      :title="nodeActionTitle"
      width="narrow"
      :close-on-escape="!submitting"
      @close="closeNodeAction"
    >
      <div class="space-y-4">
        <div
          class="rounded-xl border p-3 text-sm leading-6"
          :class="nodeActionDialog.kind === 'drain'
            ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200'
            : 'border-primary-200 bg-primary-50 text-primary-800 dark:border-primary-900/50 dark:bg-primary-950/20 dark:text-primary-200'"
        >
          {{ nodeActionDescription }}
        </div>
        <div class="rounded-xl bg-surface-subtle p-3">
          <div class="text-xs text-content-subtle">{{ t('admin.cluster.dialog.targetNode') }}</div>
          <div class="mt-1 font-mono text-sm font-semibold text-content">{{ nodeActionDialog.node?.node_id }}</div>
        </div>
        <TextArea
          v-model="operationReason"
          :label="t('admin.cluster.dialog.reason')"
          :placeholder="t('admin.cluster.dialog.reasonPlaceholder')"
          :hint="t('admin.cluster.dialog.reasonHint', { count: operationReason.trim().length })"
          :error="reasonError"
          :disabled="submitting"
          :rows="4"
          required
        />
      </div>
      <template #footer>
        <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button type="button" class="dialog-button dialog-button-secondary" :disabled="submitting" @click="closeNodeAction">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="dialog-button"
            :class="nodeActionDialog.kind === 'drain' ? 'dialog-button-danger' : 'dialog-button-primary'"
            :disabled="submitting || !reasonValid"
            @click="submitNodeAction"
          >
            {{ submitting ? t('admin.cluster.dialog.submitting') : nodeActionConfirmText }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="cacheDialogOpen"
      :title="t('admin.cluster.cacheDialog.title')"
      width="normal"
      :close-on-escape="!submitting"
      @close="closeCacheDialog"
    >
      <div class="space-y-5">
        <div class="rounded-2xl border border-primary-200 bg-primary-50 p-4 dark:border-primary-800/70 dark:bg-primary-950/30">
          <div class="flex items-start gap-3">
            <Icon name="server" size="sm" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-300" />
            <div>
              <div class="text-sm font-semibold text-content">{{ t('admin.cluster.cacheDialog.appliesToAll') }}</div>
              <div class="mt-1 text-sm leading-5 text-content-muted">{{ t('admin.cluster.cacheDialog.appliesToAllHint') }}</div>
            </div>
          </div>
        </div>

        <fieldset>
          <legend class="mb-2 text-sm font-semibold text-content">{{ t('admin.cluster.cacheDialog.scope') }}</legend>
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <label v-for="scope in cacheScopeOptions" :key="scope.value" class="selection-row">
              <input
                v-model="cacheScope"
                type="radio"
                name="cache-scope"
                class="selection-control"
                :value="scope.value"
                :disabled="submitting"
              />
              <span>
                <span class="block text-sm font-medium text-content">{{ scope.label }}</span>
                <span class="mt-0.5 block text-xs text-content-muted">{{ scope.hint }}</span>
              </span>
            </label>
          </div>
        </fieldset>

        <TextArea
          v-model="operationReason"
          :label="t('admin.cluster.dialog.reason')"
          :placeholder="t('admin.cluster.cacheDialog.reasonPlaceholder')"
          :hint="t('admin.cluster.dialog.reasonHint', { count: operationReason.trim().length })"
          :error="reasonError"
          :disabled="submitting"
          :rows="4"
          required
        />
      </div>
      <template #footer>
        <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button type="button" class="dialog-button dialog-button-secondary" :disabled="submitting" @click="closeCacheDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="dialog-button dialog-button-primary" :disabled="submitting || !cacheRequestValid" @click="submitCacheRefresh">
            {{ submitting ? t('admin.cluster.dialog.submitting') : t('admin.cluster.cacheDialog.confirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="Boolean(selectedNode)"
      :title="t('admin.cluster.details.title')"
      width="wide"
      @close="selectedNode = null"
    >
      <div v-if="selectedNode" class="space-y-5">
        <div class="flex flex-col gap-3 rounded-2xl bg-surface-subtle p-4 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <div class="break-all font-mono text-base font-bold text-content">{{ selectedNode.node_id }}</div>
            <div class="mt-1 text-sm text-content-muted">{{ selectedNode.hostname }}</div>
          </div>
          <span class="status-pill self-start" :class="statusClass(selectedNode.status)">{{ statusLabel(selectedNode.status) }}</span>
        </div>

        <div v-if="selectedNode.readiness_message" class="rounded-xl border border-line p-3 text-sm text-content-muted">
          <span class="font-semibold text-content">{{ t('admin.cluster.details.readiness') }}：</span>
          {{ selectedNode.readiness_message }}
        </div>

        <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          <MetricCell :label="t('admin.cluster.details.version')" :value="selectedNode.version || '—'" />
          <MetricCell :label="t('admin.cluster.details.commit')" :value="selectedNode.commit || '—'" mono />
          <MetricCell :label="t('admin.cluster.details.startedAt')" :value="formatDateTime(selectedNode.started_at)" />
          <MetricCell :label="t('admin.cluster.details.buildDate')" :value="formatDateTime(selectedNode.build_date)" />
          <MetricCell :label="t('admin.cluster.nodes.cpu')" :value="formatPercent(selectedNode.cpu_usage_percent)" />
          <MetricCell :label="t('admin.cluster.nodes.memory')" :value="formatMemoryUsage(selectedNode)" />
          <MetricCell :label="t('admin.cluster.details.goroutines')" :value="String(selectedNode.goroutine_count)" />
          <MetricCell :label="t('admin.cluster.details.fileDescriptors')" :value="`${selectedNode.fd_open} / ${selectedNode.fd_limit}`" />
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <MetricGroup :title="t('admin.cluster.details.gatewayConnections')">
            <MetricLine label="HTTP" :value="selectedNode.active_http" />
            <MetricLine label="SSE" :value="selectedNode.active_sse" />
            <MetricLine label="WebSocket" :value="selectedNode.active_ws" />
          </MetricGroup>
          <MetricGroup title="PostgreSQL">
            <MetricLine :label="t('admin.cluster.details.active')" :value="selectedNode.db_conn_active" />
            <MetricLine :label="t('admin.cluster.details.idle')" :value="selectedNode.db_conn_idle" />
            <MetricLine :label="t('admin.cluster.details.waiting')" :value="selectedNode.db_conn_waiting" />
            <MetricLine :label="t('admin.cluster.details.poolLimit')" :value="selectedNode.db_conn_max_open" />
          </MetricGroup>
          <MetricGroup title="Redis">
            <MetricLine :label="t('admin.cluster.details.total')" :value="selectedNode.redis_conn_total" />
            <MetricLine :label="t('admin.cluster.details.idle')" :value="selectedNode.redis_conn_idle" />
            <MetricLine :label="t('admin.cluster.details.poolLimit')" :value="selectedNode.redis_pool_size" />
          </MetricGroup>
        </div>

        <div>
          <div class="mb-2 text-sm font-semibold text-content">{{ t('admin.cluster.details.cacheVersions') }}</div>
          <div class="flex flex-wrap gap-2">
            <span
              v-for="[scope, version] in Object.entries(selectedNode.cache_versions || {})"
              :key="scope"
              class="rounded-lg border border-line bg-surface-subtle px-2.5 py-1.5 font-mono text-xs text-content-muted"
            >
              {{ cacheScopeLabel(scope) }} · v{{ version }}
            </span>
            <span v-if="Object.keys(selectedNode.cache_versions || {}).length === 0" class="text-sm text-content-subtle">—</span>
          </div>
        </div>

        <button
          type="button"
          class="dialog-button dialog-button-secondary w-full sm:w-auto"
          @click="openNodeLogs(selectedNode)"
        >
          {{ t('admin.cluster.details.viewLogs') }}
        </button>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import TextArea from '@/components/common/TextArea.vue'
import Icon from '@/components/icons/Icon.vue'
import { clusterAPI } from '@/api/admin/cluster'
import type {
  ClusterCacheScope,
  ClusterInstance,
  ClusterOperation,
  ClusterOperationStatus,
  ClusterSummary,
  ClusterTaskLease
} from '@/api/admin/cluster'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const appStore = useAppStore()
const router = useRouter()

const MetricCell = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
    mono: { type: Boolean, default: false }
  },
  setup(props) {
    return () => h('div', { class: 'min-w-0' }, [
      h('div', { class: 'text-xs text-content-subtle' }, props.label),
      h('div', {
        class: [
          'mt-1 break-words text-sm font-semibold text-content',
          props.mono ? 'font-mono' : ''
        ]
      }, props.value)
    ])
  }
})

const DependencyBadge = defineComponent({
  props: {
    ok: { type: Boolean, required: true },
    label: { type: String, required: true }
  },
  setup(props) {
    return () => h('span', {
      class: [
        'inline-flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs font-semibold',
        props.ok
          ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
          : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
      ]
    }, [
      h('span', { class: ['h-1.5 w-1.5 rounded-full', props.ok ? 'bg-emerald-500' : 'bg-red-500'] }),
      props.label
    ])
  }
})

const MetricGroup = defineComponent({
  props: { title: { type: String, required: true } },
  setup(props, { slots }) {
    return () => h('div', { class: 'rounded-2xl border border-line p-4' }, [
      h('div', { class: 'mb-3 text-sm font-semibold text-content' }, props.title),
      h('div', { class: 'space-y-2' }, slots.default?.())
    ])
  }
})

const MetricLine = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: [String, Number], required: true }
  },
  setup(props) {
    return () => h('div', { class: 'flex items-center justify-between gap-3 text-sm' }, [
      h('span', { class: 'text-content-muted' }, props.label),
      h('span', { class: 'font-mono font-semibold text-content' }, String(props.value))
    ])
  }
})

const summary = ref<ClusterSummary | null>(null)
const instances = ref<ClusterInstance[]>([])
const tasks = ref<ClusterTaskLease[]>([])
const operations = ref<ClusterOperation[]>([])
const selectedNode = ref<ClusterInstance | null>(null)
const loading = ref(false)
const hasLoaded = ref(false)
const errorMessage = ref('')
const submitting = ref(false)
const operationReason = ref('')

let refreshTimer: ReturnType<typeof setTimeout> | null = null
let refreshController: AbortController | null = null
let fetchSequence = 0

const hasActiveOperations = computed(() =>
  operations.value.some((operation) => ['pending', 'running'].includes(operation.status))
)

const summaryCards = computed(() => [
  {
    key: 'ready',
    label: t('admin.cluster.summary.ready'),
    value: summary.value?.counts.ready ?? 0,
    hint: t('admin.cluster.summary.expected', { count: summary.value?.expected_nodes ?? 0 }),
    railClass: 'bg-emerald-500'
  },
  {
    key: 'draining',
    label: t('admin.cluster.summary.draining'),
    value: summary.value?.counts.draining ?? 0,
    hint: t('admin.cluster.summary.notReceivingTraffic'),
    railClass: 'bg-amber-500'
  },
  {
    key: 'unhealthy',
    label: t('admin.cluster.summary.unhealthy'),
    value: summary.value?.counts.unhealthy ?? 0,
    hint: t('admin.cluster.summary.requiresAttention'),
    railClass: 'bg-red-500'
  },
  {
    key: 'stale',
    label: t('admin.cluster.summary.staleOffline'),
    value: (summary.value?.counts.stale ?? 0) + (summary.value?.counts.offline ?? 0),
    hint: t('admin.cluster.summary.staleOfflineHint', {
      stale: summary.value?.counts.stale ?? 0,
      offline: summary.value?.counts.offline ?? 0
    }),
    railClass: 'bg-gray-400'
  },
  {
    key: 'connections',
    label: t('admin.cluster.summary.connections'),
    value:
      (summary.value?.active_connections.http ?? 0) +
      (summary.value?.active_connections.sse ?? 0) +
      (summary.value?.active_connections.websocket ?? 0),
    hint: t('admin.cluster.summary.connectionBreakdown', {
      http: summary.value?.active_connections.http ?? 0,
      sse: summary.value?.active_connections.sse ?? 0,
      ws: summary.value?.active_connections.websocket ?? 0
    }),
    railClass: 'bg-primary-500'
  },
  {
    key: 'pools',
    label: t('admin.cluster.summary.pools'),
    value: `${summary.value?.pools.database_open ?? 0}/${summary.value?.pools.database_max ?? 0}`,
    hint: t('admin.cluster.summary.redisPool', {
      current: summary.value?.pools.redis_total ?? 0,
      max: summary.value?.pools.redis_max ?? 0
    }),
    railClass: 'bg-accent-500'
  }
])

const nodeActionDialog = reactive<{
  show: boolean
  kind: 'drain' | 'resume'
  node: ClusterInstance | null
}>({
  show: false,
  kind: 'drain',
  node: null
})

const cacheDialogOpen = ref(false)
const cacheScope = ref<ClusterCacheScope>('all_safe')

const reasonValid = computed(() => {
  const length = operationReason.value.trim().length
  return length >= 8 && length <= 500
})

const reasonError = computed(() => {
  const length = operationReason.value.trim().length
  if (length === 0) return ''
  if (length < 8) return t('admin.cluster.dialog.reasonTooShort')
  if (length > 500) return t('admin.cluster.dialog.reasonTooLong')
  return ''
})

const cacheRequestValid = computed(() => reasonValid.value)

const cacheScopeOptions = computed<Array<{ value: ClusterCacheScope; label: string; hint: string }>>(() => [
  {
    value: 'all_safe',
    label: t('admin.cluster.cacheScopes.allSafe'),
    hint: t('admin.cluster.cacheScopes.allSafeHint')
  },
  {
    value: 'channel_routing',
    label: t('admin.cluster.cacheScopes.channelRouting'),
    hint: t('admin.cluster.cacheScopes.channelRoutingHint')
  },
  {
    value: 'runtime_settings',
    label: t('admin.cluster.cacheScopes.runtimeSettings'),
    hint: t('admin.cluster.cacheScopes.runtimeSettingsHint')
  },
  {
    value: 'policy_metadata',
    label: t('admin.cluster.cacheScopes.policyMetadata'),
    hint: t('admin.cluster.cacheScopes.policyMetadataHint')
  }
])

const nodeActionTitle = computed(() =>
  nodeActionDialog.kind === 'drain'
    ? t('admin.cluster.dialog.drainTitle')
    : t('admin.cluster.dialog.resumeTitle')
)

const nodeActionDescription = computed(() =>
  nodeActionDialog.kind === 'drain'
    ? t('admin.cluster.dialog.drainDescription')
    : t('admin.cluster.dialog.resumeDescription')
)

const nodeActionConfirmText = computed(() =>
  nodeActionDialog.kind === 'drain'
    ? t('admin.cluster.dialog.confirmDrain')
    : t('admin.cluster.dialog.confirmResume')
)

function scheduleRefresh(): void {
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    void fetchData()
  }, hasActiveOperations.value ? 2000 : 10000)
}

function isCanceledRequest(error: unknown): boolean {
  return Boolean(
    error &&
    typeof error === 'object' &&
    'code' in error &&
    (error as { code?: string }).code === 'ERR_CANCELED'
  )
}

async function fetchData(): Promise<void> {
  if (refreshController) refreshController.abort()
  refreshController = new AbortController()
  const currentSequence = ++fetchSequence
  loading.value = true

  try {
    const [summaryData, instanceData, taskData, operationData] = await Promise.all([
      clusterAPI.getSummary({ signal: refreshController.signal }),
      clusterAPI.getInstances({ signal: refreshController.signal }),
      clusterAPI.getTasks({ signal: refreshController.signal }),
      clusterAPI.getOperations(50, { signal: refreshController.signal })
    ])
    if (currentSequence !== fetchSequence) return

    summary.value = summaryData
    instances.value = instanceData
    tasks.value = taskData
    operations.value = operationData
    errorMessage.value = ''

    if (selectedNode.value) {
      selectedNode.value =
        instanceData.find((node) => node.node_id === selectedNode.value?.node_id) ?? null
    }
  } catch (error) {
    if (currentSequence !== fetchSequence || isCanceledRequest(error)) return
    errorMessage.value = extractApiErrorMessage(error, t('admin.cluster.loadFailed'))
  } finally {
    if (currentSequence === fetchSequence) {
      loading.value = false
      hasLoaded.value = true
      scheduleRefresh()
    }
  }
}

function refresh(): void {
  void fetchData()
}

function canDrain(node: ClusterInstance): boolean {
  return Boolean(
    summary.value?.enabled &&
    node.ready &&
    node.desired_state === 'active' &&
    (summary.value?.counts.ready ?? 0) > 2
  )
}

function canResume(node: ClusterInstance): boolean {
  return Boolean(
    summary.value?.enabled &&
    node.desired_state === 'draining' &&
    !['offline', 'stale'].includes(node.status) &&
    node.db_ok &&
    node.redis_ok
  )
}

function openNodeAction(kind: 'drain' | 'resume', node: ClusterInstance): void {
  if ((kind === 'drain' && !canDrain(node)) || (kind === 'resume' && !canResume(node))) return
  operationReason.value = ''
  nodeActionDialog.kind = kind
  nodeActionDialog.node = node
  nodeActionDialog.show = true
}

function closeNodeAction(): void {
  if (submitting.value) return
  nodeActionDialog.show = false
  nodeActionDialog.node = null
  operationReason.value = ''
}

function openCacheDialog(): void {
  operationReason.value = ''
  cacheScope.value = 'all_safe'
  cacheDialogOpen.value = true
}

function closeCacheDialog(): void {
  if (submitting.value) return
  cacheDialogOpen.value = false
  operationReason.value = ''
}

function openNodeLogs(node: ClusterInstance): void {
  selectedNode.value = null
  void router.push({
    name: 'AdminOps',
    query: { host: node.hostname },
    hash: '#ops-system-logs'
  })
}

function newIdempotencyKey(): string {
  if (!globalThis.crypto || typeof globalThis.crypto.randomUUID !== 'function') {
    throw new Error(t('admin.cluster.errors.uuidUnavailable'))
  }
  return globalThis.crypto.randomUUID()
}

async function submitNodeAction(): Promise<void> {
  const node = nodeActionDialog.node
  if (!node || !reasonValid.value || submitting.value) return

  submitting.value = true
  try {
    const request = { reason: operationReason.value.trim() }
    if (nodeActionDialog.kind === 'drain') {
      await clusterAPI.drainInstance(node.node_id, request, newIdempotencyKey())
    } else {
      await clusterAPI.resumeInstance(node.node_id, request, newIdempotencyKey())
    }
    nodeActionDialog.show = false
    nodeActionDialog.node = null
    operationReason.value = ''
    appStore.showSuccess(t('admin.cluster.actions.operationQueued'))
    await fetchData()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.cluster.actions.operationFailed')))
  } finally {
    submitting.value = false
  }
}

async function submitCacheRefresh(): Promise<void> {
  if (!cacheRequestValid.value || submitting.value) return

  submitting.value = true
  try {
    await clusterAPI.refreshCache(
      {
        scope: cacheScope.value,
        reason: operationReason.value.trim()
      },
      newIdempotencyKey()
    )
    cacheDialogOpen.value = false
    operationReason.value = ''
    appStore.showSuccess(t('admin.cluster.actions.cacheRefreshQueued'))
    await fetchData()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.cluster.actions.operationFailed')))
  } finally {
    submitting.value = false
  }
}

function openNodeDetails(node: ClusterInstance): void {
  selectedNode.value = node
}

function statusClass(status: string): string {
  switch (status) {
    case 'ready':
    case 'succeeded':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'draining':
    case 'starting':
    case 'pending':
    case 'running':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'unhealthy':
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'stale':
    case 'offline':
      return 'bg-gray-200 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
    default:
      return 'bg-surface-subtle text-content-muted'
  }
}

function statusLabel(status: string): string {
  const key = `admin.cluster.status.${status}`
  const translated = t(key)
  return translated === key ? status : translated
}

function operationStatusClass(status: ClusterOperationStatus): string {
  return statusClass(status)
}

function operationStatusLabel(status: string): string {
  const key = `admin.cluster.operationStatus.${status}`
  const translated = t(key)
  return translated === key ? status : translated
}

function operationKindLabel(kind: string): string {
  const key = `admin.cluster.operationKind.${kind}`
  const translated = t(key)
  return translated === key ? kind : translated
}

function cacheScopeLabel(scope: string): string {
  const scopeKeys: Record<string, string> = {
    all_safe: 'allSafe',
    channel_routing: 'channelRouting',
    runtime_settings: 'runtimeSettings',
    policy_metadata: 'policyMetadata'
  }
  const key = `admin.cluster.cacheScopes.${scopeKeys[scope] ?? scope}`
  const translated = t(key)
  return translated === key ? scope : translated
}

function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).format(date)
}

function formatRelativeTime(value?: string | null): string {
  if (!value) return '—'
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return '—'
  const seconds = Math.round((timestamp - Date.now()) / 1000)
  const formatter = new Intl.RelativeTimeFormat(locale.value, { numeric: 'auto' })
  if (Math.abs(seconds) < 60) return formatter.format(seconds, 'second')
  const minutes = Math.round(seconds / 60)
  if (Math.abs(minutes) < 60) return formatter.format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return formatter.format(hours, 'hour')
  return formatter.format(Math.round(hours / 24), 'day')
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return `${value.toFixed(value >= 10 ? 0 : 1)}%`
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size.toFixed(unitIndex >= 3 ? 1 : 0)} ${units[unitIndex]}`
}

function formatMemoryUsage(node: ClusterInstance): string {
  const used = formatBytes(node.memory_used_bytes)
  if (!node.memory_limit_bytes) return used
  return `${used} / ${formatBytes(node.memory_limit_bytes)}`
}

function formatDuration(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(1)} s`
}

onMounted(() => {
  void fetchData()
})

onUnmounted(() => {
  fetchSequence += 1
  if (refreshTimer) clearTimeout(refreshTimer)
  if (refreshController) refreshController.abort()
})
</script>

<style scoped>
.status-pill {
  @apply inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold;
}

.node-action {
  @apply inline-flex min-h-11 items-center justify-center rounded-xl border px-3 text-xs font-semibold transition disabled:cursor-not-allowed disabled:opacity-40;
}

.node-action-danger {
  @apply border-red-200 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-900/50 dark:bg-red-950/20 dark:text-red-300 dark:hover:bg-red-950/40;
}

.node-action-primary {
  @apply border-primary-200 bg-primary-50 text-primary-700 hover:bg-primary-100 dark:border-primary-900/50 dark:bg-primary-950/20 dark:text-primary-300 dark:hover:bg-primary-950/40;
}

.dialog-button {
  @apply inline-flex min-h-11 items-center justify-center rounded-xl px-4 text-sm font-semibold transition disabled:cursor-not-allowed disabled:opacity-50;
}

.dialog-button-secondary {
  @apply border border-line bg-surface-elevated text-content hover:bg-surface-hover;
}

.dialog-button-primary {
  @apply bg-primary-600 text-white hover:bg-primary-700;
}

.dialog-button-danger {
  @apply bg-red-600 text-white hover:bg-red-700;
}

.selection-row {
  @apply flex min-h-11 cursor-pointer items-start gap-3 rounded-xl border border-line bg-surface-elevated p-3 transition hover:border-primary-300 hover:bg-primary-50/40 dark:hover:border-primary-800 dark:hover:bg-primary-950/20;
}

.selection-control {
  @apply mt-0.5 h-5 w-5 flex-none rounded border-line text-primary-600 focus:ring-primary-500;
}
</style>
