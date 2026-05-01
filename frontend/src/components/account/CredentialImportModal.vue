<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <form :id="formId" class="space-y-4" @submit.prevent="handleImport">
      <p class="text-sm text-gray-600 dark:text-dark-300">
        {{ hint }}
      </p>

      <div
        class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
      >
        {{ warning }}
      </div>

      <div class="grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-800">
        <button
          type="button"
          :class="[
            'rounded-md px-3 py-2 text-sm font-medium transition-colors',
            importMode === 'text'
              ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400'
              : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
          ]"
          @click="importMode = 'text'"
        >
          {{ t('userAccounts.importTextMode') }}
        </button>
        <button
          type="button"
          :class="[
            'rounded-md px-3 py-2 text-sm font-medium transition-colors',
            importMode === 'file'
              ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400'
              : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
          ]"
          @click="importMode = 'file'"
        >
          {{ t('userAccounts.importFileMode') }}
        </button>
      </div>

      <div v-if="importMode === 'text'" class="space-y-2">
        <label class="input-label">{{ t('userAccounts.importTextLabel') }}</label>
        <textarea
          v-model="textContent"
          class="input min-h-64 resize-y font-mono text-xs leading-5"
          :placeholder="t('userAccounts.importTextPlaceholder')"
        />
        <p class="input-hint">{{ t('userAccounts.importTextHint') }}</p>
      </div>

      <div v-else class="space-y-3">
        <label class="input-label">{{ t('userAccounts.importFile') }}</label>
        <div
          class="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="min-w-0">
            <div class="truncate text-sm text-gray-700 dark:text-dark-200">
              {{ selectedFilesText || t('userAccounts.importSelectFile') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('userAccounts.importFileFormatHint') }}
            </div>
          </div>
          <div class="flex shrink-0 gap-2">
            <button type="button" class="btn btn-secondary" @click="openFilePicker">
              <Icon name="document" size="sm" class="mr-2" />
              {{ t('userAccounts.importChooseFiles') }}
            </button>
            <button type="button" class="btn btn-secondary" @click="openDirectoryPicker">
              <Icon name="inbox" size="sm" class="mr-2" />
              {{ t('userAccounts.importChooseDirectory') }}
            </button>
          </div>
        </div>
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          accept="application/json,text/plain,.json,.txt"
          multiple
          @change="handleFileChange"
        />
        <input
          ref="directoryInput"
          type="file"
          class="hidden"
          accept="application/json,text/plain,.json,.txt"
          multiple
          webkitdirectory
          @change="handleDirectoryChange"
        />
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
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" :disabled="importing" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          type="submit"
          :form="formId"
          :disabled="importing"
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
  formId?: string
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
  skipped: number
  failed: number
  errors: CredentialImportError[]
}

const props = withDefaults(defineProps<Props>(), {
  formId: 'credential-import-form'
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
  return name.endsWith('.json') || name.endsWith('.txt') || sourceFile.type === 'application/json' || sourceFile.type === 'text/plain'
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
      skipped: nextResult.skipped,
      failed: nextResult.failed
    }

    if (nextResult.failed > 0 || nextResult.skipped > 0) {
      if (nextResult.created > 0) {
        emit('imported', { close: false })
      }
      appStore.showWarning(t('userAccounts.importCompletedWithIssues', params))
    } else {
      if (nextResult.created > 0) {
        emit('imported', { close: true })
      }
      appStore.showSuccess(t('userAccounts.importSuccess', params))
    }
  } catch (error: any) {
    appStore.showError(error?.message || error?.response?.data?.message || error?.response?.data?.detail || t('userAccounts.importFailed'))
  } finally {
    importing.value = false
  }
}
</script>
