//go:build unit

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type teamHTTPFixture struct {
	svc                  *service.TeamService
	client               *dbent.Client
	router               *gin.Engine
	owner, admin, member *dbent.User
	teamID               int64
}

func newTeamHTTPFixture(t *testing.T) *teamHTTPFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	svc := service.NewTeamService(client, db, nil, nil)

	mk := func(email string, balance float64) *dbent.User {
		u, err := client.User.Create().SetEmail(email).SetPasswordHash("h").SetRole(service.RoleUser).SetStatus(service.StatusActive).SetBalance(balance).Save(context.Background())
		require.NoError(t, err)
		return u
	}
	f := &teamHTTPFixture{svc: svc, client: client, owner: mk("o@example.com", 100), admin: mk("a@example.com", 0), member: mk("m@example.com", 0)}
	ctx := context.Background()
	summary, err := svc.Upgrade(ctx, f.owner.ID, "HTTP Team")
	require.NoError(t, err)
	f.teamID = summary.Team.ID
	for _, u := range []*dbent.User{f.admin, f.member} {
		_, err = svc.Invite(ctx, f.owner.ID, u.Email)
		require.NoError(t, err)
		_, err = svc.RespondInvitation(ctx, u.ID, true)
		require.NoError(t, err)
	}

	h := NewTeamHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: id})
		c.Next()
	})
	team := r.Group("/team")
	team.GET("", h.GetContext)
	team.PUT("", h.Rename)
	team.DELETE("", h.Dissolve)
	team.GET("/usage", h.ListUsage)
	team.GET("/usage/summary", h.UsageSummary)
	team.GET("/usage/members", h.MemberSpendStats)
	team.POST("/transfer-owner", h.TransferOwnership)
	team.POST("/members/:id/balance", h.AllocateBalance)
	team.PUT("/members/:id/admin", h.SetMemberAdmin)
	team.GET("/transactions", h.ListTransactions)
	f.router = r
	return f
}

func (f *teamHTTPFixture) do(userID int64, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestTeamAdminHTTP_RoleChangeIsImmediateAndTransferStaysOwnerOnly(t *testing.T) {
	f := newTeamHTTPFixture(t)
	adminPath := fmt.Sprintf("/team/members/%d/admin", f.admin.ID)
	memberAlloc := fmt.Sprintf("/team/members/%d/balance", f.member.ID)

	// 未被任命：管理接口 403，成员视角角色为 member。
	require.Equal(t, http.StatusForbidden, f.do(f.admin.ID, http.MethodPut, "/team", `{"name":"x"}`).Code)
	require.Equal(t, http.StatusForbidden, f.do(f.admin.ID, http.MethodGet, "/team/usage/members", "").Code)

	// 管理员不能任命管理员；主账号可以。
	require.NotEqual(t, http.StatusOK, f.do(f.admin.ID, http.MethodPut, fmt.Sprintf("/team/members/%d/admin", f.member.ID), `{"is_admin":true}`).Code)
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, adminPath, `{"is_admin":true}`).Code)

	// 同一个会话（同一个用户 ID，无需重新登录）下一次请求立即获得权限。
	ctxRec := f.do(f.admin.ID, http.MethodGet, "/team", "")
	require.Equal(t, http.StatusOK, ctxRec.Code)
	var ctxBody struct {
		Data struct {
			Role string `json:"role"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ctxRec.Body.Bytes(), &ctxBody))
	require.Equal(t, "admin", ctxBody.Data.Role)
	require.Equal(t, http.StatusOK, f.do(f.admin.ID, http.MethodPut, "/team", `{"name":"Renamed"}`).Code)
	require.Equal(t, http.StatusOK, f.do(f.admin.ID, http.MethodPost, memberAlloc, `{"amount":5}`).Code)
	require.Equal(t, http.StatusOK, f.do(f.admin.ID, http.MethodGet, "/team/usage/members?start_date=2026-10-01&end_date=2026-10-31", "").Code)
	require.Equal(t, http.StatusBadRequest, f.do(f.admin.ID, http.MethodGet, "/team/usage/members?start_date=nope", "").Code)

	// 管理员不能转让主账号，也不能自己任命/撤销管理员。
	transfer := fmt.Sprintf(`{"member_id":%d}`, f.member.ID)
	require.NotEqual(t, http.StatusOK, f.do(f.admin.ID, http.MethodPost, "/team/transfer-owner", transfer).Code)
	require.NotEqual(t, http.StatusOK, f.do(f.admin.ID, http.MethodPut, adminPath, `{"is_admin":false}`).Code)
	ownerCtx, err := f.svc.GetContext(context.Background(), f.owner.ID)
	require.NoError(t, err)
	require.Equal(t, service.TeamRoleOwner, ownerCtx.Role)

	// 撤销后下一次请求立即 403。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, adminPath, `{"is_admin":false}`).Code)
	require.Equal(t, http.StatusForbidden, f.do(f.admin.ID, http.MethodPut, "/team", `{"name":"again"}`).Code)
	require.Equal(t, http.StatusForbidden, f.do(f.admin.ID, http.MethodPost, memberAlloc, `{"amount":5}`).Code)

	// 主账号转让依旧可用。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPost, "/team/transfer-owner", transfer).Code)
}

func TestTeamAdminHTTP_DissolveIsOwnerOnly(t *testing.T) {
	f := newTeamHTTPFixture(t)
	adminPath := fmt.Sprintf("/team/members/%d/admin", f.admin.ID)
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, adminPath, `{"is_admin":true}`).Code)

	// 管理员解散团队 -> 403 TEAM_OWNER_REQUIRED，团队仍然存在，且管理员其他权限不受影响。
	rec := f.do(f.admin.ID, http.MethodDelete, "/team", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
	var body struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "TEAM_OWNER_REQUIRED", body.Reason)
	teamCtx, err := f.svc.GetContext(context.Background(), f.owner.ID)
	require.NoError(t, err)
	require.NotNil(t, teamCtx.Team)
	require.Equal(t, http.StatusOK, f.do(f.admin.ID, http.MethodPut, "/team", `{"name":"still-manager"}`).Code)

	// 普通成员同样 403。
	require.Equal(t, http.StatusForbidden, f.do(f.member.ID, http.MethodDelete, "/team", "").Code)

	// 主账号可以解散。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodDelete, "/team", "").Code)
	after, err := f.svc.GetContext(context.Background(), f.owner.ID)
	require.NoError(t, err)
	require.Nil(t, after.Team)
}

func TestTeamAdminHTTP_LedgerScopeFollowsRole(t *testing.T) {
	f := newTeamHTTPFixture(t)
	// 主账号给普通成员分配额度，产生一条流水（涉及 member）。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPost, fmt.Sprintf("/team/members/%d/balance", f.member.ID), `{"amount":5}`).Code)

	count := func(userID int64) (int, int) {
		rec := f.do(userID, http.MethodGet, "/team/transactions", "")
		if rec.Code != http.StatusOK {
			return rec.Code, 0
		}
		var body struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return rec.Code, body.Data.Total
	}

	// 普通成员仍可查看自己的流水（不会因为新增管理员权限检查而 403），且只是团队流水的子集。
	code, ownerTotal := count(f.owner.ID)
	require.Equal(t, http.StatusOK, code)
	code, memberTotal := count(f.member.ID)
	require.Equal(t, http.StatusOK, code)
	require.GreaterOrEqual(t, memberTotal, 1)
	require.Less(t, memberTotal, ownerTotal)
	code, adminBefore := count(f.admin.ID)
	require.Equal(t, http.StatusOK, code)
	require.Less(t, adminBefore, ownerTotal)

	// 设为管理员后立即能看到团队全部流水，与主账号一致。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, fmt.Sprintf("/team/members/%d/admin", f.admin.ID), `{"is_admin":true}`).Code)
	code, adminTotal := count(f.admin.ID)
	require.Equal(t, http.StatusOK, code)
	_, ownerTotal = count(f.owner.ID)
	require.Equal(t, ownerTotal, adminTotal)

	// 撤销后立即回到仅自己的流水。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, fmt.Sprintf("/team/members/%d/admin", f.admin.ID), `{"is_admin":false}`).Code)
	code, adminAfter := count(f.admin.ID)
	require.Equal(t, http.StatusOK, code)
	_, ownerTotal = count(f.owner.ID)
	require.Less(t, adminAfter, ownerTotal)
}

// 团队用量：主账号 / 管理员看全部（含主账号自己的用量）；普通成员只看自己，且不能通过 member_id 偷看别人。
func TestTeamAdminHTTP_UsageScopeFollowsRoleAndIncludesOwner(t *testing.T) {
	f := newTeamHTTPFixture(t)
	ctx := context.Background()
	account, err := f.client.Account.Create().SetName("acc").SetPlatform("openai").SetType("apikey").Save(ctx)
	require.NoError(t, err)
	addLog := func(u *dbent.User, n int) {
		key, err := f.client.APIKey.Create().SetUserID(u.ID).SetKey(fmt.Sprintf("sk-usage-%d", u.ID)).SetName("k").Save(ctx)
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := f.client.UsageLog.Create().SetUserID(u.ID).SetAPIKeyID(key.ID).SetAccountID(account.ID).
				SetRequestID(fmt.Sprintf("r-%d-%d", u.ID, i)).SetModel("gpt-test").SetInputTokens(1).SetOutputTokens(1).
				SetTotalCost(1).SetActualCost(1).Save(ctx)
			require.NoError(t, err)
		}
	}
	addLog(f.owner, 1)
	addLog(f.admin, 2)
	addLog(f.member, 3)

	type page struct {
		Data struct {
			Items []struct {
				UserID int64 `json:"user_id"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	get := func(userID int64, path string) (int, page) {
		rec := f.do(userID, http.MethodGet, path, "")
		var body page
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	// 普通成员：只有自己的 3 条；member_id 指向别人时也不会泄露别人的数据。
	code, body := get(f.member.ID, "/team/usage")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 3, body.Data.Total)
	for _, item := range body.Data.Items {
		require.Equal(t, f.member.ID, item.UserID)
	}
	code, body = get(f.member.ID, fmt.Sprintf("/team/usage?member_id=%d", f.owner.ID))
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 3, body.Data.Total)
	for _, item := range body.Data.Items {
		require.Equal(t, f.member.ID, item.UserID, "a member must never see another member's rows")
	}

	// 主账号：全部 6 条（含自己的）；管理员（被任命后）同样；两者都可以按主账号自己的用量筛选。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPut, fmt.Sprintf("/team/members/%d/admin", f.admin.ID), `{"is_admin":true}`).Code)
	for _, uid := range []int64{f.owner.ID, f.admin.ID} {
		code, body = get(uid, "/team/usage")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, 6, body.Data.Total)
		code, body = get(uid, fmt.Sprintf("/team/usage?member_id=%d", f.owner.ID))
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, 1, body.Data.Total, "the owner's own usage must be filterable")
		require.Equal(t, f.owner.ID, body.Data.Items[0].UserID)
	}

	// 没有加入任何团队的用户拿不到团队用量。
	outsider, err := f.client.User.Create().SetEmail("out@example.com").SetPasswordHash("h").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	code, _ = get(outsider.ID, "/team/usage")
	require.NotEqual(t, http.StatusOK, code)
}
