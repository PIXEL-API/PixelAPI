<template>
  <div class="table-page-layout">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<style scoped>
.table-page-layout {
  @apply flex flex-col gap-6;
}

.table-page-layout.data-page {
  @apply min-w-0 gap-4 sm:gap-5;
}

.layout-section-fixed {
  @apply flex-shrink-0;
}

.table-page-layout.data-page .layout-section-fixed {
  @apply min-w-0;
}

.layout-section-scrollable {
  @apply flex min-h-0 flex-1 flex-col;
}

.table-page-layout.data-page .layout-section-scrollable {
  @apply min-w-0;
}

/* 表格滚动容器 - 增强版表体滚动方案 */
.table-scroll-container {
  @apply flex h-full flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800;
}

.table-page-layout.data-page .table-scroll-container {
  @apply rounded-panel border-line bg-surface shadow-panel;
}

.table-scroll-container :deep(.table-wrapper) {
  @apply flex-1 overflow-x-auto overflow-y-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  @apply bg-gray-50/80 backdrop-blur-sm dark:bg-dark-800/80;
}

.table-page-layout.data-page .table-scroll-container :deep(thead) {
  @apply bg-surface-subtle/95;
}

.table-scroll-container :deep(tbody) {
  /* 保持默认 table-row-group 显示，不使用 block */
}

.table-scroll-container :deep(th) {
  @apply border-b border-gray-200 px-5 py-4 text-left text-sm font-medium text-gray-600 dark:border-dark-700 dark:text-dark-300;
}

.table-page-layout.data-page .table-scroll-container :deep(th) {
  @apply border-line text-content-muted;
}

.table-scroll-container :deep(td) {
  @apply border-b border-gray-100 px-5 py-4 text-sm text-gray-700 dark:border-dark-800 dark:text-gray-300;
}

.table-page-layout.data-page .table-scroll-container :deep(td) {
  @apply border-line text-content-muted;
}

/* 仅在桌面且有足够垂直空间时启用表体内部滚动。 */
@media (min-width: 1024px) and (min-height: 720px) {
  .table-page-layout {
    height: calc(100vh - 64px - 4rem);
    height: calc(100dvh - 64px - 4rem);
  }
}

/* 移动端使用页面自然滚动，避免 100vh 与浏览器工具栏造成裁切。 */
@media (max-width: 1023px) {
  .table-page-layout .table-scroll-container,
  .table-page-layout.data-page .table-scroll-container {
    @apply h-auto overflow-visible border-none bg-transparent shadow-none;
  }

  .table-page-layout .layout-section-scrollable {
    @apply min-h-fit flex-none;
  }

  .table-page-layout .table-scroll-container :deep(.table-wrapper) {
    @apply overflow-x-auto overflow-y-visible;
  }

  .table-page-layout .table-scroll-container :deep(table) {
    @apply flex-none;
    display: table;
    min-width: 100%;
  }
}

/* 低高度桌面保留卡片边界，但改为外层自然滚动。 */
@media (min-width: 1024px) and (max-height: 719px) {
  .table-page-layout .layout-section-scrollable {
    @apply flex-none;
  }

  .table-page-layout .table-scroll-container {
    height: auto;
  }

}
</style>
