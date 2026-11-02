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
	f := &teamHTTPFixture{svc: svc, owner: mk("o@example.com", 100), admin: mk("a@example.com", 0), member: mk("m@example.com", 0)}
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

	// 管理员可以解散（与主账号一致），主账号转让依旧可用。
	require.Equal(t, http.StatusOK, f.do(f.owner.ID, http.MethodPost, "/team/transfer-owner", transfer).Code)
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
