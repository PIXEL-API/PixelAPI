import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { RootProvider } from 'fumadocs-ui/provider/next';
import './global.css';

export const metadata: Metadata = {
  title: {
    default: 'Pixel API 文档',
    template: '%s | Pixel API 文档',
  },
  description: 'Pixel API 使用指南、账号共享、API 接入和问题排查文档。',
  openGraph: {
    title: 'Pixel API 文档',
    description: 'Pixel API 使用指南、账号共享、API 接入和问题排查文档。',
    siteName: 'Pixel API 文档',
    type: 'website',
    locale: 'zh_CN',
  },
};

const zhTranslations = {
  'Search(search dialog)': '搜索文档',
  'Search(search trigger)': '搜索文档',
  'No results found(search dialog)': '没有找到相关内容',
  'Open Search(search trigger)(aria-label)': '打开搜索',
  'Close Search(search dialog)(aria-label)': '关闭搜索',
  'On this page(table of contents)': '本页目录',
  'Table of Contents(inline table of contents)': '目录',
  'No Headings(table of contents)': '本页没有标题',
  'Next Page(pagination)': '下一页',
  'Previous Page(pagination)': '上一页',
  'Last updated on(page footer)': '最后更新于',
  'Edit on GitHub(edit page)': '在 GitHub 上编辑',
  'Back to Home(404 page)': '返回首页',
  'Page Not Found(404 page)': '页面不存在',
  'The page you are looking for might have been removed, had its name changed, or is temporarily unavailable.(404 page)':
    '你访问的页面可能已被移除、更名或暂时不可用。',
  'Copy Markdown(page actions)': '复制 Markdown',
  'Open(page actions)': '打开',
  'Copied Text(code block)(aria-label)': '已复制',
  'Copy Text(code block)(aria-label)': '复制代码',
  'Copy Anchor Link(heading anchor)(aria-label)': '复制锚点链接',
  'Toggle Theme(theme switcher)(aria-label)': '切换主题',
  'Light(theme switcher)(aria-label)': '浅色',
  'Dark(theme switcher)(aria-label)': '深色',
  'System(theme switcher)(aria-label)': '跟随系统',
  'Toggle Menu(mobile menu)(aria-label)': '打开菜单',
  'Open Sidebar(sidebar)(aria-label)': '展开侧栏',
  'Collapse Sidebar(sidebar)(aria-label)': '收起侧栏',
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <RootProvider
          i18n={{
            locale: 'zh-CN',
            translations: zhTranslations,
          }}
          search={{
            options: {
              api: '/api/search',
            },
          }}
        >
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
