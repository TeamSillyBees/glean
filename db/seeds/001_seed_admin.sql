-- 由本地 setup 工具填入随机密码哈希后生成 001_seed_admin.local.sql；不要直接执行此文件。
-- 该种子数据同时准备 admin、viewer 两个基础角色，便于 user 模块默认角色逻辑直接工作。

INSERT INTO sys_role (
    id,
    code,
    name,
    remark,
    is_builtin,
    is_enable,
    sort_id,
    created_at,
    updated_at,
    created_by,
    updated_by
) VALUES
(
    1000000000101,
    'admin',
    '系统管理员',
    '默认超级管理员角色',
    true,
    true,
    0,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    NULL,
    NULL
),
(
    1000000000102,
    'viewer',
    '只读用户',
    '默认基础只读角色',
    true,
    true,
    10,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    NULL,
    NULL
)
ON CONFLICT (code) DO NOTHING;

-- 权限编码是后端路由、数据库授权关系和 Casbin 策略之间的唯一稳定标识。
INSERT INTO sys_menu (
    id, title, menu_type, permission_code, is_visible, is_cached, sort_id,
    created_at, updated_at, created_by, updated_by
) VALUES
    (1000000001001, '用户查看', 1, 'system:user:read', false, true, 10, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001002, '用户编辑', 1, 'system:user:write', false, true, 11, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001003, '用户删除', 1, 'system:user:delete', false, true, 12, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001004, '文件查看', 1, 'system:file:read', false, true, 20, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001005, '文件编辑', 1, 'system:file:write', false, true, 21, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001006, '文件删除', 1, 'system:file:delete', false, true, 22, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001007, 'HTTP 示例查看', 1, 'system:httpdemo:read', false, true, 30, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001008, '任务示例创建', 1, 'system:taskdemo:create', false, true, 31, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001009, '角色查看', 1, 'system:role:read', false, true, 40, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001010, '角色编辑', 1, 'system:role:write', false, true, 41, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001011, '角色删除', 1, 'system:role:delete', false, true, 42, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001012, '权限资源查看', 1, 'system:permission:read', false, true, 50, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001013, '权限资源编辑', 1, 'system:permission:write', false, true, 51, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001014, '权限资源删除', 1, 'system:permission:delete', false, true, 52, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001015, '用户角色查看', 1, 'system:user-role:read', false, true, 60, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001016, '用户角色编辑', 1, 'system:user-role:write', false, true, 61, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001017, '角色权限查看', 1, 'system:role-permission:read', false, true, 70, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL),
    (1000000001018, '角色权限编辑', 1, 'system:role-permission:write', false, true, 71, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL, NULL)
ON CONFLICT (permission_code) DO NOTHING;

INSERT INTO sys_role_menu (
    id, role_id, menu_id, created_at, updated_at, created_by, updated_by
)
SELECT
    1000000002000 + ROW_NUMBER() OVER (ORDER BY sm.id),
    sr.id,
    sm.id,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    NULL,
    NULL
FROM sys_role AS sr
CROSS JOIN sys_menu AS sm
WHERE sr.code = 'admin' AND sm.permission_code IS NOT NULL
ON CONFLICT (role_id, menu_id) DO NOTHING;

INSERT INTO sys_role_menu (
    id, role_id, menu_id, created_at, updated_at, created_by, updated_by
)
SELECT
    1000000003000 + ROW_NUMBER() OVER (ORDER BY sm.id),
    sr.id,
    sm.id,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    NULL,
    NULL
FROM sys_role AS sr
CROSS JOIN sys_menu AS sm
WHERE sr.code = 'viewer'
  AND sm.permission_code IN ('system:user:read', 'system:httpdemo:read')
ON CONFLICT (role_id, menu_id) DO NOTHING;

INSERT INTO sys_user (
    id,
    username,
    password_hash,
    nickname,
    phone,
    email,
    avatar_url,
    is_super_admin,
    is_enable,
    sort_id,
    created_at,
    updated_at,
    created_by,
    updated_by
) VALUES (
    1000000000001,
    'admin',
    '__INITRA_ADMIN_PASSWORD_HASH__',
    'System Admin',
    NULL,
    'admin@example.com',
    NULL,
    true,
    true,
    0,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    NULL,
    NULL
)
ON CONFLICT (username) DO NOTHING;

INSERT INTO sys_user_role (
    id,
    user_id,
    role_id,
    created_by,
    updated_by,
    created_at,
    updated_at
)
SELECT
    1000000000201,
    su.id,
    sr.id,
    NULL,
    NULL,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
FROM sys_user AS su
INNER JOIN sys_role AS sr ON sr.code = 'admin'
WHERE su.username = 'admin'
ON CONFLICT (user_id, role_id) DO NOTHING;
