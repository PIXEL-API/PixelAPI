-- 退款生命周期终态化（批次 B-4）所需的两个订单列。
--
-- 背景：Stripe / 微信 / 支付宝的 Refund() 在「受理成功但尚未落地」时会返回
-- status=pending 且 error=nil（stripe.go:217 / wxpay.go:487 / alipay.go:387），
-- 而 gwRefund 此前把整个响应丢弃（`_, err = prov.Refund(...)`），把 err==nil
-- 一律当成终态成功。结果是未落地的退款被直接标成 REFUNDED，并写入 refund_at
-- ——营收报表只按 refund_at 落桶、不看 status，且全仓没有清空 refund_at 的路径。
--
-- 引入 REFUND_PENDING 中间态后需要两样东西落库：
--
-- 1. refund_trade_no：网关侧退款单号。终态化发生在另一个请求（管理员点回查），
--    那时内存里的 RefundResponse 早已不在，没有退款单号就无法向网关回查，
--    订单会永久卡在 pending。
-- 2. refund_deduct_on_settle：管理员发起退款时可以选择「不扣用户余额」
--    （PrepareRefund 的 deduct=false）。这个意图必须跨请求保留，否则回查
--    终态化时会扣掉管理员明确不想扣的钱。
--
-- 注意：本次不需要为 REFUND_PENDING 这个状态值本身做迁移——
-- payment_orders.status 是 VARCHAR(30) 且无 CHECK 约束（见 092_payment_orders.sql:21），
-- 'REFUND_PENDING' 共 14 字符，可直接写入。
--
-- 锁风险：PG 11+ 对「带非易失默认值的 ADD COLUMN」只改 catalog，不重写表，
-- 因此这两条 ALTER 是 O(1) 元数据操作，与 payment_orders 的行数无关。

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS refund_trade_no VARCHAR(128) NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS refund_deduct_on_settle BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN payment_orders.refund_trade_no IS '网关侧退款单号，REFUND_PENDING 终态化回查用';
COMMENT ON COLUMN payment_orders.refund_deduct_on_settle IS 'pending 退款确认成功后是否扣回余额/订阅';
