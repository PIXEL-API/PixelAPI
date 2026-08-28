<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { listMyIdeas, publishIdea, deleteIdea } from '@/api/ideas'
import type { IdeaPost } from '@/types/ideas'

const posts = ref<IdeaPost[]>([])
const loading = ref(false)

const statusLabels: Record<string, string> = {
  draft: '草稿',
  pending_review: '待审核',
  manual_review: '人工审核',
  published: '已发布',
  pending_revision: '待审核(修订)',
  rejected: '已拒绝',
  hidden: '已下架',
  moderation_failed: '审核失败',
  deleted: '已删除',
}

async function load() {
  loading.value = true
  try {
    const res = await listMyIdeas()
    posts.value = res.items
  } finally {
    loading.value = false
  }
}

async function publish(id: number) {
  await publishIdea(id)
  await load()
}

async function remove(id: number) {
  if (!window.confirm('确定删除这篇文章吗？')) return
  await deleteIdea(id)
  await load()
}

onMounted(load)
</script>

<template>
  <div class="my-ideas-page">
    <div class="my-ideas-header">
      <h1>我的文章</h1>
      <router-link to="/ideas/new" class="btn btn-primary">写文章</router-link>
    </div>

    <div v-if="loading" class="ideas-empty">加载中…</div>
    <div v-else-if="posts.length === 0" class="ideas-empty">还没有文章</div>
    <table v-else class="my-ideas-table">
      <thead>
        <tr>
          <th>标题</th>
          <th>状态</th>
          <th>阅读/赞/收藏</th>
          <th>更新时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in posts" :key="p.id">
          <td><router-link :to="`/ideas/${p.id}`">{{ p.revision?.title }}</router-link></td>
          <td>{{ statusLabels[p.status] || p.status }}</td>
          <td>{{ p.view_count }} / {{ p.like_count }} / {{ p.favorite_count }}</td>
          <td>{{ new Date(p.updated_at).toLocaleDateString('zh-CN') }}</td>
          <td>
            <button v-if="p.can_edit" class="link-btn" @click="$router.push(`/ideas/${p.id}/edit`)">编辑</button>
            <button v-if="p.status === 'draft' || p.status === 'rejected'" class="link-btn" @click="publish(p.id)">发布</button>
            <button class="link-btn danger" @click="remove(p.id)">删除</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.my-ideas-page { max-width: 960px; margin: 0 auto; padding: 24px 16px; }
.my-ideas-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.my-ideas-table { width: 100%; border-collapse: collapse; }
.my-ideas-table th, .my-ideas-table td { text-align: left; padding: 10px 8px; border-bottom: 1px solid #eee; font-size: 14px; }
.ideas-empty { text-align: center; color: #999; padding: 60px 0; }
.btn { display: inline-block; padding: 8px 16px; border-radius: 6px; text-decoration: none; border: none; cursor: pointer; }
.btn-primary { background: #2563eb; color: #fff; }
.link-btn { border: none; background: none; color: #2563eb; cursor: pointer; margin-right: 8px; }
.link-btn.danger { color: #dc2626; }
</style>
