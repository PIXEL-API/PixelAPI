-- idx_accounts_extra_gin (migration 045, GIN on accounts.extra) was built for
-- @> containment lookups, but no code path issues them: FindByExtraField and
-- all other extra queries go through ent sqljson, which renders ->>/-> value
-- comparisons that a whole-column GIN index cannot serve. Production showed
-- idx_scan=0 at 1162MB, and the index blocked HOT updates on the hottest
-- UPDATE path (per-request quota writes on accounts). It was removed by hand
-- on 2026-08-01; this migration makes the removal durable so fresh
-- deployments never re-create it.
DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_extra_gin;
