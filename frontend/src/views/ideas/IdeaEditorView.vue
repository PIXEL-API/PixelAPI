<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getIdea, createIdea, updateIdea, publishIdea } from '@/api/ideas'

const route = useRoute()
const router = useRouter()
const postId = route.params.id ? Number(route.params.id) : null

const title = ref('')
const summary = ref('')
const body = ref('')
const tagsInput = ref('')
const saving = ref(false)
const error = ref('')

async function save(publish: boolean) {
  saving.value = true
  error.value = ''
  try {
    const tags = tagsInput.value.split(/[,，]/).map((s) => s.trim()).filter(Boolean)
    const payload = { title: title.value, summary: summary.value, body: body.value, tags }
    let post
    if (postId) {
      post = await updateIdea(postId, payload)
    } else {
      post = await createIdea(payload)
    }
    if (publish && post.id) {
      post = await publishIdea(post.id)
    }
    router.push(`/ideas/${post.id}`)
  } catch (e: any) {
    error.value = e?.message || '保存失败'
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  if (postId) {
    const post = await getIdea(postId)
    if (post.revision) {
      title.value = post.revision.title
      summary.value = post.revision.summary
      body.value = post.revision.body
    }
    tagsInput.value = (post.tags || []).map((t) => t.name).join(', ')
  }
})
</script>

<template>
  <div class="idea-editor-page">
    <h1>{{ postId ? '编辑文章' : '写文章' }}</h1>
    <div v-if="error" class="editor-error">{{ error }}</div>
    <input v-model="title" class="editor-title" placeholder="标题" maxlength="120" />
    <textarea v-model="summary" class="editor-summary" placeholder="摘要（可选）" maxlength="500" rows="2"></textarea>
    <textarea v-model="body" class="editor-body" placeholder="正文（支持 Markdown）" rows="20"></textarea>
    <input v-model="tagsInput" class="editor-tags" placeholder="标签，用逗号分隔（最多 5 个）" />

    <div class="editor-actions">
      <button class="btn" :disabled="saving" @click="save(false)">保存草稿</button>
      <button class="btn btn-primary" :disabled="saving" @click="save(true)">发布</button>
      <router-link class="btn" to="/ideas/mine">返回我的文章</router-link>
    </div>
  </div>
</template>

<style scoped>
.idea-editor-page { max-width: 800px; margin: 0 auto; padding: 24px 16px; display: flex; flex-direction: column; gap: 14px; }
.editor-error { color: #dc2626; }
.editor-title, .editor-summary, .editor-body, .editor-tags { width: 100%; padding: 10px 12px; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; box-sizing: border-box; }
.editor-title { font-size: 20px; }
.editor-body { font-family: inherit; resize: vertical; }
.editor-actions { display: flex; gap: 10px; }
.btn { padding: 8px 16px; border: 1px solid #ddd; border-radius: 6px; background: #fff; cursor: pointer; text-decoration: none; color: #333; }
.btn-primary { background: #2563eb; color: #fff; border: none; }
</style>
