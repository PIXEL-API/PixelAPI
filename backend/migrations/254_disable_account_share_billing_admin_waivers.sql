-- Disable all future administrator billing-intent waivers while preserving
-- existing immutable audit history for accountability.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

DROP TRIGGER IF EXISTS trg_account_share_billing_admin_waiver_immutable
    ON account_share_billing_intent_admin_waivers;
CREATE TRIGGER trg_account_share_billing_admin_waiver_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON account_share_billing_intent_admin_waivers
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_billing_admin_waiver();

COMMENT ON TABLE account_share_billing_intent_admin_waivers
    IS 'Immutable historical audit only. New administrator billing-intent waivers are disabled.';
