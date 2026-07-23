-- OPS 日分区迁移的空影子基础设施。
--
-- 安全边界：
--   1. 本迁移只创建空的分区父表和分区创建函数；
--   2. 不创建任何日分区，不复制数据，不修改、附加或重命名正式表；
--   3. 日分区必须由运维显式传入 UTC 零点后创建。

SET LOCAL lock_timeout = '2s';

DO $$
DECLARE
    relation_kind "char";
BEGIN
    SELECT c.relkind
    INTO relation_kind
    FROM pg_catalog.pg_class AS c
    JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relname = 'ops_system_logs';

    IF relation_kind IS DISTINCT FROM 'r'::"char" THEN
        RAISE EXCEPTION 'public.ops_system_logs must exist as an ordinary table before creating its shadow parent';
    END IF;

    SELECT c.relkind
    INTO relation_kind
    FROM pg_catalog.pg_class AS c
    JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relname = 'ops_error_logs';

    IF relation_kind IS DISTINCT FROM 'r'::"char" THEN
        RAISE EXCEPTION 'public.ops_error_logs must exist as an ordinary table before creating its shadow parent';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS public.ops_system_logs_daily_shadow (
    LIKE public.ops_system_logs
        INCLUDING DEFAULTS
        INCLUDING GENERATED
        INCLUDING IDENTITY
        INCLUDING STORAGE
        INCLUDING COMPRESSION
        INCLUDING COMMENTS
        INCLUDING CONSTRAINTS,
    CONSTRAINT ops_system_logs_daily_shadow_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE IF NOT EXISTS public.ops_error_logs_daily_shadow (
    LIKE public.ops_error_logs
        INCLUDING DEFAULTS
        INCLUDING GENERATED
        INCLUDING IDENTITY
        INCLUDING STORAGE
        INCLUDING COMPRESSION
        INCLUDING COMMENTS
        INCLUDING CONSTRAINTS,
    CONSTRAINT ops_error_logs_daily_shadow_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

-- 双写和回填必须复用正式表已经分配的 id。影子表不允许自行推进正式序列。
ALTER TABLE public.ops_system_logs_daily_shadow ALTER COLUMN id DROP DEFAULT;
ALTER TABLE public.ops_error_logs_daily_shadow ALTER COLUMN id DROP DEFAULT;

DO $$
DECLARE
    parent_name text;
    parent_oid oid;
    source_name text;
    source_oid oid;
    partition_key text;
    required_primary_key text;
    id_index_name text;
    id_attribute_number smallint;
BEGIN
    FOREACH parent_name IN ARRAY ARRAY[
        'ops_system_logs_daily_shadow',
        'ops_error_logs_daily_shadow'
    ]
    LOOP
        source_name := pg_catalog.replace(parent_name, '_daily_shadow', '');

        SELECT c.oid, pg_catalog.pg_get_partkeydef(c.oid)
        INTO parent_oid, partition_key
        FROM pg_catalog.pg_class AS c
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relname = parent_name
          AND c.relkind = 'p';

        IF parent_oid IS NULL OR partition_key IS DISTINCT FROM 'RANGE (created_at)' THEN
            RAISE EXCEPTION 'public.% must be a RANGE (created_at) partitioned table', parent_name;
        END IF;

        SELECT c.oid
        INTO source_oid
        FROM pg_catalog.pg_class AS c
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relname = source_name
          AND c.relkind = 'r';

        IF source_oid IS NULL THEN
            RAISE EXCEPTION 'required source table public.% does not exist', source_name;
        END IF;

        IF EXISTS (
            (
                SELECT a.attnum,
                       a.attname,
                       a.atttypid,
                       a.atttypmod,
                       a.attcollation,
                       a.attnotnull,
                       a.attidentity,
                       a.attgenerated,
                       CASE
                           WHEN a.attname = 'id' THEN ''
                           ELSE COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
                       END
                FROM pg_catalog.pg_attribute AS a
                LEFT JOIN pg_catalog.pg_attrdef AS d
                  ON d.adrelid = a.attrelid
                 AND d.adnum = a.attnum
                WHERE a.attrelid = source_oid
                  AND a.attnum > 0
                  AND NOT a.attisdropped
                EXCEPT
                SELECT a.attnum,
                       a.attname,
                       a.atttypid,
                       a.atttypmod,
                       a.attcollation,
                       a.attnotnull,
                       a.attidentity,
                       a.attgenerated,
                       CASE
                           WHEN a.attname = 'id' THEN ''
                           ELSE COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
                       END
                FROM pg_catalog.pg_attribute AS a
                LEFT JOIN pg_catalog.pg_attrdef AS d
                  ON d.adrelid = a.attrelid
                 AND d.adnum = a.attnum
                WHERE a.attrelid = parent_oid
                  AND a.attnum > 0
                  AND NOT a.attisdropped
            )
            UNION ALL
            (
                SELECT a.attnum,
                       a.attname,
                       a.atttypid,
                       a.atttypmod,
                       a.attcollation,
                       a.attnotnull,
                       a.attidentity,
                       a.attgenerated,
                       CASE
                           WHEN a.attname = 'id' THEN ''
                           ELSE COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
                       END
                FROM pg_catalog.pg_attribute AS a
                LEFT JOIN pg_catalog.pg_attrdef AS d
                  ON d.adrelid = a.attrelid
                 AND d.adnum = a.attnum
                WHERE a.attrelid = parent_oid
                  AND a.attnum > 0
                  AND NOT a.attisdropped
                EXCEPT
                SELECT a.attnum,
                       a.attname,
                       a.atttypid,
                       a.atttypmod,
                       a.attcollation,
                       a.attnotnull,
                       a.attidentity,
                       a.attgenerated,
                       CASE
                           WHEN a.attname = 'id' THEN ''
                           ELSE COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
                       END
                FROM pg_catalog.pg_attribute AS a
                LEFT JOIN pg_catalog.pg_attrdef AS d
                  ON d.adrelid = a.attrelid
                 AND d.adnum = a.attnum
                WHERE a.attrelid = source_oid
                  AND a.attnum > 0
                  AND NOT a.attisdropped
            )
        ) THEN
            RAISE EXCEPTION 'public.% columns/defaults do not match public.%', parent_name, source_name;
        END IF;

        required_primary_key := parent_name || '_pkey';
        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_constraint AS con
            WHERE con.conrelid = parent_oid
              AND con.contype = 'p'
              AND con.conname = required_primary_key
              AND pg_catalog.pg_get_constraintdef(con.oid, true) = 'PRIMARY KEY (created_at, id)'
        ) THEN
            RAISE EXCEPTION 'public.% must have PRIMARY KEY (created_at, id)', parent_name;
        END IF;

        id_index_name := parent_name || '_id_idx';
        IF pg_catalog.to_regclass('public.' || id_index_name) IS NULL THEN
            IF EXISTS (
                SELECT 1
                FROM pg_catalog.pg_inherits
                WHERE inhparent = parent_oid
            ) THEN
                RAISE EXCEPTION 'refusing to build missing index % after partitions exist', id_index_name;
            END IF;

            EXECUTE pg_catalog.format(
                'CREATE INDEX %I ON public.%I (id)',
                id_index_name,
                parent_name
            );
        END IF;

        SELECT a.attnum
        INTO id_attribute_number
        FROM pg_catalog.pg_attribute AS a
        WHERE a.attrelid = parent_oid
          AND a.attname = 'id'
          AND NOT a.attisdropped;

        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class AS idx
            JOIN pg_catalog.pg_namespace AS idx_ns ON idx_ns.oid = idx.relnamespace
            JOIN pg_catalog.pg_index AS ind ON ind.indexrelid = idx.oid
            JOIN pg_catalog.pg_am AS am ON am.oid = idx.relam
            WHERE idx_ns.nspname = 'public'
              AND idx.relname = id_index_name
              AND ind.indrelid = parent_oid
              AND ind.indisvalid
              AND ind.indisready
              AND NOT ind.indisunique
              AND ind.indnkeyatts = 1
              AND ind.indkey[0] = id_attribute_number
              AND ind.indexprs IS NULL
              AND ind.indpred IS NULL
              AND am.amname = 'btree'
        ) THEN
            RAISE EXCEPTION 'public.% must be a valid non-unique btree index on public.%(id)', id_index_name, parent_name;
        END IF;
    END LOOP;
END $$;

COMMENT ON TABLE public.ops_system_logs_daily_shadow IS
    'Empty UTC daily-partitioned shadow for an operator-controlled ops_system_logs migration; writes must provide the source id; never auto-backfilled or auto-switched.';
COMMENT ON TABLE public.ops_error_logs_daily_shadow IS
    'Empty UTC daily-partitioned shadow for an operator-controlled ops_error_logs migration; writes must provide the source id; never auto-backfilled or auto-switched.';

CREATE OR REPLACE FUNCTION public.create_ops_daily_shadow_partitions(p_day_start timestamptz)
RETURNS void
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, public
SET "TimeZone" = 'UTC'
SET lock_timeout = '2s'
AS $$
DECLARE
    day_end timestamptz;
    day_suffix text;
    parent_name text;
    parent_oid oid;
    partition_name text;
    partition_oid oid;
    attached_parent_oid oid;
    partition_bound text;
    utc_start_literal text;
    utc_end_literal text;
BEGIN
    IF p_day_start IS NULL THEN
        RAISE EXCEPTION 'p_day_start is required';
    END IF;

    IF p_day_start <> (
        pg_catalog.date_trunc('day', p_day_start AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
    ) THEN
        RAISE EXCEPTION 'p_day_start must be an exact UTC day boundary: %', p_day_start;
    END IF;

    IF NOT pg_catalog.pg_try_advisory_xact_lock(
        pg_catalog.hashtextextended('ops_daily_shadow_partition_manager', 0)
    ) THEN
        RAISE EXCEPTION 'another OPS shadow partition operation is in progress';
    END IF;

    day_end := p_day_start + INTERVAL '1 day';
    day_suffix := pg_catalog.to_char(p_day_start AT TIME ZONE 'UTC', 'YYYYMMDD');
    utc_start_literal := pg_catalog.to_char(
        p_day_start AT TIME ZONE 'UTC',
        'YYYY-MM-DD"T"HH24:MI:SS"Z"'
    );
    utc_end_literal := pg_catalog.to_char(
        day_end AT TIME ZONE 'UTC',
        'YYYY-MM-DD"T"HH24:MI:SS"Z"'
    );

    FOREACH parent_name IN ARRAY ARRAY[
        'ops_system_logs_daily_shadow',
        'ops_error_logs_daily_shadow'
    ]
    LOOP
        SELECT c.oid
        INTO parent_oid
        FROM pg_catalog.pg_class AS c
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relname = parent_name
          AND c.relkind = 'p';

        IF parent_oid IS NULL THEN
            RAISE EXCEPTION 'required partitioned parent public.% does not exist', parent_name;
        END IF;

        partition_name := parent_name || '_' || day_suffix;
        EXECUTE pg_catalog.format(
            'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.%I '
            'FOR VALUES FROM (%L::timestamptz) TO (%L::timestamptz)',
            partition_name,
            parent_name,
            utc_start_literal,
            utc_end_literal
        );

        SELECT c.oid,
               i.inhparent,
               pg_catalog.pg_get_expr(c.relpartbound, c.oid, true)
        INTO partition_oid, attached_parent_oid, partition_bound
        FROM pg_catalog.pg_class AS c
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        LEFT JOIN pg_catalog.pg_inherits AS i ON i.inhrelid = c.oid
        WHERE n.nspname = 'public'
          AND c.relname = partition_name
          AND c.relispartition;

        IF partition_oid IS NULL OR attached_parent_oid IS DISTINCT FROM parent_oid THEN
            RAISE EXCEPTION 'public.% exists but is not attached to public.%', partition_name, parent_name;
        END IF;

        IF pg_catalog.strpos(
            partition_bound,
            pg_catalog.to_char(p_day_start AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
        ) = 0 OR pg_catalog.strpos(
            partition_bound,
            pg_catalog.to_char(day_end AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS')
        ) = 0 THEN
            RAISE EXCEPTION 'public.% has unexpected bounds: %', partition_name, partition_bound;
        END IF;
    END LOOP;
END;
$$;

REVOKE ALL ON FUNCTION public.create_ops_daily_shadow_partitions(timestamptz) FROM PUBLIC;

COMMENT ON FUNCTION public.create_ops_daily_shadow_partitions(timestamptz) IS
    'Explicitly creates one UTC day partition for both empty OPS shadow parents. It never copies, attaches, renames, or switches production tables.';
