-- Reviews are authored against a room/owner relationship. Historical rows
-- retain their physical account identity when one exists, while new reviews
-- can remain valid after accounts are detached, replaced, or deleted.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_reviews
	ALTER COLUMN account_identity_id DROP NOT NULL;
