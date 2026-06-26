# 数据、支付、账务与计费风险

## 正向控制

- 支付 webhook handler 有 body size 限制、provider lookup、notification verify 和 unknown order ack 策略。
- 商城余额支付链路在 `backend\internal\service\shop.go` 中使用事务、用户余额锁定和 ledger 记录。
- `redeem_service.go`、`promo_service.go`、`shop.go` 等余额变更后存在 billing cache invalidation，对比显示退款路径是特例缺口。

## [P1] 支付网关退款 pending 被本地标记为成功

- 状态：已确认问题
- 类型：资金 / 支付 / 状态机
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\payment_refund.go:348`
- 证据 1：`payment_refund.go:348` 调用 `prov.Refund` 后丢弃 `RefundResponse`，`ExecuteRefund` 在 `payment_refund.go:324` 到 `:327` 只要 `gwRefund` 无 error 就执行 `markRefundOk`。
- 证据 2：provider 明确可能返回 pending：`backend\internal\payment\provider\wxpay.go:487`、`backend\internal\payment\provider\stripe.go:207`、`backend\internal\payment\provider\alipay.go:331`；状态定义在 `backend\internal\payment\types.go:67` 到 `:71`。
- 触发场景：微信、Stripe、支付宝退款请求被受理但尚未最终结算，provider 返回 pending 且无 error。
- 用户体验：后台和用户订单显示已退款，但支付渠道后续可能失败或仍处理中。
- 代码逻辑影响：本地订单终态早于 provider 终态；余额/订阅扣减已经发生。
- 风险后果：账实不一致，用户权益已扣但真实退款未成功，后续对账和客服处理困难。
- 建议：只有 `ProviderStatusSuccess` 才标记 refunded；pending 应落库为退款处理中，持久化 `refund_id/status`，通过轮询或 webhook 终结。
- 置信度：High

## [P1] 退款幂等 request id 未持久化且按时间戳生成

- 状态：待确认风险
- 类型：资金 / 支付 / 幂等
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\payment\provider\wxpay.go:476`
- 证据 1：微信退款 `OutRefundNo` 使用 `time.Now().UnixNano()`；支付宝 `backend\internal\payment\provider\alipay.go:325` 的 `OutRequestNo` 也使用时间戳。
- 证据 2：服务层 `backend\internal\service\payment_refund.go:348` 到 `:353` 调用退款只传 `OrderID/Amount/Reason`；`backend\migrations\092_payment_orders.sql:22` 到 `:28` 未见持久化 provider refund request id。
- 触发场景：网关已接收退款但本地超时或网络错误，订单进入可重试状态后再次退款。
- 用户体验：管理员以为只是重试同一退款，但支付网关收到不同退款请求号。
- 代码逻辑影响：退款幂等键不稳定，无法审计每次退款批次。
- 风险后果：同一订单可能被重复退款。
- 建议：按订单退款批次生成确定性 request id，落库并加唯一约束；重试必须复用同一 request id。
- 置信度：Medium

## [P1] 管理员余额调整非原子读改写且审计可缺失

- 状态：已确认问题
- 类型：资金 / 管理后台 / 数据一致性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\admin_service.go:960`
- 证据 1：`admin_service.go:960` 到 `:983` 先 `GetByID` 读旧余额，内存中 set/add/subtract 后调用 `userRepo.Update(ctx, user)`，没有事务或行锁包裹整个读改写。
- 证据 2：`backend\internal\repository\user_repo.go:219` 使用 `SetBalance(userIn.Balance)` 覆盖余额；同仓库已有原子增量 `user_repo.go:711` 到 `:713` 使用 `AddBalance(amount)`，但该路径没有复用。
- 触发场景：两个管理员并发调整余额，或管理员调整与支付/退款/兑换并发发生。
- 用户体验：后台显示调整成功，但用户余额可能丢失其中一次变更。
- 代码逻辑影响：旧快照覆盖当前余额，审计记录在余额更新后才写入。
- 风险后果：账务不一致、用户余额错、后续追责困难。
- 建议：改为事务 + row lock 或统一使用原子 `AddBalance`；余额更新和审计记录同事务；审计失败应返回失败。
- 置信度：High

## [P1] 管理员余额调整审计失败仍返回成功

- 状态：已确认问题
- 类型：资金 / 审计 / 管理后台
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\admin_service.go:999`
- 证据 1：`admin_service.go:999` 到 `:1004` 余额已更新后，`GenerateRedeemCode` 失败只日志并 `return user, nil`。
- 证据 2：`admin_service.go:1017` 到 `:1019` `redeemCodeRepo.Create` 失败只日志，不回滚也不返回错误；对比 `admin_service.go:1025` 到 `:1080` 积分调整使用事务。
- 触发场景：redeem code 生成失败、审计表写入失败、数据库瞬时错误。
- 用户体验：管理员看到余额调整成功，却查不到对应余额调整记录。
- 代码逻辑影响：账务变更与审计链路不在同一事务。
- 风险后果：资金操作无法追责，余额争议无法还原。
- 建议：余额调整与审计记录必须同事务；审计失败时返回错误并回滚。
- 置信度：High

## [P1] 退款扣减/回滚余额不失效 billing balance cache

- 状态：已确认问题
- 类型：资金 / 计费 / 缓存一致性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\payment_refund.go:293`
- 证据 1：`payment_refund.go:293` 退款扣余额直接调用 `userRepo.DeductBalance`；`payment_refund.go:413` 回滚余额直接调用 `userRepo.UpdateBalance`；`payment_service.go` 中未注入 `BillingCacheService`。
- 证据 2：其它余额路径会失效 cache，例如 `backend\internal\service\user_service.go:1062` 到 `:1079`、`admin_service.go:989` 到 `:997`、`redeem_service.go:437`、`promo_service.go:158`、`shop.go:1716`；`billing_cache_service.go:289` 到 `:317` 优先读缓存，`:792` 到 `:805` 用缓存余额判断计费资格。
- 触发场景：用户余额退款后，Redis 仍缓存旧余额。
- 用户体验：用户可能继续通过网关调用，或看到余额短时间不一致。
- 代码逻辑影响：真实余额与计费资格 cache 不一致。
- 风险后果：退款后仍消费、余额欠费、对账困难。
- 建议：PaymentService 注入 BillingCacheService；退款扣减和回滚后同步失效或原子更新余额 cache；补退款 cache 回归测试。
- 置信度：High

## [P1] 商城无限库存商品在前台被判售罄

- 状态：已确认问题
- 类型：商城 / 用户体验 / 业务收入
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:489`
- 证据 1：`StoreView.vue:488` 到 `:490` 的 `isProductPurchasable` 只判断抽奖、流量倍率商品或 `product.stock > 0`，没有纳入 `stock_unlimited`。
- 证据 2：`frontend\src\types\store.ts:65` 定义 `stock_unlimited`，管理后台 `frontend\src\views\admin\store\StoreProductsView.vue:23` 按无限库存展示。
- 触发场景：后台配置某商品为无限库存且 `stock=0`。
- 用户体验：前台显示库存 0，购买按钮显示售罄并禁用。
- 代码逻辑影响：前后端和后台配置的无限库存语义不一致。
- 风险后果：可售商品无法下单，直接影响营收和用户信任。
- 建议：`isProductPurchasable`、`stockBadgeText`、总库存统计均识别 `stock_unlimited`，前台显示“不限库存”。
- 置信度：High

## [P2] 商城购买数量允许小数进入提交

- 状态：已确认问题
- 类型：商城 / API 契约 / 用户体验
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:162`
- 证据 1：`StoreView.vue:162` 数量输入为 `type="number"` 和 `v-model.number`，只有 min/max，没有 `step="1"`。
- 证据 2：`StoreView.vue:447` 只校验范围，`StoreView.vue:624` 原样提交 `quantity.value`。
- 触发场景：用户输入 `1.5`。
- 用户体验：金额按小数计算，按钮可能可点，提交后才出现服务端错误或异常反馈。
- 代码逻辑影响：前端允许非整数数量，不符合商品数量业务语义。
- 风险后果：订单体验不稳定，可能产生难懂错误或边界订单。
- 建议：输入层增加 `step="1"`，提交前用 `Number.isInteger` fail-fast 校验并给明确 Toast。
- 置信度：High
