SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_listings
    DROP CONSTRAINT IF EXISTS account_share_listings_seat_limit_chk;

ALTER TABLE account_share_listings
    ADD CONSTRAINT account_share_listings_seat_limit_chk
    CHECK (seat_limit BETWEEN 1 AND 30) NOT VALID;

ALTER TABLE account_share_listings
    VALIDATE CONSTRAINT account_share_listings_seat_limit_chk;

COMMENT ON COLUMN account_share_listings.seat_limit
    IS 'Owner-configured live consumer membership limit; independent from account concurrency; valid range 1..30';
