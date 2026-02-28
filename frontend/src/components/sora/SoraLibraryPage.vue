<template>
  <div class="sora-gallery-page">
    <!-- 筛选栏 -->
    <div class="sora-gallery-filter-bar">
      <div class="sora-gallery-filters">
        <button
          v-for="f in filters"
          :key="f.value"
          :class="['sora-gallery-filter', activeFilter === f.value && 'active']"
          @click="activeFilter = f.value"
        >
          {{ f.label }}
        </button>
      </div>
      <span class="sora-gallery-count">
        {{ t('sora.galleryCount', { count: filteredItems.length }) }}
      </span>
    </div>

    <!-- 作品网格 -->
    <div v-if="filteredItems.length > 0" class="sora-gallery-grid">
      <div
        v-for="item in filteredItems"
        :key="item.id"
        class="sora-gallery-card"
        @click="openPreview(item)"
      >
        <div class="sora-gallery-card-thumb">
          <!-- 媒体 -->
          <video
            v-if="item.media_type === 'video' && item.media_url"
            :src="item.media_url"
            class="sora-gallery-card-image"
            muted
            loop
            @mouseenter="($event.target as HTMLVideoElement).play()"
            @mouseleave="($event.target as HTMLVideoElement).pause()"
          />
          <img
            v-else-if="item.media_url"
            :src="item.media_url"
            class="sora-gallery-card-image"
            alt=""
          />
          <div v-else class="sora-gallery-card-image sora-gallery-card-placeholder" :class="getGradientClass(item.id)">
            {{ item.media_type === 'video' ? '🎬' : '🎨' }}
          </div>

          <!-- 类型角标 -->
          <span
            class="sora-gallery-card-badge"
            :class="item.media_type === 'video' ? 'video' : 'image'"
          >
            {{ item.media_type === 'video' ? 'VIDEO' : 'IMAGE' }}
          </span>

          <!-- Hover 操作层 -->
          <div class="sora-gallery-card-overlay">
            <button
              v-if="item.media_url"
              class="sora-gallery-card-action"
              title="下载"
              @click.stop="handleDownload(item)"
            >
              📥
            </button>
            <button
              class="sora-gallery-card-action"
              title="删除"
              @click.stop="handleDelete(item.id)"
            >
              🗑
            </button>
          </div>

          <!-- 视频播放指示 -->
          <div v-if="item.media_type === 'video'" class="sora-gallery-card-play">▶</div>

          <!-- 视频时长 -->
          <span v-if="item.media_type === 'video'" class="sora-gallery-card-duration">
            {{ formatDuration(item) }}
          </span>
        </div>

        <!-- 卡片底部信息 -->
        <div class="sora-gallery-card-info">
          <div class="sora-gallery-card-model">{{ item.model }}</div>
          <div class="sora-gallery-card-time">{{ formatTime(item.created_at) }}</div>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <div v-else-if="!loading" class="sora-gallery-empty">
      <div class="sora-gallery-empty-icon">🎬</div>
      <h2 class="sora-gallery-empty-title">{{ t('sora.galleryEmptyTitle') }}</h2>
      <p class="sora-gallery-empty-desc">{{ t('sora.galleryEmptyDesc') }}</p>
      <button class="sora-gallery-empty-btn" @click="emit('switchToGenerate')">
        {{ t('sora.startCreating') }}
      </button>
    </div>

    <!-- 加载更多 -->
    <div v-if="hasMore && filteredItems.length > 0" class="sora-gallery-load-more">
      <button
        class="sora-gallery-load-more-btn"
        :disabled="loading"
        @click="loadMore"
      >
        {{ loading ? t('sora.loading') : t('sora.loadMore') }}
      </button>
    </div>

    <!-- 预览弹窗 -->
    <SoraMediaPreview
      :visible="previewVisible"
      :generation="previewItem"
      @close="previewVisible = false"
      @save="handleSaveFromPreview"
      @download="handleDownloadUrl"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import soraAPI, { type SoraGeneration } from '@/api/sora'
import SoraMediaPreview from './SoraMediaPreview.vue'

const emit = defineEmits<{
  'switchToGenerate': []
}>()

const { t } = useI18n()

const items = ref<SoraGeneration[]>([])
const loading = ref(false)
const page = ref(1)
const hasMore = ref(true)
const activeFilter = ref('all')
const previewVisible = ref(false)
const previewItem = ref<SoraGeneration | null>(null)

const filters = computed(() => [
  { value: 'all', label: t('sora.filterAll') },
  { value: 'video', label: t('sora.filterVideo') },
  { value: 'image', label: t('sora.filterImage') }
])

const filteredItems = computed(() => {
  if (activeFilter.value === 'all') return items.value
  return items.value.filter(i => i.media_type === activeFilter.value)
})

const gradientClasses = [
  'gradient-bg-1', 'gradient-bg-2', 'gradient-bg-3', 'gradient-bg-4',
  'gradient-bg-5', 'gradient-bg-6', 'gradient-bg-7', 'gradient-bg-8'
]

function getGradientClass(id: number): string {
  return gradientClasses[id % gradientClasses.length]
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  if (diff < 60000) return t('sora.justNow')
  if (diff < 3600000) return t('sora.minutesAgo', { n: Math.floor(diff / 60000) })
  if (diff < 86400000) return t('sora.hoursAgo', { n: Math.floor(diff / 3600000) })
  if (diff < 2 * 86400000) return t('sora.yesterday')
  return d.toLocaleDateString()
}

function formatDuration(item: SoraGeneration): string {
  // 从模型名提取时长，如 sora2-landscape-10s -> 0:10
  const match = item.model.match(/(\d+)s$/)
  if (match) {
    const sec = parseInt(match[1])
    return `0:${sec.toString().padStart(2, '0')}`
  }
  return '0:10'
}

async function loadItems(pageNum: number) {
  loading.value = true
  try {
    const res = await soraAPI.listGenerations({
      status: 'completed',
      storage_type: 's3,local',
      page: pageNum,
      page_size: 20
    })
    const rows = Array.isArray(res.data) ? res.data : []
    if (pageNum === 1) {
      items.value = rows
    } else {
      items.value.push(...rows)
    }
    hasMore.value = items.value.length < res.total
  } catch (e) {
    console.error('Failed to load library:', e)
  } finally {
    loading.value = false
  }
}

function loadMore() {
  page.value++
  loadItems(page.value)
}

function openPreview(item: SoraGeneration) {
  previewItem.value = item
  previewVisible.value = true
}

async function handleDelete(id: number) {
  if (!confirm(t('sora.confirmDelete'))) return
  try {
    await soraAPI.deleteGeneration(id)
    items.value = items.value.filter(i => i.id !== id)
  } catch (e) {
    console.error('Delete failed:', e)
  }
}

function handleDownload(item: SoraGeneration) {
  if (item.media_url) {
    window.open(item.media_url, '_blank')
  }
}

function handleDownloadUrl(url: string) {
  window.open(url, '_blank')
}

async function handleSaveFromPreview(id: number) {
  try {
    await soraAPI.saveToStorage(id)
    const gen = await soraAPI.getGeneration(id)
    const idx = items.value.findIndex(i => i.id === id)
    if (idx >= 0) items.value[idx] = gen
  } catch (e) {
    console.error('Save failed:', e)
  }
}

onMounted(() => loadItems(1))
</script>

<style scoped>
.sora-gallery-page {
  padding: 24px;
  padding-bottom: 40px;
}

/* 筛选栏 */
.sora-gallery-filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;
}

.sora-gallery-filters {
  display: flex;
  gap: 4px;
  background: var(--sora-bg-secondary, #1A1A1A);
  border-radius: var(--sora-radius-full, 9999px);
  padding: 3px;
}

.sora-gallery-filter {
  padding: 6px 18px;
  border-radius: var(--sora-radius-full, 9999px);
  font-size: 13px;
  font-weight: 500;
  color: var(--sora-text-secondary, #A0A0A0);
  background: none;
  border: none;
  cursor: pointer;
  transition: all 150ms ease;
  user-select: none;
}

.sora-gallery-filter:hover {
  color: var(--sora-text-primary, #FFF);
}

.sora-gallery-filter.active {
  background: var(--sora-bg-tertiary, #242424);
  color: var(--sora-text-primary, #FFF);
}

.sora-gallery-count {
  font-size: 13px;
  color: var(--sora-text-tertiary, #666);
}

/* 网格 */
.sora-gallery-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

/* 卡片 */
.sora-gallery-card {
  position: relative;
  border-radius: var(--sora-radius-md, 12px);
  overflow: hidden;
  background: var(--sora-bg-secondary, #1A1A1A);
  border: 1px solid var(--sora-border-color, #2A2A2A);
  cursor: pointer;
  transition: all 250ms ease;
}

.sora-gallery-card:hover {
  border-color: var(--sora-bg-hover, #333);
  transform: translateY(-2px);
  box-shadow: var(--sora-shadow-lg, 0 8px 32px rgba(0,0,0,0.5));
}

.sora-gallery-card-thumb {
  position: relative;
  width: 100%;
  aspect-ratio: 16/9;
  overflow: hidden;
}

.sora-gallery-card-image {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 400ms ease;
}

.sora-gallery-card:hover .sora-gallery-card-image {
  transform: scale(1.05);
}

.sora-gallery-card-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
}

/* 渐变背景 */
.gradient-bg-1 { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); }
.gradient-bg-2 { background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%); }
.gradient-bg-3 { background: linear-gradient(135deg, #4facfe 0%, #00f2fe 100%); }
.gradient-bg-4 { background: linear-gradient(135deg, #43e97b 0%, #38f9d7 100%); }
.gradient-bg-5 { background: linear-gradient(135deg, #fa709a 0%, #fee140 100%); }
.gradient-bg-6 { background: linear-gradient(135deg, #a18cd1 0%, #fbc2eb 100%); }
.gradient-bg-7 { background: linear-gradient(135deg, #fccb90 0%, #d57eeb 100%); }
.gradient-bg-8 { background: linear-gradient(135deg, #e0c3fc 0%, #8ec5fc 100%); }

/* 类型角标 */
.sora-gallery-card-badge {
  position: absolute;
  top: 8px;
  left: 8px;
  padding: 3px 8px;
  border-radius: var(--sora-radius-sm, 8px);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  backdrop-filter: blur(8px);
}

.sora-gallery-card-badge.video {
  background: rgba(20, 184, 166, 0.8);
  color: white;
}

.sora-gallery-card-badge.image {
  background: rgba(16, 185, 129, 0.8);
  color: white;
}

/* Hover 操作层 */
.sora-gallery-card-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  opacity: 0;
  transition: opacity 150ms ease;
}

.sora-gallery-card:hover .sora-gallery-card-overlay {
  opacity: 1;
}

.sora-gallery-card-action {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.15);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: white;
  border: none;
  cursor: pointer;
  transition: all 150ms ease;
}

.sora-gallery-card-action:hover {
  background: rgba(255, 255, 255, 0.25);
  transform: scale(1.1);
}

/* 播放指示 */
.sora-gallery-card-play {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.2);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  color: white;
  opacity: 0;
  transition: all 150ms ease;
  pointer-events: none;
}

.sora-gallery-card:hover .sora-gallery-card-play {
  opacity: 1;
}

/* 视频时长 */
.sora-gallery-card-duration {
  position: absolute;
  bottom: 8px;
  right: 8px;
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.7);
  font-size: 11px;
  font-family: "SF Mono", "Fira Code", monospace;
  color: white;
}

/* 卡片信息 */
.sora-gallery-card-info {
  padding: 12px;
}

.sora-gallery-card-model {
  font-size: 11px;
  font-family: "SF Mono", "Fira Code", monospace;
  color: var(--sora-text-tertiary, #666);
  margin-bottom: 4px;
}

.sora-gallery-card-time {
  font-size: 12px;
  color: var(--sora-text-muted, #4A4A4A);
}

/* 空状态 */
.sora-gallery-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 120px 40px;
  text-align: center;
}

.sora-gallery-empty-icon {
  font-size: 64px;
  margin-bottom: 24px;
  opacity: 0.3;
}

.sora-gallery-empty-title {
  font-size: 20px;
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--sora-text-secondary, #A0A0A0);
}

.sora-gallery-empty-desc {
  font-size: 14px;
  color: var(--sora-text-tertiary, #666);
  max-width: 360px;
  line-height: 1.6;
}

.sora-gallery-empty-btn {
  margin-top: 24px;
  padding: 10px 28px;
  background: var(--sora-accent-gradient, linear-gradient(135deg, #14b8a6, #0d9488));
  border-radius: var(--sora-radius-full, 9999px);
  font-size: 14px;
  font-weight: 500;
  color: white;
  border: none;
  cursor: pointer;
  transition: all 150ms ease;
}

.sora-gallery-empty-btn:hover {
  box-shadow: var(--sora-shadow-glow, 0 0 20px rgba(20,184,166,0.3));
}

/* 加载更多 */
.sora-gallery-load-more {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}

.sora-gallery-load-more-btn {
  padding: 10px 28px;
  background: var(--sora-bg-secondary, #1A1A1A);
  border: 1px solid var(--sora-border-color, #2A2A2A);
  border-radius: var(--sora-radius-full, 9999px);
  font-size: 13px;
  color: var(--sora-text-secondary, #A0A0A0);
  cursor: pointer;
  transition: all 150ms ease;
}

.sora-gallery-load-more-btn:hover {
  background: var(--sora-bg-tertiary, #242424);
  color: var(--sora-text-primary, #FFF);
}

.sora-gallery-load-more-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 响应式 */
@media (max-width: 1200px) {
  .sora-gallery-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 900px) {
  .sora-gallery-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 600px) {
  .sora-gallery-page {
    padding: 16px;
  }

  .sora-gallery-grid {
    grid-template-columns: 1fr;
  }
}
</style>
