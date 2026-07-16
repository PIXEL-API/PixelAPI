ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_account_level_check;

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_required_account_level_check;

ALTER TABLE accounts
    ALTER COLUMN account_level TYPE VARCHAR(64);

ALTER TABLE groups
    ALTER COLUMN required_account_level TYPE VARCHAR(64);
