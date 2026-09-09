<template>
  <section class="room-reviews" :aria-busy="loading" aria-label="本房间评价" data-testid="room-reviews-panel">
    <div class="room-reviews-summary">
      <div class="room-reviews-rating">
        <span>本房间评分</span>
        <strong>{{ ratingCount > 0 ? scoreLabel(ratingAverage) : '—' }}<small>/ 10</small></strong>
        <span>{{ ratingCount > 0 ? `${ratingCount} 次评分` : '暂无评分' }}</span>
      </div>
      <div class="room-reviews-summary-copy">
        <strong>来自使用过这个房间的用户</strong>
        <p>评分统计包含本房间全部有效评分；下方仅展示已审核通过的文字评价。</p>
      </div>
    </div>

    <div class="room-reviews-heading">
      <h3>本房间评论</h3>
      <span aria-live="polite">{{ total === null ? '尚未加载' : `${total} 条公开文字评论` }}</span>
    </div>

    <div v-if="errorMessage" class="room-reviews-error" role="alert">
      <div>
        <strong>{{ reviews.length > 0 ? '后续评论暂时无法加载' : '评论暂时无法加载' }}</strong>
        <p>{{ errorMessage }}</p>
      </div>
      <button type="button" class="room-reviews-button" :disabled="loading" @click="loadReviews(retryPage)">重新加载</button>
    </div>

    <div v-if="loading && reviews.length === 0" class="room-reviews-empty" role="status">
      <Icon name="refresh" size="md" class="animate-spin" />
      <span>正在加载本房间评论…</span>
    </div>
    <div v-else-if="!errorMessage && total !== null && reviews.length === 0" class="room-reviews-empty" data-testid="room-reviews-empty">
      <Icon name="chat" size="lg" />
      <strong>还没有公开文字评论</strong>
      <span>已提交的评分与公开文字评论分别统计。</span>
    </div>

    <div v-if="reviews.length > 0" class="room-reviews-list">
      <article v-for="review in reviews" :key="review.id" class="room-review" data-testid="room-review">
        <header class="room-review-header">
          <span class="room-review-avatar" aria-hidden="true"><Icon name="user" size="sm" /></span>
          <div class="room-review-author">
            <strong>匿名用户</strong>
            <time :datetime="review.created_at">{{ formatDateOnly(review.created_at) }}</time>
          </div>
          <span class="room-review-score"><span aria-hidden="true">★</span>{{ scoreLabel(review.score) }}<small>/ 10</small></span>
        </header>
        <p class="room-review-comment">{{ review.comment }}</p>
      </article>
    </div>

    <div v-if="reviews.length > 0" class="room-reviews-footer">
      <span>已显示 {{ reviews.length }} / {{ total }} 条</span>
      <button
        v-if="hasMore && !errorMessage"
        type="button"
        class="room-reviews-button"
        :disabled="loading"
        @click="loadReviews(page + 1)"
      >
        <Icon v-if="loading" name="refresh" size="sm" class="animate-spin" />
        {{ loading ? '加载中…' : '继续加载评论' }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { accountShareAPI, type AccountShareReview } from '@/api/accountShare'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateOnly } from '@/utils/format'
import { isCanceledRequest } from '@/utils/requestSafety'

const props = withDefaults(defineProps<{
  listingId: number
  ratingAverage?: number | null
  ratingCount?: number
}>(), {
  ratingAverage: null,
  ratingCount: 0
})

type PublicRoomReview = Pick<AccountShareReview, 'id' | 'score' | 'comment' | 'created_at'>

const REVIEW_PAGE_SIZE = 10
const reviews = ref<PublicRoomReview[]>([])
const total = ref<number | null>(null)
const page = ref(0)
const pages = ref(0)
const loading = ref(false)
const errorMessage = ref('')
const retryPage = ref(1)
const hasMore = computed(() => page.value < pages.value)
let requestController: AbortController | null = null
let requestSequence = 0

function scoreLabel(score: number | null | undefined): string {
  return typeof score === 'number' && Number.isFinite(score) && score >= 0 && score <= 10
    ? score.toFixed(1)
    : '—'
}

async function loadReviews(nextPage = 1): Promise<void> {
  if (loading.value) return
  const listingId = props.listingId
  if (!Number.isInteger(listingId) || listingId <= 0) {
    errorMessage.value = '房间信息无效，请重新打开房间详情。'
    return
  }

  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  const sequence = ++requestSequence
  retryPage.value = nextPage
  loading.value = true
  errorMessage.value = ''

  try {
    const result = await accountShareAPI.listListingReviews(listingId, nextPage, REVIEW_PAGE_SIZE, {
      signal: controller.signal
    })
    if (controller.signal.aborted || sequence !== requestSequence || props.listingId !== listingId) return

    // 公开评价仅保留展示所需字段，避免携带内部账号和消费者信息。
    const nextReviews = result.items.map(({ id, score, comment, created_at }) => ({ id, score, comment, created_at }))
    if (nextPage === 1) {
      reviews.value = nextReviews
    } else {
      const byId = new Map(reviews.value.map(review => [review.id, review]))
      for (const review of nextReviews) byId.set(review.id, review)
      reviews.value = Array.from(byId.values())
    }
    total.value = result.total
    page.value = result.page
    pages.value = result.pages
  } catch (error: unknown) {
    if (controller.signal.aborted || sequence !== requestSequence || props.listingId !== listingId || isCanceledRequest(error)) return
    errorMessage.value = extractApiErrorMessage(error, '加载本房间评论失败，请稍后重试。')
  } finally {
    if (sequence === requestSequence && requestController === controller) {
      loading.value = false
      requestController = null
    }
  }
}

watch(() => props.listingId, () => {
  requestSequence += 1
  requestController?.abort()
  requestController = null
  reviews.value = []
  total.value = null
  page.value = 0
  pages.value = 0
  retryPage.value = 1
  loading.value = false
  errorMessage.value = ''
  void loadReviews()
}, { immediate: true })

onBeforeUnmount(() => {
  requestSequence += 1
  requestController?.abort()
  requestController = null
})
</script>

<style scoped>
.room-reviews {
  --review-surface: #fff;
  --review-soft: #f8fafc;
  --review-border: #e8ebef;
  --review-text: #17202c;
  --review-muted: #657182;
  --review-accent: #1673bf;
  --review-accent-soft: #eef7ff;
  display: grid;
  min-width: 0;
  gap: 0.875rem;
  color: var(--review-text);
}

.dark .room-reviews {
  --review-surface: #191e27;
  --review-soft: #202733;
  --review-border: #303947;
  --review-text: #eef2f7;
  --review-muted: #a0abba;
  --review-accent: #87c9ff;
  --review-accent-soft: #21364b;
}

.room-reviews-summary {
  display: grid;
  grid-template-columns: minmax(5rem, 0.35fr) minmax(0, 1fr);
  align-items: center;
  gap: 0.875rem;
  border-radius: 0.625rem;
  background: var(--review-soft);
  padding: 0.75rem;
}

.room-reviews-rating {
  display: grid;
  min-width: 0;
  gap: 0.1875rem;
}

.room-reviews-rating > span {
  color: var(--review-muted);
  font-size: 0.75rem;
  line-height: 1.5;
}

.room-reviews-rating > strong {
  display: flex;
  align-items: baseline;
  gap: 0.25rem;
  color: var(--review-accent);
  font-size: 1.75rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
}

.room-reviews-rating small {
  color: var(--review-muted);
  font-size: 0.75rem;
  font-weight: 400;
}

.room-reviews-summary-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  justify-content: center;
  gap: 0.375rem;
}

.room-reviews-summary-copy strong {
  font-size: 0.8125rem;
  font-weight: 500;
  line-height: 1.5;
}

.room-reviews-summary-copy p {
  margin: 0;
  color: var(--review-muted);
  font-size: 0.8125rem;
  line-height: 1.6;
}

.room-reviews-heading,
.room-reviews-footer {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.room-reviews-heading h3 {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
}

.room-reviews-heading > span,
.room-reviews-footer > span {
  color: var(--review-muted);
  font-size: 0.75rem;
}

.room-reviews-error {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border: 1px solid #f0cece;
  border-radius: 0.625rem;
  background: #fff8f8;
  padding: 0.75rem;
  color: #a63434;
}

.room-reviews-error > div {
  min-width: 0;
  flex: 1 1 12rem;
}

.room-reviews-error strong {
  font-size: 0.8125rem;
  font-weight: 600;
}

.room-reviews-error p {
  margin: 0.375rem 0 0;
  font-size: 0.75rem;
  line-height: 1.8;
  overflow-wrap: anywhere;
}

.dark .room-reviews-error {
  border-color: #644040;
  background: #352428;
  color: #f1a6a6;
}

.room-reviews-empty {
  display: flex;
  min-height: 8rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border-block: 1px solid var(--review-border);
  padding: 1rem 0.75rem;
  color: var(--review-muted);
  text-align: center;
}

.room-reviews-empty strong {
  color: var(--review-text);
  font-size: 0.875rem;
  font-weight: 500;
}

.room-reviews-empty span {
  font-size: 0.8125rem;
  line-height: 1.8;
}

.room-reviews-list {
  display: grid;
  min-width: 0;
  border-top: 1px solid var(--review-border);
}

.room-review {
  min-width: 0;
  border-bottom: 1px solid var(--review-border);
  padding: 0.875rem 0;
}

.room-review-header {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
}

.room-review-avatar {
  display: inline-flex;
  width: 1.875rem;
  height: 1.875rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--review-border);
  border-radius: 50%;
  background: var(--review-soft);
  color: var(--review-muted);
}

.room-review-author {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 0.125rem;
}

.room-review-author strong {
  font-size: 0.8125rem;
  font-weight: 500;
}

.room-review-author time {
  color: var(--review-muted);
  font-size: 0.75rem;
}

.room-review-score {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.25rem;
  color: var(--review-accent);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
}

.room-review-score small {
  color: var(--review-muted);
  font-size: 0.75rem;
}

.room-review-comment {
  margin: 0.5rem 0 0;
  font-size: 0.875rem;
  line-height: 1.75;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.room-reviews-button {
  display: inline-flex;
  min-height: 2.75rem;
  cursor: pointer;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 1px solid var(--review-border);
  border-radius: 0.625rem;
  background: var(--review-surface);
  padding: 0.5rem 0.75rem;
  color: var(--review-accent);
  font-size: 0.8125rem;
  transition: background-color 160ms ease, border-color 160ms ease;
}

.room-reviews-button:hover:not(:disabled) {
  border-color: var(--review-accent);
  background: var(--review-accent-soft);
}

.room-reviews-button:focus-visible {
  outline: 2px solid var(--review-accent);
  outline-offset: 3px;
}

.room-reviews-button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

@media (min-width: 640px) {
  .room-reviews-summary {
    grid-template-columns: 7rem minmax(0, 1fr);
    gap: 1.25rem;
    padding: 1rem;
  }

  .room-reviews-rating > strong {
    font-size: 2rem;
  }

  .room-reviews-summary-copy strong {
    font-size: 0.875rem;
  }

  .room-review-comment {
    margin-left: 2.375rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .room-reviews-button {
    transition: none;
  }
}
</style>
