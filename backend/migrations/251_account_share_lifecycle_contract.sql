-- Contract lifecycle status names after the legacy binary is retired.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

UPDATE account_share_listings
SET status = 'suspended',
    suspended_at = COALESCE(suspended_at, updated_at, NOW()),
    status_reason_code = COALESCE(NULLIF(status_reason_code, ''), 'legacy_disabled')
WHERE status = 'disabled';

ALTER TABLE account_share_listings
    DROP CONSTRAINT IF EXISTS account_share_listings_status_chk;
ALTER TABLE account_share_listings
    ADD CONSTRAINT account_share_listings_status_chk
    CHECK (status IN ('validating', 'active', 'draining', 'paused', 'suspended')) NOT VALID;

ALTER TABLE account_share_listings
    VALIDATE CONSTRAINT account_share_listings_status_chk;
