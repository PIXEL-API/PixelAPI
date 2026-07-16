-- Codex alpha/search per-call billing override.
-- NULL uses the built-in default of USD 0.01/call; zero makes searches free.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS web_search_price_per_call DECIMAL(20,8);

COMMENT ON COLUMN groups.web_search_price_per_call IS
    'Codex alpha/search 网页搜索单次价格（USD/次）；NULL 使用默认价 0.01，0 表示免费';
