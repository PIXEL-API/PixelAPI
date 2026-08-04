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
pnpm images     # 压缩截图并生成尺寸 manifest（加了新截图才需要跑）
```

> `/api/search` 是动态路由（Orama 中文分词搜索），部署需要 Node 运行时，不能纯静态导出。

## 目录结构

| 路径 | 说明 |
| --- | --- |
| `content/docs/(guide)` | 使用指南：普通用户教程、号主教程、进阶参考、核心概念与术语 |
| `content/docs/wallet` | 钱包与商城：充值订阅、发卡商城、兑换码、订单发票、余额积分 |
| `content/docs/rewards` | 福利与邀请：消费抽奖福利活动、邀请返利、优惠码 |
| `content/docs/api` | API 参考：Chat/Images/Models/Antigravity 端点 |
| `content/docs/operations` | 帮助支持：常见问题、状态码、安全使用、联系方式、更新日志 |
| `assets/screenshots` | 截图**源文件**（不对外提供，只作源仓库） |
| `public/images/guide` | `pnpm images` 生成的定宽 WebP 产物 |
| `scripts/optimize-screenshots.mjs` | 截图压缩脚本 |
| `src/app/page.tsx` | 首页（hero、模块卡片、阅读路径、页脚） |
| `src/app/global.css` | 主题变量与全站设计系统 |
| `src/components/model-api-reference.tsx` | 可交互 API 调试组件（多语言示例 + Send） |
| `src/components/screenshot.tsx` | 截图组件（尺寸取自 manifest，懒加载 + 点击放大） |
| `src/lib/source.ts` | 内容加载器与目录图标映射 |

## 写文档

- 每页是一个 `.mdx`，frontmatter 需要 `title` 和 `description`。
- 目录顺序由各级 `meta.json` 的 `pages` 决定；`root: true` 的目录会成为顶部模块。
- 可直接使用的组件：`Callout`、`Cards`/`Card`、`Steps`/`Step`、`Tabs`/`Tab`、`Accordions`/`Accordion`、`Screenshot`、`ModelApiReference`、`QqGroups`。

### 内容约定

这几条是为了避免同一件事在多个页面各写一遍——重复的页面会让搜索结果撞车，读者不知道该看哪个。

- **教程页讲「怎么做」，参考页讲「是什么／取什么值」**，两者交叉链接，不互相复述。
  例如 `owner-pricing-limits` 讲参数该怎么权衡，`owner-params` 只列默认值和取值范围。
- **不要在页面里重复侧栏已有的目录。** 侧栏就是阅读顺序，页内再列一遍「你应该按什么顺序读」只会挤掉正文。
- **不要写「成功标准」这类元话语小节。** 需要提醒的判断标准直接写进步骤里。
- **表格只用于真正需要逐项对照查的内容**（状态码、参数取值、枚举、字段图例）。
  两三行的表格改写成句子；教学步骤用 `Steps` 或有序列表。
- **计费规则统一在 `(guide)/(user)/billing.mdx`**，其他页面链过去，不重复描述倍率、小时费、分成和提现门槛。

### 加截图

1. 原始 PNG 放进 `assets/screenshots/`。
2. 跑 `pnpm images`——会输出定宽 1600px 的 WebP 到 `public/images/guide/`，并把尺寸写进 `src/components/screenshot-manifest.ts`。
3. MDX 里用 `<Screenshot src="/images/guide/<名字>.webp" alt="...">说明文字</Screenshot>` 引用。

尺寸写进 manifest 是为了让 `<img>` 带上 `width/height`，图片加载完不会再撑开一次布局。忘了跑 `pnpm images` 的话构建会直接报错提醒你。
