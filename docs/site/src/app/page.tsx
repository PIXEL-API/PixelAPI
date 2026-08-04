import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';

const modules = [
  {
    title: '使用指南',
    description: '普通用户与号主两条路径，从发送第一条消息到共享收益提现。',
    href: '/docs',
    accent: 'var(--accent-guide)',
    icon: (
      <svg fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M12 7v14" />
        <path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z" />
      </svg>
    ),
  },
  {
    title: '钱包与商城',
    description: '充值订阅、发卡商城、兑换码、订单发票、余额与积分，一站管理资金。',
    href: '/docs/wallet',
    accent: 'var(--accent-wallet)',
    icon: (
      <svg fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M3 8.5A2.5 2.5 0 0 1 5.5 6H18a2 2 0 0 1 2 2v1" />
        <path d="M4 7v11a2 2 0 0 0 2 2h13a1 1 0 0 0 1-1v-3" />
        <path d="M20 9a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-3.5a2 2 0 0 1 0-4z" />
      </svg>
    ),
  },
  {
    title: '福利与邀请',
    description: '消费抽奖福利活动、邀请返利和优惠码，用消费和分享换更多余额积分。',
    href: '/docs/rewards',
    accent: 'var(--accent-rewards)',
    icon: (
      <svg fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" aria-hidden="true">
        <rect x="3" y="8" width="18" height="4" rx="1" />
        <path d="M5 12v7a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-7" />
        <path d="M12 8v12" />
        <path d="M12 8S10.5 4 8.5 4a2.5 2.5 0 0 0 0 4z" />
        <path d="M12 8s1.5-4 3.5-4a2.5 2.5 0 0 1 0 4z" />
      </svg>
    ),
  },
  {
    title: 'API 参考',
    description: 'OpenAI、Claude、Gemini 兼容端点，附在线调试和多语言示例。',
    href: '/docs/api',
    accent: 'var(--accent-api)',
    icon: (
      <svg fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" aria-hidden="true">
        <polyline points="16 18 22 12 16 6" />
        <polyline points="8 6 2 12 8 18" />
      </svg>
    ),
  },
  {
    title: '帮助支持',
    description: '常见问题、状态码说明、安全使用建议与联系方式。',
    href: '/docs/operations/faq',
    accent: 'var(--accent-ops)',
    icon: (
      <svg fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" />
        <path d="m9 12 2 2 4-4" />
      </svg>
    ),
  },
];

const paths = [
  {
    tag: '普通用户',
    title: '想用别人的号池、共享账号发消息',
    accent: 'var(--accent-guide)',
    links: [
      { text: '一条龙跑通总览', href: '/docs/normal-first-message' },
      { text: '创建 API Key', href: '/docs/normal-create-api-key' },
      { text: '发送第一条消息', href: '/docs/normal-send-test-message' },
      { text: '使用账号广场', href: '/docs/normal-account-mode' },
      { text: '充值与订阅', href: '/docs/wallet/purchase' },
    ],
  },
  {
    tag: '号主用户',
    title: '想把自己的账号共享出去赚收益',
    accent: 'var(--accent-rewards)',
    links: [
      { text: '收益路径总览', href: '/docs/owner-start' },
      { text: '准备账号', href: '/docs/owner-prerequisites' },
      { text: '测试私有自用', href: '/docs/owner-test-private-account' },
      { text: '创建账号广场房间', href: '/docs/owner-account-marketplace' },
      { text: '提现和收款码', href: '/docs/owner-withdrawal-setup' },
    ],
  },
  {
    tag: '省钱 · 领福利',
    title: '想充值、购物、领活动和邀请返利',
    accent: 'var(--accent-wallet)',
    links: [
      { text: '发卡商城', href: '/docs/wallet/store' },
      { text: '兑换码', href: '/docs/wallet/redeem' },
      { text: '福利活动', href: '/docs/rewards/activities' },
      { text: '邀请返利', href: '/docs/rewards/affiliate' },
    ],
  },
];

const readingPath = [
  {
    mark: '01',
    title: '快速接入',
    description: '从账号、余额、分组、API Key、Base URL 到网页测试，先完整跑通第一条请求。',
    href: '/docs/normal-first-message',
  },
  {
    mark: '02',
    title: '搞懂概念',
    description: '一条链路讲清 API Key、分组、号池和上游账号的关系，后半页是全站术语速查。',
    href: '/docs/concepts',
  },
  {
    mark: '03',
    title: '出错排查',
    description: '按错误码逐条列出原因和处理步骤，从 401 到 5xx，附客户端配置不生效的排查顺序。',
    href: '/docs/normal-troubleshooting',
  },
];

const footerColumns = [
  {
    title: '入门',
    links: [
      { text: '快速开始', href: '/docs' },
      { text: '核心概念', href: '/docs/concepts' },
      { text: '发送第一条消息', href: '/docs/normal-send-test-message' },
    ],
  },
  {
    title: '钱包与福利',
    links: [
      { text: '充值与订阅', href: '/docs/wallet/purchase' },
      { text: '发卡商城', href: '/docs/wallet/store' },
      { text: '福利活动', href: '/docs/rewards/activities' },
    ],
  },
  {
    title: '开发者',
    links: [
      { text: 'API 总览', href: '/docs/api' },
      { text: 'ChatCompletions', href: '/docs/api/chat-completions' },
      { text: '列出模型', href: '/docs/api/models' },
    ],
  },
  {
    title: '支持',
    links: [
      { text: '常见问题', href: '/docs/operations/faq' },
      { text: '状态码说明', href: '/docs/operations/status-codes' },
      { text: '安全使用', href: '/docs/operations/security' },
    ],
  },
];

export default function HomePage() {
  return (
    <HomeLayout
      {...baseOptions()}
      links={[
        { text: '使用指南', url: '/docs', active: 'nested-url' },
        { text: '钱包与商城', url: '/docs/wallet', active: 'nested-url' },
        { text: '福利与邀请', url: '/docs/rewards', active: 'nested-url' },
        { text: 'API 参考', url: '/docs/api', active: 'nested-url' },
        { text: '控制台', url: 'https://ai-pixel.online', external: true },
      ]}
    >
      <main className="flex flex-1 flex-col bg-fd-background text-fd-foreground">
        <section className="home-hero">
          <div className="mx-auto flex max-w-4xl flex-col items-center px-5 pb-14 pt-20 text-center sm:px-6 md:pb-16 md:pt-28">
            <p className="home-badge">Pixel API 文档中心</p>
            <h1 className="mt-7 text-balance text-4xl font-bold leading-[1.2] tracking-tight md:text-[3.375rem]">
              清晰接入模型能力
              <br />
              <span className="home-title-accent">从容共享号池与收益</span>
            </h1>
            <p className="mt-6 max-w-2xl text-pretty text-base leading-8 text-fd-muted-foreground md:text-lg">
              普通用户跟着文档跑通第一次调用，号主把账号共享出去赚收益。
              充值、发卡商城、福利活动、账号广场、客户端接入和排查，都在同一个入口。
            </p>
            <div className="mt-9 flex flex-col gap-3 sm:flex-row">
              <Link href="/docs/normal-first-message" className="home-cta home-cta-primary">
                我是普通用户，开始使用
                <span aria-hidden="true">→</span>
              </Link>
              <Link href="/docs/owner-start" className="home-cta home-cta-secondary">
                我是号主，共享账号赚收益
              </Link>
            </div>
          </div>

          <div className="mx-auto max-w-3xl px-5 pb-20 sm:px-6">
            <div className="home-terminal-wrap">
              <div className="home-terminal" aria-label="Pixel API 最小验证请求示例">
                <div className="home-terminal-bar">
                  <span className="home-terminal-dot" />
                  <span className="home-terminal-dot" />
                  <span className="home-terminal-dot" />
                  <span>pixel-api — 最小验证请求</span>
                </div>
                <pre className="home-terminal-body">
                  <code>
                    <span className="tk-prompt">$</span> <span className="tk-cmd">curl</span> <span className="tk-str">https://ai-pixel.online/v1/chat/completions</span> {'\\'}
                    {'\n'}    <span className="tk-flag">-H</span> <span className="tk-str">&quot;Authorization: Bearer sk-你的密钥&quot;</span> {'\\'}
                    {'\n'}    <span className="tk-flag">-d</span> <span className="tk-dim">{'\'{ "model": "gpt-5.5", "messages": [...] }\''}</span>
                    {'\n'}
                    {'\n'}<span className="tk-dim">{'{'}</span>
                    {'\n'}  <span className="tk-key">&quot;choices&quot;</span><span className="tk-dim">: [ {'{'} </span><span className="tk-key">&quot;message&quot;</span><span className="tk-dim">: {'{'} </span><span className="tk-key">&quot;content&quot;</span><span className="tk-dim">: </span><span className="tk-str">&quot;Pixel API 已连接&quot;</span><span className="tk-dim"> {'}'} {'}'} ]</span>
                    {'\n'}<span className="tk-dim">{'}'}</span>
                  </code>
                </pre>
              </div>
            </div>
          </div>
        </section>

        <section className="border-t border-fd-border">
          <div className="mx-auto max-w-6xl px-5 py-16 sm:px-6 md:py-20">
            <div className="home-section-line mb-3">
              <p className="docs-eyebrow">五个模块</p>
            </div>
            <div className="mb-10 flex flex-col justify-between gap-3 md:flex-row md:items-end">
              <h2 className="text-2xl font-bold tracking-tight md:text-3xl">按使用场景组织的文档</h2>
              <p className="max-w-md text-sm leading-6 text-fd-muted-foreground">
                进入任意模块后，左侧目录会切换为该模块自己的目录树，右侧保留页面大纲与全文搜索。
              </p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {modules.map((item) => (
                <Link
                  className="home-card"
                  href={item.href}
                  key={item.title}
                  style={{ ['--card-accent' as string]: item.accent }}
                >
                  <span className="home-card-icon">{item.icon}</span>
                  <span className="home-card-arrow" aria-hidden="true">
                    →
                  </span>
                  <h3 className="text-base font-semibold">{item.title}</h3>
                  <p className="text-sm leading-6 text-fd-muted-foreground">{item.description}</p>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <section className="border-t border-fd-border bg-fd-secondary/40">
          <div className="mx-auto max-w-6xl px-5 py-16 sm:px-6 md:py-20">
            <div className="home-section-line mb-3">
              <p className="docs-eyebrow">按身份选路径</p>
            </div>
            <div className="mb-10 flex flex-col justify-between gap-3 md:flex-row md:items-end">
              <h2 className="text-2xl font-bold tracking-tight md:text-3xl">先选你的身份，再照着读</h2>
              <p className="max-w-md text-sm leading-6 text-fd-muted-foreground">
                三条路径互相独立，从哪条开始都可以，不确定就先看普通用户第一条。
              </p>
            </div>
            <div className="grid gap-4 md:grid-cols-3">
              {paths.map((col) => (
                <div
                  className="home-path"
                  key={col.tag}
                  style={{ ['--card-accent' as string]: col.accent }}
                >
                  <span className="home-path-tag">{col.tag}</span>
                  <h3 className="mt-3 text-base font-semibold leading-6">{col.title}</h3>
                  <ul className="mt-4 space-y-1.5">
                    {col.links.map((link) => (
                      <li key={link.href}>
                        <Link className="home-path-link" href={link.href}>
                          <span aria-hidden="true">›</span>
                          {link.text}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="border-t border-fd-border">
          <div className="mx-auto max-w-6xl px-5 py-16 sm:px-6 md:py-20">
            <div className="home-section-line mb-3">
              <p className="docs-eyebrow">推荐阅读路径</p>
            </div>
            <div className="mb-10 flex flex-col justify-between gap-3 md:flex-row md:items-end">
              <h2 className="text-2xl font-bold tracking-tight md:text-3xl">从接入到排查，一路读下去</h2>
              <Link
                className="text-sm font-semibold text-fd-primary transition hover:text-fd-primary/80"
                href="/docs"
              >
                浏览全部文档 →
              </Link>
            </div>
            <div className="grid gap-4 md:grid-cols-3">
              {readingPath.map((item) => (
                <Link className="home-step" href={item.href} key={item.href}>
                  <span className="home-step-num">{item.mark}</span>
                  <h3 className="mt-4 text-lg font-semibold">{item.title}</h3>
                  <p className="mt-2 text-sm leading-6 text-fd-muted-foreground">{item.description}</p>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <footer className="home-footer">
          <div className="mx-auto max-w-6xl px-5 py-12 sm:px-6">
            <div className="grid gap-10 md:grid-cols-[1.2fr_repeat(4,minmax(0,0.8fr))]">
              <div>
                <p className="text-base font-bold">Pixel API</p>
                <p className="mt-3 max-w-xs text-sm leading-6 text-fd-muted-foreground">
                  基于 Sub2API 构建的模型网关文档中心。面向普通用户与号主，
                  内容由 MDX 与 Fumadocs 生成，目录、搜索与 API 参考随章节同步更新。
                </p>
              </div>
              {footerColumns.map((column) => (
                <div key={column.title}>
                  <p className="text-sm font-semibold">{column.title}</p>
                  <ul className="mt-3 space-y-2.5">
                    {column.links.map((link) => (
                      <li key={link.href}>
                        <Link className="home-footer-link" href={link.href}>
                          {link.text}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
            <div className="mt-10 flex flex-col justify-between gap-2 border-t border-fd-border pt-6 text-xs text-fd-muted-foreground sm:flex-row">
              <p>© {new Date().getFullYear()} Pixel API · 私有部署文档</p>
              <p>由 Fumadocs 与 Next.js 驱动</p>
            </div>
          </div>
        </footer>
      </main>
    </HomeLayout>
  );
}
