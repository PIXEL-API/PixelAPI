-- Run validation in a separate transaction from ADD CONSTRAINT NOT VALID.
-- PostgreSQL can therefore scan existing rows under the weaker validation lock.
ALTER TABLE usage_logs
    VALIDATE CONSTRAINT usage_logs_video_count_non_negative;

ALTER TABLE usage_logs
    VALIDATE CONSTRAINT usage_logs_video_billing_metadata_consistent;
