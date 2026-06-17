ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS jitter_seconds INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'channel_monitors_jitter_seconds_non_negative'
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_jitter_seconds_non_negative
            CHECK (jitter_seconds >= 0) NOT VALID;
    END IF;
END $$;

ALTER TABLE channel_monitors VALIDATE CONSTRAINT channel_monitors_jitter_seconds_non_negative;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'channel_monitors_interval_jitter_min'
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_interval_jitter_min
            CHECK (interval_seconds - jitter_seconds >= 15) NOT VALID;
    END IF;
END $$;

ALTER TABLE channel_monitors VALIDATE CONSTRAINT channel_monitors_interval_jitter_min;
