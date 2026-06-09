ALTER TABLE accounts
ADD COLUMN IF NOT EXISTS error_since TIMESTAMPTZ DEFAULT NULL;

CREATE INDEX IF NOT EXISTS idx_accounts_error_since
ON accounts (error_since)
WHERE deleted_at IS NULL AND status = 'error' AND error_since IS NOT NULL;

COMMENT ON COLUMN accounts.error_since IS 'Timestamp when the account entered error status; used for stale error account auto soft-delete.';
