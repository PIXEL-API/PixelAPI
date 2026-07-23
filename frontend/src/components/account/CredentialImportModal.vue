<template>
  <BaseDialog
    :show="show"
    :title="title"
    :width="width"
    close-on-click-outside
    @close="handleClose"
  >
    <form :id="formId" class="space-y-5" @submit.prevent="handleImport">
      <div class="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(20rem,0.9fr)] lg:items-start">
        <p class="text-sm leading-6 text-gray-600 dark:text-dark-300 lg:py-1">
          {{ hint }}
        </p>

        <div
          class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-5 text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
        >
          {{ warning }}
        </div>
      </div>

      <div
        :class="[
          'grid gap-5',
          $slots.controls
            ? 'lg:grid-cols-[minmax(0,1.08fr)_minmax(0,0.92fr)] lg:items-start'
            : ''
        ]"
      >
        <section
          v-if="$slots.controls"
          class="space-y-4 lg:rounded-2xl lg:border lg:border-gray-200 lg:bg-gray-50/70 lg:p-5 dark:lg:border-dark-700 dark:lg:bg-dark-900/30"
        >
          <slot name="controls" />
        </section>

        <section
          :class="[
            'space-y-4',
            $slots.controls
              ? 'lg:rounded-2xl lg:border lg:border-gray-200 lg:p-5 dark:lg:border-dark-700'
              : ''
          ]"
        >
          <div
            class="grid grid-cols-2 gap-1 rounded-xl border border-gray-200 bg-gray-100 p-1 dark:border-dark-700 dark:bg-dark-900/60"
            role="tablist"
          >
            <button
              type="button"
              role="tab"
              :aria-selected="importMode === 'text'"
              :class="[
                'min-h-11 cursor-pointer rounded-lg px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50',
                importMode === 'text'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300'
                  : 'text-gray-600 hover:bg-white/60 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-700/60 dark:hover:text-white'
              ]"
              @click="importMode = 'text'"
            >
              {{ t('userAccounts.importTextMode') }}
            </button>
            <button
              type="button"
              role="tab"
              :aria-selected="importMode === 'file'"
              :class="[
                'min-h-11 cursor-pointer rounded-lg px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50',
                importMode === 'file'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300'
                  : 'text-gray-600 hover:bg-white/60 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-700/60 dark:hover:text-white'
              ]"
              @click="importMode = 'file'"
            >
              {{ t('userAccounts.importFileMode') }}
            </button>
          </div>

          <div v-if="importMode === 'text'" class="space-y-2" role="tabpanel">
            <label class="input-label">{{ t('userAccounts.importTextLabel') }}</label>
            <textarea
              v-model="textContent"
              class="input min-h-64 resize-y font-mono text-xs leading-5"
              :placeholder="textPlaceholder || t('userAccounts.importTextPlaceholder')"
            />
            <p class="input-hint leading-5">{{ textHint || t('userAccounts.importTextHint') }}</p>
          </div>

          <div v-else class="space-y-3" role="tabpanel">
            <label class="input-label">{{ t('userAccounts.importFile') }}</label>
            <div
              class="flex min-h-64 flex-col justify-center gap-4 rounded-xl border border-dashed border-gray-300 bg-gray-50 px-4 py-5 dark:border-dark-600 dark:bg-dark-800 sm:px-5"
            >
              <div class="min-w-0 text-center">
                <div class="truncate text-sm font-medium text-gray-700 dark:text-dark-200">
                  {{ selectedFilesText || t('userAccounts.importSelectFile') }}
                </div>
                <div class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">
                  {{ t('userAccounts.importFileFormatHint') }}
                </div>
              </div>
              <div class="flex flex-col justify-center gap-2 sm:flex-row">
                <button type="button" class="btn btn-secondary min-h-11" @click="openFilePicker">
                  <Icon name="document" size="sm" class="mr-2" />
                  {{ t('userAccounts.importChooseFiles') }}
                </button>
                <button type="button" class="btn btn-secondary min-h-11" @click="openDirectoryPicker">
                  <Icon name="inbox" size="sm" class="mr-2" />
                  {{ t('userAccounts.importChooseDirectory') }}
                </button>
              </div>
            </div>
            <input
              ref="fileInput"
              type="file"
              class="hidden"
              :accept="fileAccept"
              multiple
              @change="handleFileChange"
            />
            <input
              ref="directoryInput"
              type="file"
              class="hidden"
              :accept="fileAccept"
              multiple
              webkitdirectory
              @change="handleDirectoryChange"
            />
          </div>
        </section>
      </div>

      <div
        v-if="result"
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('userAccounts.importResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{
            t('userAccounts.importResultSummary', {
              created: result.created,
              updated: result.updated,
              skipped: result.skipped,
              failed: result.failed
            })
          }}
        </div>

        <div v-if="result.errors.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('userAccounts.importErrors') }}
          </div>
          <div class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800">
            <div
              v-for="(item, idx) in result.errors"
              :key="idx"
              class="whitespace-pre-wrap text-gray-700 dark:text-dark-200"
            >
              {{ item.kind }} {{ item.name || '-' }} - {{ item.message }}
            </div>
          </div>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end sm:gap-3">
        <button class="btn btn-secondary min-h-11" type="button" :disabled="importing" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary min-h-11"
          type="submit"
          :form="formId"
          :disabled="importing || submitDisabled"
        >
          <Icon v-if="!importing" name="upload" size="sm" class="mr-2" />
          {{ importing ? t('userAccounts.importing') : t('userAccounts.importButton') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ImportCredentialContentsResponse } from '@/api/accounts'

interface Props {
  show: boolean
  title: string
  hint: string
  warning: string
  width?: 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'
  formId?: string
  submitDisabled?: boolean
  textHint?: string
  textPlaceholder?: string
  fileAccept?: string
  allowedExtensions?: string[]
  importer: (contents: string[]) => Promise<ImportCredentialContentsResponse>
}

interface Emits {
  (e: 'close'): void
  (e: 'imported', payload?: { close: boolean }): void
}

interface CredentialImportError {
  kind: 'account' | 'file'
  name?: string
  message: string
}

interface CredentialImportResult {
  created: number
  updated: number
  skipped: number
  failed: number
  errors: CredentialImportError[]
}

const props = withDefaults(defineProps<Props>(), {
  width: 'wide',
  formId: 'credential-import-form',
  submitDisabled: false,
  fileAccept: 'application/json,text/plain,.json,.txt',
  allowedExtensions: () => ['.json', '.txt']
})
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const importing = ref(false)
const importMode = ref<'text' | 'file'>('text')
const textContent = ref('')
const files = ref<File[]>([])
const result = ref<CredentialImportResult | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const directoryInput = ref<HTMLInputElement | null>(null)

const selectedFilesText = computed(() => {
  if (files.value.length === 0) return ''
  if (files.value.length === 1) return files.value[0]?.name || ''
  return t('userAccounts.importSelectedFiles', { count: files.value.length })
})

watch(
  () => props.show,
  (open) => {
    if (open) {
      importMode.value = 'text'
      textContent.value = ''
      files.value = []
      result.value = null
      if (fileInput.value) {
        fileInput.value.value = ''
      }
      if (directoryInput.value) {
        directoryInput.value.value = ''
      }
    }
  }
)

function openFilePicker(): void {
  fileInput.value?.click()
}

function openDirectoryPicker(): void {
  directoryInput.value?.click()
}

function handleClose(): void {
  if (importing.value) return
  emit('close')
}

function handleFileChange(event: Event): void {
  const target = event.target as HTMLInputElement
  files.value = normalizeSelectedFiles(target.files)
}

function handleDirectoryChange(event: Event): void {
  const target = event.target as HTMLInputElement
  files.value = normalizeSelectedFiles(target.files)
}

function normalizeSelectedFiles(fileList: FileList | null | undefined): File[] {
  if (!fileList) return []
  return Array.from(fileList)
    .filter(isSupportedImportFile)
    .sort((left, right) => left.name.localeCompare(right.name))
}

function isSupportedImportFile(sourceFile: File): boolean {
  const name = sourceFile.name.toLowerCase()
  const extensions = props.allowedExtensions.map(extension =>
    extension.startsWith('.') ? extension.toLowerCase() : `.${extension.toLowerCase()}`
  )
  if (extensions.some(extension => name.endsWith(extension))) return true
  if (extensions.includes('.json') && sourceFile.type === 'application/json') return true
  return extensions.includes('.txt') && sourceFile.type === 'text/plain'
}

async function readFileAsText(sourceFile: File): Promise<string> {
  if (typeof sourceFile.text === 'function') {
    return sourceFile.text()
  }
  const buffer = await sourceFile.arrayBuffer()
  return new TextDecoder().decode(buffer)
}

async function handleImport(): Promise<void> {
  importing.value = true
  const nextResult: CredentialImportResult = {
    created: 0,
    updated: 0,
    skipped: 0,
    failed: 0,
    errors: []
  }

  try {
    const contents: string[] = []
    if (importMode.value === 'text') {
      const text = textContent.value.trim()
      if (text) {
        contents.push(text)
      }
    } else {
      for (const sourceFile of files.value) {
        try {
          const text = (await readFileAsText(sourceFile)).trim()
          if (text) {
            contents.push(text)
          }
        } catch (error: any) {
          nextResult.failed += 1
          nextResult.errors.push({
            kind: 'file',
            name: sourceFile.name,
            message: error?.message || t('userAccounts.importFileReadFailed')
          })
        }
      }
    }

    if (contents.length === 0) {
      appStore.showError(
        importMode.value === 'text'
          ? t('userAccounts.importTextRequired')
          : t('userAccounts.importSelectFile')
      )
      result.value = nextResult.errors.length ? nextResult : null
      return
    }

    const response = await props.importer(contents)

    nextResult.created += response.created
    nextResult.updated += response.updated ?? 0
    nextResult.failed += response.failed
    nextResult.errors.push(
      ...(response.errors ?? []).map((item) => ({
        kind: 'account' as const,
        name: item.name || `#${item.index}`,
        message: item.message
      }))
    )

    result.value = nextResult
    const params = {
      created: nextResult.created,
      updated: nextResult.updated,
      skipped: nextResult.skipped,
      failed: nextResult.failed
    }
    const changed = nextResult.created + nextResult.updated > 0

    if (nextResult.failed > 0 || nextResult.skipped > 0) {
      if (changed) {
        emit('imported', { close: false })
      }
      appStore.showWarning(t('userAccounts.importCompletedWithIssues', params))
    } else {
      if (changed) {
        emit('imported', { close: true })
      }
      appStore.showSuccess(t('userAccounts.importSuccess', params))
    }
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.response?.data?.detail || error?.message || t('userAccounts.importFailed'))
  } finally {
    importing.value = false
  }
}
</script>
