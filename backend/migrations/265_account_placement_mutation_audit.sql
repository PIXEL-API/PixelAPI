-- 让强制改参审计能覆盖「广场公共池」投放的账号。
--
-- 背景：账号的外部投放有两种目标（account_external_placements.placement_type）：
--   - 'room'        —— 挂在某个 account_share_listings 房间下，有 listing_id
--   - 'public_pool' —— 直接投放进广场公共号池，没有任何 listing
--
-- 管理员强制修改「投放中账号」的敏感设置时，account_repo.go 会在同一事务里写一条
-- account.admin_forced_update 审计事件。但该表的 listing_id 此前是 NOT NULL
-- （234_account_share_listing_revisions.sql:64），公共池账号没有 listing_id 可写，
-- 因此这条审计路径对公共池账号根本走不通。
--
-- 这正是当初要在 mutation guard 之前另立一道粗糙前置守卫的原因：公共池账号进不了
-- 那套「diff 分级 + 强制确认 + 审计」的机制，只能一刀切拒绝。本迁移把审计表的作用域
-- 从「房间」放宽到「房间或账号投放」，让两类投放共用同一套守卫与审计。
--
-- 作用域约束：一条事件要么属于房间（listing_id），要么属于账号投放
-- （placement_account_id），不允许两者都有或都没有，避免出现无归属的孤儿审计行。
--
-- placement_account_id 刻意不加外键：审计行必须在账号被物理删除后继续存在，
-- ON DELETE RESTRICT 会让审计反过来阻塞账号删除（与 AccountDeletionGuard 冲突），
-- ON DELETE SET NULL 又会破坏上面的作用域约束。审计表记录的是历史事实，
-- 不需要引用完整性。
--
-- 锁风险：三条 ALTER 都只改 catalog（DROP NOT NULL、加可空无默认值列、加 NOT VALID
-- 约束），是 O(1) 元数据操作；随后的 VALIDATE CONSTRAINT 需要全表扫描，但只持有
-- SHARE UPDATE EXCLUSIVE，不阻塞该表的读写。与本仓既有事务型迁移一致，显式设置
-- lock_timeout，抢不到锁就快速失败回滚。

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE account_share_room_events
    ALTER COLUMN listing_id DROP NOT NULL;

ALTER TABLE account_share_room_events
    ADD COLUMN IF NOT EXISTS placement_account_id BIGINT;

ALTER TABLE account_share_room_events
    DROP CONSTRAINT IF EXISTS account_share_room_event_scope_chk;

ALTER TABLE account_share_room_events
    ADD CONSTRAINT account_share_room_event_scope_chk
    CHECK (
        (listing_id IS NOT NULL AND placement_account_id IS NULL)
        OR (listing_id IS NULL AND placement_account_id IS NOT NULL)
    ) NOT VALID;

ALTER TABLE account_share_room_events
    VALIDATE CONSTRAINT account_share_room_event_scope_chk;

COMMENT ON COLUMN account_share_room_events.placement_account_id IS
    '公共池投放账号的审计归属；与 listing_id 互斥，二者必居其一';
