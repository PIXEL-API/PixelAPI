<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listIdeas, listIdeaTags } from '@/api/ideas'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import IdeasHeader from '@/components/ideas/IdeasHeader.vue'
import type { IdeaPost, IdeaTag } from '@/types/ideas'

const { t, locale } = useI18n()
const posts = ref<IdeaPost[]>([])
const tags = ref<IdeaTag[]>([])
const keyword = ref('')
const sort = ref<'latest' | 'hot'>('latest')
const tagSlug = ref('')
const page = ref(1)
const pageSize = ref(12)
const total = ref(0)
const loading = ref(false)
const error = ref('')
const sortOptions = computed(() => [
  { key: 'latest' as const, label: t('ideas.list.latest') },
  { key: 'hot' as const, label: t('ideas.list.hot') },
])

let debounceTimer: ReturnType<typeof setTimeout> | undefined
let requestSequence = 0
watch(keyword, () => {
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    page.value = 1
    load()
  }, 400)
})

const hasFilters = computed(
  () => keyword.value !== '' || tagSlug.value !== '' || sort.value !== 'latest'
)

async function load() {
  const requestId = ++requestSequence
  loading.value = true
  error.value = ''
  try {
    const res = await listIdeas({
      keyword: keyword.value.trim() || undefined,
      sort: sort.value,
      tag: tagSlug.value || undefined,
      page: page.value,
      page_size: pageSize.value,
    })
    if (requestId !== requestSequence) return
    posts.value = res.items
    total.value = res.total
  } catch (e: any) {
    if (requestId !== requestSequence) return
    error.value = e?.message || t('ideas.common.loadFailed')
    posts.value = []
    total.value = 0
  } finally {
    if (requestId === requestSequence) loading.value = false
  }
}

function resetAndLoad() {
  page.value = 1
  load()
}

function selectTag(slug: string) {
  tagSlug.value = tagSlug.value === slug ? '' : slug
  resetAndLoad()
}

function onPage(p: number) {
  page.value = p
  load()
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function onPageSize(ps: number) {
  pageSize.value = ps
  resetAndLoad()
}

function onSort(s: 'latest' | 'hot') {
  sort.value = s
  resetAndLoad()
}

function clearFilters() {
  keyword.value = ''
  tagSlug.value = ''
  sort.value = 'latest'
  resetAndLoad()
}

function formatRelative(s?: string) {
  if (!s) return ''
  const time = new Date(s).getTime()
  const diff = Date.now() - time
  const m = 60_000, h = 3_600_000, d = 86_400_000
  if (diff < m) return t('ideas.list.time.justNow')
  if (diff < h) return t('ideas.list.time.minutesAgo', { count: Math.floor(diff / m) })
  if (diff < d) return t('ideas.list.time.hoursAgo', { count: Math.floor(diff / h) })
  if (diff < 7 * d) return t('ideas.list.time.daysAgo', { count: Math.floor(diff / d) })
  return new Date(time).toLocaleDateString(locale.value)
}

function formatCount(n: number) {
  return new Intl.NumberFormat(locale.value, {
    notation: n >= 1000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(n)
}

function authorInitial(name?: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
}

async function loadTags() {
  try {
    tags.value = await listIdeaTags()
  } catch {
    tags.value = []
  }
}

onMounted(() => {
  void loadTags()
  void load()
})

onUnmounted(() => clearTimeout(debounceTimer))
</script>

<template>
  <div class="min-h-screen bg-canvas text-content">
    <IdeasHeader />
    <div class="mx-auto max-w-6xl px-4 pb-16 pt-6 md:px-6">
      <!-- Hero -->
      <section class="relative overflow-hidden rounded-panel border border-line bg-surface p-6 md:p-10">
        <div class="pointer-events-none absolute inset-0 bg-mesh-gradient" aria-hidden="true"></div>
        <div class="pointer-events-none absolute -right-16 -top-16 h-64 w-64 rounded-full bg-brand/10 blur-3xl" aria-hidden="true"></div>
        <div class="relative flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
          <div class="max-w-2xl">
            <div class="mb-3 inline-flex items-center gap-2 rounded-full border border-line bg-surface-subtle px-3 py-1 text-xs font-medium text-content-muted">
              <span class="h-2 w-2 rounded-full bg-brand"></span>
              {{ t('ideas.list.eyebrow') }}
            </div>
            <h1 class="text-3xl font-bold tracking-tight text-content md:text-4xl">{{ t('ideas.list.title') }}</h1>
            <p class="mt-3 max-w-xl text-sm leading-relaxed text-content-muted md:text-base">
              {{ t('ideas.list.description') }}
            </p>
          </div>
          <RouterLink
            to="/ideas/new"
            class="inline-flex h-11 shrink-0 items-center justify-center gap-2 rounded-panel bg-gradient-primary px-6 text-sm font-semibold text-white shadow-card transition-transform hover:-translate-y-0.5 hover:shadow-elevated"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
            {{ t('ideas.common.writePost') }}
          </RouterLink>
        </div>
      </section>

      <!-- Toolbar -->
      <div class="sticky top-16 z-20 mt-6 rounded-panel border border-line bg-surface/90 p-3 backdrop-blur-md md:p-4">
        <div class="flex flex-col gap-3 md:flex-row md:items-center">
          <div class="relative flex-1">
            <svg class="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-content-subtle" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" />
            </svg>
            <input
              v-model="keyword"
              type="search"
              :placeholder="t('ideas.list.searchPlaceholder')"
              :aria-label="t('ideas.list.searchAria')"
              class="h-10 w-full rounded-control border border-line bg-surface-subtle pl-10 pr-4 text-sm text-content outline-none transition-colors placeholder:text-content-subtle focus:border-brand/60 focus:ring-2 focus:ring-brand/20"
            />
          </div>
          <div class="flex items-center gap-2">
            <div class="inline-flex rounded-control border border-line bg-surface-subtle p-0.5">
              <button
                v-for="s in sortOptions"
                :key="s.key"
                class="rounded-[0.375rem] px-3.5 py-1.5 text-sm font-medium transition-colors"
                :class="sort === s.key ? 'bg-surface text-brand shadow-sm' : 'text-content-muted hover:text-content'"
                @click="onSort(s.key)"
              >
                {{ s.label }}
              </button>
            </div>
            <button
              v-if="hasFilters"
              class="inline-flex h-10 items-center gap-1.5 rounded-control px-3 text-sm text-content-muted transition-colors hover:text-content"
              @click="clearFilters"
            >
              <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
                <path d="M6 18L18 6M6 6l12 12" />
              </svg>
              {{ t('ideas.list.clearFilters') }}
            </button>
          </div>
        </div>

        <!-- Tags -->
        <div v-if="tags.length" class="mt-3 flex flex-wrap gap-2">
          <button
            v-for="t in tags"
            :key="t.id"
            class="inline-flex items-center gap-1 rounded-full border px-3 py-1 text-xs font-medium transition-all"
            :class="tagSlug === t.slug ? 'border-brand bg-brand text-content-inverse shadow-sm' : 'border-line bg-surface-subtle text-content-muted hover:border-brand/40 hover:text-brand'"
            @click="selectTag(t.slug)"
          >
            {{ t.name }}
          </button>
        </div>
      </div>

      <!-- Content -->
      <div class="mt-6">
        <!-- Loading skeletons -->
        <div v-if="loading" class="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          <div v-for="i in 6" :key="i" class="rounded-panel border border-line bg-surface p-5">
            <div class="mb-4 flex gap-2">
              <Skeleton width="56" height="20" class="rounded-full" />
              <Skeleton width="44" height="20" class="rounded-full" />
            </div>
            <Skeleton height="22" class="mb-3" />
            <Skeleton height="22" width="70%" class="mb-4" />
            <Skeleton height="14" class="mb-2" />
            <Skeleton height="14" width="60%" />
          </div>
        </div>

        <!-- Error state -->
        <div v-else-if="error" class="rounded-panel border border-line bg-surface p-8">
          <EmptyState :title="error" :description="t('ideas.common.loadUnavailable')">
            <template #icon>
              <svg class="h-10 w-10 text-danger" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
              </svg>
            </template>
            <template #action>
              <button class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse transition-colors hover:bg-brand-strong" @click="load">
                <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" />
                </svg>
                {{ t('ideas.common.retry') }}
              </button>
            </template>
          </EmptyState>
        </div>

        <!-- Empty state -->
        <div v-else-if="posts.length === 0" class="rounded-panel border border-line bg-surface p-8">
          <EmptyState :title="hasFilters ? t('ideas.list.emptyFilteredTitle') : t('ideas.list.emptyDefaultTitle')" :description="hasFilters ? t('ideas.list.emptyFilteredDescription') : t('ideas.list.emptyDefaultDescription')">
            <template #icon>
              <svg class="h-10 w-10 text-brand" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 18v-5.25m0 0a6.01 6.01 0 001.5-.189m-1.5.189a6.01 6.01 0 01-1.5-.189m3.75 7.478a12.06 12.06 0 01-4.5 0m3.75 2.383a14.406 14.406 0 01-3 0M14.25 18v-.192c0-.983.658-1.823 1.508-2.316a7.5 7.5 0 10-7.517 0c.85.493 1.509 1.333 1.509 2.316V18" />
              </svg>
            </template>
            <template #action>
              <RouterLink to="/ideas/new" class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse transition-colors hover:bg-brand-strong">
                <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14" /></svg>
                {{ t('ideas.common.writePost') }}
              </RouterLink>
            </template>
          </EmptyState>
        </div>

        <!-- Cards -->
        <div v-else class="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
          <RouterLink
            v-for="p in posts"
            :key="p.id"
            :to="`/ideas/${p.id}`"
            class="group flex flex-col rounded-panel border border-line bg-surface p-5 transition-all duration-200 hover:-translate-y-1 hover:border-brand/40 hover:shadow-elevated"
          >
            <div class="mb-3 flex flex-wrap items-center gap-1.5">
              <span
                v-for="t in (p.tags || []).slice(0, 2)"
                :key="t.id"
                class="rounded-full bg-brand-soft px-2.5 py-0.5 text-xs font-medium text-brand-strong"
              >{{ t.name }}</span>
              <span v-if="p.tags && p.tags.length > 2" class="text-xs text-content-subtle">+{{ p.tags.length - 2 }}</span>
            </div>

            <h2 class="line-clamp-1 text-lg font-semibold text-content transition-colors group-hover:text-brand">
              {{ p.revision?.title || t('ideas.common.untitled') }}
            </h2>
            <p class="mt-2 line-clamp-2 flex-1 text-sm leading-relaxed text-content-muted">
              {{ p.revision?.summary || t('ideas.common.noSummary') }}
            </p>

            <div class="mt-5 flex items-center gap-3 border-t border-line pt-4">
              <div class="flex items-center gap-2">
                <span class="flex h-8 w-8 items-center justify-center rounded-full bg-gradient-primary text-xs font-bold text-white">{{ authorInitial(p.author_name) }}</span>
                <span class="text-sm text-content-muted">{{ p.author_name }}</span>
              </div>
              <div class="ml-auto flex items-center gap-3 text-xs text-content-subtle">
                <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178zM15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>{{ formatCount(p.view_count) }}</span>
                <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12z" /></svg>{{ formatCount(p.like_count) }}</span>
                <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M17.593 3.322c1.1.128 1.907 1.077 1.907 2.185V21L12 17.25 4.5 21V5.507c0-1.108.806-2.057 1.907-2.185a48.507 48.507 0 0111.186 0z" /></svg>{{ formatCount(p.favorite_count) }}</span>
              </div>
            </div>

            <div class="mt-3 flex items-center justify-between text-xs text-content-subtle">
              <span class="inline-flex items-center gap-1"><svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>{{ formatRelative(p.published_at || p.created_at) }}</span>
            </div>
          </RouterLink>
        </div>

        <!-- Pagination -->
        <div v-if="!loading && !error && total > pageSize" class="mt-8 flex justify-center">
          <Pagination :total="total" :page="page" :page-size="pageSize" @update:page="onPage" @update:page-size="onPageSize" />
        </div>
      </div>
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
.line-clamp-2 {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
