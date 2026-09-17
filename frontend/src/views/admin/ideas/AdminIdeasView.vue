<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  adminListIdeas,
  adminApproveIdea,
  adminRejectIdea,
  adminHideIdea,
  adminRestoreIdea,
  adminRetryModeration,
  adminListReports,
  adminResolveReport,
  adminListTags,
  adminCreateTag,
  adminUpdateTag,
  adminMergeTags,
} from '@/api/admin/ideas'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import { useAppStore } from '@/stores/app'
import type { IdeaPost, IdeaReport, IdeaTag } from '@/types/ideas'
import { useI18n } from 'vue-i18n'

const { t, te } = useI18n()

const appStore = useAppStore()

const tab = ref<'posts' | 'reports' | 'tags'>('posts')

// Posts
const posts = ref<IdeaPost[]>([])
const postLoading = ref(false)
const postError = ref('')
const keyword = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

// Reports
const reports = ref<IdeaReport[]>([])
const reportLoading = ref(false)
const reportFilter = ref('pending')

// Tags
const tags = ref<IdeaTag[]>([])
const tagLoading = ref(false)
const newTagName = ref('')

// Dialogs
const rejectOpen = ref(false)
const rejectPostId = ref<number | null>(null)
const rejectReason = ref('')
const rejectActing = ref(false)

const resolveOpen = ref(false)
const resolveReport = ref<IdeaReport | null>(null)
const resolveResolution = ref('')
const resolveActing = ref(false)

const editTagOpen = ref(false)
const editTag = ref<IdeaTag | null>(null)
const editTagName = ref('')

const mergeOpen = ref(false)
const mergeSource = ref<IdeaTag | null>(null)
const mergeTargetId = ref<number | null>(null)
const merging = ref(false)

const statusCls: Record<string, string> = {
  draft: 'bg-surface-subtle text-content-muted border-line',
  pending_review: 'bg-brand-soft text-brand border-brand/30',
  manual_review: 'bg-amber-50 text-amber-600 border-amber-200 dark:bg-amber-500/10 dark:text-amber-300',
  published: 'bg-green-50 text-green-600 border-green-200 dark:bg-green-500/10 dark:text-green-300',
  pending_revision: 'bg-blue-50 text-blue-600 border-blue-200 dark:bg-blue-500/10 dark:text-blue-300',
  rejected: 'bg-red-50 text-red-600 border-red-200 dark:bg-red-500/10 dark:text-red-300',
  hidden: 'bg-surface-subtle text-content-subtle border-line',
  moderation_failed: 'bg-red-50 text-red-600 border-red-200 dark:bg-red-500/10 dark:text-red-300',
  deleted: 'bg-surface-subtle text-content-subtle border-line',
}

const statusOf = (s: string) => ({
  text: te(`ideas.status.${s}`) ? t(`ideas.status.${s}`) : s,
  cls: statusCls[s] || 'bg-surface-subtle text-content-muted border-line',
})

const activeMergeTargets = computed(() => tags.value.filter((t) => t.id !== mergeSource.value?.id && t.status === 'active'))

async function loadPosts() {
  postLoading.value = true
  postError.value = ''
  try {
    const res = await adminListIdeas({ keyword: keyword.value.trim() || undefined, page: page.value, page_size: pageSize.value })
    posts.value = res.items
    total.value = res.total
  } catch (e: any) {
    postError.value = e?.message || t('ideas.common.loadFailed')
    posts.value = []
    total.value = 0
  } finally {
    postLoading.value = false
  }
}

async function loadReports() {
  reportLoading.value = true
  try {
    const res = await adminListReports({ status: reportFilter.value })
    reports.value = res.items
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.loadReportsFailed'))
  } finally {
    reportLoading.value = false
  }
}

async function loadTags() {
  tagLoading.value = true
  try {
    tags.value = await adminListTags()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.loadTagsFailed'))
  } finally {
    tagLoading.value = false
  }
}

function switchTab(t: 'posts' | 'reports' | 'tags') {
  tab.value = t
  if (t === 'posts') loadPosts()
  if (t === 'reports') loadReports()
  if (t === 'tags') loadTags()
}

function onPage(p: number) {
  page.value = p
  loadPosts()
}

function onPageSize(ps: number) {
  pageSize.value = ps
  page.value = 1
  loadPosts()
}

function searchPosts() {
  page.value = 1
  loadPosts()
}

async function approve(id: number) {
  try {
    await adminApproveIdea(id)
    appStore.showSuccess(t('ideas.admin.approveSuccess'))
    loadPosts()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.actionFailed'))
  }
}

function openReject(id: number) {
  rejectPostId.value = id
  rejectReason.value = ''
  rejectOpen.value = true
}

async function confirmReject() {
  if (!rejectPostId.value || !rejectReason.value.trim() || rejectActing.value) return
  rejectActing.value = true
  try {
    await adminRejectIdea(rejectPostId.value, rejectReason.value.trim())
    rejectOpen.value = false
    appStore.showSuccess(t('ideas.admin.rejectSuccess'))
    loadPosts()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.rejectFailed'))
  } finally {
    rejectActing.value = false
  }
}

async function hide(id: number) {
  try {
    await adminHideIdea(id)
    appStore.showSuccess(t('ideas.admin.delistSuccess'))
    loadPosts()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.delistFailed'))
  }
}

async function restore(id: number) {
  try {
    await adminRestoreIdea(id)
    appStore.showSuccess(t('ideas.admin.restoreSuccess'))
    loadPosts()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.restoreFailed'))
  }
}

async function retry(id: number) {
  try {
    await adminRetryModeration(id)
    appStore.showSuccess(t('ideas.admin.resubmitSuccess'))
    loadPosts()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.resubmitFailed'))
  }
}

function openResolve(r: IdeaReport) {
  resolveReport.value = r
  resolveResolution.value = ''
  resolveOpen.value = true
}

async function confirmResolve() {
  if (!resolveReport.value || resolveActing.value) return
  resolveActing.value = true
  try {
    await adminResolveReport(resolveReport.value.id, resolveResolution.value.trim())
    resolveOpen.value = false
    appStore.showSuccess(t('ideas.admin.reportResolved'))
    loadReports()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.reportResolveFailed'))
  } finally {
    resolveActing.value = false
  }
}

async function createTag() {
  if (!newTagName.value.trim()) return
  try {
    await adminCreateTag(newTagName.value.trim())
    newTagName.value = ''
    appStore.showSuccess(t('ideas.admin.tagCreated'))
    loadTags()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.createFailed'))
  }
}

function openEditTag(t: IdeaTag) {
  editTag.value = t
  editTagName.value = t.name
  editTagOpen.value = true
}

async function confirmEditTag() {
  if (!editTag.value) return
  const name = editTagName.value.trim()
  if (!name) {
    appStore.showWarning(t('ideas.admin.tagNameRequired'))
    return
  }
  try {
    await adminUpdateTag(editTag.value.id, { name })
    editTagOpen.value = false
    appStore.showSuccess(t('ideas.admin.tagUpdated'))
    loadTags()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.updateFailed'))
  }
}

async function toggleTag(id: number, current: string) {
  try {
    await adminUpdateTag(id, { status: current === 'active' ? 'disabled' : 'active' })
    appStore.showSuccess(current === 'active' ? t('ideas.admin.tagDisabled') : t('ideas.admin.tagEnabled'))
    loadTags()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.actionFailed'))
  }
}

function openMerge(t: IdeaTag) {
  mergeSource.value = t
  mergeTargetId.value = null
  mergeOpen.value = true
}

async function confirmMerge() {
  if (!mergeSource.value || !mergeTargetId.value || merging.value) return
  merging.value = true
  try {
    await adminMergeTags(mergeSource.value.id, mergeTargetId.value)
    mergeOpen.value = false
    appStore.showSuccess(t('ideas.admin.tagMerged'))
    loadTags()
  } catch (e: any) {
    appStore.showError(e?.message || t('ideas.admin.mergeFailed'))
  } finally {
    merging.value = false
  }
}

function formatDate(s?: string) {
  if (!s) return ''
  return new Date(s).toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

onMounted(() => loadPosts())
</script>

<template>
  <div class="min-h-full bg-canvas text-content">
    <div class="mx-auto max-w-6xl px-4 pb-16 pt-6 md:px-6">
      <div class="mb-6">
        <h1 class="text-2xl font-bold text-content">{{ t('ideas.admin.title') }}</h1>
        <p class="mt-1 text-sm text-content-muted">{{ t('ideas.admin.subtitle') }}</p>
      </div>

      <!-- Tabs -->
      <div class="mb-6 inline-flex rounded-control border border-line bg-surface p-1">
        <button
          v-for="tb in ([[ 'posts', t('ideas.admin.tabPosts') ], [ 'reports', t('ideas.admin.tabReports') ], [ 'tags', t('ideas.admin.tabTags') ]] as const)"
          :key="tb[0]"
          class="rounded-md px-4 py-2 text-sm font-medium transition-colors"
          :class="tab === tb[0] ? 'bg-surface text-brand shadow-sm' : 'text-content-muted hover:text-content'"
          @click="switchTab(tb[0])"
        >{{ tb[1] }}</button>
      </div>

      <!-- ============ Posts ============ -->
      <div v-if="tab === 'posts'" class="rounded-panel border border-line bg-surface">
        <div class="flex flex-wrap items-center gap-3 border-b border-line p-4">
          <div class="relative flex-1 min-w-[200px]">
            <svg class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-content-subtle" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" /></svg>
            <input v-model="keyword" type="search" :placeholder="t('ideas.admin.searchPlaceholder')" class="h-9 w-full rounded-control border border-line bg-surface-subtle pl-9 pr-3 text-sm outline-none placeholder:text-content-subtle focus:border-brand/60" @keyup.enter="searchPosts" />
          </div>
        </div>

        <div v-if="postLoading" class="space-y-2 p-4">
          <div v-for="i in 5" :key="i" class="flex items-center gap-4 rounded-panel border border-line p-4"><Skeleton height="18" width="30%" /><Skeleton height="14" width="15%" /><Skeleton height="18" width="60" class="ml-auto" /></div>
        </div>
        <div v-else-if="postError" class="p-8"><EmptyState :title="postError" description="内容暂时无法加载"><template #action><button class="inline-flex h-10 items-center rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse hover:bg-brand-strong" @click="loadPosts">{{ t('common.tryAgain') }}</button></template></EmptyState></div>
        <div v-else-if="posts.length === 0" class="p-8"><EmptyState :title="t('ideas.admin.noArticles')" description="符合筛选条件的文章为空" /></div>
        <div v-else class="divide-y divide-line">
          <div v-for="p in posts" :key="p.id" class="flex flex-wrap items-center gap-4 px-4 py-3.5 transition-colors hover:bg-surface-subtle/60">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-xs text-content-subtle">#{{ p.id }}</span>
                <span :class="['rounded-full border px-2 py-0.5 text-xs font-medium', statusOf(p.status).cls]">{{ statusOf(p.status).text }}</span>
              </div>
              <p class="mt-1 line-clamp-1 text-sm font-medium text-content">{{ p.revision?.title || t('ideas.admin.unnamed') }}</p>
              <p class="mt-0.5 text-xs text-content-subtle">{{ p.author_name }} · {{ formatDate(p.updated_at) }}</p>
            </div>
            <div class="flex flex-wrap items-center gap-1.5">
              <button v-if="['pending_review','manual_review','pending_revision','moderation_failed'].includes(p.status)" class="inline-flex h-8 items-center gap-1 rounded-control bg-brand px-3 text-xs font-semibold text-content-inverse hover:bg-brand-strong" @click="approve(p.id)">{{ t('ideas.admin.approve') }}</button>
              <button v-if="['pending_review','manual_review','pending_revision'].includes(p.status)" class="inline-flex h-8 items-center gap-1 rounded-control border border-danger/30 bg-surface px-3 text-xs font-medium text-danger hover:bg-danger/10" @click="openReject(p.id)">{{ t('ideas.admin.reject') }}</button>
              <button v-if="p.status === 'published'" class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted hover:text-danger" @click="hide(p.id)">{{ t('ideas.admin.takedown') }}</button>
              <button v-if="p.status === 'hidden'" class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted hover:text-brand" @click="restore(p.id)">{{ t('ideas.admin.restore') }}</button>
              <button v-if="['pending_review','moderation_failed','manual_review','pending_revision'].includes(p.status)" class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted hover:text-brand" @click="retry(p.id)">{{ t('ideas.admin.retryAi') }}</button>
            </div>
          </div>
        </div>

        <div v-if="!postLoading && !postError && total > pageSize" class="border-t border-line p-4">
          <Pagination :total="total" :page="page" :page-size="pageSize" @update:page="onPage" @update:page-size="onPageSize" />
        </div>
      </div>

      <!-- ============ Reports ============ -->
      <div v-else-if="tab === 'reports'" class="rounded-panel border border-line bg-surface">
        <div class="flex flex-wrap items-center gap-3 border-b border-line p-4">
          <div class="inline-flex rounded-control border border-line bg-surface-subtle p-0.5">
            <button v-for="rf in (['pending','resolved'] as const)" :key="rf" class="rounded px-3 py-1 text-sm font-medium" :class="reportFilter === rf ? 'bg-surface text-brand shadow-sm' : 'text-content-muted'" @click="reportFilter = rf; loadReports()">{{ rf === 'pending' ? t('ideas.admin.pending') : t('ideas.admin.handled') }}</button>
          </div>
        </div>

        <div v-if="reportLoading" class="space-y-2 p-4"><div v-for="i in 3" :key="i" class="rounded-panel border border-line p-4"><Skeleton height="18" width="40%" /><Skeleton height="14" width="60%" class="mt-2" /></div></div>
        <div v-else-if="reports.length === 0" class="p-8"><EmptyState :title="reportFilter === 'pending' ? t('ideas.admin.noPendingReports') : t('ideas.admin.noHandledReports')" description="社区很干净" /></div>
        <div v-else class="divide-y divide-line">
          <div v-for="r in reports" :key="r.id" class="flex flex-wrap items-center gap-4 px-4 py-4">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-xs text-content-subtle">#{{ r.id }}</span>
                <span v-if="r.reason" class="rounded-full bg-danger/10 px-2 py-0.5 text-xs font-medium text-danger">{{ r.reason }}</span>
              </div>
              <p class="mt-1 line-clamp-1 text-sm font-medium text-content">《{{ r.post_title || t('ideas.admin.unknownArticle') }}》</p>
              <p v-if="r.detail" class="mt-1 line-clamp-1 text-xs text-content-muted">{{ r.detail }}</p>
              <p class="mt-1 text-xs text-content-subtle">{{ t('ideas.admin.reportedAt', { createdAt: formatDate(r.created_at) }) }}</p>
            </div>
            <button v-if="r.status === 'pending'" class="inline-flex h-9 items-center gap-1.5 rounded-control border border-brand/30 bg-brand-soft px-4 text-sm font-medium text-brand hover:bg-brand/10" @click="openResolve(r)">{{ t('ideas.admin.handle') }}</button>
            <span v-else class="text-xs text-content-subtle">{{ t('ideas.admin.handled') }}</span>
          </div>
        </div>
      </div>

      <!-- ============ Tags ============ -->
      <div v-else class="rounded-panel border border-line bg-surface">
        <div class="flex flex-wrap items-center gap-3 border-b border-line p-4">
          <input v-model="newTagName" :placeholder="t('ideas.admin.newTagPlaceholder')" class="h-9 flex-1 min-w-[180px] rounded-control border border-line bg-surface-subtle px-3 text-sm outline-none focus:border-brand/60" @keyup.enter="createTag" />
          <button class="inline-flex h-9 items-center gap-1.5 rounded-control bg-brand px-4 text-sm font-semibold text-content-inverse hover:bg-brand-strong" @click="createTag">
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>{{ t('ideas.admin.newTag') }}
          </button>
        </div>

        <div v-if="tagLoading" class="space-y-2 p-4"><div v-for="i in 4" :key="i" class="rounded-panel border border-line p-4"><Skeleton height="18" width="20%" /><Skeleton height="14" width="30%" class="mt-2" /></div></div>
        <div v-else-if="tags.length === 0" class="p-8"><EmptyState :title="t('ideas.admin.noTags')" description="创建一个标签，方便内容归类" /></div>
        <div v-else class="divide-y divide-line">
          <div v-for="tag in tags" :key="tag.id" class="flex flex-wrap items-center gap-4 px-4 py-3.5">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-sm font-medium text-content">{{ tag.name }}</span>
                <span :class="['rounded-full border px-2 py-0.5 text-xs font-medium', tag.status === 'active' ? 'bg-green-50 text-green-600 border-green-200 dark:bg-green-500/10 dark:text-green-300' : 'bg-surface-subtle text-content-subtle border-line']">{{ tag.status === 'active' ? t('common.enable') : t('common.disable') }}</span>
              </div>
              <p class="mt-0.5 text-xs text-content-subtle">{{ t('ideas.admin.tagSlugUsage', { slug: tag.slug, usageCount: tag.usage_count }) }}</p>
            </div>
            <div class="flex items-center gap-1.5">
              <button class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted hover:text-brand" @click="openEditTag(tag)">{{ t('common.edit') }}</button>
              <button class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted hover:text-brand" @click="openMerge(tag)">{{ t('ideas.admin.merge') }}</button>
              <button class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium" :class="tag.status === 'active' ? 'text-content-muted hover:text-danger' : 'text-content-muted hover:text-brand'" @click="toggleTag(tag.id, tag.status)">{{ tag.status === 'active' ? t('common.disable') : t('common.enable') }}</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Reject dialog -->
      <BaseDialog :show="rejectOpen" :title="t('ideas.admin.rejectTitle')" width="narrow" @close="rejectOpen = false">
        <div class="space-y-4">
          <p class="text-sm text-content-muted">{{ t('ideas.admin.rejectReasonDesc') }}</p>
          <textarea v-model="rejectReason" rows="4" :placeholder="t('ideas.admin.rejectReasonPlaceholder')" class="w-full rounded-control border border-line bg-surface-subtle px-3 py-2 text-sm outline-none focus:border-brand/60"></textarea>
          <div class="flex justify-end gap-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm text-content-muted hover:text-content" @click="rejectOpen = false">{{ t('common.cancel') }}</button>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-danger px-5 text-sm font-semibold text-content-inverse disabled:opacity-60" :disabled="!rejectReason.trim() || rejectActing" @click="confirmReject">
              <span v-if="rejectActing" class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>{{ rejectActing ? t('common.submitting') : t('ideas.admin.confirmReject') }}
            </button>
          </div>
        </div>
      </BaseDialog>

      <!-- Resolve dialog -->
      <BaseDialog :show="resolveOpen" :title="t('ideas.admin.reportTitle')" width="narrow" @close="resolveOpen = false">
        <div v-if="resolveReport" class="space-y-4">
          <div class="rounded-panel border border-line bg-surface-subtle p-3 text-sm">
            <p class="font-medium text-content">《{{ resolveReport.post_title || t('ideas.admin.unknownArticle') }}》</p>
            <p class="mt-1 text-content-muted">{{ t('ideas.admin.reportReason', { reason: resolveReport.reason }) }}</p>
            <p v-if="resolveReport.detail" class="mt-1 text-content-muted">{{ t('ideas.admin.reportDetail', { detail: resolveReport.detail }) }}</p>
          </div>
          <textarea v-model="resolveResolution" rows="3" :placeholder="t('ideas.admin.reportNotePlaceholder')" class="w-full rounded-control border border-line bg-surface-subtle px-3 py-2 text-sm outline-none focus:border-brand/60"></textarea>
          <div class="flex justify-end gap-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm text-content-muted hover:text-content" @click="resolveOpen = false">{{ t('common.cancel') }}</button>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse disabled:opacity-60" :disabled="resolveActing" @click="confirmResolve">
              <span v-if="resolveActing" class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>{{ resolveActing ? t('common.processing') : t('ideas.admin.confirmHandle') }}
            </button>
          </div>
        </div>
      </BaseDialog>

      <!-- Edit tag dialog -->
      <BaseDialog :show="editTagOpen" :title="t('ideas.admin.editTagTitle')" width="narrow" @close="editTagOpen = false">
        <div class="space-y-4">
          <input v-model="editTagName" :placeholder="t('ideas.admin.tagName')" class="h-10 w-full rounded-control border border-line bg-surface-subtle px-3 text-sm outline-none focus:border-brand/60" @keyup.enter="confirmEditTag" />
          <div class="flex justify-end gap-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm text-content-muted hover:text-content" @click="editTagOpen = false">{{ t('common.cancel') }}</button>
            <button class="inline-flex h-10 items-center rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse disabled:opacity-60" :disabled="!editTagName.trim()" @click="confirmEditTag">{{ t('common.save') }}</button>
          </div>
        </div>
      </BaseDialog>

      <!-- Merge tag dialog -->
      <BaseDialog :show="mergeOpen" :title="t('ideas.admin.mergeTagTitle')" width="narrow" @close="mergeOpen = false">
        <div v-if="mergeSource" class="space-y-4">
          <p class="text-sm text-content-muted">{{ t('ideas.admin.mergePrefix') }}<span class="font-medium text-content">{{ mergeSource.name }}</span>{{ t('ideas.admin.mergeSuffix') }}</p>
          <select v-model="mergeTargetId" class="h-10 w-full rounded-control border border-line bg-surface-subtle px-3 text-sm outline-none focus:border-brand/60">
            <option :value="null" disabled>{{ t('ideas.admin.mergeTargetPlaceholder') }}</option>
            <option v-for="tag in activeMergeTargets" :key="tag.id" :value="tag.id">{{ tag.name }}（{{ tag.usage_count }}）</option>
          </select>
          <div class="flex justify-end gap-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm text-content-muted hover:text-content" @click="mergeOpen = false">{{ t('common.cancel') }}</button>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse disabled:opacity-60" :disabled="!mergeTargetId || merging" @click="confirmMerge">
              <span v-if="merging" class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>{{ merging ? t('ideas.admin.merging') : t('ideas.admin.confirmMerge') }}
            </button>
          </div>
        </div>
      </BaseDialog>
    </div>
  </div>
</template>

<style scoped>
.line-clamp-1 {
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
