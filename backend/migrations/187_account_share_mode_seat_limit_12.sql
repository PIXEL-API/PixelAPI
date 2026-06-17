ALTER TABLE account_share_listings
    DROP CONSTRAINT IF EXISTS account_share_listings_seat_limit_chk;

ALTER TABLE account_share_listings
    ADD CONSTRAINT account_share_listings_seat_limit_chk CHECK (seat_limit BETWEEN 2 AND 12);
