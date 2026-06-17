-- Adds non-refundable load-factor credits for user-owned personal accounts.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS load_factor_credits_balance INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS load_factor_credits_used_total INTEGER NOT NULL DEFAULT 0;

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS load_factor_paid_ceiling INTEGER NOT NULL DEFAULT 10;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_load_factor_credits_balance_check'
            AND conrelid = 'users'::regclass
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_load_factor_credits_balance_check
            CHECK (load_factor_credits_balance >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_load_factor_credits_used_total_check'
            AND conrelid = 'users'::regclass
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_load_factor_credits_used_total_check
            CHECK (load_factor_credits_used_total >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'accounts_load_factor_paid_ceiling_check'
            AND conrelid = 'accounts'::regclass
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_load_factor_paid_ceiling_check
            CHECK (load_factor_paid_ceiling >= 10);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS user_load_factor_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    direction VARCHAR(10) NOT NULL,
    amount INTEGER NOT NULL,
    reason VARCHAR(50) NOT NULL,
    ref_type VARCHAR(50) NOT NULL DEFAULT '',
    ref_id BIGINT,
    balance_before INTEGER NOT NULL,
    balance_after INTEGER NOT NULL,
    operator_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_load_factor_ledger_direction_check
        CHECK (direction IN ('debit', 'credit')),
    CONSTRAINT user_load_factor_ledger_amount_check
        CHECK (amount >= 0),
    CONSTRAINT user_load_factor_ledger_balance_check
        CHECK (balance_before >= 0 AND balance_after >= 0)
);

CREATE INDEX IF NOT EXISTS idx_user_load_factor_ledger_user_time
    ON user_load_factor_ledger (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_load_factor_ledger_account_time
    ON user_load_factor_ledger (account_id, created_at DESC)
    WHERE account_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_load_factor_ledger_operator_time
    ON user_load_factor_ledger (operator_user_id, created_at DESC)
    WHERE operator_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_load_factor_ledger_ref
    ON user_load_factor_ledger (ref_type, ref_id)
    WHERE ref_id IS NOT NULL;
