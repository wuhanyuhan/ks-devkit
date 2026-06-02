-- keystone-dev 开发环境种子数据
-- 幂等：所有 INSERT 使用 WHERE NOT EXISTS
-- 凭证：admin / admin（schema.sql 内置，首次登录强制改密）
-- API Key: ks_deadbeefdeadbeefdeadbeefdeadbeef
-- 注意：id 使用 9001+ 高位值，避免与 schema.sql 内置 seed 数据冲突

-- 0. 标记 schema.sql 已包含所有迁移变更，migrator 启动时跳过
CREATE TABLE IF NOT EXISTS schema_version (
    id          INT UNSIGNED NOT NULL DEFAULT 1,
    version     INT UNSIGNED NOT NULL DEFAULT 0,
    updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
INSERT INTO schema_version (id, version) VALUES (1, 3)
ON DUPLICATE KEY UPDATE version = GREATEST(version, 3);

-- 管理员账号由 schema.sql 内置（admin/admin），此处不再重复创建

-- 1. 测试 MCP Server（指向开发者本地 8080 端口）
INSERT INTO t_mcp_servers (id, name, description, transport_type, connection_config, status)
SELECT 9001, 'dev-mcp-server', '开发测试 MCP Server（指向宿主机 8080 端口）',
       'streamable_http',
       '{"url":"http://host.docker.internal:8080/mcp"}',
       1
WHERE NOT EXISTS (SELECT 1 FROM t_mcp_servers WHERE name = 'dev-mcp-server');

-- 3. 测试 Agent
INSERT INTO t_agents (id, name, description, system_prompt, status, published)
SELECT 9001, 'dev-agent', '开发测试 Agent',
       '你是一个有用的助手。请使用可用的工具来帮助用户完成任务。',
       1, 1
WHERE NOT EXISTS (SELECT 1 FROM t_agents WHERE name = 'dev-agent');

-- 4. Agent ↔ MCP Server 绑定（允许所有工具）
INSERT INTO t_agent_mcp_servers (agent_id, mcp_server_id, allowed_tools)
SELECT 9001, 9001, '["*"]'
WHERE NOT EXISTS (
    SELECT 1 FROM t_agent_mcp_servers WHERE agent_id = 9001 AND mcp_server_id = 9001
);

-- 5. 测试 API Key
INSERT INTO t_api_keys (id, name, key_hash, key_prefix, key_type, agent_ids, rate_limit, status)
SELECT 9001, 'dev-test-key',
       '7972e2ee1939685cdfadfbb00eef3df129bb06ce4c86b848ca0d8d379e95e3ca', 'ks_deadb', 'open',
       '[9001]', 60, 1
WHERE NOT EXISTS (SELECT 1 FROM t_api_keys WHERE name = 'dev-test-key');
