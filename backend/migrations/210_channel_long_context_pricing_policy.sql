ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS long_context_pricing_enabled BOOLEAN,
    ADD COLUMN IF NOT EXISTS long_context_input_token_threshold INTEGER;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'channel_model_pricing_long_context_policy_valid'
          AND conrelid = 'channel_model_pricing'::regclass
    ) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_long_context_policy_valid
            CHECK (
                (long_context_pricing_enabled IS NULL AND long_context_input_token_threshold IS NULL)
                OR (long_context_pricing_enabled IS FALSE AND long_context_input_token_threshold IS NULL)
                OR (
                    long_context_pricing_enabled IS TRUE
                    AND long_context_input_token_threshold IS NOT NULL
                    AND long_context_input_token_threshold > 0
                )
            ) NOT VALID;
    END IF;
END $$;

-- Validation is intentionally deferred to 210a so this transaction can commit
-- and release the ADD CONSTRAINT lock before PostgreSQL scans historical rows.

COMMENT ON COLUMN channel_model_pricing.long_context_pricing_enabled IS
    'NULL and FALSE disable model price-card long-context multipliers; TRUE enables them with an explicit threshold.';
COMMENT ON COLUMN channel_model_pricing.long_context_input_token_threshold IS
    'Positive input-token threshold used only when long_context_pricing_enabled is TRUE.';
