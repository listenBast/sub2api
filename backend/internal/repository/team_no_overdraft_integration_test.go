//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 不透支（sub2api-custom: team billing）：在真实 Postgres 上逐条执行 teamUsageBillingDeductSQL，
// 统一计费路径（applyUsageBillingEffects）与兼容路径（DeductBalanceByMode）都要得到相同结果。

var noOverdraftSeq atomic.Int64

type noOverdraftQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func noOverdraftQueryRow(t *testing.T, ctx context.Context, q noOverdraftQuerier, dest []any, query string, args ...any) {
	t.Helper()
	rows, err := q.QueryContext(ctx, query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "expected one row")
	require.NoError(t, rows.Scan(dest...))
	require.NoError(t, rows.Err())
}

// seedNoOverdraftUser 建一个成员账号（以及按需建团队和成员关系），返回用户 ID。
func seedNoOverdraftUser(t *testing.T, ctx context.Context, q noOverdraftQuerier, membership string, personal, team float64) int64 {
	t.Helper()
	n := noOverdraftSeq.Add(1)
	stamp := time.Now().UnixNano()
	var userID int64
	noOverdraftQueryRow(t, ctx, q, []any{&userID},
		`INSERT INTO users (email, password_hash, role, status, balance, team_balance, concurrency) VALUES ($1,'x','user','active',$2,$3,1) RETURNING id`,
		fmt.Sprintf("no-od-member-%d-%d@x.test", stamp, n), personal, team)
	if membership == "" {
		return userID
	}
	var ownerID, teamID int64
	noOverdraftQueryRow(t, ctx, q, []any{&ownerID},
		`INSERT INTO users (email, password_hash, role, status, balance, concurrency) VALUES ($1,'x','user','active',100,1) RETURNING id`,
		fmt.Sprintf("no-od-owner-%d-%d@x.test", stamp, n))
	noOverdraftQueryRow(t, ctx, q, []any{&teamID}, `INSERT INTO teams (name, owner_id) VALUES ('no-od',$1) RETURNING id`, ownerID)
	_, err := q.ExecContext(ctx, `INSERT INTO team_memberships (team_id, user_id, invited_by, status) VALUES ($1,$2,$3,$4)`, teamID, userID, ownerID, membership)
	require.NoError(t, err)
	return userID
}

func readNoOverdraftBalances(t *testing.T, ctx context.Context, q noOverdraftQuerier, userID int64) (float64, float64) {
	t.Helper()
	var personalText, teamText string
	noOverdraftQueryRow(t, ctx, q, []any{&personalText, &teamText}, `SELECT balance::text, team_balance::text FROM users WHERE id = $1`, userID)
	personal, err := strconv.ParseFloat(personalText, 64)
	require.NoError(t, err)
	team, err := strconv.ParseFloat(teamText, 64)
	require.NoError(t, err)
	return personal, team
}

func TestTeamNoOverdraftIntegration_UnifiedBillingPath(t *testing.T) {
	ctx := context.Background()
	for _, tc := range noOverdraftCases {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			userID := seedNoOverdraftUser(t, ctx, tx, tc.membership, tc.personal, tc.team)

			result := &service.UsageBillingApplyResult{Applied: true}
			err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
				UserID: userID, BalanceCost: tc.amount, BalanceMode: tc.mode,
			}, result)
			require.NoError(t, err)
			require.NotNil(t, result.NewBalance)
			require.NotNil(t, result.NewTeamBalance)
			require.InDelta(t, tc.wantPersonalPart, result.PersonalBalanceCost, 1e-9, "personal part")
			require.InDelta(t, tc.wantTeamPart, result.TeamBalanceCost, 1e-9, "team part")
			require.InDelta(t, tc.wantUncovered, result.UncoveredBalanceCost, 1e-9, "uncovered")
			require.Equal(t, !tc.wantSufficient(), result.BalanceOverdrafted)
			require.InDelta(t, tc.wantBalance, *result.NewBalance, 1e-9)
			require.InDelta(t, tc.wantTeamBalance, *result.NewTeamBalance, 1e-9)

			personal, team := readNoOverdraftBalances(t, ctx, tx, userID)
			require.InDelta(t, tc.wantBalance, personal, 1e-9, "db balance")
			require.InDelta(t, tc.wantTeamBalance, team, 1e-9, "db team_balance")
			// 不透支：本次扣费不会让任何一个资金池比扣费前更负，也不会从非负变成负数。
			require.True(t, personal >= 0 || personal == tc.personal, "personal must never be pushed below 0")
			require.True(t, team >= 0 || team == tc.team, "team must never be pushed below 0")
		})
	}
}

func TestTeamNoOverdraftIntegration_CompatibilityPath(t *testing.T) {
	ctx := context.Background()
	repo := newUserRepositoryWithSQL(integrationEntClient, integrationDB)
	for _, tc := range noOverdraftCases {
		t.Run(tc.name, func(t *testing.T) {
			entTx := testEntTx(t)
			txCtx := dbent.NewTxContext(ctx, entTx)
			client := entTx.Client()
			userID := seedNoOverdraftUser(t, txCtx, client, tc.membership, tc.personal, tc.team)

			res, err := repo.DeductBalanceByMode(txCtx, userID, tc.amount, tc.mode)
			require.NoError(t, err)
			require.InDelta(t, tc.wantPersonalPart, res.Personal, 1e-9, "personal part")
			require.InDelta(t, tc.wantTeamPart, res.Team, 1e-9, "team part")
			require.InDelta(t, tc.wantUncovered, res.Uncovered, 1e-9, "uncovered")

			personal, team := readNoOverdraftBalances(t, txCtx, client, userID)
			require.InDelta(t, tc.wantBalance, personal, 1e-9, "db balance")
			require.InDelta(t, tc.wantTeamBalance, team, 1e-9, "db team_balance")
		})
	}
}

func TestTeamNoOverdraftIntegration_MissingAndDeletedUser(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	var deletedID int64
	noOverdraftQueryRow(t, ctx, tx, []any{&deletedID},
		`INSERT INTO users (email, password_hash, role, status, balance, concurrency, deleted_at) VALUES ($1,'x','user','active',5,1,NOW()) RETURNING id`,
		fmt.Sprintf("no-od-deleted-%d@x.test", time.Now().UnixNano()))
	for _, id := range []int64{deletedID, 999999999} {
		_, err := deductTeamUsageBillingBalance(ctx, tx, id, 1, service.BalanceModeTeamFirst)
		require.ErrorIs(t, err, service.ErrUserNotFound)
	}
	personal, _ := readNoOverdraftBalances(t, ctx, tx, deletedID)
	require.InDelta(t, 5, personal, 1e-9, "deleted user is never charged")
}

// 并发：团队额度 5、个人 0，20 个并发请求各扣 1（各自独立事务提交）。
// 行锁保证最终恰好扣掉 5，其余 15 记为 uncovered，余额不会出现负数。
func TestTeamNoOverdraftIntegration_ConcurrentChargesNeverOverdraw(t *testing.T) {
	ctx := context.Background()
	userID := seedNoOverdraftUser(t, ctx, integrationDB, "active", 0, 5)
	var teamID, ownerID int64
	noOverdraftQueryRow(t, ctx, integrationDB, []any{&teamID, &ownerID},
		`SELECT t.id, t.owner_id FROM team_memberships tm JOIN teams t ON t.id = tm.team_id WHERE tm.user_id = $1`, userID)
	// 本测试需要真实提交，结束后把建的数据全部删掉，避免影响其他按全表统计的集成测试。
	t.Cleanup(func() {
		for _, q := range []struct {
			sql string
			arg int64
		}{
			{`DELETE FROM team_memberships WHERE team_id = $1`, teamID},
			{`DELETE FROM teams WHERE id = $1`, teamID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM users WHERE id = $1`, ownerID},
		} {
			_, err := integrationDB.ExecContext(ctx, q.sql, q.arg)
			require.NoError(t, err)
		}
	})

	const workers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var charged, uncovered float64
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := integrationDB.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = tx.Rollback() }()
			result := &service.UsageBillingApplyResult{Applied: true}
			if err := (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
				UserID: userID, BalanceCost: 1, BalanceMode: service.BalanceModeTeamFirst,
			}, result); err != nil {
				errs <- err
				return
			}
			if err := tx.Commit(); err != nil {
				errs <- err
				return
			}
			mu.Lock()
			charged += result.PersonalBalanceCost + result.TeamBalanceCost
			uncovered += result.UncoveredBalanceCost
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	personal, team := readNoOverdraftBalances(t, ctx, integrationDB, userID)
	require.InDelta(t, 0, personal, 1e-9)
	require.InDelta(t, 0, team, 1e-9)
	require.InDelta(t, 5, charged, 1e-9)
	require.InDelta(t, 15, uncovered, 1e-9)
}
