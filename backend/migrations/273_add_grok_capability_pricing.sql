-- Grok 上游能力定价收口：模型族视频价、原生搜索价与 Voice 音频价。
-- 全部字段为可空新增列；NULL 表示尚未配置，不改写任何现有分组数据。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_model_prices JSONB,
    ADD COLUMN IF NOT EXISTS search_price_per_1k DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS audio_realtime_price_per_min DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS audio_tts_price_per_million_chars DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS audio_stt_price_per_hour DECIMAL(20,8);

COMMENT ON COLUMN groups.video_model_prices IS
    'Grok 视频模型族×分辨率每秒价格（USD/s）；NULL/空表示沿用旧分辨率列或内置官方价';
COMMENT ON COLUMN groups.search_price_per_1k IS
    'Grok 原生 web_search/x_search 每千次成功调用价格（USD）；NULL 表示缺少必需计费配置，0 表示显式免费';
COMMENT ON COLUMN groups.audio_realtime_price_per_min IS
    'Grok Voice Realtime 每分钟价格（USD）；NULL 表示缺少必需计费配置';
COMMENT ON COLUMN groups.audio_tts_price_per_million_chars IS
    'Grok TTS 每百万字符价格（USD）；NULL 表示缺少必需计费配置';
COMMENT ON COLUMN groups.audio_stt_price_per_hour IS
    'Grok STT 每小时价格（USD）；NULL 表示缺少必需计费配置';
