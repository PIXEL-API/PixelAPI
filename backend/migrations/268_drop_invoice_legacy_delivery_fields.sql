-- 旧的单张发票号码/文件交付流程已经由批量导出流程替代。
-- 这些字段不再出现在服务模型或查询中；显式删列，若生产 schema 不一致则直接失败。
SET LOCAL lock_timeout = '2s';

ALTER TABLE invoice_requests
    DROP COLUMN invoice_number,
    DROP COLUMN invoice_code,
    DROP COLUMN invoice_file_url,
    DROP COLUMN invoice_file_name;
