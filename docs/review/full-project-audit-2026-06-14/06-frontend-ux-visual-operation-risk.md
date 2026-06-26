# 前端 UX、视觉与操作体验风险

## 覆盖范围

覆盖 `frontend/src/views`、`frontend/src/components`、`frontend/src/api`、`frontend/src/types`、`frontend/src/stores`、`frontend/src/composables`、`frontend/src/i18n`。重点看登录、注册、商城、支付、API Key、账号绑定、账号共享、管理后台、表单、弹窗、Toast、移动端可达性和前后端契约。

## [P1] 商城无限库存商品在前台被判售罄

- 状态：已确认问题
- 类型：前端体验 / 商城 / 业务阻断
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:489`
- 证据 1：`StoreView.vue:488` 到 `:490` 的 `isProductPurchasable` 未识别 `stock_unlimited`。
- 证据 2：`frontend\src\types\store.ts:65` 已定义 `stock_unlimited`，`frontend\src\views\admin\store\StoreProductsView.vue:23` 管理后台按无限库存展示。
- 触发场景：商品设置为无限库存且 `stock=0`。
- 用户体验：用户看到库存 0，购买按钮显示售罄，无法购买。
- 代码逻辑影响：管理后台配置语义和前台购买判定不一致。
- 风险后果：可售商品被阻断，影响收入和用户信任。
- 建议：购买判断、库存 badge、库存统计统一识别 `stock_unlimited`。
- 置信度：High

## [P2] 商城结算弹窗绕过标准 Dialog，移动端可能不可操作

- 状态：待确认风险
- 类型：前端体验 / 移动端 / 商城
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:147`
- 证据 1：`StoreView.vue:147` 手写 Teleport 面板 `w-full max-w-lg ... p-5`，没有 `max-height`、`overflow-y-auto` 或 body 滚动锁。
- 证据 2：全局标准样式 `frontend\src\style.css:379` 有 `max-h-[95vh]`，`:400` 有 `overflow-y-auto`；StoreView 没有复用 BaseDialog/标准 modal。
- 触发场景：移动端或低高度窗口打开结算，支付方式多、交付内容长。
- 用户体验：确认购买、关闭、交付内容可能被挤出视口，背景还能滚动。
- 代码逻辑影响：关键购买路径弹窗体验与全局 Dialog 规范不一致。
- 风险后果：用户无法完成购买或重复点击。
- 建议：改用 BaseDialog，或补 `max-h-[95vh] overflow-y-auto`、body 滚动锁和稳定 footer。
- 置信度：Medium

## [P2] BaseDialog 多弹窗场景会提前解除 body 滚动锁

- 状态：已确认问题
- 类型：前端体验 / 弹窗栈 / 移动端
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\components\common\BaseDialog.vue:133`
- 证据 1：`BaseDialog.vue:122` 打开时 `document.body.classList.add('modal-open')`，`:133` 关闭任意弹窗时无条件 remove，`:151` 卸载也无条件 remove。
- 证据 2：实际有父弹窗内开子弹窗用法：`frontend\src\components\user\UserAttributesConfigModal.vue:100`、`frontend\src\components\admin\TLSFingerprintProfilesModal.vue:124`、`frontend\src\components\account\CreateAccountModal.vue:2945`。
- 触发场景：父弹窗打开后再打开编辑/帮助/删除子弹窗，关闭子弹窗但父弹窗仍显示。
- 用户体验：底层页面恢复滚动，移动端可能滚到弹窗背后，焦点和滚动位置不稳定。
- 代码逻辑影响：全局滚动锁按单实例管理，无法支持弹窗栈。
- 风险后果：管理端和账号创建流程操作不稳定。
- 建议：滚动锁改为引用计数或弹窗栈管理，仅最后一个弹窗关闭时移除 `modal-open`。
- 置信度：High

## [P2] CreateAccountModal 传入不存在的 BaseDialog 宽度 prop

- 状态：已确认问题
- 类型：前端体验 / 组件契约
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\components\account\CreateAccountModal.vue:2949`
- 证据 1：`CreateAccountModal.vue:2949` 传入 `max-width="max-w-3xl"`。
- 证据 2：`frontend\src\components\common\BaseDialog.vue:59` 到 `:64` 只声明 `width?: DialogWidth`，`:92` 起按 `width` 计算宽度类，没有消费 `max-width`。
- 触发场景：账号创建/绑定流程中打开 Gemini 帮助弹窗。
- 用户体验：帮助弹窗没有变宽，长内容拥挤，需要更多滚动。
- 代码逻辑影响：调用方以为设置生效，实际被组件忽略。
- 风险后果：帮助内容可读性下降，用户更难完成账号配置。
- 建议：改为 `width="wide"`，或给 BaseDialog 增加受控宽度 prop。
- 置信度：High

## [P2] 商城购买数量允许小数

- 状态：已确认问题
- 类型：前端体验 / 表单校验 / API 契约
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:162`
- 证据 1：数量输入只有 `min/max`，没有 `step="1"`。
- 证据 2：`StoreView.vue:447` 只校验范围，`:624` 原样提交。
- 触发场景：用户输入 `1.5`。
- 用户体验：金额按小数计算，提交后才出现错误，用户不理解。
- 代码逻辑影响：前端与后端整数数量契约不一致。
- 风险后果：订单创建失败或异常反馈。
- 建议：增加整数校验和明确 Toast。
- 置信度：High

## [P2] 注册促销码/邀请码错误反馈断链

- 状态：已确认问题
- 类型：前端体验 / 注册路径 / 错误态
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\auth\RegisterView.vue:888`
- 证据 1：促销码/邀请码校验中或无效时只写 `errorMessage.value` 并 return。
- 证据 2：`RegisterView.vue:431` 的 `validationToastMessage` 不包含 `errorMessage`，`:458` 只 watch `validationToastMessage` 显示 Toast。
- 触发场景：用户在促销码/邀请码仍在校验或校验失败后点击注册。
- 用户体验：提交被拦截，但没有新的明确提示，像按钮无反应。
- 代码逻辑影响：错误状态写入未渲染/未通知变量。
- 风险后果：注册转化下降，用户重复点击或放弃。
- 建议：提前返回分支直接 `appStore.showError(errorMessage.value)`，或统一错误提示通道。
- 置信度：Medium

## [P2] Toast 在 320px 窄屏可能横向溢出

- 状态：已确认问题
- 类型：前端体验 / 移动端 / 全局反馈
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\components\common\Toast.vue:20`
- 证据 1：Toast 容器固定 `right-4 top-4`。
- 证据 2：Toast 本体固定 `min-w-[320px] max-w-md`，320px 视口还叠加右侧 16px 偏移。
- 触发场景：移动端窄屏触发登录、支付、商城、API 错误 Toast。
- 用户体验：Toast 左侧或关闭按钮可能被裁切，长错误不可读。
- 代码逻辑影响：全局反馈组件没有移动端响应式约束。
- 风险后果：用户看不清关键错误，重复操作或误判成功。
- 建议：移动端使用 `left-4 right-4 w-auto min-w-0`，`sm` 以上再恢复桌面宽度。
- 置信度：High

## [P3] 大型组件过大，关键路径维护风险高

- 状态：已确认问题
- 类型：维护性 / 前端架构
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\components\account\CreateAccountModal.vue`
- 证据 1：`CreateAccountModal.vue` 超过 5000 行，`EditAccountModal.vue` 超过 3800 行，`AccountShareView.vue` 超过 4600 行。
- 证据 2：这些组件同时承载表单、权限、API 调用、校验、帮助弹窗和状态恢复。
- 触发场景：新增账号共享、OAuth、模型映射、管理员批量编辑等功能。
- 用户体验：小改动容易引入局部状态回归，表现为弹窗不刷新、按钮不可用、提示不一致。
- 代码逻辑影响：组件职责过多，测试和复用困难。
- 风险后果：回归概率增加，关键路径缺陷难定位。
- 建议：按表单区块、API adapter、校验器、帮助弹窗、状态机拆分；每次拆分配套组件测试。
- 置信度：High
