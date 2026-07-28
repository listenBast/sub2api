-- 团队模式：主账号、成员状态及不可变资金流水。
CREATE TABLE IF NOT EXISTS teams (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT teams_owner_id_key UNIQUE (owner_id),
    CONSTRAINT teams_status_check CHECK (status IN ('active', 'suspended'))
);

CREATE INDEX IF NOT EXISTS idx_teams_status ON teams(status);

CREATE TABLE IF NOT EXISTS team_memberships (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    invited_by BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'invited',
    joined_at TIMESTAMPTZ,
    exit_requested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT team_memberships_user_id_key UNIQUE (user_id),
    CONSTRAINT team_memberships_team_user_key UNIQUE (team_id, user_id),
    CONSTRAINT team_memberships_status_check CHECK (status IN ('invited', 'active', 'exit_pending'))
);

CREATE INDEX IF NOT EXISTS idx_team_memberships_team_status
    ON team_memberships(team_id, status);

CREATE TABLE IF NOT EXISTS team_transactions (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    operator_id BIGINT NOT NULL,
    member_id BIGINT,
    action VARCHAR(40) NOT NULL,
    amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    owner_balance_before NUMERIC(20,8) NOT NULL DEFAULT 0,
    owner_balance_after NUMERIC(20,8) NOT NULL DEFAULT 0,
    member_balance_before NUMERIC(20,8),
    member_balance_after NUMERIC(20,8),
    note VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_team_transactions_team_created
    ON team_transactions(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_team_transactions_member_created
    ON team_transactions(member_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_team_transactions_action
    ON team_transactions(action);
