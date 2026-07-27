-- The migration runner removes only same-named invalid indexes before retry.
-- A valid historical index must remain in place while this migration reruns.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_billing_intents_membership_history
    ON public.account_share_request_billing_intents(
        membership_id,
        settled_at DESC,
        id DESC
    )
    WHERE status = 'settled'
      AND usage_payload IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_billing_intents_consumer_spend
    ON public.account_share_request_billing_intents(
        listing_id,
        consumer_user_id_snapshot,
        settled_at DESC,
        id DESC
    )
    WHERE status = 'settled'
      AND usage_payload IS NOT NULL;
