CREATE TABLE IF NOT EXISTS account_share_account_identities (
    id BIGSERIAL PRIMARY KEY,
    platform VARCHAR(32) NOT NULL,
    identity_type VARCHAR(32) NOT NULL DEFAULT 'email',
    identity_value VARCHAR(255) NOT NULL,
    identity_hint VARCHAR(255) NOT NULL DEFAULT '',
    first_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    last_account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT account_share_account_identities_type_chk CHECK (identity_type IN ('email')),
    CONSTRAINT account_share_account_identities_value_chk CHECK (trim(identity_value) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_account_identities_live
    ON account_share_account_identities(platform, identity_type, identity_value)
    WHERE deleted_at IS NULL;

ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS account_identity_id BIGINT REFERENCES account_share_account_identities(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS rating_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_score_sum INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_avg NUMERIC(4,2) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_rating_count_chk'
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_rating_count_chk CHECK (rating_count >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_rating_score_sum_chk'
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_rating_score_sum_chk CHECK (rating_score_sum >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_rating_avg_chk'
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_rating_avg_chk CHECK (rating_avg >= 0 AND rating_avg <= 10);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_account_share_listings_identity
    ON account_share_listings(account_identity_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_listings_rating
    ON account_share_listings(rating_avg DESC, rating_count DESC)
    WHERE deleted_at IS NULL;

WITH source_accounts AS (
    SELECT
        l.id AS listing_id,
        a.id AS account_id,
        lower(trim(a.platform)) AS platform,
        lower(trim(COALESCE(
            NULLIF(a.credentials->>'email', ''),
            NULLIF(a.credentials->>'email_address', ''),
            NULLIF(a.extra->>'email', ''),
            NULLIF(a.extra->>'email_address', '')
        ))) AS email
    FROM account_share_listings l
    JOIN accounts a ON a.id = l.account_id
    WHERE l.deleted_at IS NULL
        AND a.deleted_at IS NULL
),
valid_accounts AS (
    SELECT *
    FROM source_accounts
    WHERE platform <> '' AND email <> ''
),
inserted_identities AS (
    INSERT INTO account_share_account_identities (
        platform, identity_type, identity_value, identity_hint,
        first_account_id, last_account_id, created_at, updated_at
    )
    SELECT DISTINCT
        platform,
        'email',
        email,
        '',
        MIN(account_id) OVER (PARTITION BY platform, email),
        MAX(account_id) OVER (PARTITION BY platform, email),
        NOW(),
        NOW()
    FROM valid_accounts
    ON CONFLICT (platform, identity_type, identity_value) WHERE deleted_at IS NULL
    DO UPDATE SET
        last_account_id = EXCLUDED.last_account_id,
        updated_at = NOW()
    RETURNING id, platform, identity_value
)
UPDATE account_share_listings l
SET account_identity_id = i.id,
    updated_at = l.updated_at
FROM valid_accounts va
JOIN account_share_account_identities i
    ON i.platform = va.platform
    AND i.identity_type = 'email'
    AND i.identity_value = va.email
    AND i.deleted_at IS NULL
WHERE l.id = va.listing_id
    AND l.account_identity_id IS NULL;

CREATE TABLE IF NOT EXISTS account_share_reviews (
    id BIGSERIAL PRIMARY KEY,
    account_identity_id BIGINT NOT NULL REFERENCES account_share_account_identities(id) ON DELETE RESTRICT,
    listing_id BIGINT REFERENCES account_share_listings(id) ON DELETE SET NULL,
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    membership_id BIGINT NOT NULL REFERENCES account_share_memberships(id) ON DELETE CASCADE,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    consumer_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    score SMALLINT NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    comment_status VARCHAR(20) NOT NULL DEFAULT 'none',
    comment_reject_reason TEXT NOT NULL DEFAULT '',
    moderation_attempts INTEGER NOT NULL DEFAULT 0,
    moderation_last_error TEXT NOT NULL DEFAULT '',
    moderation_requested_at TIMESTAMPTZ,
    moderation_next_retry_at TIMESTAMPTZ,
    moderated_at TIMESTAMPTZ,
    moderation_model_snapshot VARCHAR(255) NOT NULL DEFAULT '',
    moderation_url_snapshot TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT account_share_reviews_score_chk CHECK (score BETWEEN 0 AND 10),
    CONSTRAINT account_share_reviews_comment_status_chk CHECK (comment_status IN ('none', 'pending', 'approved', 'rejected', 'failed')),
    CONSTRAINT account_share_reviews_comment_status_comment_chk CHECK (
        (comment = '' AND comment_status = 'none')
        OR (comment <> '' AND comment_status IN ('pending', 'approved', 'rejected', 'failed'))
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_reviews_membership_live
    ON account_share_reviews(membership_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_reviews_identity
    ON account_share_reviews(account_identity_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_reviews_listing_public
    ON account_share_reviews(listing_id, created_at DESC)
    WHERE deleted_at IS NULL AND comment_status = 'approved';

CREATE INDEX IF NOT EXISTS idx_account_share_reviews_owner_public
    ON account_share_reviews(owner_user_id, created_at DESC)
    WHERE deleted_at IS NULL AND comment_status = 'approved';

CREATE INDEX IF NOT EXISTS idx_account_share_reviews_moderation_queue
    ON account_share_reviews(comment_status, moderation_next_retry_at, created_at)
    WHERE deleted_at IS NULL AND comment_status IN ('pending', 'failed');
