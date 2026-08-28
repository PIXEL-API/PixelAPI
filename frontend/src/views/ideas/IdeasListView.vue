<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { listIdeas, listIdeaTags } from '@/api/ideas'
import type { IdeaPost, IdeaTag } from '@/types/ideas'

const posts = ref<IdeaPost[]>([])
const tags = ref<IdeaTag[]>([])
const keyword = ref('')
const sort = ref('latest')
const tag = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await listIdeas({ keyword: keyword.value || undefined, sort: sort.value, tag: tag.value || undefined })
    posts.value = res.items
  } finally {
    loading.value = false
  }
}

function selectTag(slug: string) {
  tag.value = tag.value === slug ? '' : slug
  load()
}

function formatDate(s?: string) {
  if (!s) return ''
  return new Date(s).toLocaleDateString('zh-CN')
}

onMounted(async () => {
  tags.value = await listIdeaTags()
  await load()
})
</script>

<template>
  <div class="ideas-page">
    <div class="ideas-toolbar">
      <input v-model="keyword" class="ideas-search" placeholder="搜索文章标题或摘要" @keyup.enter="load" />
      <select v-model="sort" @change="load">
        <option value="latest">最新</option>
        <option value="hot">热门</option>
      </select>
      <router-link to="/ideas/new" class="btn btn-primary">写文章</router-link>
    </div>

    <div class="ideas-tags">
      <button
        v-for="t in tags"
        :key="t.id"
        class="ideas-tag"
        :class="{ active: tag === t.slug }"
        @click="selectTag(t.slug)"
      >
        {{ t.name }}
      </button>
    </div>

    <div v-if="loading" class="ideas-empty">加载中…</div>
    <div v-else-if="posts.length === 0" class="ideas-empty">还没有文章，来写第一篇吧</div>
    <div v-else class="ideas-grid">
      <div v-for="p in posts" :key="p.id" class="idea-card" @click="$router.push(`/ideas/${p.id}`)">
        <h3 class="idea-title">{{ p.revision?.title || '未命名' }}</h3>
        <p class="idea-summary">{{ p.revision?.summary || '' }}</p>
        <div class="idea-meta">
          <span>{{ p.author_name }}</span>
          <span>{{ formatDate(p.published_at) }}</span>
          <span>阅读 {{ p.view_count }}</span>
          <span>赞 {{ p.like_count }}</span>
          <span>收藏 {{ p.favorite_count }}</span>
        </div>
        <div class="idea-card-tags">
          <span v-for="t in p.tags" :key="t.id" class="tag-chip">{{ t.name }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ideas-page { max-width: 960px; margin: 0 auto; padding: 24px 16px; }
.ideas-toolbar { display: flex; gap: 12px; margin-bottom: 16px; }
.ideas-search { flex: 1; padding: 8px 12px; border: 1px solid #ddd; border-radius: 6px; }
.ideas-tags { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; }
.ideas-tag { padding: 4px 12px; border: 1px solid #ddd; border-radius: 16px; background: #fff; cursor: pointer; }
.ideas-tag.active { background: #2563eb; color: #fff; border-color: #2563eb; }
.ideas-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }
.idea-card { border: 1px solid #eee; border-radius: 10px; padding: 16px; cursor: pointer; transition: box-shadow .15s; }
.idea-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,.08); }
.idea-title { margin: 0 0 8px; font-size: 18px; }
.idea-summary { color: #666; font-size: 14px; margin: 0 0 12px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.idea-meta { display: flex; gap: 12px; color: #999; font-size: 12px; flex-wrap: wrap; }
.idea-card-tags { margin-top: 8px; display: flex; gap: 6px; flex-wrap: wrap; }
.tag-chip { background: #f1f5f9; color: #475569; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.ideas-empty { text-align: center; color: #999; padding: 60px 0; }
.btn { display: inline-block; padding: 8px 16px; border-radius: 6px; text-decoration: none; border: none; cursor: pointer; }
.btn-primary { background: #2563eb; color: #fff; }
</style>
