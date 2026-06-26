# 前端体验、状态与安全审查

## 已确认问题

### F-01 P2 商城平台支付抽奖后，顶部余额/积分不会及时刷新

- 位置：`frontend/src/views/StoreView.vue:724`、`backend/internal/service/shop.go:998`、`backend/internal/service/shop.go:1014`
- 证据：`handlePaymentSuccess()` 平台支付成功后等待订单 delivered，只在 `order.load_factor_credits_awarded > 0` 时调用 `authStore.refreshUser()`。
- 对照证据：后端平台支付抽奖会在余额抽奖时 `AddBalance(drawReward.Amount)`，在积分抽奖时 `applyPointsAdjustmentInTx`。余额/积分直付路径在 `StoreView.vue:633-638` 有本地更新逻辑。
- 影响：用户完成余额/积分抽奖商品的平台支付后，结果页能看到奖励，但导航栏余额/积分仍可能旧值，直到自动刷新或手动刷新。
- 建议：平台支付成功后，只要订单含 `draw_reward_amount`，或商品类型为余额/积分抽奖，就刷新 `authStore`。`PaymentResultView` 成功加载商城订单后也应刷新。
- 置信度：高。

### F-02 P2 公告弹窗在部分页面关闭后可能永久锁住页面滚动

- 位置：`frontend/src/components/common/AnnouncementPopup.vue:113`、`frontend/src/components/common/AnnouncementBell.vue:415`、`frontend/src/App.vue:118`
- 证据：`AnnouncementPopup` 只在 popup 出现时设置 `document.body.style.overflow = 'hidden'`，关闭时不恢复。
- 对照证据：恢复逻辑在 `AnnouncementBell` 的 watch 中，但 `AnnouncementBell` 只随 `AppHeader` 出现；`AnnouncementPopup` 是 `App.vue` 全局渲染。
- 影响：已登录用户在 `/home`、支付结果页等不渲染 Header 的页面触发公告后，关闭公告可能仍无法滚动。
- 建议：滚动锁由 `AnnouncementPopup` 自己成对管理，关闭和卸载时恢复。更稳的方式是封装 ref-count scroll-lock composable。
- 置信度：高。

### F-03 P2 管理端批量删除账号确认和失败反馈不足

- 位置：`frontend/src/views/admin/AccountsView.vue:1256`、`frontend/src/i18n/locales/zh.ts:261`、`frontend/src/i18n/locales/zh.ts:4132`
- 证据：批量删除使用原生 `confirm(t('common.confirm'))`，中文文案只有“确认”；删除失败只 `console.error`，没有 toast 或表单反馈。
- 对照证据：同文件单账号删除使用 `ConfirmDialog` 和具体账号名；i18n 已有 `bulkDeleteTitle`、`bulkDeleteConfirm`、`bulkDeleteSuccess`、`bulkDeletePartial`，但当前未使用。
- 影响：管理员看不到选中数量和不可撤销提示，失败时也可能误以为删除成功。该操作直接影响账号资产，属于高风险 UX 缺陷。
- 建议：复用 `ConfirmDialog`，展示数量、不可撤销说明、提交中状态、防重复点击、成功/失败 toast。失败时保留选择并显示具体错误。
- 置信度：高。

### F-04 P2 首页自定义 HTML 与 localStorage token 组合放大 XSS 影响面

- 位置：`frontend/src/views/HomeView.vue:11`、`frontend/src/api/auth.ts:34`、`frontend/src/api/client.ts:58`
- 证据：首页对 `homeContent` 直接 `v-html`，注释说明“admin-only setting, XSS risk is acceptable”。认证 token 和 refresh token 存储在 localStorage，API 请求从 localStorage 读取 Bearer token。
- 对照证据：管理端设置页保存 `form.home_content` 到 `home_content`，见 `SettingsView.vue:4237` 和 `SettingsView.vue:7955`。
- 影响：如果管理员账号被盗，或设置写入链路被滥用，攻击者可以在公共首页执行脚本并读取 token。单独的 admin-only HTML 是产品选择，但和 localStorage token 组合后影响面明显扩大。
- 建议：如果目标是富文本首页，使用 DOMPurify sanitize。若目标是完全自定义页面，改用 sandbox iframe，并限制 `allow`、`referrerpolicy`、导航能力；中长期考虑 httpOnly refresh cookie 降低 XSS 后 token 外泄。
- 置信度：中高，是否允许完全可信 HTML 需要产品确认。

### F-05 P2 首次导航依赖注入配置，注入缺失时路由门禁可能误判

- 位置：`frontend/src/router/index.ts:870`、`frontend/src/router/index.ts:880`、`backend/internal/web/embed_on.go:170`
- 证据：`/purchase` 标记 `requiresPayment`，守卫直接读 `appStore.cachedPublicSettings?.payment_enabled`，未加载时为 falsy 并重定向。`/admin/risk-control` 同理依赖 `risk_control_enabled === true`。
- 对照证据：后端 embed 注入失败时会直接返回未注入的 HTML，见 `embed_on.go:170-175` 和 `embed_on.go:178-180`。
- 影响：配置注入失败或缺字段时，用户直达购买页/风控页可能被错误踢回；配置未知时的后端模式限制也可能判断不准。
- 建议：路由守卫在评估 feature meta 前先确保 public settings 已加载，或统一复用 featureFlags 的 opt-in/opt-out 语义。
- 置信度：中高，建议补一个无 `window.__APP_CONFIG__` 的首屏路由测试。

### F-06 P2 账号模式用户页用中文名称识别模式分组

- 位置：`frontend/src/views/user/AccountShareView.vue:1456`
- 证据：用户页通过分组名称等于或包含“账号模式”识别账号模式分组。
- 影响：管理员重命名分组后，用户页可能不再展示正确 API key 或绑定状态。
- 建议：见后端报告 B-05，前后端改为结构化字段契约。
- 置信度：高。

## 待确认风险

### F-R01 P3 首页 iframe 模式缺少 sandbox 和 referrerpolicy

- 位置：`frontend/src/views/HomeView.vue:5`
- 证据：当 `homeContent` 是 URL 时，首页直接用全屏 iframe 渲染，并设置 `allowfullscreen`，没有 sandbox/referrerpolicy。
- 风险：第三方页面能接管公共首页视觉和部分交互体验。是否允许完全接管首页需要产品确认。
- 建议：如果作为运营页面嵌入，应加 sandbox 白名单和 referrerpolicy；如果作为“完全替换首页”，设置页应明确告知管理员风险。
- 置信度：中。

## 已覆盖但未列为问题

- 公告 Markdown 渲染不是同类 XSS 问题：`AnnouncementPopup.vue` 使用 marked 后再 DOMPurify sanitize。
- 法律文档 Markdown 渲染不是同类 XSS 问题：`LegalDocumentView.vue` 使用 DOMPurify。
- AppSidebar 的自定义 SVG 使用 sanitizeSvg，未按 v-html 直接风险列入。
