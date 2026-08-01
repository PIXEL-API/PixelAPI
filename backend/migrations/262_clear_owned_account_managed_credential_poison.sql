SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

-- 清理"系统自己写进自有账号、又被自有账号凭证安全扫描拒绝"的历史脏数据。
--
-- 背景：validateOwnedAccountSourceForPlatform 过去在每次所有者更新时对库内完整
-- credentials/extra 重跑一遍凭证安全扫描，于是任何由后台写入者留下的值都会让所有者
-- 收到 400 OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED——切调度、启停、改名、改并发、
-- 批量操作、上架账号广场全部失效。代码侧已经改成只扫描本次请求的增量，并在写入端
-- 收口；这里把已经落库的三类污染值清掉，让准入闸口（上架公共池 / 共享审核，仍然
-- 全量扫描）也能恢复正常。
--
-- 三类污染：
--   1. Grok OAuth 的 credentials.base_url：GrokOAuthService.BuildAccountCredentials
--      过去无条件写入 CLI 默认地址，令牌刷新每次都会重新写回。出站地址由
--      Account.GetGrokBaseURL() 在请求时回退到同一个常量，删掉不改变路由行为。
--      只删默认值，管理员显式配置的自定义中继地址保留。
--   2. extra.openai_compact_last_error：compact 探测把上游原始报错（含完整 URL）
--      原样存进 extra。
--   3. extra.model_rate_limits 下键名落在禁用键名单上的条目：模型名来自客户端请求体。

-- 1. 自有 Grok 账号的默认 base_url。
UPDATE accounts
SET credentials = credentials - 'base_url',
    updated_at = NOW()
WHERE owner_user_id IS NOT NULL
  AND platform = 'grok'
  AND credentials ? 'base_url'
  AND btrim(credentials ->> 'base_url') IN (
      'https://cli-chat-proxy.grok.com/v1',
      'https://api.x.ai/v1'
  );

-- 2. 带 URL 的 compact 探测报错文本。
UPDATE accounts
SET extra = extra - 'openai_compact_last_error',
    updated_at = NOW()
WHERE owner_user_id IS NOT NULL
  AND extra ? 'openai_compact_last_error'
  AND extra ->> 'openai_compact_last_error' ~* '(https?://|api_key|bearer |authorization:|cookie:)';

-- 3. 键名会与禁用凭证字段冲突的 model_rate_limits 条目。
UPDATE accounts a
SET extra = jsonb_set(
        a.extra,
        '{model_rate_limits}',
        COALESCE(
            (
                SELECT jsonb_object_agg(entry.key, entry.value)
                FROM jsonb_each(a.extra -> 'model_rate_limits') AS entry
                WHERE replace(replace(lower(btrim(entry.key)), '-', '_'), '.', '_') NOT IN (
                    'api_key', 'apikey', 'x_api_key', 'xapikey',
                    'authorization', 'authorization_header', 'authorizationheader',
                    'base_url', 'baseurl', 'api_base_url', 'api_baseurl',
                    'custom_base_url', 'custom_baseurl',
                    'custom_base_url_enabled', 'custom_baseurl_enabled',
                    'upstream', 'upstream_url', 'upstreamurl',
                    'upstream_base_url', 'upstream_baseurl',
                    'upstream_endpoint', 'upstreamendpoint',
                    'endpoint', 'endpoint_url', 'endpointurl',
                    'url', 'host', 'proxy_url', 'proxyurl',
                    'cookie', 'cookies', 'set_cookie', 'setcookie',
                    'auth_mode', 'authmode',
                    'aws_access_key_id', 'awsaccesskeyid',
                    'aws_secret_access_key', 'awssecretaccesskey',
                    'aws_session_token', 'awssessiontoken',
                    'access_key_id', 'accesskeyid', 'secret_access_key',
                    'session_key', 'sessionkey', 'session_token', 'claude_session_key',
                    'access_token', 'accesstoken', 'refresh_token', 'refreshtoken',
                    'id_token', 'idtoken'
                )
            ),
            '{}'::jsonb
        ),
        false
    ),
    updated_at = NOW()
WHERE a.owner_user_id IS NOT NULL
  AND jsonb_typeof(a.extra -> 'model_rate_limits') = 'object'
  AND EXISTS (
      SELECT 1
      FROM jsonb_each(a.extra -> 'model_rate_limits') AS entry
      WHERE replace(replace(lower(btrim(entry.key)), '-', '_'), '.', '_') IN (
          'api_key', 'apikey', 'x_api_key', 'xapikey',
          'authorization', 'authorization_header', 'authorizationheader',
          'base_url', 'baseurl', 'api_base_url', 'api_baseurl',
          'custom_base_url', 'custom_baseurl',
          'custom_base_url_enabled', 'custom_baseurl_enabled',
          'upstream', 'upstream_url', 'upstreamurl',
          'upstream_base_url', 'upstream_baseurl',
          'upstream_endpoint', 'upstreamendpoint',
          'endpoint', 'endpoint_url', 'endpointurl',
          'url', 'host', 'proxy_url', 'proxyurl',
          'cookie', 'cookies', 'set_cookie', 'setcookie',
          'auth_mode', 'authmode',
          'aws_access_key_id', 'awsaccesskeyid',
          'aws_secret_access_key', 'awssecretaccesskey',
          'aws_session_token', 'awssessiontoken',
          'access_key_id', 'accesskeyid', 'secret_access_key',
          'session_key', 'sessionkey', 'session_token', 'claude_session_key',
          'access_token', 'accesstoken', 'refresh_token', 'refreshtoken',
          'id_token', 'idtoken'
      )
  );
