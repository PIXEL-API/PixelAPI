import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';

export default function NotFound() {
  return (
    <HomeLayout {...baseOptions()}>
      <main className="flex flex-1 flex-col items-center justify-center px-5 py-24 text-center">
        <p className="home-badge">404</p>
        <h1 className="mt-6 text-3xl font-bold tracking-tight md:text-4xl">页面不存在</h1>
        <p className="mt-4 max-w-md text-base leading-7 text-fd-muted-foreground">
          你访问的页面可能已被移除、更名或暂时不可用。可以回到首页，或从文档目录重新进入。
        </p>
        <div className="mt-8 flex flex-col gap-3 sm:flex-row">
          <Link
            href="/"
            className="inline-flex min-h-11 items-center justify-center gap-2 rounded-lg bg-fd-primary px-6 py-2.5 text-sm font-semibold text-fd-primary-foreground shadow-sm transition hover:bg-fd-primary/90"
          >
            返回首页
          </Link>
          <Link
            href="/docs"
            className="inline-flex min-h-11 items-center justify-center gap-2 rounded-lg border border-fd-border px-6 py-2.5 text-sm font-semibold transition hover:bg-fd-accent"
          >
            浏览文档
          </Link>
        </div>
      </main>
    </HomeLayout>
  );
}
