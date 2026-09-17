-- Account Share: room join password.
--
-- Room owners may set a custom password that gates new joins only. The hash is
-- verified when a join intent is created; memberships that already exist are
-- never revalidated, so rotating the password does not affect joined users.
-- Owners and admins bypass the check. Only a bcrypt hash is stored here; the
-- plaintext password is never persisted or returned by the API.

ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS join_password_hash VARCHAR(120);
