<script setup lang="ts">
import { ref, onMounted } from 'vue'
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
} from '@/api/admin/ideas'
import type { IdeaPost, IdeaReport, IdeaTag } from '@/types/ideas'

const tab = ref<'posts' | 'reports' | 'tags'>('posts')
const posts = ref<IdeaPost[]>([])
const reports = ref<IdeaReport[]>([])
const tags = ref<IdeaTag[]>([])
const keyword = ref('')
const newTagName = ref('')
const loading = ref(false)

const statusLabels: Record<string, string> = {
  draft: '草稿', pending_review: '待审核', manual_review: '人工审核', published: '已发布',
  pending_revision: '待审核(修订)', rejected: '已拒绝', hidden: '已下架', moderation_failed: '审核失败', deleted: '已删除',
}

async function loadPosts() {
  loading.value = true
  try {
    const res = await adminListIdeas({ keyword: keyword.value || undefined })
    posts.value = res.items
  } finally {
    loading.value = false
  }
}

async function loadReports() {
  const res = await adminListReports({ status: 'pending' })
  reports.value = res.items
}

async function loadTags() {
  tags.value = await adminListTags()
}

async function approve(id: number) {
  await adminApproveIdea(id)
  await loadPosts()
}

async function reject(id: number) {
  const reason = window.prompt('请输入拒绝原因：')
  if (reason == null) return
  await adminRejectIdea(id, reason)
  await loadPosts()
}

async function hide(id: number) {
  await adminHideIdea(id)
  await loadPosts()
}

async function restore(id: number) {
  await adminRestoreIdea(id)
  await loadPosts()
}

async function retry(id: number) {
  await adminRetryModeration(id)
  await loadPosts()
}

async function resolveReport(id: number) {
  const resolution = window.prompt('处理说明（可选）：') || ''
  await adminResolveReport(id, resolution)
  await loadReports()
}

async function createTag() {
  if (!newTagName.value.trim()) return
  await adminCreateTag(newTagName.value.trim())
  newTagName.value = ''
  await loadTags()
}

async function toggleTag(id: number, current: string) {
  await adminUpdateTag(id, { status: current === 'active' ? 'disabled' : 'active' })
  await loadTags()
}

async function switchTab(t: 'posts' | 'reports' | 'tags') {
  tab.value = t
  if (t === 'posts') await loadPosts()
  if (t === 'reports') await loadReports()
  if (t === 'tags') await loadTags()
}

onMounted(() => loadPosts())
</script>

<template>
  <div class="admin-ideas-page">
    <div class="admin-tabs">
      <button :class="{ active: tab === 'posts' }" @click="switchTab('posts')">文章管理</button>
      <button :class="{ active: tab === 'reports' }" @click="switchTab('reports')">举报处理</button>
      <button :class="{ active: tab === 'tags' }" @click="switchTab('tags')">标签治理</button>
    </div>

    <div v-if="tab === 'posts'">
      <div class="admin-toolbar">
        <input v-model="keyword" placeholder="搜索标题" @keyup.enter="loadPosts" />
        <button class="btn" @click="loadPosts">搜索</button>
      </div>
      <div v-if="loading">加载中…</div>
      <table v-else class="admin-table">
        <thead>
          <tr><th>ID</th><th>标题</th><th>作者</th><th>状态</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in posts" :key="p.id">
            <td>{{ p.id }}</td>
            <td>{{ p.revision?.title }}</td>
            <td>{{ p.author_name }}</td>
            <td>{{ statusLabels[p.status] || p.status }}</td>
            <td>
              <button v-if="['pending_review','manual_review','pending_revision','moderation_failed'].includes(p.status)" class="link-btn" @click="approve(p.id)">通过</button>
              <button v-if="['pending_review','manual_review','pending_revision'].includes(p.status)" class="link-btn danger" @click="reject(p.id)">拒绝</button>
              <button v-if="p.status === 'published'" class="link-btn danger" @click="hide(p.id)">下架</button>
              <button v-if="p.status === 'hidden'" class="link-btn" @click="restore(p.id)">恢复</button>
              <button v-if="['pending_review','moderation_failed','manual_review','pending_revision'].includes(p.status)" class="link-btn" @click="retry(p.id)">重试AI</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="tab === 'reports'">
      <table class="admin-table">
        <thead>
          <tr><th>ID</th><th>文章</th><th>原因</th><th>说明</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="r in reports" :key="r.id">
            <td>{{ r.id }}</td>
            <td>{{ r.post_title }}</td>
            <td>{{ r.reason }}</td>
            <td>{{ r.detail }}</td>
            <td><button class="link-btn" @click="resolveReport(r.id)">处理</button></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="tab === 'tags'">
      <div class="admin-toolbar">
        <input v-model="newTagName" placeholder="新标签名" />
        <button class="btn" @click="createTag">新建标签</button>
      </div>
      <table class="admin-table">
        <thead>
          <tr><th>ID</th><th>名称</th><th>slug</th><th>状态</th><th>使用次数</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="t in tags" :key="t.id">
            <td>{{ t.id }}</td>
            <td>{{ t.name }}</td>
            <td>{{ t.slug }}</td>
            <td>{{ t.status }}</td>
            <td>{{ t.usage_count }}</td>
            <td><button class="link-btn" @click="toggleTag(t.id, t.status)">{{ t.status === 'active' ? '停用' : '启用' }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.admin-ideas-page { max-width: 1100px; margin: 0 auto; padding: 24px 16px; }
.admin-tabs { display: flex; gap: 8px; margin-bottom: 20px; border-bottom: 1px solid #eee; }
.admin-tabs button { padding: 10px 16px; border: none; background: none; cursor: pointer; border-bottom: 2px solid transparent; }
.admin-tabs button.active { border-bottom-color: #2563eb; color: #2563eb; font-weight: 600; }
.admin-toolbar { display: flex; gap: 10px; margin-bottom: 16px; }
.admin-toolbar input { padding: 8px 12px; border: 1px solid #ddd; border-radius: 6px; }
.admin-table { width: 100%; border-collapse: collapse; }
.admin-table th, .admin-table td { text-align: left; padding: 10px 8px; border-bottom: 1px solid #eee; font-size: 14px; }
.btn { padding: 8px 16px; border: 1px solid #ddd; border-radius: 6px; background: #fff; cursor: pointer; }
.link-btn { border: none; background: none; color: #2563eb; cursor: pointer; margin-right: 8px; }
.link-btn.danger { color: #dc2626; }
</style>
