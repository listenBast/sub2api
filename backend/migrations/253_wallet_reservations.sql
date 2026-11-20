-- Fork-only groundwork. No gateway is switched to this ledger by this migration.
-- Reserved money has already been removed from users.balance/team_balance.
-- Never expire these holds automatically: an upstream request may still be billable.
CREATE TABLE IF NOT EXISTS wallet_reservations (
    request_id text PRIMARY KEY CHECK (length(request_id) BETWEEN 1 AND 200),
    user_id bigint NOT NULL REFERENCES users(id),
    api_key_id bigint NOT NULL DEFAULT 0,
    team_id bigint,
    dispatched_at timestamptz,
    no_charge_reason text,
    billing_receipt jsonb,
    fingerprint text NOT NULL CHECK (length(fingerprint) BETWEEN 1 AND 200),
    pricing_digest text NOT NULL CHECK (length(pricing_digest) BETWEEN 1 AND 200),
    balance_mode text NOT NULL CHECK (balance_mode IN ('personal_only','team_only','personal_first','team_first')),
    personal_units bigint NOT NULL CHECK (personal_units >= 0),
    team_units bigint NOT NULL CHECK (team_units >= 0),
    state text NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved','settled','released')),
    actual_units bigint,
    spent_personal_units bigint,
    spent_team_units bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz,
    CHECK (personal_units + team_units > 0),
    CHECK (
       (state = 'reserved' AND actual_units IS NULL AND spent_personal_units IS NULL
        AND spent_team_units IS NULL AND finalized_at IS NULL)
       OR
       (state IN ('settled','released') AND actual_units IS NOT NULL
        AND spent_personal_units IS NOT NULL AND spent_team_units IS NOT NULL AND actual_units >= 0
        AND spent_personal_units >= 0 AND spent_team_units >= 0
        AND spent_personal_units <= personal_units AND spent_team_units <= team_units
        AND spent_personal_units + spent_team_units = actual_units
        AND actual_units <= personal_units + team_units AND finalized_at IS NOT NULL
        AND (state <> 'released' OR actual_units = 0))
    )
);
CREATE INDEX IF NOT EXISTS wallet_reservations_pending_user_idx
    ON wallet_reservations(user_id, created_at) WHERE state = 'reserved';

-- A known successful fixed-price money event is durable before entering the
-- in-memory billing lanes. Claims are leased; settlement/dedup/delete is atomic.
CREATE TABLE IF NOT EXISTS wallet_billing_outbox (
    reservation_id text PRIMARY KEY REFERENCES wallet_reservations(request_id),
    fingerprint text NOT NULL,
    command jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    retry_after timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
