SET LOCAL lock_timeout = '2s';

ALTER TABLE invoice_profiles
    ADD COLUMN remark TEXT NOT NULL DEFAULT '';

ALTER TABLE invoice_requests
    ADD COLUMN remark TEXT NOT NULL DEFAULT '';
