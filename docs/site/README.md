# Pixel API 文档站

基于 [Next.js 16](https://nextjs.org) + [Fumadocs 16](https://fumadocs.dev) 的中文文档站，内容在 `content/docs`，站点代码在 `src`。

## 开发

```bash
pnpm install
pnpm dev        # http://localhost:3000
```

## 构建与运行

```bash
pnpm build
pnpm start      # 生产模式，默认 3000 端口
pnpm lint
```

> `/api/search` 是动态路由（Orama 中文分词搜索），部署需要 Node 运行时，不能纯静态导出。

## 目录结构

| 路径 | 说明 |
| --- | --- |
| `content/docs/(guide)` | 使用指南：普通用户教程、号主教程、功能参考、术语表 |
| `content/docs/wallet` | 钱包与商城：充值订阅、发卡商城、兑换码、订单发票、余额积分 |
| `content/docs/rewards` | 福利与邀请：消费抽奖福利活动、邀请返利、优惠码 |
| `content/docs/api` | API 参考：Chat/Images/Models/Antigravity 端点 |
| `content/docs/operations` | 帮助支持：问题排查、状态码说明、常见问题、安全使用 |
| `src/app/page.tsx` | 首页（hero、模块卡片、阅读路径、页脚） |
| `src/app/global.css` | 主题变量与全站设计系统 |
| `src/components/model-api-reference.tsx` | 可交互 API 调试组件（多语言示例 + Send） |
| `src/components/screenshot.tsx` | 截图组件（懒加载 + 点击放大） |
| `src/lib/source.ts` | 内容加载器与目录图标映射 |

## 写文档

- 每页是一个 `.mdx`，frontmatter 需要 `title` 和 `description`。
- 目录顺序由各级 `meta.json` 的 `pages` 决定；`root: true` 的目录会成为顶部模块。
- 可直接使用的组件：`Callout`、`Cards`/`Card`、`Steps`/`Step`、`Tabs`/`Tab`、`Accordions`/`Accordion`、`Screenshot`、`ModelApiReference`。
- 截图放在 `public/images/guide/`，用 `<Screenshot src="..." alt="...">说明文字</Screenshot>` 引用。
