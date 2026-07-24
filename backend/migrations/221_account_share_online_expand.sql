-- Expand-only phase for the account-share online migration. Every change in
-- this file remains compatible with the previous application release.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS room_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS platform VARCHAR(50),
    ADD COLUMN IF NOT EXISTS account_level VARCHAR(64);

ALTER TABLE account_share_listings
    DROP CONSTRAINT IF EXISTS account_share_listings_account_id_key,
    DROP CONSTRAINT IF EXISTS account_share_listings_account_id_fkey;

ALTER TABLE account_share_listings
    ALTER COLUMN account_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_legacy_account_fk'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_legacy_account_fk
            FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL
            NOT VALID;
    END IF;
END
$$;

ALTER TABLE account_share_mode_settlement_entries
    ADD COLUMN IF NOT EXISTS account_cost NUMERIC(20,10);

ALTER TABLE account_share_mode_settlement_entries
    ALTER COLUMN account_cost DROP DEFAULT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_account_cost_nonnegative_chk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_account_cost_nonnegative_chk
            CHECK (account_cost >= 0) NOT VALID;
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS account_share_online_migration_progress (
    phase VARCHAR(64) PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    high_water_mark BIGINT NOT NULL DEFAULT 0,
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_online_migration_progress_bounds_chk
        CHECK (last_id >= 0 AND high_water_mark >= 0 AND last_id <= high_water_mark)
);

CREATE OR REPLACE FUNCTION account_share_online_compat_affiliate_ledger()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.action = 'reverse'
       AND EXISTS (
           SELECT 1
           FROM public.user_balance_ledger balance_entry
           WHERE balance_entry.user_id = NEW.user_id
             AND balance_entry.direction = 'debit'
             AND balance_entry.reason = 'account_share_mode_invite_waiver_refund'
             AND balance_entry.amount = NEW.amount
             AND balance_entry.created_at = NEW.created_at
             AND COALESCE(balance_entry.metadata->>'consumer_user_id', '') ~ '^[0-9]+$'
             AND (balance_entry.metadata->>'consumer_user_id')::bigint = NEW.source_user_id
       ) THEN
        UPDATE public.user_affiliates
        SET aff_history_quota = aff_history_quota + NEW.amount,
            updated_at = NOW()
        WHERE user_id = NEW.user_id;
        NEW.action := 'share_reverse';
        RETURN NEW;
    END IF;

    IF NEW.action = 'accrue'
       AND NEW.source_order_id IS NULL
       AND EXISTS (
           SELECT 1
           FROM public.user_balance_ledger balance_entry
           WHERE balance_entry.user_id = NEW.user_id
             AND balance_entry.direction = 'credit'
             AND balance_entry.reason = 'invite_share_income'
             AND balance_entry.amount = NEW.amount
             AND balance_entry.created_at = NEW.created_at
             AND COALESCE(balance_entry.metadata->>'consumer_user_id', '') ~ '^[0-9]+$'
             AND (balance_entry.metadata->>'consumer_user_id')::bigint = NEW.source_user_id
       ) THEN
        UPDATE public.user_affiliates
        SET aff_history_quota = aff_history_quota - NEW.amount,
            updated_at = NOW()
        WHERE user_id = NEW.user_id
          AND aff_history_quota >= NEW.amount;
        IF NOT FOUND THEN
            RAISE EXCEPTION
                'cannot isolate live account-share inviter ledger for user_id %',
                NEW.user_id
                USING ERRCODE = '23514';
        END IF;
        NEW.action := 'share_accrue';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_affiliate_ledger
    ON user_affiliate_ledger;

CREATE TRIGGER trg_account_share_online_compat_affiliate_ledger
BEFORE INSERT ON user_affiliate_ledger
FOR EACH ROW
WHEN (NEW.action IN ('accrue', 'reverse'))
EXECUTE FUNCTION account_share_online_compat_affiliate_ledger();

CREATE OR REPLACE FUNCTION account_share_online_compat_settlement_cost()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.settlement_type = 'usage_request'
       AND NEW.usage_log_id IS NOT NULL
       AND COALESCE(NEW.account_cost, 0) = 0 THEN
        SELECT ROUND(
            COALESCE(usage_log.account_stats_cost, usage_log.total_cost, 0)
            * COALESCE(usage_log.account_rate_multiplier, 1),
            10
        )
        INTO NEW.account_cost
        FROM public.usage_logs usage_log
        WHERE usage_log.id = NEW.usage_log_id;
    END IF;
    IF NEW.settlement_type <> 'usage_request' THEN
        NEW.account_cost := COALESCE(NEW.account_cost, 0);
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_settlement_cost
    ON account_share_mode_settlement_entries;

CREATE TRIGGER trg_account_share_online_compat_settlement_cost
BEFORE INSERT OR UPDATE
ON account_share_mode_settlement_entries
FOR EACH ROW
WHEN (NEW.account_cost IS NULL)
EXECUTE FUNCTION account_share_online_compat_settlement_cost();

COMMENT ON COLUMN account_share_mode_settlement_entries.account_cost
    IS 'Immutable account-side cost snapshot retained after usage log cleanup.';
COMMENT ON COLUMN user_affiliate_ledger.action
    IS 'accrue|transfer|share_accrue|share_reverse';
