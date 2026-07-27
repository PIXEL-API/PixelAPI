-- The migration runner removes only same-named invalid indexes before retry.
-- Never drop a valid target here: IF NOT EXISTS preserves it for verification.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_identity
    ON public.account_share_memberships(id, listing_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_revision_identity
    ON public.account_share_memberships(id, listing_id, listing_revision_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_listing_revision_terms_identity
    ON public.account_share_listing_revisions(listing_id, id, revision_number);
