<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
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
import type { IdeaPost } from '@/types/ideas'

marked.setOptions({ breaks: true, gfm: true })

const route = useRoute()
const id = Number(route.params.id)
const post = ref<IdeaPost | null>(null)
const loading = ref(true)
const error = ref('')

const rewardType = ref<'balance' | 'points'>('balance')
const rewardAmount = ref(1)
const rewardDialogOpen = ref(false)
const reportOpen = ref(false)
const reportReason = ref('')
const reportDetail = ref('')

const renderedHtml = computed(() => {
  const body = post.value?.revision?.body || ''
  return DOMPurify.sanitize(marked.parse(body) as string)
})

async function load() {
  loading.value = true
  try {
    post.value = await getIdea(id)
    await recordIdeaView(id)
  } catch (e: any) {
    error.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function toggleLike() {
  if (!post.value) return
  if (post.value.liked) {
    const { count } = await unlikeIdea(id)
    post.value.liked = false
    post.value.like_count = count
  } else {
    const { count } = await likeIdea(id)
    post.value.liked = true
    post.value.like_count = count
  }
}

async function toggleFavorite() {
  if (!post.value) return
  if (post.value.favorited) {
    const { count } = await unfavoriteIdea(id)
    post.value.favorited = false
    post.value.favorite_count = count
  } else {
    const { count } = await favoriteIdea(id)
    post.value.favorited = true
    post.value.favorite_count = count
  }
}

async function submitReward() {
  if (!post.value) return
  const key = `reward-${Date.now()}-${Math.random().toString(36).slice(2)}`
  try {
    await rewardIdea(id, { asset_type: rewardType.value, amount: rewardAmount.value }, key)
    rewardDialogOpen.value = false
    window.alert('打赏成功')
  } catch (e: any) {
    window.alert(e?.message || '打赏失败')
  }
}

async function submitReport() {
  if (!reportReason.value) return
  try {
    await reportIdea(id, { reason: reportReason.value, detail: reportDetail.value })
    reportOpen.value = false
    reportReason.value = ''
    reportDetail.value = ''
    window.alert('举报已提交')
  } catch (e: any) {
    window.alert(e?.message || '举报失败')
  }
}

onMounted(load)
</script>

<template>
  <div class="idea-detail-page">
    <div v-if="loading" class="idea-empty">加载中…</div>
    <div v-else-if="error" class="idea-empty">{{ error }}</div>
    <article v-else-if="post" class="idea-detail">
      <h1 class="detail-title">{{ post.revision?.title }}</h1>
      <div class="detail-meta">
        <span>{{ post.author_name }}</span>
        <span v-if="post.published_at">{{ new Date(post.published_at).toLocaleDateString('zh-CN') }}</span>
        <span>阅读 {{ post.view_count }}</span>
      </div>
      <div class="detail-tags">
        <span v-for="t in post.tags" :key="t.id" class="tag-chip">{{ t.name }}</span>
      </div>

      <div class="detail-actions">
        <button class="btn" :class="{ active: post.liked }" @click="toggleLike">赞 {{ post.like_count }}</button>
        <button class="btn" :class="{ active: post.favorited }" @click="toggleFavorite">收藏 {{ post.favorite_count }}</button>
        <button v-if="post.can_reward" class="btn" @click="rewardDialogOpen = true">打赏</button>
        <button class="btn" @click="reportOpen = true">举报</button>
        <router-link v-if="post.can_edit" class="btn" :to="`/ideas/${post.id}/edit`">编辑</router-link>
      </div>

      <div class="detail-body markdown-body" v-html="renderedHtml"></div>
    </article>

    <div v-if="rewardDialogOpen" class="modal-mask" @click.self="rewardDialogOpen = false">
      <div class="modal">
        <h3>打赏</h3>
        <select v-model="rewardType">
          <option value="balance">余额</option>
          <option value="points">积分</option>
        </select>
        <input v-model.number="rewardAmount" type="number" min="1" max="100" />
        <div class="modal-actions">
          <button class="btn btn-primary" @click="submitReward">确认打赏</button>
          <button class="btn" @click="rewardDialogOpen = false">取消</button>
        </div>
      </div>
    </div>

    <div v-if="reportOpen" class="modal-mask" @click.self="reportOpen = false">
      <div class="modal">
        <h3>举报</h3>
        <input v-model="reportReason" placeholder="举报原因" />
        <textarea v-model="reportDetail" placeholder="补充说明（可选）"></textarea>
        <div class="modal-actions">
          <button class="btn btn-primary" @click="submitReport">提交举报</button>
          <button class="btn" @click="reportOpen = false">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.idea-detail-page { max-width: 800px; margin: 0 auto; padding: 24px 16px; }
.idea-empty { text-align: center; color: #999; padding: 60px 0; }
.detail-title { margin: 0 0 12px; font-size: 28px; }
.detail-meta { display: flex; gap: 16px; color: #999; font-size: 13px; margin-bottom: 12px; }
.detail-tags { display: flex; gap: 6px; margin-bottom: 16px; }
.tag-chip { background: #f1f5f9; color: #475569; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.detail-actions { display: flex; gap: 10px; margin-bottom: 20px; }
.btn { padding: 8px 16px; border: 1px solid #ddd; border-radius: 6px; background: #fff; cursor: pointer; text-decoration: none; color: #333; }
.btn.active { background: #2563eb; color: #fff; border-color: #2563eb; }
.btn-primary { background: #2563eb; color: #fff; border: none; }
.detail-body { line-height: 1.7; }
.modal-mask { position: fixed; inset: 0; background: rgba(0,0,0,.4); display: flex; align-items: center; justify-content: center; }
.modal { background: #fff; padding: 20px; border-radius: 10px; min-width: 320px; display: flex; flex-direction: column; gap: 12px; }
.modal-actions { display: flex; gap: 10px; justify-content: flex-end; }
</style>
