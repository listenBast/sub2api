-- 团队管理员：主账号可把已加入的成员设为团队管理员。
-- 管理员拥有除“转让主账号”外与主账号一致的团队管理权限；权限在每次请求时按此列实时判定。
-- 仅新增带默认值的列，存量成员全部为非管理员，对现有数据和旧版本代码均安全（可重复执行）。
ALTER TABLE team_memberships
    ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
