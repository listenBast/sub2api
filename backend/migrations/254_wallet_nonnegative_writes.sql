-- Fork-only defense in depth for EVERY database writer, including old/admin paths.
-- Preserve historical debt, permit repayment, but reject creation/worsening of
-- negative wallets. This is NOT a substitute for before-service reservations.
CREATE OR REPLACE FUNCTION reject_wallet_overdraft() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.balance::text IN ('NaN','Infinity','-Infinity')
       OR NEW.team_balance::text IN ('NaN','Infinity','-Infinity') THEN
        RAISE EXCEPTION 'wallet balances must be finite' USING ERRCODE='23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.balance < 0 OR COALESCE(NEW.team_balance,0) < 0 THEN
            RAISE EXCEPTION 'new wallet balances must be nonnegative' USING ERRCODE='23514';
        END IF;
    ELSE
        IF (NEW.balance < 0 AND NEW.balance < OLD.balance)
           OR (COALESCE(NEW.team_balance,0) < 0 AND COALESCE(NEW.team_balance,0) < COALESCE(OLD.team_balance,0)) THEN
            RAISE EXCEPTION 'wallet overdraft is not permitted' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END
$$;
DROP TRIGGER IF EXISTS users_reject_wallet_overdraft ON users;
CREATE TRIGGER users_reject_wallet_overdraft
    BEFORE INSERT OR UPDATE OF balance,team_balance ON users
    FOR EACH ROW EXECUTE FUNCTION reject_wallet_overdraft();


-- A pending team hold belongs to the allocation's ORIGINAL team. Do not let a
-- member exit/switch teams while a refundable old-team reservation exists.
CREATE OR REPLACE FUNCTION reject_reserved_team_membership_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.team_id=OLD.team_id
       AND NEW.user_id=OLD.user_id AND NEW.status IN ('active','exit_pending') THEN RETURN NEW; END IF;
    PERFORM id FROM users WHERE id=OLD.user_id FOR UPDATE;
    IF EXISTS(SELECT 1 FROM wallet_reservations WHERE user_id=OLD.user_id
              AND team_units>0 AND state='reserved') THEN
        RAISE EXCEPTION 'pending team charges must be reconciled before leaving or changing teams' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END
$$;
DROP TRIGGER IF EXISTS team_membership_reserved_funds ON team_memberships;
CREATE TRIGGER team_membership_reserved_funds BEFORE UPDATE OF team_id,status,user_id OR DELETE
    ON team_memberships FOR EACH ROW EXECUTE FUNCTION reject_reserved_team_membership_change();
