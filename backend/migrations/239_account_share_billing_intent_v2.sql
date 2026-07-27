-- Expand the durable account-share billing payload contract to explicit V2
-- allowlists. V1 rows remain valid and unknown or sensitive keys still fail.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_request_billing_intents
    DROP CONSTRAINT IF EXISTS account_share_billing_intent_payload_chk;

ALTER TABLE account_share_request_billing_intents
    ADD CONSTRAINT account_share_billing_intent_payload_chk
    CHECK (
        jsonb_typeof(command_payload) = 'object'
        AND command_payload ->> 'schema_version' = command_schema_version::text
        AND (
            (
                command_schema_version = 1
                AND account_share_jsonb_has_only_keys(
                    command_payload,
                    ARRAY[
                        'schema_version',
                        'group_id',
                        'subscription_id',
                        'account_type',
                        'requested_model',
                        'routed_model',
                        'inbound_endpoint',
                        'upstream_endpoint',
                        'request_type',
                        'service_tier',
                        'reasoning_effort',
                        'billing_type',
                        'prefer_points_billing',
                        'rate_multiplier',
                        'owner_share_ratio',
                        'invite_share_ratio',
                        'platform_share_ratio',
                        'policy_id',
                        'policy_version'
                    ]::text[]
                )
            )
            OR
            (
                command_schema_version = 2
                AND account_share_jsonb_has_only_keys(
                    command_payload,
                    ARRAY[
                        'schema_version',
                        'request_payload_hash',
                        'group_id',
                        'subscription_id',
                        'account_type',
                        'requested_model',
                        'routed_model',
                        'inbound_endpoint',
                        'upstream_endpoint',
                        'request_type',
                        'service_tier',
                        'reasoning_effort',
                        'billing_type',
                        'prefer_points_billing',
                        'rate_multiplier',
                        'rate_multiplier_source',
                        'account_rate_multiplier',
                        'hourly_rate',
                        'owner_share_ratio',
                        'invite_share_ratio',
                        'platform_share_ratio',
                        'policy_id',
                        'policy_version',
                        'channel_id',
                        'model_mapping_chain',
                        'share_mode_snapshot',
                        'share_status_snapshot',
                        'share_platform_snapshot'
                    ]::text[]
                )
            )
        )
        AND (usage_payload IS NULL OR jsonb_typeof(usage_payload) = 'object')
        AND (
            usage_payload IS NULL
            OR usage_payload ->> 'schema_version' = usage_schema_version::text
        )
        AND (
            usage_payload IS NULL
            OR (
                usage_schema_version = 1
                AND account_share_jsonb_has_only_keys(
                    usage_payload,
                    ARRAY[
                        'schema_version',
                        'usage_occurred_at',
                        'input_tokens',
                        'output_tokens',
                        'cache_creation_tokens',
                        'cache_creation_5m_tokens',
                        'cache_creation_1h_tokens',
                        'cache_read_tokens',
                        'image_input_tokens',
                        'image_output_tokens',
                        'image_count',
                        'image_size',
                        'media_type',
                        'video_count',
                        'video_resolution',
                        'video_duration_seconds',
                        'duration_ms',
                        'first_token_ms',
                        'balance_cost',
                        'subscription_cost',
                        'private_group_commission_cost',
                        'api_key_quota_cost',
                        'api_key_rate_limit_cost',
                        'account_quota_cost',
                        'base_charge',
                        'hourly_charge',
                        'total_charge'
                    ]::text[]
                )
            )
            OR (
                usage_schema_version = 2
                AND account_share_jsonb_has_only_keys(
                    usage_payload,
                    ARRAY[
                        'schema_version',
                        'usage_occurred_at',
                        'model',
                        'upstream_model',
                        'service_tier',
                        'reasoning_effort',
                        'input_tokens',
                        'output_tokens',
                        'cache_creation_tokens',
                        'cache_creation_5m_tokens',
                        'cache_creation_1h_tokens',
                        'cache_read_tokens',
                        'image_input_tokens',
                        'image_output_tokens',
                        'image_count',
                        'image_size',
                        'media_type',
                        'video_count',
                        'video_resolution',
                        'video_duration_seconds',
                        'duration_ms',
                        'first_token_ms',
                        'billing_tier',
                        'billing_mode',
                        'cache_ttl_overridden',
                        'applied_rate_multiplier',
                        'input_cost',
                        'output_cost',
                        'cache_creation_cost',
                        'cache_read_cost',
                        'image_input_cost',
                        'image_output_cost',
                        'total_cost',
                        'actual_cost',
                        'account_stats_cost',
                        'balance_cost',
                        'subscription_cost',
                        'private_group_commission_cost',
                        'api_key_quota_cost',
                        'api_key_rate_limit_cost',
                        'account_quota_cost',
                        'base_charge',
                        'hourly_charge',
                        'total_charge'
                    ]::text[]
                )
            )
        )
        AND (response_summary IS NULL OR jsonb_typeof(response_summary) = 'object')
        AND (
            response_summary IS NULL
            OR account_share_jsonb_has_only_keys(
                response_summary,
                ARRAY[
                    'schema_version',
                    'http_status',
                    'provider_request_id',
                    'finish_reason',
                    'streamed',
                    'error_code'
                ]::text[]
            )
        )
    ) NOT VALID;

ALTER TABLE account_share_request_billing_intents
    VALIDATE CONSTRAINT account_share_billing_intent_payload_chk;

COMMENT ON CONSTRAINT account_share_billing_intent_payload_chk
    ON account_share_request_billing_intents
    IS 'Versioned V1/V2 allowlists reject unknown and sensitive billing payload keys.';
