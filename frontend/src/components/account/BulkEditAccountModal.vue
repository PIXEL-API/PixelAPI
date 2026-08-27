<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.bulkEdit.title')"
    width="wide"
    @close="handleClose"
  >
    <form id="bulk-edit-account-form" class="space-y-5" @submit.prevent="() => handleSubmit()">
      <!-- Info -->
      <div class="rounded-lg bg-blue-50 p-4 dark:bg-blue-900/20">
        <p class="text-sm text-blue-700 dark:text-blue-400">
          <svg class="mr-1.5 inline h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          {{ t('admin.accounts.bulkEdit.selectionInfo', { count: targetMode === 'filtered' ? targetPreviewCount : accountIds.length }) }}
        </p>
      </div>

      <!-- Mixed platform warning -->
      <div v-if="isMixedPlatform" class="rounded-lg bg-amber-50 p-4 dark:bg-amber-900/20">
        <p class="text-sm text-amber-700 dark:text-amber-400">
          <svg class="mr-1.5 inline h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
          {{ t('admin.accounts.bulkEdit.mixedPlatformWarning', { platforms: targetSelectedPlatforms.join(', ') }) }}
        </p>
      </div>

      <section
        v-if="isUserScope"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="bulk-external-placement-section"
      >
        <div class="mb-4 flex items-start justify-between gap-4">
          <div>
            <label
              class="input-label mb-0"
              for="bulk-edit-external-placement-enabled"
            >
              {{ t('userAccounts.externalPlacement.bulkTitle') }}
            </label>
            <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-300">
              {{
                t(
                  'userAccounts.externalPlacement.bulkHint',
                  { count: targetPreviewCount }
                )
              }}
            </p>
          </div>
          <label
            for="bulk-edit-external-placement-enabled"
            class="inline-flex min-h-11 min-w-11 shrink-0 cursor-pointer items-center justify-center rounded-xl transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
          >
            <input
              v-model="enableExternalPlacement"
              id="bulk-edit-external-placement-enabled"
              type="checkbox"
              aria-controls="bulk-edit-external-placement"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </label>
        </div>
        <div
          id="bulk-edit-external-placement"
          :class="!enableExternalPlacement && 'pointer-events-none opacity-50'"
        >
          <ExternalPlacementSelector
            v-model="selectedExternalPlacementTarget"
            :platform="bulkPlacementPlatform"
            :disabled="submitting || !enableExternalPlacement"
            input-name="bulk-edit-external-placement-target"
            :legend="t('userAccounts.externalPlacement.bulkLegend')"
            :platform-mode-disabled-reason="bulkPlacementPlatformModeDisabledReason"
          />
        </div>
        <div
          v-if="placementSubmitError"
          class="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm leading-6 text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
          role="alert"
          data-testid="bulk-placement-submit-error"
        >
          <p>{{ placementSubmitError }}</p>
          <ul v-if="placementFailedDetails.length" class="mt-2 space-y-1" data-testid="bulk-placement-failed-details">
            <li v-for="detail in placementFailedDetails" :key="detail.accountId" class="flex gap-2">
              <span class="shrink-0 font-medium">#{{ detail.accountId }}</span>
              <span>{{ detail.reason }}</span>
            </li>
          </ul>
        </div>
      </section>

      <!-- OpenAI passthrough -->
      <div
        v-if="!isUserScope && allOpenAIPassthroughCapable"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <div class="mb-3 flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-openai-passthrough-label"
              class="input-label mb-0"
              for="bulk-edit-openai-passthrough-enabled"
            >
              {{ t('admin.accounts.openai.oauthPassthrough') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.openai.oauthPassthroughDesc') }}
            </p>
          </div>
          <input
            v-model="enableOpenAIPassthrough"
            id="bulk-edit-openai-passthrough-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-passthrough-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-passthrough-body"
          :class="!enableOpenAIPassthrough && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-openai-passthrough-label"
        >
          <button
            id="bulk-edit-openai-passthrough-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              openaiPassthroughEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="openaiPassthroughEnabled = !openaiPassthroughEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                openaiPassthroughEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <!-- Base URL (API Key only) -->
      <div v-if="canManageBaseUrl" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-base-url-label"
            class="input-label mb-0"
            for="bulk-edit-base-url-enabled"
          >
            {{ t('admin.accounts.baseUrl') }}
          </label>
          <input
            v-model="enableBaseUrl"
            id="bulk-edit-base-url-enabled"
            type="checkbox"
            aria-controls="bulk-edit-base-url"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <input
          v-model="baseUrl"
          id="bulk-edit-base-url"
          type="text"
          :disabled="!enableBaseUrl"
          class="input"
          :class="!enableBaseUrl && 'cursor-not-allowed opacity-50'"
          :placeholder="t('admin.accounts.bulkEdit.baseUrlPlaceholder')"
          aria-labelledby="bulk-edit-base-url-label"
        />
        <GrokBaseUrlPresets
          v-if="allTargetsGrok"
          class="mt-2"
          @select="baseUrl = $event; enableBaseUrl = true"
        />
        <p class="input-hint">
          {{ t('admin.accounts.bulkEdit.baseUrlNotice') }}
        </p>
      </div>

      <!-- Model restriction -->
      <div v-if="canManageModelRestriction" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-model-restriction-label"
            class="input-label mb-0"
            for="bulk-edit-model-restriction-enabled"
          >
            {{ t('admin.accounts.modelRestriction') }}
          </label>
          <input
            v-model="enableModelRestriction"
            id="bulk-edit-model-restriction-enabled"
            type="checkbox"
            aria-controls="bulk-edit-model-restriction-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>

        <div
          id="bulk-edit-model-restriction-body"
          :class="!enableModelRestriction && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-model-restriction-label"
        >
          <div
            v-if="isOpenAIModelRestrictionDisabled"
            class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
          >
            <p class="text-xs text-amber-700 dark:text-amber-400">
              {{ t('admin.accounts.openai.modelRestrictionDisabledByPassthrough') }}
            </p>
          </div>

          <template v-else>
            <!-- Mode Toggle -->
            <div v-if="!isUserScope" class="mb-4 flex gap-2">
              <button
                type="button"
                :class="[
                  'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                  modelRestrictionMode === 'whitelist'
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                ]"
                @click="modelRestrictionMode = 'whitelist'"
              >
                <svg
                  class="mr-1.5 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
                {{ t('admin.accounts.modelWhitelist') }}
              </button>
              <button
                type="button"
                :class="[
                  'flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                  modelRestrictionMode === 'mapping'
                    ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                ]"
                @click="modelRestrictionMode = 'mapping'"
              >
                <svg
                  class="mr-1.5 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
                  />
                </svg>
                {{ t('admin.accounts.modelMapping') }}
              </button>
            </div>

            <!-- Whitelist Mode -->
            <div v-if="isUserScope || modelRestrictionMode === 'whitelist'">
              <div class="mb-3 rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
                <p class="text-xs text-blue-700 dark:text-blue-400">
                  <svg
                    class="mr-1 inline h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  {{ t('admin.accounts.selectAllowedModels') }}
                </p>
              </div>

              <ModelWhitelistSelector
                v-model="allowedModels"
                :platforms="targetSelectedPlatforms"
                :allowed-options="channelPricingModels ?? []"
                :allow-custom="false"
              />

              <p
                v-if="channelPricingModelsLoading"
                class="mt-2 text-xs text-gray-500 dark:text-gray-400"
              >
                {{ t(isUserScope ? 'admin.accounts.userModelOptionsLoading' : 'admin.accounts.pricedModelsLoading') }}
              </p>
              <p
                v-else-if="channelPricingModelsLoadError"
                class="mt-2 text-xs text-red-600 dark:text-red-400"
                role="alert"
              >
                {{ t(
                  isUserScope
                    ? 'admin.accounts.userModelOptionsLoadFailed'
                    : 'admin.accounts.pricedModelsLoadFailed',
                  { message: channelPricingModelsLoadError }
                ) }}
              </p>
              <p
                v-else-if="channelPricingModels !== null && channelPricingModels.length === 0"
                class="mt-2 text-xs text-amber-600 dark:text-amber-400"
              >
                {{ t(isUserScope ? 'admin.accounts.userModelOptionsEmpty' : 'admin.accounts.pricedModelsEmpty') }}
              </p>

              <p
                v-if="isUserScope && allowedModels.length === 0"
                class="mt-2 text-xs text-amber-600 dark:text-amber-400"
              >
                {{ t('admin.accounts.userModelSelectionRequired') }}
              </p>
              <p v-else class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.selectedModels', { count: allowedModels.length }) }}
                <span v-if="!isUserScope && allowedModels.length === 0">{{
                  t('admin.accounts.supportsAllModels')
                }}</span>
              </p>
            </div>

            <!-- Mapping Mode -->
            <div v-else>
              <div class="mb-3 rounded-lg bg-purple-50 p-3 dark:bg-purple-900/20">
                <p class="text-xs text-purple-700 dark:text-purple-400">
                  <svg
                    class="mr-1 inline h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  {{ t('admin.accounts.mapRequestModels') }}
                </p>
              </div>

              <!-- Model Mapping List -->
              <div v-if="modelMappings.length > 0" class="mb-3 space-y-2">
                <div
                  v-for="(mapping, index) in modelMappings"
                  :key="index"
                  class="flex items-center gap-2"
                >
                  <input
                    v-model="mapping.from"
                    type="text"
                    class="input flex-1"
                    :placeholder="t('admin.accounts.requestModel')"
                  />
                  <svg
                    class="h-4 w-4 flex-shrink-0 text-gray-400"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M14 5l7 7m0 0l-7 7m7-7H3"
                    />
                  </svg>
                  <input
                    v-model="mapping.to"
                    type="text"
                    class="input flex-1"
                    :placeholder="t('admin.accounts.actualModel')"
                  />
                  <button
                    type="button"
                    class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                    @click="removeModelMapping(index)"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                      />
                    </svg>
                  </button>
                </div>
              </div>

              <button
                type="button"
                class="mb-3 w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
                @click="addModelMapping"
              >
                <svg
                  class="mr-1 inline h-4 w-4"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    stroke-width="2"
                    d="M12 4v16m8-8H4"
                  />
                </svg>
                {{ t('admin.accounts.addMapping') }}
              </button>

              <!-- Quick Add Buttons -->
              <div class="flex flex-wrap gap-2">
                <button
                  v-for="preset in filteredPresets"
                  :key="preset.label"
                  type="button"
                  :class="['rounded-lg px-3 py-1 text-xs transition-colors', preset.color]"
                  @click="addPresetMapping(preset.from, preset.to)"
                >
                  + {{ preset.label }}
                </button>
              </div>
            </div>
          </template>
        </div>
      </div>
      <div
        v-else-if="isUserScope && userTargetTypesSupportModelRestriction"
        class="border-t border-gray-200 pt-4 text-sm text-amber-600 dark:border-dark-600 dark:text-amber-400"
        data-testid="bulk-user-model-same-platform-hint"
      >
        {{ t('admin.accounts.userModelSamePlatformRequired') }}
      </div>

      <!-- Custom error codes -->
      <div v-if="canManageCustomErrorCodes" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <label
              id="bulk-edit-custom-error-codes-label"
              class="input-label mb-0"
              for="bulk-edit-custom-error-codes-enabled"
            >
              {{ t('admin.accounts.customErrorCodes') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.customErrorCodesHint') }}
            </p>
          </div>
          <input
            v-model="enableCustomErrorCodes"
            id="bulk-edit-custom-error-codes-enabled"
            type="checkbox"
            aria-controls="bulk-edit-custom-error-codes-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>

        <div v-if="enableCustomErrorCodes" id="bulk-edit-custom-error-codes-body" class="space-y-3">
          <div class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20">
            <p class="text-xs text-amber-700 dark:text-amber-400">
              <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.customErrorCodesWarning') }}
            </p>
          </div>

          <!-- Error Code Buttons -->
          <div class="flex flex-wrap gap-2">
            <button
              v-for="code in commonErrorCodes"
              :key="code.value"
              type="button"
              :class="[
                'rounded-lg px-3 py-1.5 text-sm font-medium transition-colors',
                selectedErrorCodes.includes(code.value)
                  ? 'bg-red-100 text-red-700 ring-1 ring-red-500 dark:bg-red-900/30 dark:text-red-400'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
              ]"
              @click="toggleErrorCode(code.value)"
            >
              {{ code.value }} {{ code.label }}
            </button>
          </div>

          <!-- Manual input -->
          <div class="flex items-center gap-2">
            <input
              v-model="customErrorCodeInput"
              id="bulk-edit-custom-error-code-input"
              type="number"
              min="100"
              max="599"
              class="input flex-1"
              :placeholder="t('admin.accounts.enterErrorCode')"
              aria-labelledby="bulk-edit-custom-error-codes-label"
              @keyup.enter="addCustomErrorCode"
            />
            <button type="button" class="btn btn-secondary px-3" @click="addCustomErrorCode">
              <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M12 4v16m8-8H4"
                />
              </svg>
            </button>
          </div>

          <!-- Selected codes summary -->
          <div class="flex flex-wrap gap-1.5">
            <span
              v-for="code in selectedErrorCodes.sort((a, b) => a - b)"
              :key="code"
              class="inline-flex items-center gap-1 rounded-full bg-red-100 px-2.5 py-0.5 text-sm font-medium text-red-700 dark:bg-red-900/30 dark:text-red-400"
            >
              {{ code }}
              <button
                type="button"
                class="hover:text-red-900 dark:hover:text-red-300"
                @click="removeErrorCode(code)"
              >
                <Icon name="x" size="xs" class="h-3.5 w-3.5" :stroke-width="2" />
              </button>
            </span>
            <span v-if="selectedErrorCodes.length === 0" class="text-xs text-gray-400">
              {{ t('admin.accounts.noneSelectedUsesDefault') }}
            </span>
          </div>
        </div>
      </div>

      <!-- Intercept warmup requests (Anthropic only) -->
      <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-intercept-warmup-label"
              class="input-label mb-0"
              for="bulk-edit-intercept-warmup-enabled"
            >
              {{ t('admin.accounts.interceptWarmupRequests') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.interceptWarmupRequestsDesc') }}
            </p>
          </div>
          <input
            v-model="enableInterceptWarmup"
            id="bulk-edit-intercept-warmup-enabled"
            type="checkbox"
            aria-controls="bulk-edit-intercept-warmup-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div v-if="enableInterceptWarmup" id="bulk-edit-intercept-warmup-body" class="mt-3">
          <button
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              interceptWarmupRequests ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="interceptWarmupRequests = !interceptWarmupRequests"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                interceptWarmupRequests ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div
        v-if="allHeaderOverrideCapable"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="bulk-grok-header-override"
      >
        <div class="flex items-center justify-between">
          <div class="flex-1 pr-4">
            <label
              id="bulk-edit-header-override-label"
              class="input-label mb-0"
              for="bulk-edit-header-override-enabled"
            >
              {{ t('admin.accounts.headerOverride.title') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.headerOverride.hint') }}
            </p>
          </div>
          <input
            v-model="enableHeaderOverride"
            id="bulk-edit-header-override-enabled"
            type="checkbox"
            aria-controls="bulk-edit-header-override-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          v-if="enableHeaderOverride"
          id="bulk-edit-header-override-body"
          class="mt-3 space-y-3"
        >
          <button
            type="button"
            role="switch"
            :aria-checked="headerOverrideEnabled"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              headerOverrideEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="headerOverrideEnabled = !headerOverrideEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                headerOverrideEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
          <template v-if="headerOverrideEnabled">
            <p class="rounded-lg bg-blue-50 p-3 text-xs text-blue-700 dark:bg-blue-900/20 dark:text-blue-400">
              {{ t('admin.accounts.headerOverride.info') }}
            </p>
            <p class="text-xs text-amber-600 dark:text-amber-400">
              {{ t('admin.accounts.headerOverride.bulkReplaceHint') }}
            </p>
            <HeaderOverrideEditor
              :rows="headerOverrideRows"
              @update:rows="headerOverrideRows = $event"
            />
          </template>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.headerOverride.bulkDisableHint') }}
          </p>
        </div>
      </div>

      <!-- Proxy -->
      <div v-if="canManageProxy" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-proxy-label"
            class="input-label mb-0"
            for="bulk-edit-proxy-enabled"
          >
            {{ t('admin.accounts.proxy') }}
          </label>
          <input
            v-model="enableProxy"
            id="bulk-edit-proxy-enabled"
            type="checkbox"
            aria-controls="bulk-edit-proxy-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-proxy-body" :class="!enableProxy && 'pointer-events-none opacity-50'">
          <ProxySelector
            v-model="proxyId"
            :proxies="proxies"
            aria-labelledby="bulk-edit-proxy-label"
          />
        </div>
      </div>

      <!-- Scheduling and account settings -->
      <div class="grid grid-cols-2 gap-4 border-t border-gray-200 pt-4 dark:border-dark-600 lg:grid-cols-4">
        <div v-if="canManageAccountLevel">
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-account-level-label"
              class="input-label mb-0"
              for="bulk-edit-account-level-enabled"
            >
              {{ t('admin.accounts.accountLevel.label') }}
            </label>
            <input
              v-model="enableAccountLevel"
              id="bulk-edit-account-level-enabled"
              type="checkbox"
              aria-controls="bulk-edit-account-level"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <Select
            v-model="accountLevel"
            id="bulk-edit-account-level"
            :options="accountLevelOptions"
            :disabled="!enableAccountLevel"
            :class="!enableAccountLevel && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-account-level-label"
          />
          <p class="input-hint">{{ t('admin.accounts.accountLevel.manualHint') }}</p>
        </div>
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-concurrency-label"
              class="input-label mb-0"
              for="bulk-edit-concurrency-enabled"
            >
              {{ t('admin.accounts.concurrency') }}
            </label>
            <input
              v-model="enableConcurrency"
              id="bulk-edit-concurrency-enabled"
              type="checkbox"
              aria-controls="bulk-edit-concurrency"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="concurrency"
            id="bulk-edit-concurrency"
            type="number"
            :min="isUserScope ? PERSONAL_ACCOUNT_MIN_CONCURRENCY : 1"
            :max="isUserScope ? PERSONAL_ACCOUNT_MAX_CONCURRENCY : undefined"
            step="1"
            :disabled="!enableConcurrency"
            class="input"
            :class="!enableConcurrency && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-concurrency-label"
            @blur="normalizeConcurrencyInput"
          />
        </div>
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-load-factor-label"
              class="input-label mb-0"
              for="bulk-edit-load-factor-enabled"
            >
              {{ t('admin.accounts.loadFactor') }}
            </label>
            <input
              v-model="enableLoadFactor"
              id="bulk-edit-load-factor-enabled"
              type="checkbox"
              aria-controls="bulk-edit-load-factor"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="loadFactor"
            id="bulk-edit-load-factor"
            type="number"
            :min="isUserScope ? PERSONAL_ACCOUNT_MIN_LOAD_FACTOR : 1"
            :max="isUserScope ? PERSONAL_ACCOUNT_MAX_LOAD_FACTOR : undefined"
            step="1"
            :disabled="!enableLoadFactor"
            class="input"
            :class="!enableLoadFactor && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-load-factor-label"
            @input="normalizeLoadFactorInput"
          />
          <p class="input-hint">
            {{ isUserScope ? t('userAccounts.bulkLoadFactorCreditsHint') : t('admin.accounts.loadFactorHint') }}
          </p>
        </div>
        <div>
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-priority-label"
              class="input-label mb-0"
              for="bulk-edit-priority-enabled"
            >
              {{ t('admin.accounts.priority') }}
            </label>
            <input
              v-model="enablePriority"
              id="bulk-edit-priority-enabled"
              type="checkbox"
              aria-controls="bulk-edit-priority"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="priority"
            id="bulk-edit-priority"
            type="number"
            min="1"
            :disabled="!enablePriority"
            class="input"
            :class="!enablePriority && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-priority-label"
          />
        </div>
        <div v-if="canManageBillingRate">
          <div class="mb-3 flex items-center justify-between">
            <label
              id="bulk-edit-rate-multiplier-label"
              class="input-label mb-0"
              for="bulk-edit-rate-multiplier-enabled"
            >
              {{ t('admin.accounts.billingRateMultiplier') }}
            </label>
            <input
              v-model="enableRateMultiplier"
              id="bulk-edit-rate-multiplier-enabled"
              type="checkbox"
              aria-controls="bulk-edit-rate-multiplier"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
          </div>
          <input
            v-model.number="rateMultiplier"
            id="bulk-edit-rate-multiplier"
            type="number"
            min="0"
            step="0.01"
            :disabled="!enableRateMultiplier"
            class="input"
            :class="!enableRateMultiplier && 'cursor-not-allowed opacity-50'"
            aria-labelledby="bulk-edit-rate-multiplier-label"
          />
          <p class="input-hint">{{ t('admin.accounts.billingRateMultiplierHint') }}</p>
        </div>
      </div>

      <!-- Status -->
      <div v-if="!isUserScope" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-status-label"
            class="input-label mb-0"
            for="bulk-edit-status-enabled"
          >
            {{ t('common.status') }}
          </label>
          <input
            v-model="enableStatus"
            id="bulk-edit-status-enabled"
            type="checkbox"
            aria-controls="bulk-edit-status"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-status" :class="!enableStatus && 'pointer-events-none opacity-50'">
          <Select
            v-model="status"
            :options="statusOptions"
            aria-labelledby="bulk-edit-status-label"
          />
        </div>
      </div>

      <!-- OpenAI OAuth WS mode -->
      <div v-if="!isUserScope && allOpenAIOAuth" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-ws-mode-label"
            class="input-label mb-0"
            for="bulk-edit-openai-ws-mode-enabled"
          >
            {{ t('admin.accounts.openai.wsMode') }}
          </label>
          <input
            v-model="enableOpenAIWSMode"
            id="bulk-edit-openai-ws-mode-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-ws-mode"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-ws-mode"
          :class="!enableOpenAIWSMode && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.wsModeDesc') }}
          </p>
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(openAIWSModeConcurrencyHintKey) }}
          </p>
          <Select
            v-model="openaiOAuthResponsesWebSocketV2Mode"
            data-testid="bulk-edit-openai-ws-mode-select"
            :options="openAIWSModeOptions"
            aria-labelledby="bulk-edit-openai-ws-mode-label"
          />
        </div>
      </div>

      <!-- OpenAI OAuth Codex CLI only -->
      <div v-if="!isUserScope && allOpenAIOAuth" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-codex-cli-only-label"
            class="input-label mb-0"
            for="bulk-edit-openai-codex-cli-only-enabled"
          >
            {{ t('admin.accounts.openai.codexCLIOnly') }}
          </label>
          <input
            v-model="enableCodexCLIOnly"
            id="bulk-edit-openai-codex-cli-only-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-codex-cli-only"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-codex-cli-only"
          :class="!enableCodexCLIOnly && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexCLIOnlyDesc') }}
          </p>
          <button
            id="bulk-edit-openai-codex-cli-only-toggle"
            type="button"
            :class="[
              'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              codexCLIOnlyEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
            @click="codexCLIOnlyEnabled = !codexCLIOnlyEnabled"
          >
            <span
              :class="[
                'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                codexCLIOnlyEnabled ? 'translate-x-5' : 'translate-x-0'
              ]"
            />
          </button>
        </div>
      </div>

      <div v-if="!isUserScope && allOpenAIOAuth" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label class="input-label mb-0">{{ t('admin.accounts.openai.codexFingerprintMode') }}</label>
          <input
            v-model="enableCodexFingerprintMode"
            type="checkbox"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div :class="!enableCodexFingerprintMode && 'pointer-events-none opacity-50'">
          <p class="mb-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexFingerprintModeDesc') }}
          </p>
          <Select
            v-model="codexFingerprintMode"
            data-testid="bulk-codex-fingerprint-mode-select"
            :options="codexFingerprintModeOptions"
          />
        </div>
      </div>

      <div v-if="allOpenAIOAuth" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-codex-quota-limit-label"
            class="input-label mb-0"
            for="bulk-edit-openai-codex-quota-limit-enabled"
          >
            {{ t('admin.accounts.openai.codexQuotaLimit') }}
          </label>
          <input
            v-model="enableCodexQuotaLimit"
            id="bulk-edit-openai-codex-quota-limit-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-codex-quota-limit"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-codex-quota-limit"
          :class="!enableCodexQuotaLimit && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.codexQuotaLimitDesc') }}
          </p>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.openai.codex5hLimitPercent') }}</label>
              <input
                v-model.number="bulkCodex5hLimitPercent"
                type="number"
                min="1"
                max="100"
                step="0.1"
                class="input"
              />
            </div>
            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.openai.codex7dLimitPercent') }}</label>
              <input
                v-model.number="bulkCodex7dLimitPercent"
                type="number"
                min="1"
                max="100"
                step="0.1"
                class="input"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- OpenAI API Key WS mode -->
      <div v-if="!isUserScope && allOpenAIAPIKey" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-openai-apikey-ws-mode-label"
            class="input-label mb-0"
            for="bulk-edit-openai-apikey-ws-mode-enabled"
          >
            {{ t('admin.accounts.openai.wsMode') }}
          </label>
          <input
            v-model="enableOpenAIAPIKeyWSMode"
            id="bulk-edit-openai-apikey-ws-mode-enabled"
            type="checkbox"
            aria-controls="bulk-edit-openai-apikey-ws-mode"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div
          id="bulk-edit-openai-apikey-ws-mode"
          :class="!enableOpenAIAPIKeyWSMode && 'pointer-events-none opacity-50'"
        >
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.wsModeDesc') }}
          </p>
          <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(openAIAPIKeyWSModeConcurrencyHintKey) }}
          </p>
          <Select
            v-model="openaiAPIKeyResponsesWebSocketV2Mode"
            data-testid="bulk-edit-openai-apikey-ws-mode-select"
            :options="openAIWSModeOptions"
            aria-labelledby="bulk-edit-openai-apikey-ws-mode-label"
          />
        </div>
      </div>

      <!-- RPM Limit (仅全部为 Anthropic OAuth/SetupToken 时显示) -->
      <div v-if="!isUserScope && allAnthropicOAuthOrSetupToken" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-rpm-limit-label"
            class="input-label mb-0"
            for="bulk-edit-rpm-limit-enabled"
          >
            {{ t('admin.accounts.quotaControl.rpmLimit.label') }}
          </label>
          <input
            v-model="enableRpmLimit"
            id="bulk-edit-rpm-limit-enabled"
            type="checkbox"
            aria-controls="bulk-edit-rpm-limit-body"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>

        <div
          id="bulk-edit-rpm-limit-body"
          :class="!enableRpmLimit && 'pointer-events-none opacity-50'"
          role="group"
          aria-labelledby="bulk-edit-rpm-limit-label"
        >
          <div class="mb-3 flex items-center justify-between">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.quotaControl.rpmLimit.hint') }}</span>
            <button
              type="button"
              @click="rpmLimitEnabled = !rpmLimitEnabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
                rpmLimitEnabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  rpmLimitEnabled ? 'translate-x-5' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="rpmLimitEnabled" class="space-y-3">
            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpm') }}</label>
              <input
                v-model.number="bulkBaseRpm"
                type="number"
                min="1"
                max="1000"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.baseRpmPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpmHint') }}</p>
            </div>

            <div>
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.strategy') }}</label>
              <div class="flex gap-2">
                <button
                  type="button"
                  @click="bulkRpmStrategy = 'tiered'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    bulkRpmStrategy === 'tiered'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  {{ t('admin.accounts.quotaControl.rpmLimit.strategyTiered') }}
                </button>
                <button
                  type="button"
                  @click="bulkRpmStrategy = 'sticky_exempt'"
                  :class="[
                    'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
                    bulkRpmStrategy === 'sticky_exempt'
                      ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
                  ]"
                >
                  {{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExempt') }}
                </button>
              </div>
            </div>

            <div v-if="bulkRpmStrategy === 'tiered'">
              <label class="input-label text-xs">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBuffer') }}</label>
              <input
                v-model.number="bulkRpmStickyBuffer"
                type="number"
                min="1"
                step="1"
                class="input"
                :placeholder="t('admin.accounts.quotaControl.rpmLimit.stickyBufferPlaceholder')"
              />
              <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBufferHint') }}</p>
            </div>

            </div>
          </div>

        <!-- 用户消息限速模式（独立于 RPM 开关，始终可见） -->
        <div class="mt-4">
          <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueue') }}</label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 mb-2">
            {{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueueHint') }}
          </p>
          <div class="flex space-x-2">
            <button type="button" v-for="opt in umqModeOptions" :key="opt.value"
              @click="userMsgQueueMode = userMsgQueueMode === opt.value ? null : opt.value"
              :class="[
                'px-3 py-1.5 text-sm rounded-md border transition-colors',
                userMsgQueueMode === opt.value
                  ? 'bg-primary-600 text-white border-primary-600'
                  : 'bg-white dark:bg-dark-700 text-gray-700 dark:text-gray-300 border-gray-300 dark:border-dark-500 hover:bg-gray-50 dark:hover:bg-dark-600'
              ]">
              {{ opt.label }}
            </button>
          </div>
        </div>
      </div>

      <!-- Groups -->
      <div v-if="canManageGroups" class="border-t border-gray-200 pt-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <label
            id="bulk-edit-groups-label"
            class="input-label mb-0"
            for="bulk-edit-groups-enabled"
          >
            {{ t('nav.groups') }}
          </label>
          <input
            v-model="enableGroups"
            id="bulk-edit-groups-enabled"
            type="checkbox"
            aria-controls="bulk-edit-groups"
            class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          />
        </div>
        <div id="bulk-edit-groups" :class="!enableGroups && 'pointer-events-none opacity-50'">
          <GroupSelector
            v-model="groupIds"
            :groups="bulkEditableGroups"
            aria-labelledby="bulk-edit-groups-label"
          />
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="bulk-edit-account-form"
          :disabled="submitting"
          class="btn btn-primary"
        >
          <svg
            v-if="submitting"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            />
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
          {{
            submitting ? t('admin.accounts.bulkEdit.updating') : t('admin.accounts.bulkEdit.submit')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showMixedChannelWarning"
    :title="t('admin.accounts.mixedChannelWarningTitle')"
    :message="mixedChannelWarningMessage"
    :confirm-text="t('common.confirm')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="handleMixedChannelConfirm"
    @cancel="handleMixedChannelCancel"
  />

  <!-- 批量修改命中共享投放中的账号：需要理由与二次确认 -->
  <BaseDialog
    :show="showPlacementForceDialog"
    :title="t('admin.accounts.placementGuard.forceTitle')"
    width="normal"
    @close="clearPlacementGuardDialog"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.placementGuard.bulkForceMessage', { fields: placementGuardFieldLabels }) }}
      </p>
      <p class="text-sm text-amber-600 dark:text-amber-400">
        {{ t('admin.accounts.placementGuard.forceScopeHint') }}
      </p>
      <div>
        <label class="input-label" for="bulk-placement-force-reason">
          {{ t('admin.accounts.placementGuard.reasonLabel') }}
        </label>
        <textarea
          id="bulk-placement-force-reason"
          v-model="placementForceReason"
          rows="3"
          class="input"
          :placeholder="t('admin.accounts.placementGuard.reasonPlaceholder')"
        ></textarea>
        <p class="input-hint mt-1">{{ t('admin.accounts.placementGuard.reasonHint') }}</p>
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="clearPlacementGuardDialog">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-danger"
          :disabled="placementForceConfirmDisabled || submitting"
          @click="handlePlacementForceConfirm"
        >
          {{ t('admin.accounts.placementGuard.forceConfirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import { accountsAPI } from '@/api/accounts'
import type { AccountBatchTask } from '@/api/accounts'
import type {
  Proxy as ProxyConfig,
  AdminGroup,
  AccountExternalPlacementTarget,
  AccountPlatform,
  AccountType,
  AccountLevel,
  GroupPlatform
} from '@/types'
import type { AccountApiScope } from '@/composables/useAccountOAuth'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ExternalPlacementSelector from '@/components/account-share/ExternalPlacementSelector.vue'
import Select from '@/components/common/Select.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import GrokBaseUrlPresets from '@/components/account/GrokBaseUrlPresets.vue'
import HeaderOverrideEditor from '@/components/account/HeaderOverrideEditor.vue'
import {
  buildHeaderOverridesObject,
  isHeaderOverrideCapable,
  validateHeaderOverrideRows,
  HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY,
  HEADER_OVERRIDES_CREDENTIAL_KEY,
  type HeaderOverrideRow
} from '@/components/account/credentialsBuilder'
import {
  PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY,
  PERSONAL_ACCOUNT_DEFAULT_LOAD_FACTOR,
  PERSONAL_ACCOUNT_DEFAULT_PRIORITY,
  PERSONAL_ACCOUNT_MAX_LOAD_FACTOR,
  PERSONAL_ACCOUNT_MAX_CONCURRENCY,
  PERSONAL_ACCOUNT_MIN_LOAD_FACTOR,
  PERSONAL_ACCOUNT_MIN_CONCURRENCY,
  normalizePersonalAccountConcurrency,
  normalizePersonalAccountLoadFactor
} from '@/components/account/personalAccountTemplate'
import {
  buildModelMappingObject as buildModelMappingPayload,
  getPresetMappingsByPlatform
} from '@/composables/useModelWhitelist'
import {
  OPENAI_WS_MODE_CTX_POOL,
  OPENAI_WS_MODE_OFF,
  OPENAI_WS_MODE_PASSTHROUGH,
  isOpenAIWSModeEnabled,
  resolveOpenAIWSModeConcurrencyHintKey
} from '@/utils/openaiWsMode'
import { openAIAccountLevelOptions } from '@/utils/openaiAccountLevels'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import {
  extractAccountMutationVersionChallenge,
  isConfirmedAccountMutationPayload
} from '@/utils/accountMutationGuard'
import type { OpenAIWSMode } from '@/utils/openaiWsMode'
interface Props {
  show: boolean
  accountIds: number[]
  selectedPlatforms: AccountPlatform[]
  selectedTypes: AccountType[]
  selectedAccountLevels?: AccountLevel[]
  target?: {
    mode: 'selected' | 'filtered'
    filters?: Record<string, unknown>
    previewCount?: number
    selectedPlatforms?: AccountPlatform[]
    selectedTypes?: AccountType[]
  }
  proxies: ProxyConfig[]
  groups: AdminGroup[]
  accountScope?: AccountApiScope
  allowProxy?: boolean
  allowBillingRate?: boolean
  allowBaseUrl?: boolean
  ownerUserId?: number
}

const props = withDefaults(defineProps<Props>(), {
  accountScope: 'admin',
  allowProxy: true,
  allowBillingRate: true,
  allowBaseUrl: true,
  selectedAccountLevels: () => [],
  ownerUserId: 0
})
const emit = defineEmits<{
  close: []
  updated: [payload?: { async?: boolean; task?: AccountBatchTask }]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const accountScope = computed(() => props.accountScope ?? 'admin')
const isUserScope = computed(() => accountScope.value === 'user')
const canManageProxy = computed(() => !isUserScope.value && props.allowProxy !== false)
const canManageBillingRate = computed(() => !isUserScope.value && props.allowBillingRate !== false)
const canManageBaseUrl = computed(() => !isUserScope.value && props.allowBaseUrl !== false)

// Platform awareness
const targetMode = computed(() => props.target?.mode ?? 'selected')
const targetPreviewCount = computed(() => props.target?.previewCount ?? props.accountIds.length)
const targetSelectedPlatforms = computed(() => props.target?.selectedPlatforms ?? props.selectedPlatforms)
const targetSelectedTypes = computed(() => props.target?.selectedTypes ?? props.selectedTypes)
const allTargetsGrok = computed(
  () =>
    !isUserScope.value &&
    targetSelectedPlatforms.value.length > 0 &&
    targetSelectedPlatforms.value.every(platform => platform === 'grok') &&
    targetSelectedTypes.value.length === 1
)
const isMixedPlatform = computed(() => targetSelectedPlatforms.value.length > 1)
const bulkPlacementPlatform = computed<AccountPlatform | ''>(() => (
  targetSelectedPlatforms.value.length === 1 ? targetSelectedPlatforms.value[0] : ''
))
const bulkPlacementPlatformModeDisabledReason = computed(() => {
  if (targetSelectedPlatforms.value.length !== 1) {
    return t('userAccounts.externalPlacement.bulkRoomSamePlatform')
  }
  if (
    bulkPlacementPlatform.value !== 'openai'
    && bulkPlacementPlatform.value !== 'anthropic'
  ) {
    return t('userAccounts.externalPlacement.unsupportedPlatform')
  }
  return ''
})
const targetFilterPlatform = computed(() => {
  const platform = props.target?.filters?.platform
  return typeof platform === 'string' ? platform : ''
})
const targetIsKnownOpenAIOnly = computed(() =>
  targetMode.value === 'filtered'
    ? targetFilterPlatform.value === 'openai'
    : targetSelectedPlatforms.value.length === 1 && targetSelectedPlatforms.value[0] === 'openai'
)
const bulkGroupPlatform = computed<GroupPlatform | undefined>(() => {
  if (targetSelectedPlatforms.value.length !== 1) return undefined
  return targetSelectedPlatforms.value[0] as GroupPlatform
})
const selectedTypesAllowOAuthOnlyGroups = computed(
  () =>
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(type => type === 'oauth' || type === 'setup-token')
)
const canManageGroups = computed(() => !isUserScope.value)
const bulkEditableGroups = computed<AdminGroup[]>(() => {
  if (!isUserScope.value) {
    return props.groups
  }
  const platform = bulkGroupPlatform.value
  if (!platform) {
    return []
  }
  return props.groups.filter((group) => {
    if (group.status !== 'active' || group.platform !== platform) {
      return false
    }
    return !group.require_oauth_only || selectedTypesAllowOAuthOnlyGroups.value
  })
})
const userTargetTypesSupportModelRestriction = computed(
  () => targetSelectedTypes.value.length > 0 && (
    targetSelectedTypes.value.every(type => type === 'oauth' || type === 'setup-token') ||
    (
      targetSelectedPlatforms.value.length === 1 &&
      targetSelectedPlatforms.value[0] === 'opencode' &&
      targetSelectedTypes.value.every(type => type === 'apikey')
    )
  )
)
const canManageModelRestriction = computed(
  () => !isUserScope.value || (
    userTargetTypesSupportModelRestriction.value &&
    targetSelectedPlatforms.value.length === 1
  )
)
const canManageCustomErrorCodes = computed(() => !isUserScope.value)
const canManageAccountLevel = computed(() => !isUserScope.value && targetIsKnownOpenAIOnly.value)

const allOpenAIPassthroughCapable = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'oauth' || t === 'apikey')
  )
})

const allOpenAIOAuth = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'oauth')
  )
})

const allOpenAIAPIKey = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'openai' &&
    targetSelectedTypes.value.length > 0 &&
    targetSelectedTypes.value.every(t => t === 'apikey')
  )
})

const allHeaderOverrideCapable = computed(
  () =>
    allTargetsGrok.value &&
    targetSelectedTypes.value.every(type => isHeaderOverrideCapable('grok', type))
)

// 是否全部为 Anthropic OAuth/SetupToken（RPM 配置仅在此条件下显示）
const allAnthropicOAuthOrSetupToken = computed(() => {
  return (
    targetSelectedPlatforms.value.length === 1 &&
    targetSelectedPlatforms.value[0] === 'anthropic' &&
    targetSelectedTypes.value.every(t => t === 'oauth' || t === 'setup-token')
  )
})

const filteredPresets = computed(() => {
  if (targetSelectedPlatforms.value.length === 0) return []

  const dedupedPresets = new Map<string, ReturnType<typeof getPresetMappingsByPlatform>[number]>()
  for (const platform of targetSelectedPlatforms.value) {
    for (const preset of getPresetMappingsByPlatform(platform)) {
      const key = `${preset.from}=>${preset.to}`
      if (!dedupedPresets.has(key)) {
        dedupedPresets.set(key, preset)
      }
    }
  }

  return Array.from(dedupedPresets.values())
})

// Model mapping type
interface ModelMapping {
  from: string
  to: string
}

// State - field enable flags
const enableBaseUrl = ref(false)
const enableModelRestriction = ref(false)
const enableCustomErrorCodes = ref(false)
const enableInterceptWarmup = ref(false)
const enableHeaderOverride = ref(false)
const enableProxy = ref(false)
const enableConcurrency = ref(false)
const enableLoadFactor = ref(false)
const enablePriority = ref(false)
const enableAccountLevel = ref(false)
const enableRateMultiplier = ref(false)
const enableStatus = ref(false)
const enableGroups = ref(false)
const enableOpenAIPassthrough = ref(false)
const enableOpenAIWSMode = ref(false)
const enableOpenAIAPIKeyWSMode = ref(false)
const enableCodexCLIOnly = ref(false)
const enableCodexFingerprintMode = ref(false)
const enableCodexQuotaLimit = ref(false)
const enableRpmLimit = ref(false)
const enableExternalPlacement = ref(false)

// State - field values
const submitting = ref(false)
const selectedExternalPlacementTarget = ref<AccountExternalPlacementTarget>('private')
const placementSubmitError = ref('')
// 批量模式切换失败的账号明细（账号 id → 中文原因），在错误区逐条列出。
// 后端批量上限 1000，明细最多展示前几条，避免超长弹窗。
const BULK_PLACEMENT_FAILURE_DETAIL_MAX = 5
const placementFailedDetails = ref<{ accountId: number; reason: string }[]>([])
let pendingPlacementIntentSignature = ''
let pendingPlacementIdempotencyKey = ''
const showMixedChannelWarning = ref(false)
const mixedChannelWarningMessage = ref('')
const pendingUpdatesForConfirm = ref<Record<string, unknown> | null>(null)

// 投放守卫：批量修改里若包含正在共享投放的账号，后端会要求管理员填理由并二次确认。
const showPlacementForceDialog = ref(false)
const placementForceReason = ref('')
const placementGuardFields = ref<string[]>([])
const placementExpectedVersions = ref<Record<string, number> | null>(null)

const parsePlacementGuardFields = (metadata: unknown): string[] => {
  if (!metadata || typeof metadata !== 'object') return []
  const raw = (metadata as Record<string, unknown>).changed_fields
  if (typeof raw !== 'string') return []
  return raw.split(',').map((field) => field.trim()).filter(Boolean)
}

const formatPlacementGuardFields = (fields: string[]) =>
  fields
    .map((field) => {
      const key = `admin.accounts.placementGuard.fields.${field}`
      const label = t(key)
      return label === key ? field : label
    })
    .join('、')

const placementGuardFieldLabels = computed(() => formatPlacementGuardFields(placementGuardFields.value))
const placementForceConfirmDisabled = computed(() => placementForceReason.value.trim().length === 0)

const baseUrl = ref('')
const modelRestrictionMode = ref<'whitelist' | 'mapping'>('whitelist')
const allowedModels = ref<string[]>([])
const channelPricingModels = ref<string[] | null>(null)
const channelPricingModelsLoading = ref(false)
const channelPricingModelsLoadError = ref('')
let channelPricingRequestVersion = 0

const loadChannelPricingModels = async () => {
  const requestVersion = ++channelPricingRequestVersion
  channelPricingModelsLoadError.value = ''

  const selectedPlatforms = targetSelectedPlatforms.value
  if (!props.show || selectedPlatforms.length === 0 || (isUserScope.value && selectedPlatforms.length !== 1)) {
    channelPricingModels.value = null
    channelPricingModelsLoading.value = false
    return
  }

  channelPricingModelsLoading.value = true
  channelPricingModels.value = null
  try {
    const result = isUserScope.value
      ? await accountsAPI.getModelOptions(selectedPlatforms[0])
      : await adminAPI.channels.getPricedModelOptions(selectedPlatforms)
    if (requestVersion !== channelPricingRequestVersion) return
    channelPricingModels.value = Array.from(
      new Set(
        (result.models || [])
          .map(model => model.trim())
          .filter(Boolean)
      )
    )
  } catch (error: any) {
    if (requestVersion !== channelPricingRequestVersion) return
    channelPricingModels.value = []
    channelPricingModelsLoadError.value = error?.message || t(
      isUserScope.value
        ? 'admin.accounts.userModelOptionsLoadFailedDefault'
        : 'admin.accounts.pricedModelsLoadFailedDefault'
    )
    appStore.showError(
      t(
        isUserScope.value
          ? 'admin.accounts.userModelOptionsLoadFailed'
          : 'admin.accounts.pricedModelsLoadFailed',
        { message: channelPricingModelsLoadError.value }
      )
    )
  } finally {
    if (requestVersion === channelPricingRequestVersion) {
      channelPricingModelsLoading.value = false
    }
  }
}

watch(
  [() => props.show, isUserScope, targetMode, targetSelectedPlatforms],
  () => {
    void loadChannelPricingModels()
  },
  { immediate: true }
)

const modelMappings = ref<ModelMapping[]>([])
const selectedErrorCodes = ref<number[]>([])
const customErrorCodeInput = ref<number | null>(null)
const interceptWarmupRequests = ref(false)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const proxyId = ref<number | null>(null)
const concurrency = ref(1)
const loadFactor = ref<number | null>(null)
const priority = ref(1)
const accountLevel = ref<AccountLevel>('unknown')
const rateMultiplier = ref(1)
const status = ref<'active' | 'inactive' | 'disabled'>('active')
const groupIds = ref<number[]>([])
const openaiPassthroughEnabled = ref(false)
const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const openaiAPIKeyResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
const codexCLIOnlyEnabled = ref(false)
type CodexFingerprintMode = 'off' | 'device' | 'session' | 'full'
const codexFingerprintMode = ref<CodexFingerprintMode>('session')
const codexFingerprintModeOptions = computed(() => [
  { value: 'off' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintOff') },
  { value: 'device' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintDevice') },
  { value: 'session' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintSession') },
  { value: 'full' as CodexFingerprintMode, label: t('admin.accounts.openai.codexFingerprintFull') }
])
const CODEX_QUOTA_DEFAULT_LIMIT_PERCENT = 100
const bulkCodex5hLimitPercent = ref(CODEX_QUOTA_DEFAULT_LIMIT_PERCENT)
const bulkCodex7dLimitPercent = ref(CODEX_QUOTA_DEFAULT_LIMIT_PERCENT)
const rpmLimitEnabled = ref(false)
const bulkBaseRpm = ref<number | null>(null)
const bulkRpmStrategy = ref<'tiered' | 'sticky_exempt'>('tiered')
const bulkRpmStickyBuffer = ref<number | null>(null)
const userMsgQueueMode = ref<string | null>(null)
const umqModeOptions = computed(() => [
  { value: '', label: t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
  { value: 'throttle', label: t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
  { value: 'serialize', label: t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') },
])
const placementIntentSignature = computed(() => selectedExternalPlacementTarget.value)

watch(
  selectedExternalPlacementTarget,
  () => {
    placementSubmitError.value = ''
    placementFailedDetails.value = []
    pendingPlacementIntentSignature = ''
    pendingPlacementIdempotencyKey = ''
  }
)

const getPendingPlacementIdempotencyKey = (): string => {
  const signature = placementIntentSignature.value
  if (
    pendingPlacementIdempotencyKey
    && pendingPlacementIntentSignature === signature
  ) {
    return pendingPlacementIdempotencyKey
  }
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error(t('userAccounts.externalPlacement.uuidUnavailable'))
  }
  pendingPlacementIntentSignature = signature
  pendingPlacementIdempotencyKey = `batch-placement-${requestID}`
  return pendingPlacementIdempotencyKey
}

const normalizeConcurrencyInput = () => {
  if (isUserScope.value) {
    concurrency.value = normalizePersonalAccountConcurrency(concurrency.value)
    return
  }
  concurrency.value = Math.max(1, concurrency.value || 1)
}

const normalizeLoadFactorInput = () => {
  if (isUserScope.value) {
    loadFactor.value = normalizePersonalAccountLoadFactor(loadFactor.value)
    return
  }
  loadFactor.value = loadFactor.value && loadFactor.value >= 1 ? loadFactor.value : null
}

// Common HTTP error codes
const commonErrorCodes = [
  { value: 401, label: 'Unauthorized' },
  { value: 403, label: 'Forbidden' },
  { value: 429, label: 'Rate Limit' },
  { value: 500, label: 'Server Error' },
  { value: 502, label: 'Bad Gateway' },
  { value: 503, label: 'Unavailable' },
  { value: 529, label: 'Overloaded' }
]

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: isUserScope.value ? 'disabled' : 'inactive', label: t('common.inactive') }
])
const accountLevelOptions = computed(() =>
  openAIAccountLevelOptions(adminSettingsStore.openAIAccountLevels, {
    includeUnknown: true,
    unknownLabel: t('admin.accounts.accountLevel.unknown')
  })
)
const isOpenAIModelRestrictionDisabled = computed(
  () =>
    allOpenAIPassthroughCapable.value &&
    enableOpenAIPassthrough.value &&
    openaiPassthroughEnabled.value
)

const openAIWSModeOptions = computed(() => [
  { value: OPENAI_WS_MODE_OFF, label: t('admin.accounts.openai.wsModeOff') },
  { value: OPENAI_WS_MODE_CTX_POOL, label: t('admin.accounts.openai.wsModeCtxPool') },
  { value: OPENAI_WS_MODE_PASSTHROUGH, label: t('admin.accounts.openai.wsModePassthrough') }
])
const openAIWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openaiOAuthResponsesWebSocketV2Mode.value)
)
const openAIAPIKeyWSModeConcurrencyHintKey = computed(() =>
  resolveOpenAIWSModeConcurrencyHintKey(openaiAPIKeyResponsesWebSocketV2Mode.value)
)

// Model mapping helpers
const addModelMapping = () => {
  modelMappings.value.push({ from: '', to: '' })
}

const removeModelMapping = (index: number) => {
  modelMappings.value.splice(index, 1)
}

const addPresetMapping = (from: string, to: string) => {
  const exists = modelMappings.value.some((m) => m.from === from)
  if (exists) {
    appStore.showInfo(t('admin.accounts.mappingExists', { model: from }))
    return
  }
  modelMappings.value.push({ from, to })
}

// Error code helpers
const toggleErrorCode = (code: number) => {
  const index = selectedErrorCodes.value.indexOf(code)
  if (index === -1) {
    // Adding code - check for 429/529 warning
    if (code === 429) {
      if (!confirm(t('admin.accounts.customErrorCodes429Warning'))) {
        return
      }
    } else if (code === 529) {
      if (!confirm(t('admin.accounts.customErrorCodes529Warning'))) {
        return
      }
    }
    selectedErrorCodes.value.push(code)
  } else {
    selectedErrorCodes.value.splice(index, 1)
  }
}

const addCustomErrorCode = () => {
  const code = customErrorCodeInput.value
  if (code === null || code < 100 || code > 599) {
    appStore.showError(t('admin.accounts.invalidErrorCode'))
    return
  }
  if (selectedErrorCodes.value.includes(code)) {
    appStore.showInfo(t('admin.accounts.errorCodeExists'))
    return
  }
  // Check for 429/529 warning
  if (code === 429) {
    if (!confirm(t('admin.accounts.customErrorCodes429Warning'))) {
      return
    }
  } else if (code === 529) {
    if (!confirm(t('admin.accounts.customErrorCodes529Warning'))) {
      return
    }
  }
  selectedErrorCodes.value.push(code)
  customErrorCodeInput.value = null
}

const removeErrorCode = (code: number) => {
  const index = selectedErrorCodes.value.indexOf(code)
  if (index !== -1) {
    selectedErrorCodes.value.splice(index, 1)
  }
}

const buildModelMappingObject = (): Record<string, string> | null => {
  return buildModelMappingPayload(
    modelRestrictionMode.value,
    allowedModels.value,
    modelMappings.value
  )
}

const normalizeCodexQuotaLimitInput = (value: number) => {
  if (!Number.isFinite(value)) return CODEX_QUOTA_DEFAULT_LIMIT_PERCENT
  return Math.min(100, Math.max(1, value))
}

const buildUpdatePayload = (): Record<string, unknown> | null => {
  const updates: Record<string, unknown> = {}
  const credentials: Record<string, unknown> = {}
  let credentialsChanged = false
  const ensureExtra = (): Record<string, unknown> => {
    if (!updates.extra) {
      updates.extra = {}
    }
    return updates.extra as Record<string, unknown>
  }

  if (canManageProxy.value && enableProxy.value) {
    // 后端期望 proxy_id: 0 表示清除代理，而不是 null
    updates.proxy_id = proxyId.value === null ? 0 : proxyId.value
  }

  if (enableConcurrency.value) {
    updates.concurrency = concurrency.value
  }

  if (enableLoadFactor.value) {
    if (isUserScope.value) {
      updates.load_factor = normalizePersonalAccountLoadFactor(loadFactor.value)
    } else {
      // 空值/NaN/0 时发送 0（后端约定 <= 0 表示清除）
      const lf = loadFactor.value
      updates.load_factor = (lf != null && !Number.isNaN(lf) && lf > 0) ? lf : 0
    }
  }

  if (enablePriority.value) {
    updates.priority = priority.value
  }

  if (canManageAccountLevel.value && enableAccountLevel.value) {
    updates.account_level = accountLevel.value
  }

  if (isUserScope.value) {
    if ('concurrency' in updates) {
      updates.concurrency = normalizePersonalAccountConcurrency(updates.concurrency)
    }
  }

  if (canManageBillingRate.value && enableRateMultiplier.value) {
    updates.rate_multiplier = rateMultiplier.value
  }

  if (enableStatus.value) {
    updates.status = status.value
  }

  if (canManageGroups.value && enableGroups.value) {
    updates.group_ids = groupIds.value
  }

  if (canManageBaseUrl.value && enableBaseUrl.value) {
    const baseUrlValue = baseUrl.value.trim()
    if (baseUrlValue) {
      credentials.base_url = baseUrlValue
      credentialsChanged = true
    }
  }

  if (enableOpenAIPassthrough.value) {
    const extra = ensureExtra()
    extra.openai_passthrough = openaiPassthroughEnabled.value
    if (!openaiPassthroughEnabled.value) {
      extra.openai_oauth_passthrough = false
    }
  }

  if (canManageModelRestriction.value && enableModelRestriction.value && !isOpenAIModelRestrictionDisabled.value) {
    // 统一使用 model_mapping 字段
    if (isUserScope.value || modelRestrictionMode.value === 'whitelist') {
      // 白名单模式：将模型转换为 model_mapping 格式（key=value）
      const mapping: Record<string, string> = {}
      for (const m of allowedModels.value) {
        mapping[m] = m
      }
      credentials.model_mapping = mapping
      credentialsChanged = true
    } else {
      // 映射模式下空配置同样表示“支持所有模型”。
      const modelMapping = buildModelMappingObject()
      credentials.model_mapping = modelMapping ?? {}
      credentialsChanged = true
    }
  }

  if (canManageCustomErrorCodes.value && enableCustomErrorCodes.value) {
    credentials.custom_error_codes_enabled = true
    credentials.custom_error_codes = [...selectedErrorCodes.value]
    credentialsChanged = true
  }

  if (enableInterceptWarmup.value) {
    credentials.intercept_warmup_requests = interceptWarmupRequests.value
    credentialsChanged = true
  }

  if (!isUserScope.value && allHeaderOverrideCapable.value && enableHeaderOverride.value) {
    credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = headerOverrideEnabled.value
    credentials[HEADER_OVERRIDES_CREDENTIAL_KEY] = headerOverrideEnabled.value
      ? buildHeaderOverridesObject(headerOverrideRows.value)
      : {}
    credentialsChanged = true
  }

  if (credentialsChanged) {
    updates.credentials = credentials
  }

  if (enableOpenAIWSMode.value) {
    const extra = ensureExtra()
    extra.openai_oauth_responses_websockets_v2_mode = openaiOAuthResponsesWebSocketV2Mode.value
    extra.openai_oauth_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(
      openaiOAuthResponsesWebSocketV2Mode.value
    )
  }

  if (enableOpenAIAPIKeyWSMode.value) {
    const extra = ensureExtra()
    extra.openai_apikey_responses_websockets_v2_mode = openaiAPIKeyResponsesWebSocketV2Mode.value
    extra.openai_apikey_responses_websockets_v2_enabled = isOpenAIWSModeEnabled(
      openaiAPIKeyResponsesWebSocketV2Mode.value
    )
  }

  if (enableCodexCLIOnly.value) {
    const extra = ensureExtra()
    extra.codex_cli_only = codexCLIOnlyEnabled.value
  }

  if (enableCodexFingerprintMode.value) {
    const extra = ensureExtra()
    // Bulk updates merge JSONB patches into the existing extra object.  An
    // omitted key cannot clear an existing off/device/full value, so the
    // default session mode must be written explicitly here.
    extra.codex_fingerprint_mode = codexFingerprintMode.value
  }

  if (enableCodexQuotaLimit.value) {
    const extra = ensureExtra()
    bulkCodex5hLimitPercent.value = normalizeCodexQuotaLimitInput(Number(bulkCodex5hLimitPercent.value))
    bulkCodex7dLimitPercent.value = normalizeCodexQuotaLimitInput(Number(bulkCodex7dLimitPercent.value))
    extra.codex_5h_limit_percent = bulkCodex5hLimitPercent.value
    extra.codex_7d_limit_percent = bulkCodex7dLimitPercent.value
  }

  // RPM limit settings (写入 extra 字段)
  if (enableRpmLimit.value) {
    const extra = ensureExtra()
    if (rpmLimitEnabled.value && bulkBaseRpm.value != null && bulkBaseRpm.value > 0) {
      extra.base_rpm = bulkBaseRpm.value
      extra.rpm_strategy = bulkRpmStrategy.value
      if (bulkRpmStickyBuffer.value != null && bulkRpmStickyBuffer.value > 0) {
        extra.rpm_sticky_buffer = bulkRpmStickyBuffer.value
      }
    } else {
      // 关闭 RPM 限制 - 设置 base_rpm 为 0，并用空值覆盖关联字段
      // 后端使用 JSONB || merge 语义，不会删除已有 key，
      // 所以必须显式发送空值来重置（后端读取时会 fallback 到默认值）
      extra.base_rpm = 0
      extra.rpm_strategy = ''
      extra.rpm_sticky_buffer = 0
    }
    updates.extra = extra
  }

  // UMQ mode（独立于 RPM 保存）
  if (userMsgQueueMode.value !== null) {
    const umqExtra = ensureExtra()
    umqExtra.user_msg_queue_mode = userMsgQueueMode.value  // '' = 清除账号级覆盖
    umqExtra.user_msg_queue_enabled = false  // 清理旧字段（JSONB merge）
  }

  return Object.keys(updates).length > 0 ? updates : null
}

const mixedChannelConfirmed = ref(false)

// 是否需要预检查：改了分组 + 全是单一的 antigravity 或 anthropic 平台
// 多平台混合的情况由 submitBulkUpdate 的 409 catch 兜底
const canPreCheck = () =>
  !isUserScope.value &&
  enableGroups.value &&
  groupIds.value.length > 0 &&
  targetSelectedPlatforms.value.length === 1 &&
  (targetSelectedPlatforms.value[0] === 'antigravity' || targetSelectedPlatforms.value[0] === 'anthropic')

const handleClose = () => {
  showMixedChannelWarning.value = false
  mixedChannelWarningMessage.value = ''
  pendingUpdatesForConfirm.value = null
  mixedChannelConfirmed.value = false
  emit('close')
}

const sanitizeBulkUpdatePayload = (payload: Record<string, unknown>) => {
  const next = { ...payload }
  if (isUserScope.value && next.status === 'inactive') {
    next.status = 'disabled'
  }
  if (!canManageProxy.value) {
    delete next.proxy_id
  }
  if (!canManageBillingRate.value) {
    delete next.rate_multiplier
  }
  if (!canManageGroups.value) {
    delete next.group_ids
  }
  if (!canManageAccountLevel.value) {
    delete next.account_level
  }
  if (isUserScope.value) {
    delete next.status
    delete next.account_level
    if ('concurrency' in next) {
      next.concurrency = normalizePersonalAccountConcurrency(next.concurrency)
    }
    if ('load_factor' in next) {
      next.load_factor = normalizePersonalAccountLoadFactor(next.load_factor)
    }
    if ('priority' in next) {
      next.priority = typeof next.priority === 'number' && Number(next.priority) > 0
        ? next.priority
        : PERSONAL_ACCOUNT_DEFAULT_PRIORITY
    }
  }
  if (next.credentials && typeof next.credentials === 'object') {
    const credentials = { ...(next.credentials as Record<string, unknown>) }
    if (!canManageBaseUrl.value) {
      delete credentials.base_url
    }
    if (!canManageModelRestriction.value) {
      delete credentials.model_mapping
    }
    if (isUserScope.value) {
      delete credentials.base_url
      delete credentials.header_override_enabled
      delete credentials.header_overrides
    }
    if (!canManageCustomErrorCodes.value) {
      delete credentials.custom_error_codes_enabled
      delete credentials.custom_error_codes
    }
    if (Object.keys(credentials).length > 0) {
      next.credentials = credentials
    } else {
      delete next.credentials
    }
  }
  return next
}

// 预检查：提交前调接口检测，有风险就弹窗阻止，返回 false 表示需要用户确认
const preCheckMixedChannelRisk = async (built: Record<string, unknown>): Promise<boolean> => {
  if (!canPreCheck()) return true
  if (mixedChannelConfirmed.value) return true

  try {
    const result = await adminAPI.accounts.checkMixedChannelRisk({
      platform: targetSelectedPlatforms.value[0],
      group_ids: groupIds.value
    })
    if (!result.has_risk) return true

    pendingUpdatesForConfirm.value = built
    mixedChannelWarningMessage.value = result.message || t('admin.accounts.bulkEdit.failed')
    showMixedChannelWarning.value = true
    return false
  } catch (error: any) {
    appStore.showError(error.message || t('admin.accounts.bulkEdit.failed'))
    return false
  }
}

const handleSubmit = async () => {
  if (targetMode.value === 'selected' && props.accountIds.length === 0) {
    appStore.showError(t('admin.accounts.bulkEdit.noSelection'))
    return
  }
  if (
    isUserScope.value &&
    enableModelRestriction.value &&
    targetSelectedPlatforms.value.length !== 1
  ) {
    appStore.showError(t('admin.accounts.userModelSamePlatformRequired'))
    return
  }
  const hasAnyFieldEnabled =
    (canManageBaseUrl.value && enableBaseUrl.value) ||
    enableOpenAIPassthrough.value ||
    (canManageModelRestriction.value && enableModelRestriction.value) ||
    (canManageCustomErrorCodes.value && enableCustomErrorCodes.value) ||
    enableInterceptWarmup.value ||
    (!isUserScope.value && allHeaderOverrideCapable.value && enableHeaderOverride.value) ||
    (canManageProxy.value && enableProxy.value) ||
    enableConcurrency.value ||
    enableLoadFactor.value ||
    enablePriority.value ||
    (canManageAccountLevel.value && enableAccountLevel.value) ||
    (canManageBillingRate.value && enableRateMultiplier.value) ||
    enableStatus.value ||
    (canManageGroups.value && enableGroups.value) ||
    enableOpenAIWSMode.value ||
    enableOpenAIAPIKeyWSMode.value ||
    enableCodexCLIOnly.value ||
    enableCodexFingerprintMode.value ||
    enableCodexQuotaLimit.value ||
    enableRpmLimit.value ||
    userMsgQueueMode.value !== null ||
    (isUserScope.value && enableExternalPlacement.value)

  if (!hasAnyFieldEnabled) {
    appStore.showError(t('admin.accounts.bulkEdit.noFieldsSelected'))
    return
  }

  if (isUserScope.value && enableModelRestriction.value) {
    if (channelPricingModelsLoading.value || channelPricingModels.value === null) {
      appStore.showError(t('admin.accounts.userModelOptionsNotReady'))
      return
    }
    if (channelPricingModelsLoadError.value) {
      appStore.showError(
        t('admin.accounts.userModelOptionsLoadFailed', { message: channelPricingModelsLoadError.value })
      )
      return
    }
    if (allowedModels.value.length === 0) {
      appStore.showError(t('admin.accounts.userModelSelectionRequired'))
      return
    }
    const allowedSet = new Set(channelPricingModels.value)
    const invalidModels = allowedModels.value.filter(model => !allowedSet.has(model))
    if (invalidModels.length > 0) {
      appStore.showError(
        t('admin.accounts.userModelSelectionInvalid', { models: invalidModels.join(', ') })
      )
      return
    }
  }

  if (!isUserScope.value && allTargetsGrok.value && enableBaseUrl.value) {
    const trimmedBaseUrl = baseUrl.value.trim()
    if (trimmedBaseUrl) {
      try {
        const parsedBaseUrl = new URL(trimmedBaseUrl)
        if (parsedBaseUrl.protocol !== 'http:' && parsedBaseUrl.protocol !== 'https:') {
          throw new Error('invalid protocol')
        }
      } catch {
        appStore.showError(t('admin.accounts.grokCustomBaseUrl.invalid'))
        return
      }
    }
  }

  if (
    !isUserScope.value &&
    allHeaderOverrideCapable.value &&
    enableHeaderOverride.value &&
    headerOverrideEnabled.value
  ) {
    if (!headerOverrideRows.value.some(row => row.name.trim())) {
      appStore.showError(t('admin.accounts.headerOverride.bulkEmptyRows'))
      return
    }
    const headerError = validateHeaderOverrideRows(headerOverrideRows.value)
    if (headerError) {
      appStore.showError(t(`admin.accounts.headerOverride.${headerError}`))
      return
    }
  }

  const built = buildUpdatePayload()
  if (!built && !enableExternalPlacement.value) {
    appStore.showError(t('admin.accounts.bulkEdit.noFieldsSelected'))
    return
  }
  if (built) {
    const canContinue = await preCheckMixedChannelRisk(built)
    if (!canContinue) return
  }

  await submitBulkUpdate(built)
}

const submitBulkUpdate = async (baseUpdates: Record<string, unknown> | null) => {
  // 每次提交开始清空上一轮的错误与失败明细，避免同目标重试/整请求失败时残留陈旧内容。
  placementSubmitError.value = ''
  placementFailedDetails.value = []
  let placementIdempotencyKey = ''
  if (isUserScope.value && enableExternalPlacement.value) {
    try {
      placementIdempotencyKey = getPendingPlacementIdempotencyKey()
    } catch (error) {
      appStore.showError(extractApiErrorMessage(
        error,
        t('userAccounts.externalPlacement.convertFailed')
      ))
      return
    }
  }
  submitting.value = true

  try {
    let success = 0
    let failed = 0
    let baseSuccess = 0
    let placementSuccess = 0
    let placementFailed = 0
    let placementAccountIDs = [...props.accountIds]

    if (baseUpdates) {
      // 无论是预检查确认还是 409 兜底确认，只要 mixedChannelConfirmed 为 true 就带上 flag
      const updates = mixedChannelConfirmed.value
        ? { ...baseUpdates, confirm_mixed_channel_risk: true }
        : baseUpdates
      const payload = sanitizeBulkUpdatePayload(updates)
      const res = isUserScope.value
        ? await accountsAPI.bulkUpdate(props.accountIds, payload)
        : targetMode.value === 'filtered' && props.target?.filters
        ? await adminAPI.accounts.bulkUpdate({
          filters: props.target.filters,
          ...payload
        })
        : await adminAPI.accounts.bulkUpdate(props.accountIds, payload)
      if (isUserScope.value && res.async && res.task) {
        if (enableExternalPlacement.value) {
          placementSubmitError.value = t(
            'userAccounts.externalPlacement.asyncBaseUpdatePending'
          )
          appStore.showError(placementSubmitError.value)
          return
        }
        appStore.showSuccess(t('admin.accounts.bulkActions.asyncSubmitted', { count: res.task.total }))
        emit('updated', { async: true, task: res.task })
        handleClose()
        return
      }

      success = res.success || 0
      failed = res.failed || 0
      baseSuccess = success
      placementAccountIDs = res.success_ids?.length
        ? [...res.success_ids]
        : res.results.filter((item) => item.success).map((item) => item.account_id)
      if (enableExternalPlacement.value && placementAccountIDs.length === 0) {
        appStore.showError(t('admin.accounts.bulkEdit.failed'))
        return
      }
    }

    if (isUserScope.value && enableExternalPlacement.value) {
      try {
        const placementResult = await accountsAPI.convertExternalPlacementBatch({
          account_ids: placementAccountIDs,
          target: selectedExternalPlacementTarget.value,
          idempotency_key: placementIdempotencyKey
        })
        placementSuccess = placementResult.success || 0
        placementFailed = placementResult.failed || 0
        success = placementSuccess
        failed += placementFailed
        // 批量转换是 200 + results[].reason 的形式（不走 catch），失败原因在结果里逐条透出。
        // 收集失败账号的中文原因（按 reason 码映射），在错误区展示；上限截断避免超长弹窗。
        placementFailedDetails.value = (placementResult.results || [])
          .filter((item) => !item.success)
          .slice(0, BULK_PLACEMENT_FAILURE_DETAIL_MAX)
          .map((item) => ({
            accountId: item.account_id,
            reason: item.reason
              ? extractI18nErrorMessage(
                { reason: item.reason, metadata: undefined } as unknown,
                t,
                'userAccounts.externalPlacement.errors',
                item.message || item.error || t('userAccounts.externalPlacement.convertFailed')
              )
              : item.message || item.error || t('userAccounts.externalPlacement.convertFailed')
          }))
        if (placementResult.failed === 0) {
          pendingPlacementIntentSignature = ''
          pendingPlacementIdempotencyKey = ''
        }
      } catch (error) {
        const detail = extractApiErrorMessage(
          error,
          t('userAccounts.externalPlacement.convertFailed')
        )
        placementSubmitError.value = baseUpdates
          ? t('userAccounts.externalPlacement.baseSavedPlacementFailed', { error: detail })
          : detail
        appStore.showError(placementSubmitError.value)
        if (baseUpdates && baseSuccess > 0) {
          await authStore.refreshUser().catch((refreshError) => {
            console.error('Failed to refresh user after partial bulk account update:', refreshError)
          })
          emit('updated')
          handleClose()
        }
        return
      }
    }

    if (placementFailed > 0 && enableExternalPlacement.value) {
      // 只要模式切换有失败就展示失败汇总 + 逐条原因（无论是否同时保存了基础设置）。
      // 没有 baseUpdates 时是纯切换场景，用不带「其他设置已保存」字样的文案。
      placementSubmitError.value = baseUpdates
        ? t('userAccounts.externalPlacement.bulkBaseSavedPlacementPartial', {
          saved: baseSuccess,
          success: placementSuccess,
          failed: placementFailed
        })
        : t('userAccounts.externalPlacement.bulkConvertPartial', {
          success: placementSuccess,
          failed: placementFailed
        })
      appStore.showError(placementSubmitError.value)
    } else if (success > 0 && failed === 0) {
      appStore.showSuccess(t('admin.accounts.bulkEdit.success', { count: success }))
    } else if (success > 0) {
      appStore.showError(t('admin.accounts.bulkEdit.partialSuccess', { success, failed }))
    } else {
      appStore.showError(t('admin.accounts.bulkEdit.failed'))
    }

    // 部分失败（placementFailed>0）时不关闭弹窗：失败明细只在弹窗错误区渲染，
    // 关闭即被清空，用户就看不到哪些账号因何失败。全成功才关。
    if (success > 0 || baseSuccess > 0) {
      if (isUserScope.value) {
        await authStore.refreshUser().catch((error) => {
          console.error('Failed to refresh user after bulk account update:', error)
        })
      }
      pendingUpdatesForConfirm.value = null
      emit('updated')
      if (placementFailed === 0) {
        handleClose()
      }
    }
  } catch (error: any) {
    // 兜底：多平台混合场景下，预检查跳过，由后端 409 触发确认框
    if (error.status === 409 && error.error === 'mixed_channel_warning') {
      pendingUpdatesForConfirm.value = baseUpdates
      mixedChannelWarningMessage.value = error.message
      showMixedChannelWarning.value = true
      return
    }
    const reasonCode = typeof error?.reason === 'string' ? error.reason : ''
    if (!isUserScope.value && reasonCode === 'ACCOUNT_MUTATION_VERSION_CONFLICT') {
      appStore.showError(t('admin.accounts.placementGuard.versionConflict'))
      return
    }
    if (!isUserScope.value && reasonCode === 'ACCOUNT_MUTATION_FORCE_REQUIRED') {
      if (isConfirmedAccountMutationPayload(baseUpdates)) {
        appStore.showError(t('admin.accounts.placementGuard.confirmationFailed'))
        return
      }
      const challenge = extractAccountMutationVersionChallenge(error?.metadata)
      if (challenge.missingRequiredVersions) {
        appStore.showError(t('admin.accounts.placementGuard.challengeInvalid'))
        return
      }
      placementGuardFields.value = parsePlacementGuardFields(error?.metadata)
      placementExpectedVersions.value = challenge.expectedVersions ?? null
      placementForceReason.value = ''
      pendingUpdatesForConfirm.value = baseUpdates
      showPlacementForceDialog.value = true
      return
    }
    if (!isUserScope.value && reasonCode === 'OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED') {
      // 批量场景刻意不提供"一键转私有"：那会把一批账号一次性下架，
      // 影响面远超管理员此刻的意图。这里只精确点名是哪个账号、哪些字段卡住，
      // 让管理员到单账号编辑里处理——那边才有带影响说明的转换流程。
      const metadata = (error?.metadata ?? {}) as Record<string, unknown>
      appStore.showError(
        t('admin.accounts.placementGuard.bulkConversionBlocked', {
          accountId: String(metadata.account_id ?? '-'),
          fields: formatPlacementGuardFields(parsePlacementGuardFields(error?.metadata))
        })
      )
      return
    }
    appStore.showError(error.message || t('admin.accounts.bulkEdit.failed'))
    console.error('Error bulk updating accounts:', error)
  } finally {
    submitting.value = false
  }
}

const handlePlacementForceConfirm = async () => {
  const reason = placementForceReason.value.trim()
  const updates = pendingUpdatesForConfirm.value
  if (!reason || !updates) return
  const expectedVersions = placementExpectedVersions.value
  clearPlacementGuardDialog()
  await submitBulkUpdate({
    ...updates,
    force_active_edit: true,
    confirmed: true,
    reason,
    ...(expectedVersions
      ? { expected_versions: expectedVersions }
      : {})
  })
}

const clearPlacementGuardDialog = () => {
  showPlacementForceDialog.value = false
  placementForceReason.value = ''
  placementGuardFields.value = []
  placementExpectedVersions.value = null
  pendingUpdatesForConfirm.value = null
}

const handleMixedChannelConfirm = async () => {
  showMixedChannelWarning.value = false
  mixedChannelConfirmed.value = true
  if (pendingUpdatesForConfirm.value) {
    await submitBulkUpdate(pendingUpdatesForConfirm.value)
  }
}

const handleMixedChannelCancel = () => {
  showMixedChannelWarning.value = false
  pendingUpdatesForConfirm.value = null
}

// Reset form when modal closes
watch(
  () => props.show,
  (newShow) => {
    if (!newShow) {
      // Reset all enable flags
      enableBaseUrl.value = false
      enableModelRestriction.value = false
      enableCustomErrorCodes.value = false
      enableInterceptWarmup.value = false
      enableHeaderOverride.value = false
      enableProxy.value = false
      enableConcurrency.value = false
      enableLoadFactor.value = false
      enablePriority.value = false
      enableAccountLevel.value = false
      enableRateMultiplier.value = false
      enableStatus.value = false
      enableGroups.value = false
      enableOpenAIPassthrough.value = false
      enableOpenAIWSMode.value = false
      enableOpenAIAPIKeyWSMode.value = false
      enableCodexCLIOnly.value = false
      enableCodexFingerprintMode.value = false
      enableCodexQuotaLimit.value = false
      enableRpmLimit.value = false
      enableExternalPlacement.value = false

      // Reset all values
      baseUrl.value = ''
      openaiPassthroughEnabled.value = false
      modelRestrictionMode.value = 'whitelist'
      allowedModels.value = []
      modelMappings.value = []
      selectedErrorCodes.value = []
      customErrorCodeInput.value = null
      interceptWarmupRequests.value = false
      headerOverrideEnabled.value = false
      headerOverrideRows.value = []
      proxyId.value = null
      concurrency.value = isUserScope.value ? PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY : 1
      loadFactor.value = isUserScope.value ? PERSONAL_ACCOUNT_DEFAULT_LOAD_FACTOR : null
      priority.value = PERSONAL_ACCOUNT_DEFAULT_PRIORITY
      accountLevel.value = 'unknown'
      rateMultiplier.value = 1
      status.value = 'active'
      groupIds.value = []
      openaiOAuthResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      openaiAPIKeyResponsesWebSocketV2Mode.value = OPENAI_WS_MODE_OFF
      codexCLIOnlyEnabled.value = false
      codexFingerprintMode.value = 'session'
      bulkCodex5hLimitPercent.value = CODEX_QUOTA_DEFAULT_LIMIT_PERCENT
      bulkCodex7dLimitPercent.value = CODEX_QUOTA_DEFAULT_LIMIT_PERCENT
      rpmLimitEnabled.value = false
      bulkBaseRpm.value = null
      bulkRpmStrategy.value = 'tiered'
      bulkRpmStickyBuffer.value = null
      userMsgQueueMode.value = null
      selectedExternalPlacementTarget.value = 'private'
      placementSubmitError.value = ''
      placementFailedDetails.value = []
      pendingPlacementIntentSignature = ''
      pendingPlacementIdempotencyKey = ''

      // Reset mixed channel warning state
      showMixedChannelWarning.value = false
      mixedChannelWarningMessage.value = ''
      pendingUpdatesForConfirm.value = null
      mixedChannelConfirmed.value = false
      showPlacementForceDialog.value = false
      placementForceReason.value = ''
      placementGuardFields.value = []
      placementExpectedVersions.value = null
    }
  }
)

watch(
  [bulkEditableGroups, canManageGroups],
  () => {
    if (!canManageGroups.value) {
      enableGroups.value = false
      groupIds.value = []
      return
    }
    const allowedGroupIDs = new Set(bulkEditableGroups.value.map((group) => group.id))
    const nextGroupIDs = groupIds.value.filter((groupID) => allowedGroupIDs.has(groupID))
    if (nextGroupIDs.length !== groupIds.value.length) {
      groupIds.value = nextGroupIDs
    }
  },
  { immediate: true }
)
</script>
