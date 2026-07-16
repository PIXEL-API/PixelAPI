-- Run validation in a separate transaction from ADD CONSTRAINT NOT VALID.
-- PostgreSQL can therefore scan existing rows under the weaker validation lock.
ALTER TABLE channel_model_pricing
    VALIDATE CONSTRAINT channel_model_pricing_long_context_policy_valid;
