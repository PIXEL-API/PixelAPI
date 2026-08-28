<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { listMyIdeas, publishIdea, deleteIdea } from '@/api/ideas'
import EmptyState from '@/components/common/EmptyState.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAppStore } from '@/stores/app'
import type { IdeaPost } from '@/types/ideas'

const router = useRouter()
const appStore = useAppStore()

const posts = ref<IdeaPost[]>([])
const loading = ref(false)
const error = ref('')
const statusFilter = ref('all')
const actingId = ref<number | null>(null)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

const statusLabels: Record<string, { text: string; cls: string }> = {
  draft: { text: '草稿', cls: 'bg-surface-subtle text-content-muted border-line' },
  pending_review: { text: '待审核', cls: 'bg-brand-soft text-brand border-brand/30' },
  manual_review: { text: '人工审核', cls: 'bg-amber-50 text-amber-600 border-amber-200 dark:bg-amber-500/10 dark:text-amber-300' },
  published: { text: '已发布', cls: 'bg-green-50 text-green-600 border-green-200 dark:bg-green-500/10 dark:text-green-300' },
  pending_revision: { text: '修订待审', cls: 'bg-blue-50 text-blue-600 border-blue-200 dark:bg-blue-500/10 dark:text-blue-300' },
  rejected: { text: '已拒绝', cls: 'bg-red-50 text-red-600 border-red-200 dark:bg-red-500/10 dark:text-red-300' },
  hidden: { text: '已下架', cls: 'bg-surface-subtle text-content-subtle border-line' },
  moderation_failed: { text: '审核失败', cls: 'bg-red-50 text-red-600 border-red-200 dark:bg-red-500/10 dark:text-red-300' },
  deleted: { text: '已删除', cls: 'bg-surface-subtle text-content-subtle border-line' },
}

const filters = [
  { key: 'all', label: '全部' },
  { key: 'draft', label: '草稿' },
  { key: 'pending_review', label: '待审核' },
  { key: 'published', label: '已发布' },
  { key: 'rejected', label: '已拒绝' },
]

const filtered = computed(() =>
  statusFilter.value === 'all' ? posts.value : posts.value.filter((p) => p.status === statusFilter.value)
)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await listMyIdeas({ page: page.value, page_size: pageSize.value })
    posts.value = res.items
    total.value = res.total
  } catch (e: any) {
    error.value = e?.message || '加载失败'
    posts.value = []
  } finally {
    loading.value = false
  }
}

function onPage(p: number) {
  page.value = p
  load()
}

function onPageSize(ps: number) {
  pageSize.value = ps
  page.value = 1
  load()
}

function statusOf(p: IdeaPost) {
  return statusLabels[p.status] || { text: p.status, cls: 'bg-surface-subtle text-content-muted border-line' }
}

async function publish(p: IdeaPost) {
  actingId.value = p.id
  try {
    await publishIdea(p.id)
    appStore.showSuccess('已提交审核，请耐心等待')
    await load()
  } catch (e: any) {
    appStore.showError(e?.message || '发布失败')
  } finally {
    actingId.value = null
  }
}

async function remove(p: IdeaPost) {
  if (!window.confirm(`确定删除《${p.revision?.title || '这篇文章'}》吗？删除后不可恢复。`)) return
  actingId.value = p.id
  try {
    await deleteIdea(p.id)
    appStore.showSuccess('文章已删除')
    await load()
  } catch (e: any) {
    appStore.showError(e?.message || '删除失败')
  } finally {
    actingId.value = null
  }
}

function formatDate(s?: string) {
  if (!s) return ''
  return new Date(s).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

function formatCount(n: number) {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

onMounted(load)
</script>

<template>
  <div class="min-h-full bg-canvas text-content">
    <div class="mx-auto max-w-4xl px-4 pb-16 pt-6 md:px-6">
      <!-- Header -->
      <div class="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 class="text-2xl font-bold text-content">我的文章</h1>
          <p class="mt-1 text-sm text-content-muted">管理你发布的想法，跟踪审核进度</p>
        </div>
        <RouterLink
          to="/ideas/new"
          class="inline-flex h-10 items-center gap-2 rounded-panel bg-gradient-primary px-5 text-sm font-semibold text-white shadow-card transition-transform hover:-translate-y-0.5"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>
          写文章
        </RouterLink>
      </div>

      <!-- Filter tabs -->
      <div class="mb-4 flex flex-wrap gap-2">
        <button
          v-for="f in filters"
          :key="f.key"
          class="rounded-full border px-3.5 py-1.5 text-sm font-medium transition-all"
          :class="statusFilter === f.key ? 'border-brand bg-brand text-content-inverse' : 'border-line bg-surface text-content-muted hover:border-brand/40 hover:text-brand'"
          @click="statusFilter = f.key"
        >{{ f.label }}</button>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="space-y-3">
        <div v-for="i in 3" :key="i" class="rounded-panel border border-line bg-surface p-5"><Skeleton height="22" width="50%" class="mb-3" /><Skeleton height="14" width="30%" /></div>
      </div>

      <!-- Error -->
      <div v-else-if="error" class="rounded-panel border border-line bg-surface p-10">
        <EmptyState :title="error" description="内容暂时无法加载，请稍后重试">
          <template #icon><svg class="h-10 w-10 text-danger" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" /></svg></template>
          <template #action><button class="inline-flex h-10 items-center rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse hover:bg-brand-strong" @click="load">重试</button></template>
        </EmptyState>
      </div>

      <!-- Empty -->
      <div v-else-if="filtered.length === 0" class="rounded-panel border border-line bg-surface p-10">
        <EmptyState :title="statusFilter === 'all' ? '还没有写过文章' : '该分类下暂无文章'" :description="statusFilter === 'all' ? '写下你的第一个想法，与大家分享' : '换个分类看看'">
          <template #icon><svg class="h-10 w-10 text-brand" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 18v-5.25m0 0a6.01 6.01 0 001.5-.189m-1.5.189a6.01 6.01 0 01-1.5-.189m3.75 7.478a12.06 12.06 0 01-4.5 0m3.75 2.383a14.406 14.406 0 01-3 0M14.25 18v-.192c0-.983.658-1.823 1.508-2.316a7.5 7.5 0 10-7.517 0c.85.493 1.509 1.333 1.509 2.316V18" /></svg></template>
          <template #action>
            <RouterLink to="/ideas/new" class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse hover:bg-brand-strong">
              <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>写文章
            </RouterLink>
          </template>
        </EmptyState>
      </div>

      <!-- List -->
      <div v-else class="space-y-3">
        <div
          v-for="p in filtered"
          :key="p.id"
          class="group flex flex-wrap items-center gap-4 rounded-panel border border-line bg-surface p-5 transition-all hover:border-brand/40 hover:shadow-card-hover"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <RouterLink :to="`/ideas/${p.id}`" class="line-clamp-1 text-base font-semibold text-content transition-colors hover:text-brand">{{ p.revision?.title || '未命名' }}</RouterLink>
              <span :class="['rounded-full border px-2.5 py-0.5 text-xs font-medium', statusOf(p).cls]">{{ statusOf(p).text }}</span>
            </div>
            <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-content-subtle">
              <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178zM15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>{{ formatCount(p.view_count) }} 阅读</span>
              <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12z" /></svg>{{ formatCount(p.like_count) }} 赞</span>
              <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M17.593 3.322c1.1.128 1.907 1.077 1.907 2.185V21L12 17.25 4.5 21V5.507c0-1.108.806-2.057 1.907-2.185a48.507 48.507 0 0111.186 0z" /></svg>{{ formatCount(p.favorite_count) }} 收藏</span>
              <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>{{ formatDate(p.updated_at) }}</span>
            </div>
          </div>

          <div class="flex items-center gap-1.5">
            <button
              v-if="['draft','published','rejected'].includes(p.status)"
              class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted transition-colors hover:border-brand/40 hover:text-brand"
              @click="router.push(`/ideas/${p.id}/edit`)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zM15 12.75V18a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 18V7.5A2.25 2.25 0 015.25 5.25H10.5" /></svg>
              编辑
            </button>
            <button
              v-if="p.status === 'draft' || p.status === 'rejected'"
              class="inline-flex h-8 items-center gap-1 rounded-control bg-brand px-3 text-xs font-semibold text-content-inverse transition-colors disabled:opacity-60"
              :disabled="actingId === p.id"
              @click="publish(p)"
            >
              <span v-if="actingId === p.id" class="h-3 w-3 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
              发布
            </button>
            <button
              class="inline-flex h-8 items-center gap-1 rounded-control border border-line bg-surface px-3 text-xs font-medium text-content-muted transition-colors hover:border-danger/40 hover:text-danger disabled:opacity-60"
              :disabled="actingId === p.id"
              @click="remove(p)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" /></svg>
              删除
            </button>
          </div>
        </div>
      </div>

      <!-- Pagination -->
      <div v-if="!loading && !error && filtered.length > 0 && total > pageSize" class="mt-8 flex justify-center">
        <Pagination :total="total" :page="page" :page-size="pageSize" @update:page="onPage" @update:page-size="onPageSize" />
      </div>

      <!-- acting spinner -->
      <div v-if="actingId && filtered.length === 0" class="flex justify-center py-8"><LoadingSpinner /></div>
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
