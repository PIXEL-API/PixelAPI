<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { getIdea, createIdea, updateIdea, publishIdea, listIdeaTags } from '@/api/ideas'
import Skeleton from '@/components/common/Skeleton.vue'
import IdeasHeader from '@/components/ideas/IdeasHeader.vue'
import { useAppStore } from '@/stores/app'
import type { IdeaTag } from '@/types/ideas'

marked.setOptions({ breaks: true, gfm: true })

const appStore = useAppStore()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const postId = ref<number | null>(route.params.id ? Number(route.params.id) : null)
const postStatus = ref<string>('')

const title = ref('')
const summary = ref('')
const body = ref('')
const tags = ref<string[]>([])
const tagInput = ref('')
const availableTags = ref<IdeaTag[]>([])

const saving = ref(false)
const error = ref('')
const loadingEdit = ref(false)
const mode = ref<'editor' | 'preview'>('editor')
const bodyTextarea = ref<HTMLTextAreaElement | null>(null)

const renderedHtml = computed(() => DOMPurify.sanitize(marked.parse(body.value || '') as string))
const titleCount = computed(() => title.value.length)
const bodyCount = computed(() => body.value.length)
const canSave = computed(() => title.value.trim() !== '' && body.value.trim() !== '')

const toolbarTools = computed(() => [
  { key: 'h2', label: t('ideas.editor.toolbar.heading'), icon: 'H2-', before: '## ', block: true },
  { key: 'b', label: t('ideas.editor.toolbar.bold'), icon: 'B', before: '**', after: '**' },
  { key: 'i', label: t('ideas.editor.toolbar.italic'), icon: 'I', before: '*', after: '*' },
  { key: 'quote', label: t('ideas.editor.toolbar.quote'), icon: '❝', before: '> ', block: true },
  { key: 'code', label: t('ideas.editor.toolbar.code'), icon: '</>', before: '```\n', after: '\n```', block: true },
  { key: 'link', label: t('ideas.editor.toolbar.link'), icon: '🔗', before: '[', after: '](url)' },
  { key: 'ul', label: t('ideas.editor.toolbar.list'), icon: '•', before: '- ', block: true },
  { key: 'hr', label: t('ideas.editor.toolbar.separator'), icon: '—', before: '\n---\n', block: true },
])

function applyTool(tool: { before: string; after?: string; block?: boolean }) {
  const el = bodyTextarea.value
  if (!el) {
    body.value += tool.before + (tool.after || '')
    return
  }
  const start = el.selectionStart
  const end = el.selectionEnd
  if (tool.block) {
    const lineStart = body.value.lastIndexOf('\n', start - 1) + 1
    body.value = body.value.slice(0, lineStart) + tool.before + body.value.slice(lineStart)
    nextTick(() => {
      el.focus()
      el.selectionStart = el.selectionEnd = lineStart + tool.before.length
    })
    return
  }
  const sel = body.value.slice(start, end)
  const after = tool.after || tool.before
  body.value = body.value.slice(0, start) + tool.before + sel + after + body.value.slice(end)
  nextTick(() => {
    el.focus()
    el.selectionStart = start + tool.before.length
    el.selectionEnd = end + tool.before.length
  })
}

function addTag(name: string) {
  const n = name.trim()
  if (!n) return
  if (tags.value.includes(n)) {
    tagInput.value = ''
    return
  }
  if (tags.value.length >= 5) {
    appStore.showWarning(t('ideas.editor.tagLimit'))
    return
  }
  tags.value.push(n)
  tagInput.value = ''
}

function removeTag(name: string) {
  tags.value = tags.value.filter((t) => t !== name)
}

function onTagKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' || e.key === ',') {
    e.preventDefault()
    addTag(tagInput.value)
  } else if (e.key === 'Backspace' && !tagInput.value && tags.value.length) {
    tags.value.pop()
  }
}

function toggleAvailableTag(name: string) {
  if (tags.value.includes(name)) removeTag(name)
  else addTag(name)
}

async function load() {
  loadingEdit.value = true
  error.value = ''
  try {
    availableTags.value = await listIdeaTags()
  } catch {
    availableTags.value = []
  }
  if (postId.value) {
    try {
      const post = await getIdea(postId.value)
      postStatus.value = post.status
      if (post.revision) {
        title.value = post.revision.title
        summary.value = post.revision.summary
        body.value = post.revision.body
      }
      tags.value = (post.tags || []).map((t) => t.name)
    } catch (e: any) {
      error.value = e?.message || t('ideas.editor.loadFailed')
    }
  }
  loadingEdit.value = false
}

const isReviewing = computed(
  () =>
    !!postId.value &&
    ['pending_review', 'manual_review', 'pending_revision', 'moderation_failed'].includes(postStatus.value)
)

async function save(publish: boolean) {
  error.value = ''
  if (!title.value.trim()) {
    error.value = t('ideas.editor.titleRequired')
    return
  }
  if (!body.value.trim()) {
    error.value = t('ideas.editor.bodyRequired')
    return
  }
  if (isReviewing.value) {
    error.value = t('ideas.editor.reviewing')
    return
  }
  saving.value = true
  try {
    const payload = { title: title.value, summary: summary.value, body: body.value, tags: tags.value }
    let redirectId: number | null = null
    if (postId.value) {
      if (postStatus.value === 'published') {
        // 已发布：保存即提交修订（后端转入 pending_revision 审核），不再调用 publish。
        const post = await updateIdea(postId.value, payload)
        redirectId = post.id
        appStore.showSuccess(t('ideas.editor.revisionSubmitted'))
      } else {
        // 草稿 / 被拒：先更新内容，再按需发布（后端现已允许 rejected 重新编辑）。
        const post = await updateIdea(postId.value, payload)
        redirectId = post.id
        if (publish) {
          await publishIdea(post.id)
          redirectId = post.id
          appStore.showSuccess(t('ideas.editor.reviewSubmitted'))
        } else {
          appStore.showSuccess(t('ideas.editor.draftSaved'))
        }
      }
    } else {
      // 新建：创建成功后回填 postId，避免发布失败时重试重复创建草稿。
      let post = await createIdea(payload)
      redirectId = post.id
      postId.value = post.id
      if (publish) {
        post = await publishIdea(post.id)
        redirectId = post.id
        appStore.showSuccess(t('ideas.editor.reviewSubmitted'))
      } else {
        appStore.showSuccess(t('ideas.editor.draftSaved'))
      }
    }
    if (redirectId) router.push(`/ideas/${redirectId}`)
  } catch (e: any) {
    error.value = e?.message || t('ideas.editor.saveFailed')
    appStore.showError(e?.message || t('ideas.editor.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="min-h-screen bg-canvas text-content">
    <IdeasHeader />
    <div class="mx-auto max-w-5xl px-4 pb-16 pt-6 md:px-6">
      <!-- Header -->
      <div class="mb-6 flex items-center gap-3">
        <RouterLink to="/ideas" class="inline-flex min-h-11 items-center gap-1.5 rounded-control px-2 py-2 text-sm text-content-muted transition-colors hover:bg-surface-subtle hover:text-content">
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.5 19.5L3 12m0 0l7.5-7.5M3 12h18" /></svg>
          {{ t('ideas.common.backToPlaza') }}
        </RouterLink>
        <h1 class="text-xl font-bold text-content">{{ postId ? t('ideas.editor.editTitle') : t('ideas.editor.newTitle') }}</h1>
        <span class="ml-auto rounded-full border border-line bg-surface-subtle px-3 py-1 text-xs text-content-muted">
          {{ mode === 'editor' ? t('ideas.editor.editing') : t('ideas.editor.previewing') }}
        </span>
      </div>

      <div v-if="loadingEdit" class="h-96 space-y-4 rounded-panel border border-line bg-surface p-6">
        <Skeleton height="36" width="60%" />
        <Skeleton height="16" width="40%" />
        <Skeleton variant="text" class="mb-2" /><Skeleton variant="text" class="mb-2" /><Skeleton variant="text" width="70%" />
        <Skeleton height="120" class="mt-6" />
      </div>

      <div v-else class="space-y-4">
        <!-- Error banner -->
        <div v-if="error" class="flex items-center gap-2 rounded-control border border-danger/30 bg-danger/10 px-4 py-2.5 text-sm text-danger">
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" /></svg>
          {{ error }}
        </div>

        <!-- Editor card -->
        <div class="overflow-hidden rounded-panel border border-line bg-surface">
          <!-- Title -->
          <div class="border-b border-line px-6 pt-6 md:px-8">
            <input
              v-model="title"
              maxlength="120"
              :placeholder="t('ideas.editor.titlePlaceholder')"
              class="w-full bg-transparent text-2xl font-bold text-content outline-none placeholder:text-content-subtle md:text-3xl"
            />
            <div class="mt-2 h-1 w-full rounded-full" :class="titleCount > 120 ? 'bg-danger' : 'bg-gradient-primary'" :style="{ width: Math.min(100, (titleCount / 120) * 100) + '%' }"></div>
            <div class="mt-1 flex justify-between text-xs text-content-subtle">
              <span>{{ t('ideas.editor.titleHint') }}</span>
              <span>{{ titleCount }}/120</span>
            </div>
          </div>

          <!-- Summary -->
          <div class="border-b border-line px-6 py-4 md:px-8">
            <textarea
              v-model="summary"
              maxlength="500"
              rows="2"
              :placeholder="t('ideas.editor.summaryPlaceholder')"
              class="w-full resize-none bg-transparent text-sm leading-relaxed text-content-muted outline-none placeholder:text-content-subtle"
            ></textarea>
          </div>

          <!-- Tags -->
          <div class="border-b border-line px-6 py-4 md:px-8">
            <div class="flex flex-wrap items-center gap-2">
              <span
                v-for="tag in tags"
                :key="tag"
                class="inline-flex items-center gap-1 rounded-full bg-brand-soft px-3 py-1 text-sm font-medium text-brand-strong"
              >
                {{ tag }}
                <button class="text-brand/60 transition-colors hover:text-danger" :aria-label="t('ideas.editor.removeTag', { tag })" @click="removeTag(tag)">
                  <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 18L18 6M6 6l12 12" /></svg>
                </button>
              </span>
              <input
                v-model="tagInput"
                :placeholder="t('ideas.editor.tagPlaceholder')"
                class="min-w-[180px] flex-1 bg-transparent text-sm text-content outline-none placeholder:text-content-subtle"
                @keydown="onTagKeydown"
                @blur="addTag(tagInput)"
              />
            </div>
            <div v-if="availableTags.length" class="mt-3 flex flex-wrap gap-1.5">
              <button
                v-for="t in availableTags"
                :key="t.id"
                class="rounded-full border px-2.5 py-0.5 text-xs transition-all"
                :class="tags.includes(t.name) ? 'border-brand bg-brand text-content-inverse' : 'border-line bg-surface-subtle text-content-muted hover:border-brand/40 hover:text-brand'"
                @click="toggleAvailableTag(t.name)"
              >{{ t.name }}</button>
            </div>
          </div>

          <!-- Markdown body -->
          <div>
            <!-- Toolbar -->
            <div class="flex flex-wrap items-center gap-1 border-b border-line bg-surface-subtle/60 px-4 py-2 md:px-6">
              <button
                v-for="tool in toolbarTools"
                :key="tool.key"
                class="rounded px-2 py-1 text-xs font-semibold text-content-muted transition-colors hover:bg-surface-hover hover:text-content"
                :title="tool.label"
                @click="applyTool(tool)"
              >{{ tool.icon }}</button>
              <span class="flex-1"></span>
              <div class="inline-flex rounded-control border border-line bg-surface p-0.5">
                <button class="rounded px-2.5 py-1 text-xs font-medium" :class="mode === 'editor' ? 'bg-surface text-brand shadow-sm' : 'text-content-muted'" @click="mode = 'editor'">{{ t('ideas.editor.editorTab') }}</button>
                <button class="rounded px-2.5 py-1 text-xs font-medium" :class="mode === 'preview' ? 'bg-surface text-brand shadow-sm' : 'text-content-muted'" @click="mode = 'preview'">{{ t('ideas.editor.previewTab') }}</button>
              </div>
            </div>

            <div class="grid md:grid-cols-2">
              <!-- Editor -->
              <div :class="mode === 'editor' ? 'block' : 'hidden md:block'" class="relative min-h-[420px] md:border-r md:border-line">
                <textarea
                  ref="bodyTextarea"
                  v-model="body"
                  maxlength="20000"
                  :placeholder="t('ideas.editor.bodyPlaceholder')"
                  class="h-full min-h-[420px] w-full resize-y bg-transparent px-6 py-5 font-mono text-sm leading-relaxed text-content outline-none placeholder:text-content-subtle md:px-8"
                ></textarea>
                <div class="pointer-events-none absolute bottom-3 right-4 text-xs text-content-subtle">{{ bodyCount }}/20000</div>
              </div>
              <!-- Preview -->
              <div :class="mode === 'preview' ? 'block' : 'hidden md:block'" class="markdown-preview min-h-[420px] px-6 py-5 md:border-l md:border-line md:px-8">
                <div v-if="body.trim()" class="prose-preview" v-html="renderedHtml"></div>
                <div v-else class="flex h-full min-h-[420px] items-center justify-center text-sm text-content-subtle">{{ t('ideas.editor.previewEmpty') }}</div>
              </div>
            </div>
          </div>
        </div>

        <!-- Actions -->
        <div class="flex flex-wrap items-center justify-end gap-3">
          <button v-if="postStatus !== 'published'" class="inline-flex h-11 items-center gap-2 rounded-panel border border-line bg-surface px-5 text-sm font-medium text-content-muted transition-colors hover:text-content disabled:opacity-60" :disabled="saving" @click="save(false)">
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7.5A2.25 2.25 0 015.25 5.25h13.5A2.25 2.25 0 0121 7.5v9A2.25 2.25 0 0118.75 18.75H5.25A2.25 2.25 0 013 16.5v-9zM3 9.75h18" /></svg>
            {{ saving ? t('ideas.editor.savingDraft') : t('ideas.editor.saveDraft') }}
          </button>
          <button class="inline-flex h-11 items-center gap-2 rounded-panel bg-gradient-primary px-6 text-sm font-semibold text-white shadow-card transition-transform hover:-translate-y-0.5 disabled:opacity-60" :disabled="saving || !canSave" @click="save(true)">
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 12.75l6 6 9-13.5" /></svg>
            {{ postStatus === 'published'
              ? (saving ? t('ideas.editor.submittingRevision') : t('ideas.editor.submitRevision'))
              : (saving ? t('ideas.editor.submittingReview') : t('ideas.editor.submitReview')) }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.markdown-preview {
  overflow: auto;
}
.prose-preview :deep(h1) { font-size: 1.5rem; font-weight: 700; margin: 1em 0 0.4em; color: rgb(var(--ui-text)); }
.prose-preview :deep(h2) { font-size: 1.3rem; font-weight: 700; margin: 1em 0 0.4em; color: rgb(var(--ui-text)); }
.prose-preview :deep(h3) { font-size: 1.1rem; font-weight: 600; margin: 0.8em 0 0.3em; color: rgb(var(--ui-text)); }
.prose-preview :deep(p) { margin: 0.6em 0; line-height: 1.75; color: rgb(var(--ui-text-muted)); }
.prose-preview :deep(ul) { margin: 0.6em 0; padding-left: 1.5em; list-style: disc; color: rgb(var(--ui-text-muted)); }
.prose-preview :deep(ol) { margin: 0.6em 0; padding-left: 1.5em; list-style: decimal; color: rgb(var(--ui-text-muted)); }
.prose-preview :deep(blockquote) { margin: 0.8em 0; border-left: 3px solid rgb(var(--ui-brand)); padding: 0.4em 1em; color: rgb(var(--ui-text-subtle)); background: rgb(var(--ui-surface-subtle)); }
.prose-preview :deep(code) { background: rgb(var(--ui-surface-subtle)); padding: 0.15em 0.4em; border-radius: 0.25rem; font-size: 0.85em; color: rgb(var(--ui-text)); }
.prose-preview :deep(pre) { margin: 0.8em 0; padding: 1em; background: rgb(var(--ui-surface-subtle)); border: 1px solid rgb(var(--ui-border)); border-radius: 0.5rem; overflow-x: auto; }
.prose-preview :deep(pre code) { background: transparent; padding: 0; }
.prose-preview :deep(a) { color: rgb(var(--ui-brand)); text-decoration: underline; }
.prose-preview :deep(img) { max-width: 100%; border-radius: 0.5rem; }
.prose-preview :deep(hr) { border: none; border-top: 1px solid rgb(var(--ui-border)); margin: 1em 0; }
.prose-preview :deep(strong) { color: rgb(var(--ui-text)); }
</style>
