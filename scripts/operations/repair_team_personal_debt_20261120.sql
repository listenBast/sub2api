\set ON_ERROR_STOP on
-- Explicit operator gates; no database changes in the default audit mode.
\if :{?apply}
\else
  \set apply false
\endif
\if :{?maintenance_confirmed}
\else
  \set maintenance_confirmed false
\endif

\if :apply
  \if :maintenance_confirmed
  \else
    \echo 'REFUSED: stop admission, drain/reconcile billing, verify backup, and confirm maintenance first.'
    DO $gate$ BEGIN RAISE EXCEPTION 'Maintenance confirmation required'; END $gate$;
  \endif
  \if :{?reviewed_ids}
  \else
    DO $gate$ BEGIN RAISE EXCEPTION 'reviewed_ids required'; END $gate$;
  \endif
  \if :{?reviewed_team_id}
  \else
    DO $gate$ BEGIN RAISE EXCEPTION 'reviewed_team_id required'; END $gate$;
  \endif
  \if :{?batch_id}
  \else
    DO $gate$ BEGIN RAISE EXCEPTION 'batch_id required'; END $gate$;
  \endif
BEGIN;
SELECT set_config('wallet_repair.reviewed_ids', :'reviewed_ids', true);
SELECT set_config('wallet_repair.reviewed_team_id', :'reviewed_team_id', true);
SELECT set_config('wallet_repair.batch_id', :'batch_id', true);
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';
SELECT pg_advisory_xact_lock(748593021120::bigint);
-- Maintenance-only: prevent concurrent allocations, membership changes and
-- balance mutation throughout revalidation, audit insertion and correction.
LOCK TABLE teams, team_memberships IN SHARE MODE;
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE IF NOT EXISTS wallet_balance_repairs (
    batch_id text NOT NULL,
    user_id bigint NOT NULL,
    team_id bigint NOT NULL,
    personal_before numeric(20,8) NOT NULL,
    team_before numeric(20,8) NOT NULL,
    personal_after numeric(20,8) NOT NULL,
    team_after numeric(20,8) NOT NULL,
    debt numeric(20,8) NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(batch_id,user_id),
    CHECK (personal_before < 0 AND personal_after = 0 AND debt = -personal_before),
    CHECK (team_before >= debt AND team_after = team_before - debt AND team_after >= 0),
    CHECK (personal_before + team_before = personal_after + team_after)
);
DO $repair$
DECLARE
    batch constant text := current_setting('wallet_repair.batch_id');
    ids constant bigint[] := current_setting('wallet_repair.reviewed_ids')::bigint[];
    reviewed_team constant bigint := current_setting('wallet_repair.reviewed_team_id')::bigint;
    expected_count constant integer := cardinality(ids);
    n integer;
    affected integer;
    old_total numeric;
    new_total numeric;
BEGIN
    IF expected_count IS NULL OR expected_count < 1 OR reviewed_team < 1
       OR length(batch) NOT BETWEEN 1 AND 200
       OR EXISTS(SELECT 1 FROM unnest(ids) v WHERE v IS NULL OR v<=0)
       OR (SELECT count(DISTINCT v) FROM unnest(ids) v) <> expected_count THEN
        RAISE EXCEPTION 'Invalid explicit reviewed cohort/batch';
    END IF;
    SELECT count(*) INTO n FROM wallet_balance_repairs WHERE batch_id=batch;
    IF n=expected_count THEN
        IF EXISTS (SELECT 1 FROM wallet_balance_repairs
                   WHERE batch_id=batch AND (NOT user_id=ANY(ids) OR team_id<>reviewed_team)) THEN
            RAISE EXCEPTION 'Existing audit batch does not match approved scope';
        END IF;
        RAISE NOTICE 'Batch already applied; no balance changes. Any later debt needs a NEW reviewed batch.';
        RETURN;
    ELSIF n<>0 THEN
        RAISE EXCEPTION 'Partial/unexpected existing audit batch: % rows',n;
    END IF;

    -- IDs define reviewed scope, NOT stale debt amounts. Every amount and
    -- membership is read again under locks. Other negative wallets are untouched.
    SELECT count(*) INTO n
    FROM users u JOIN team_memberships tm ON tm.user_id=u.id
    JOIN teams t ON t.id=tm.team_id
    WHERE u.id=ANY(ids) AND u.deleted_at IS NULL AND u.balance<0
      AND tm.team_id=reviewed_team AND tm.status IN ('active','exit_pending') AND t.status='active';
    IF n<>expected_count OR EXISTS (
        SELECT 1 FROM team_memberships WHERE user_id=ANY(ids)
        GROUP BY user_id HAVING count(*)<>1
    ) THEN
        RAISE EXCEPTION 'Reviewed membership/debt cohort changed; re-audit before applying';
    END IF;
    IF EXISTS (SELECT 1 FROM users WHERE id=ANY(ids)
               AND (team_balance IS NULL OR team_balance < -balance)) THEN
        RAISE EXCEPTION 'Insufficient individual team quota; no debt may be forgiven or overdrawn';
    END IF;
    -- Catch newly affected active members, so a stale cohort report is not
    -- silently presented as a complete repair of all team-member personal debt.
    IF EXISTS (SELECT 1 FROM users u JOIN team_memberships tm ON tm.user_id=u.id
               WHERE u.deleted_at IS NULL AND u.balance<0
                 AND tm.status IN ('active','exit_pending') AND NOT u.id=ANY(ids)) THEN
        RAISE EXCEPTION 'Additional indebted team members found; re-audit and review scope';
    END IF;
    SELECT sum(balance+team_balance) INTO old_total FROM users WHERE id=ANY(ids);
    INSERT INTO wallet_balance_repairs
      (batch_id,user_id,team_id,personal_before,team_before,personal_after,team_after,debt)
    SELECT batch,u.id,tm.team_id,u.balance,u.team_balance,0,u.team_balance+u.balance,-u.balance
    FROM users u JOIN team_memberships tm ON tm.user_id=u.id WHERE u.id=ANY(ids);
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>expected_count THEN RAISE EXCEPTION 'Unexpected audit count %',affected; END IF;
    UPDATE users u SET balance=0,team_balance=a.team_after,updated_at=now()
    FROM wallet_balance_repairs a
    WHERE a.batch_id=batch AND a.user_id=u.id
      AND u.balance=a.personal_before AND u.team_balance=a.team_before;
    GET DIAGNOSTICS affected=ROW_COUNT;
    IF affected<>expected_count THEN RAISE EXCEPTION 'Unexpected correction count %',affected; END IF;
    SELECT sum(balance+team_balance) INTO new_total FROM users WHERE id=ANY(ids);
    IF old_total IS DISTINCT FROM new_total OR EXISTS (
      SELECT 1 FROM users WHERE id=ANY(ids) AND (balance<>0 OR team_balance<0)
    ) THEN RAISE EXCEPTION 'Conservation/nonnegative invariant violated'; END IF;
END
$repair$;
SELECT batch_id,user_id,team_id,personal_before,team_before,personal_after,team_after,debt,
       applied_at AT TIME ZONE 'Asia/Shanghai' AS applied_at_shanghai
FROM wallet_balance_repairs WHERE batch_id=current_setting('wallet_repair.batch_id') ORDER BY user_id;
COMMIT;
\else
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT u.id AS user_id,tm.team_id,tm.status AS membership_status,t.status AS team_status,
       u.balance::text AS personal_before,u.team_balance::text AS team_before,
       (-u.balance)::text AS debt,(u.team_balance+u.balance)::text AS team_after,
       (u.team_balance >= -u.balance) AS sufficient,
       u.team_balance >= -u.balance AND tm.status IN ('active','exit_pending') AND t.status='active' AS potentially_eligible
FROM users u JOIN team_memberships tm ON tm.user_id=u.id LEFT JOIN teams t ON t.id=tm.team_id
WHERE u.deleted_at IS NULL AND u.balance<0 ORDER BY tm.team_id,u.id;
COMMIT;
\echo 'READ-ONLY audit only. No repair or audit table was written.'
\endif
