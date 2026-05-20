ALTER TABLE account_leases
    ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT;

ALTER TABLE account_leases
    ALTER COLUMN group_id SET NOT NULL;

DROP INDEX IF EXISTS idx_account_leases_subsite_status;

CREATE INDEX IF NOT EXISTS idx_account_leases_subsite_group_status
    ON account_leases (subsite_id, group_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_leases_group_expires_at
    ON account_leases (group_id, expires_at)
    WHERE deleted_at IS NULL;
