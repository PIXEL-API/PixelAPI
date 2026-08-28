<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import {
  getIdea,
  likeIdea,
  unlikeIdea,
  favoriteIdea,
  unfavoriteIdea,
  rewardIdea,
  reportIdea,
  recordIdeaView,
} from '@/api/ideas'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import { useAppStore } from '@/stores/app'
import type { IdeaPost } from '@/types/ideas'

marked.setOptions({ breaks: true, gfm: true })

const appStore = useAppStore()
const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)
const post = ref<IdeaPost | null>(null)
const loading = ref(true)
const error = ref('')

const rewardDialogOpen = ref(false)
const rewardType = ref<'balance' | 'points'>('balance')
const rewardAmount = ref(1)
const rewardSubmitting = ref(false)
const likePending = ref(false)
const favoritePending = ref(false)

const reportOpen = ref(false)
const reportReason = ref('')
const reportDetail = ref('')
const reportSubmitting = ref(false)

const renderedHtml = computed(() => {
  const body = post.value?.revision?.body || ''
  return DOMPurify.sanitize(marked.parse(body) as string)
})

const authorInitial = computed(() => (post.value?.author_name || '?').trim().charAt(0).toUpperCase())

function formatDate(s?: string) {
  if (!s) return ''
  return new Date(s).toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })
}

function formatCount(n: number) {
  if (n >= 10000) return `${(n / 10000).toFixed(1)}w`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    post.value = await getIdea(id)
    void recordIdeaView(id).catch(() => undefined)
  } catch (e: any) {
    error.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function toggleLike() {
  if (!post.value || likePending.value) return
  likePending.value = true
  const prev = { liked: post.value.liked, count: post.value.like_count }
  post.value.liked = !prev.liked
  post.value.like_count = prev.liked ? Math.max(0, prev.count - 1) : prev.count + 1
  try {
    const { count } = post.value.liked ? await likeIdea(id) : await unlikeIdea(id)
    post.value.like_count = count
  } catch (e: any) {
    post.value.liked = prev.liked
    post.value.like_count = prev.count
    appStore.showError(e?.message || '操作失败，请重试')
  } finally {
    likePending.value = false
  }
}

async function toggleFavorite() {
  if (!post.value || favoritePending.value) return
  favoritePending.value = true
  const prev = { favorited: post.value.favorited, count: post.value.favorite_count }
  post.value.favorited = !prev.favorited
  post.value.favorite_count = prev.favorited ? Math.max(0, prev.count - 1) : prev.count + 1
  try {
    const { count } = post.value.favorited ? await favoriteIdea(id) : await unfavoriteIdea(id)
    post.value.favorite_count = count
  } catch (e: any) {
    post.value.favorited = prev.favorited
    post.value.favorite_count = prev.count
    appStore.showError(e?.message || '操作失败，请重试')
  } finally {
    favoritePending.value = false
  }
}

function openReward() {
  rewardType.value = 'balance'
  rewardAmount.value = 1
  rewardDialogOpen.value = true
}

async function submitReward() {
  if (!post.value || rewardSubmitting.value) return
  if (!rewardAmount.value || rewardAmount.value <= 0) {
    appStore.showWarning('请输入有效的打赏金额')
    return
  }
  rewardSubmitting.value = true
  const key = `idea-reward-${id}-${Date.now()}-${Math.random().toString(36).slice(2)}`
  try {
    await rewardIdea(id, { asset_type: rewardType.value, amount: rewardAmount.value }, key)
    rewardDialogOpen.value = false
    appStore.showSuccess('打赏成功，感谢支持！')
  } catch (e: any) {
    appStore.showError(e?.message || '打赏失败')
  } finally {
    rewardSubmitting.value = false
  }
}

function openReport() {
  reportReason.value = ''
  reportDetail.value = ''
  reportOpen.value = true
}

async function submitReport() {
  if (!reportReason.value.trim() || reportSubmitting.value) return
  reportSubmitting.value = true
  try {
    await reportIdea(id, { reason: reportReason.value.trim(), detail: reportDetail.value.trim() })
    reportOpen.value = false
    appStore.showSuccess('举报已提交，我们会尽快处理')
  } catch (e: any) {
    appStore.showError(e?.message || '举报失败')
  } finally {
    reportSubmitting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="min-h-full bg-canvas text-content">
    <div class="mx-auto max-w-4xl px-4 pb-16 pt-6 md:px-6">
      <button
        class="mb-6 inline-flex items-center gap-2 text-sm text-content-muted transition-colors hover:text-content"
        @click="router.back()"
      >
        <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.5 19.5L3 12m0 0l7.5-7.5M3 12h18" /></svg>
        返回
      </button>

      <!-- Loading -->
      <div v-if="loading" class="rounded-panel border border-line bg-surface p-8">
        <div class="mb-4 flex gap-2"><Skeleton width="72" height="24" class="rounded-full" /><Skeleton width="56" height="24" class="rounded-full" /></div>
        <Skeleton height="34" class="mb-4" />
        <Skeleton height="16" width="40%" class="mb-8" />
        <Skeleton variant="text" class="mb-2" /><Skeleton variant="text" class="mb-2" /><Skeleton variant="text" width="80%" />
      </div>

      <!-- Error -->
      <div v-else-if="error" class="rounded-panel border border-line bg-surface p-10">
        <EmptyState :title="error" description="这篇文章可能不存在或暂不可访问">
          <template #icon>
            <svg class="h-10 w-10 text-danger" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" /></svg>
          </template>
          <template #action>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse transition-colors hover:bg-brand-strong" @click="load">
              <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
              重试
            </button>
          </template>
        </EmptyState>
      </div>

      <!-- Article -->
      <article v-else-if="post" class="overflow-hidden rounded-panel border border-line bg-surface">
        <div class="border-b border-line p-6 md:p-10">
          <div class="mb-4 flex flex-wrap gap-2">
            <span
              v-for="t in (post.tags || [])"
              :key="t.id"
              class="rounded-full bg-brand-soft px-3 py-1 text-xs font-medium text-brand-strong"
            >{{ t.name }}</span>
          </div>
          <h1 class="text-2xl font-bold leading-snug tracking-tight text-content md:text-4xl">
            {{ post.revision?.title }}
          </h1>
          <div class="mt-5 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-content-muted">
            <span class="inline-flex items-center gap-2">
              <span class="flex h-9 w-9 items-center justify-center rounded-full bg-gradient-primary text-xs font-bold text-white">{{ authorInitial }}</span>
              <span class="font-medium text-content">{{ post.author_name }}</span>
            </span>
            <span class="inline-flex items-center gap-1.5"><svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>{{ formatDate(post.published_at || post.created_at) }}</span>
            <span class="inline-flex items-center gap-1.5"><svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178zM15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>{{ formatCount(post.view_count) }} 阅读</span>
          </div>
        </div>

        <!-- Action bar -->
        <div class="flex flex-wrap items-center gap-2 border-b border-line bg-surface-subtle/60 px-6 py-3 md:px-10">
          <button
            v-if="post.status === 'published'"
            class="inline-flex h-9 items-center gap-1.5 rounded-panel border px-4 text-sm font-medium transition-all"
            :class="post.liked ? 'border-danger/30 bg-danger/10 text-danger' : 'border-line bg-surface text-content-muted hover:border-danger/40 hover:text-danger'"
            @click="toggleLike"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" :fill="post.liked ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12z" /></svg>
            赞 {{ formatCount(post.like_count) }}
          </button>
          <button
            v-if="post.status === 'published'"
            class="inline-flex h-9 items-center gap-1.5 rounded-panel border px-4 text-sm font-medium transition-all"
            :class="post.favorited ? 'border-brand/30 bg-brand/10 text-brand' : 'border-line bg-surface text-content-muted hover:border-brand/40 hover:text-brand'"
            @click="toggleFavorite"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" :fill="post.favorited ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M17.593 3.322c1.1.128 1.907 1.077 1.907 2.185V21L12 17.25 4.5 21V5.507c0-1.108.806-2.057 1.907-2.185a48.507 48.507 0 0111.186 0z" /></svg>
            收藏 {{ formatCount(post.favorite_count) }}
          </button>
          <button
            v-if="post.can_reward"
            class="inline-flex h-9 items-center gap-1.5 rounded-panel bg-gradient-primary px-4 text-sm font-medium text-white transition-transform hover:-translate-y-0.5"
            @click="openReward"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.25v8.25a1.5 1.5 0 01-1.5 1.5H4.5a1.5 1.5 0 01-1.5-1.5v-8.25M12 4.875A2.625 2.625 0 109.375 7.5H12m0-2.625V7.5m0-2.625A2.625 2.625 0 1114.625 7.5H12m0 0V21m-8.625-9.75h18c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125h-18c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z" /></svg>
            打赏
          </button>
          <span class="flex-1"></span>
          <RouterLink
            v-if="post.can_edit && ['draft','published','rejected'].includes(post.status)"
            :to="`/ideas/${post.id}/edit`"
            class="inline-flex h-9 items-center gap-1.5 rounded-panel border border-line bg-surface px-4 text-sm font-medium text-content-muted transition-colors hover:text-brand"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125" /></svg>
            编辑
          </RouterLink>
          <button
            class="inline-flex h-9 items-center gap-1.5 rounded-panel border border-line bg-surface px-4 text-sm font-medium text-content-muted transition-colors hover:text-danger"
            @click="openReport"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" /></svg>
            举报
          </button>
        </div>

        <!-- Body -->
        <div class="markdown-body p-6 md:p-10" v-html="renderedHtml"></div>

        <!-- Footer -->
        <div class="flex flex-wrap items-center justify-between gap-3 border-t border-line bg-surface-subtle/60 px-6 py-4 md:px-10">
          <span class="text-xs text-content-subtle">觉得有帮助？支持作者一份心意</span>
          <button
            v-if="post.can_reward"
            class="inline-flex h-9 items-center gap-1.5 rounded-panel border border-brand/30 bg-brand-soft px-4 text-sm font-medium text-brand transition-colors hover:bg-brand/10"
            @click="openReward"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.25v8.25a1.5 1.5 0 01-1.5 1.5H4.5a1.5 1.5 0 01-1.5-1.5v-8.25M12 4.875A2.625 2.625 0 109.375 7.5H12m0-2.625V7.5m0-2.625A2.625 2.625 0 1114.625 7.5H12m0 0V21m-8.625-9.75h18c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125h-18c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z" /></svg>
            打赏作者
          </button>
        </div>
      </article>

      <!-- Reward Dialog -->
      <BaseDialog :show="rewardDialogOpen" title="打赏作者" width="narrow" @close="rewardDialogOpen = false">
        <div class="space-y-4">
          <p class="text-sm text-content-muted">感谢你的支持！打赏将直接进入作者余额。</p>
          <div>
            <label class="mb-1.5 block text-xs font-medium text-content-muted">奖励方式</label>
            <div class="grid grid-cols-2 gap-2">
              <button
                v-for="opt in ([['balance', '余额'], ['points', '积分']] as const)"
                :key="opt[0]"
                class="rounded-control border px-3 py-2 text-sm font-medium transition-all"
                :class="rewardType === opt[0] ? 'border-brand bg-brand-soft text-brand' : 'border-line bg-surface-subtle text-content-muted hover:border-brand/40'"
                @click="rewardType = opt[0]"
              >{{ opt[1] }}</button>
            </div>
          </div>
          <div>
            <label class="mb-1.5 block text-xs font-medium text-content-muted">金额</label>
            <div class="flex items-center gap-2 rounded-control border border-line bg-surface-subtle px-3">
              <span class="text-content-subtle">{{ rewardType === 'balance' ? '¥' : '' }}</span>
              <input v-model.number="rewardAmount" type="number" min="1" max="100" step="1" class="h-10 w-full bg-transparent text-sm text-content outline-none" />
              <span class="text-content-subtle">{{ rewardType === 'points' ? '积分' : '' }}</span>
            </div>
            <p class="mt-1.5 text-xs text-content-subtle">单次上限由站点配置，请勿超过站点设置。</p>
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm font-medium text-content-muted transition-colors hover:text-content" @click="rewardDialogOpen = false">取消</button>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-brand px-5 text-sm font-semibold text-content-inverse transition-colors hover:bg-brand-strong disabled:opacity-60" :disabled="rewardSubmitting" @click="submitReward">
              <span v-if="rewardSubmitting" class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
              {{ rewardSubmitting ? '处理中…' : '确认打赏' }}
            </button>
          </div>
        </div>
      </BaseDialog>

      <!-- Report Dialog -->
      <BaseDialog :show="reportOpen" title="举报这篇文章" width="narrow" @close="reportOpen = false">
        <div class="space-y-4">
          <div>
            <label class="mb-1.5 block text-xs font-medium text-content-muted">举报原因 <span class="text-danger">*</span></label>
            <select v-model="reportReason" class="h-10 w-full rounded-control border border-line bg-surface-subtle px-3 text-sm text-content outline-none focus:border-brand/60">
              <option value="">请选择举报原因</option>
              <option value="垃圾广告">垃圾广告</option>
              <option value="不实信息">不实信息</option>
              <option value="侵权">侵权</option>
              <option value="违法违规">违法违规</option>
              <option value="其他">其他</option>
            </select>
          </div>
          <div>
            <label class="mb-1.5 block text-xs font-medium text-content-muted">补充说明（可选）</label>
            <textarea v-model="reportDetail" rows="3" placeholder="补充更多细节，便于我们核实" class="w-full rounded-control border border-line bg-surface-subtle px-3 py-2 text-sm text-content outline-none focus:border-brand/60"></textarea>
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <button class="inline-flex h-10 items-center rounded-control border border-line bg-surface px-5 text-sm font-medium text-content-muted transition-colors hover:text-content" @click="reportOpen = false">取消</button>
            <button class="inline-flex h-10 items-center gap-2 rounded-control bg-danger px-5 text-sm font-semibold text-content-inverse transition-colors hover:opacity-90 disabled:opacity-60" :disabled="!reportReason || reportSubmitting" @click="submitReport">
              <span v-if="reportSubmitting" class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"></span>
              {{ reportSubmitting ? '提交中…' : '提交举报' }}
            </button>
          </div>
        </div>
      </BaseDialog>
    </div>
  </div>
</template>

<style scoped>
.markdown-body :deep(h1) {
  font-size: 1.5rem;
  font-weight: 700;
  margin: 1.25em 0 0.5em;
  color: rgb(var(--ui-text));
}
.markdown-body :deep(h2) {
  font-size: 1.3rem;
  font-weight: 700;
  margin: 1.25em 0 0.5em;
  color: rgb(var(--ui-text));
}
.markdown-body :deep(h3) {
  font-size: 1.1rem;
  font-weight: 600;
  margin: 1em 0 0.4em;
  color: rgb(var(--ui-text));
}
.markdown-body :deep(p) {
  margin: 0.75em 0;
  line-height: 1.75;
  color: rgb(var(--ui-text-muted));
}
.markdown-body :deep(ul) {
  margin: 0.75em 0;
  padding-left: 1.5em;
  list-style: disc;
  color: rgb(var(--ui-text-muted));
}
.markdown-body :deep(ol) {
  margin: 0.75em 0;
  padding-left: 1.5em;
  list-style: decimal;
  color: rgb(var(--ui-text-muted));
}
.markdown-body :deep(li) {
  margin: 0.25em 0;
}
.markdown-body :deep(blockquote) {
  margin: 1em 0;
  border-left: 3px solid rgb(var(--ui-brand));
  padding: 0.5em 1em;
  color: rgb(var(--ui-text-subtle));
  background: rgb(var(--ui-surface-subtle));
  border-radius: 0 0.5rem 0.5rem 0;
}
.markdown-body :deep(code) {
  background: rgb(var(--ui-surface-subtle));
  padding: 0.15em 0.4em;
  border-radius: 0.25rem;
  font-size: 0.85em;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: rgb(var(--ui-text));
}
.markdown-body :deep(pre) {
  margin: 1em 0;
  padding: 1em 1.25em;
  background: rgb(var(--ui-surface-subtle));
  border: 1px solid rgb(var(--ui-border));
  border-radius: 0.75rem;
  overflow-x: auto;
}
.markdown-body :deep(pre code) {
  background: transparent;
  padding: 0;
  color: rgb(var(--ui-text));
}
.markdown-body :deep(a) {
  color: rgb(var(--ui-brand));
  text-decoration: underline;
  text-underline-offset: 2px;
}
.markdown-body :deep(img) {
  max-width: 100%;
  border-radius: 0.75rem;
  margin: 1em 0;
}
.markdown-body :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 1em 0;
  font-size: 14px;
}
.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid rgb(var(--ui-border));
  padding: 0.5em 0.75em;
  text-align: left;
}
.markdown-body :deep(th) {
  background: rgb(var(--ui-surface-subtle));
  font-weight: 600;
}
.markdown-body :deep(hr) {
  border: none;
  border-top: 1px solid rgb(var(--ui-border));
  margin: 1.5em 0;
}
.markdown-body :deep(strong) {
  color: rgb(var(--ui-text));
}
</style>
