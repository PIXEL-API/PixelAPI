WITH ranked_defaults AS (
    SELECT
        id,
        ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY updated_at DESC, id DESC) AS row_num
    FROM invoice_profiles
    WHERE is_default = TRUE
)
UPDATE invoice_profiles AS profile
SET is_default = FALSE,
    updated_at = NOW()
FROM ranked_defaults AS ranked
WHERE profile.id = ranked.id
  AND ranked.row_num > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uq_invoice_profiles_user_default
    ON invoice_profiles(user_id)
    WHERE is_default = TRUE;
