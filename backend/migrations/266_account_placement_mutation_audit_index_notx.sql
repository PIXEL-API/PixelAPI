-- 公共池投放账号的强制改参审计查询索引。
--
-- 265 把 account_share_room_events 的作用域放宽到「房间或账号投放」后，
-- 按账号回查强制改参历史（工单排查、管理员追溯）需要走 placement_account_id。
-- 房间维度已有 listing_id 上的既有索引，账号维度此前不存在。
--
-- 用部分索引：绝大多数事件仍属于房间（placement_account_id IS NULL），
-- 只给公共池事件建索引可以把索引体积压到最小，同时不影响房间事件的写入放大。
--
-- CONCURRENTLY 必须放在 *_notx.sql 里（见 migrations_runner.go 的校验），
-- 由迁移运行器在事务外单独执行。

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_room_events_placement_account
    ON account_share_room_events (placement_account_id, created_at DESC)
    WHERE placement_account_id IS NOT NULL;
