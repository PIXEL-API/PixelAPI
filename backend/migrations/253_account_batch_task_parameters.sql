-- Persist operation-specific parameters for asynchronous account batch tasks.

ALTER TABLE account_batch_tasks
    ADD COLUMN IF NOT EXISTS parameters JSONB NOT NULL DEFAULT '{}'::jsonb;
