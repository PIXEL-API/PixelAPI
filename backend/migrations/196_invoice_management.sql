INSERT INTO settings (key, value, updated_at)
VALUES
    ('invoice_management_enabled', 'false', NOW()),
    ('withdrawal_management_enabled', 'true', NOW())
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS invoice_profiles (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invoice_type VARCHAR(40) NOT NULL,
    buyer_type VARCHAR(20) NOT NULL,
    title_name VARCHAR(255) NOT NULL,
    tax_id VARCHAR(64) NOT NULL DEFAULT '',
    registered_address TEXT NOT NULL DEFAULT '',
    registered_phone VARCHAR(64) NOT NULL DEFAULT '',
    bank_name VARCHAR(255) NOT NULL DEFAULT '',
    bank_account VARCHAR(128) NOT NULL DEFAULT '',
    recipient_email VARCHAR(255) NOT NULL,
    recipient_phone VARCHAR(64) NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_invoice_profiles_invoice_type CHECK (invoice_type IN ('personal_normal', 'enterprise_normal', 'enterprise_special')),
    CONSTRAINT chk_invoice_profiles_buyer_type CHECK (buyer_type IN ('personal', 'enterprise'))
);

CREATE INDEX IF NOT EXISTS idx_invoice_profiles_user_id ON invoice_profiles(user_id);
CREATE INDEX IF NOT EXISTS idx_invoice_profiles_user_default ON invoice_profiles(user_id, is_default);

CREATE TABLE IF NOT EXISTS invoice_requests (
    id BIGSERIAL PRIMARY KEY,
    request_no VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_email VARCHAR(255) NOT NULL DEFAULT '',
    invoice_type VARCHAR(40) NOT NULL,
    buyer_type VARCHAR(20) NOT NULL,
    title_name VARCHAR(255) NOT NULL,
    tax_id VARCHAR(64) NOT NULL DEFAULT '',
    registered_address TEXT NOT NULL DEFAULT '',
    registered_phone VARCHAR(64) NOT NULL DEFAULT '',
    bank_name VARCHAR(255) NOT NULL DEFAULT '',
    bank_account VARCHAR(128) NOT NULL DEFAULT '',
    recipient_email VARCHAR(255) NOT NULL,
    recipient_phone VARCHAR(64) NOT NULL DEFAULT '',
    amount DECIMAL(20, 2) NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'CNY',
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    invoice_number VARCHAR(128) NOT NULL DEFAULT '',
    invoice_code VARCHAR(128) NOT NULL DEFAULT '',
    invoice_file_url TEXT NOT NULL DEFAULT '',
    invoice_file_name VARCHAR(255) NOT NULL DEFAULT '',
    issued_at TIMESTAMPTZ,
    rejected_reason TEXT,
    admin_note TEXT,
    processed_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_invoice_requests_invoice_type CHECK (invoice_type IN ('personal_normal', 'enterprise_normal', 'enterprise_special')),
    CONSTRAINT chk_invoice_requests_buyer_type CHECK (buyer_type IN ('personal', 'enterprise')),
    CONSTRAINT chk_invoice_requests_status CHECK (status IN ('pending', 'issued', 'rejected', 'cancelled')),
    CONSTRAINT chk_invoice_requests_amount CHECK (amount > 0)
);

CREATE INDEX IF NOT EXISTS idx_invoice_requests_user_id ON invoice_requests(user_id);
CREATE INDEX IF NOT EXISTS idx_invoice_requests_status ON invoice_requests(status);
CREATE INDEX IF NOT EXISTS idx_invoice_requests_created_at ON invoice_requests(created_at);
CREATE INDEX IF NOT EXISTS idx_invoice_requests_request_no ON invoice_requests(request_no);

CREATE TABLE IF NOT EXISTS invoice_request_items (
    id BIGSERIAL PRIMARY KEY,
    invoice_request_id BIGINT NOT NULL REFERENCES invoice_requests(id) ON DELETE CASCADE,
    source_type VARCHAR(30) NOT NULL,
    source_id BIGINT NOT NULL,
    source_no VARCHAR(128) NOT NULL DEFAULT '',
    source_label VARCHAR(255) NOT NULL DEFAULT '',
    item_type VARCHAR(40) NOT NULL DEFAULT '',
    entitlement_amount DECIMAL(20, 10) NOT NULL DEFAULT 0,
    invoice_amount DECIMAL(20, 2) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_invoice_request_items_source_type CHECK (source_type IN ('payment_order', 'redeem_code')),
    CONSTRAINT chk_invoice_request_items_invoice_amount CHECK (invoice_amount > 0)
);

CREATE INDEX IF NOT EXISTS idx_invoice_request_items_request_id ON invoice_request_items(invoice_request_id);
CREATE INDEX IF NOT EXISTS idx_invoice_request_items_source ON invoice_request_items(source_type, source_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_invoice_request_items_active_source
    ON invoice_request_items(source_type, source_id)
    WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS invoice_events (
    id BIGSERIAL PRIMARY KEY,
    invoice_request_id BIGINT NOT NULL REFERENCES invoice_requests(id) ON DELETE CASCADE,
    action VARCHAR(40) NOT NULL,
    operator_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    note TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_invoice_events_request_id ON invoice_events(invoice_request_id);
