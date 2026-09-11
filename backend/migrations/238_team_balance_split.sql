-- 团队模式：成员个人余额与团队额度分离，API Key 支持选择余额扣费方式。
--
-- users.team_balance      主账号分配给成员的团队额度（成员个人余额继续存放在 users.balance）。
-- api_keys.balance_mode   该 Key 的扣费方式：
--   team_first      优先扣团队额度，不足时扣个人余额（默认）
--   personal_first  优先扣个人余额，不足时扣团队额度
--   team_only       仅扣团队额度
--   personal_only   仅扣个人余额
ALTER TABLE users ADD COLUMN IF NOT EXISTS team_balance NUMERIC(20,8) NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS balance_mode VARCHAR(20) NOT NULL DEFAULT 'team_first';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_balance_mode_check') THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_balance_mode_check
            CHECK (balance_mode IN ('team_first', 'personal_first', 'team_only', 'personal_only'));
    END IF;
END $$;

-- 存量团队成员：加入团队时个人余额已并入主账号，当前余额全部来自主账号分配，
-- 因此整体迁移为团队额度，个人余额从 0 开始重新累计。
UPDATE users u
SET team_balance = u.balance,
    balance = 0,
    updated_at = NOW()
FROM team_memberships m
WHERE m.user_id = u.id
  AND m.status IN ('active', 'exit_pending')
  AND u.balance <> 0;
