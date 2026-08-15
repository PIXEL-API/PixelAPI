SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE proxies VALIDATE CONSTRAINT proxies_fallback_mode_check;
ALTER TABLE proxies VALIDATE CONSTRAINT proxies_expiry_warn_days_check;
ALTER TABLE proxies VALIDATE CONSTRAINT proxies_backup_proxy_not_self_check;
ALTER TABLE proxies VALIDATE CONSTRAINT proxies_proxies_fallback_sources;
