//go:build unit

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type recordingBalanceService struct {
	*stubAdminService
	calls []balanceCall
}

type balanceCall struct {
	userID    int64
	balance   float64
	operation string
	notes     string
}

func (s *recordingBalanceService) UpdateUserBalance(ctx context.Context, userID int64, balance float64, operation, notes string) (*service.User, error) {
	s.calls = append(s.calls, balanceCall{userID, balance, operation, notes})
	return s.stubAdminService.UpdateUserBalance(ctx, userID, balance, operation, notes)
}

func TestAdminTeamHandler_OwnerBalanceAdminRoleAndStats(t *testing.T) {
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
	teamSvc := service.NewTeamService(client, db, nil, nil)
	ctx := context.Background()
	mk := func(email string) *dbent.User {
		u, err := client.User.Create().SetEmail(email).SetPasswordHash("h").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
		require.NoError(t, err)
		return u
	}
	owner, member := mk("ao@example.com"), mk("am@example.com")
	summary, err := teamSvc.AdminCreateTeam(ctx, 999, owner.Email, "Admin HTTP Team")
	require.NoError(t, err)
	_, err = teamSvc.AdminAddMember(ctx, 999, summary.ID, member.Email, "")
	require.NoError(t, err)

	rec := &recordingBalanceService{stubAdminService: newStubAdminService()}
	h := NewTeamHandler(teamSvc, rec)
	r := gin.New()
	r.POST("/teams/:id/owner-balance", h.AdjustOwnerBalance)
	r.PUT("/teams/:id/members/:member_id/admin", h.SetMemberAdmin)
	r.GET("/teams/:id/usage/members", h.MemberSpendStats)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 复用用户管理的余额调整服务，目标是团队主账号。
	require.Equal(t, http.StatusOK, do(http.MethodPost, fmt.Sprintf("/teams/%d/owner-balance", summary.ID), `{"balance":12.5,"operation":"add","notes":"充值"}`).Code)
	require.Equal(t, http.StatusOK, do(http.MethodPost, fmt.Sprintf("/teams/%d/owner-balance", summary.ID), `{"balance":2,"operation":"subtract"}`).Code)
	require.Equal(t, []balanceCall{{owner.ID, 12.5, "add", "充值"}, {owner.ID, 2, "subtract", ""}}, rec.calls)
	// 非法操作 / 金额 / 不存在的团队不会触达余额服务。
	require.Equal(t, http.StatusBadRequest, do(http.MethodPost, fmt.Sprintf("/teams/%d/owner-balance", summary.ID), `{"balance":1,"operation":"multiply"}`).Code)
	require.Equal(t, http.StatusBadRequest, do(http.MethodPost, fmt.Sprintf("/teams/%d/owner-balance", summary.ID), `{"balance":0,"operation":"add"}`).Code)
	require.Equal(t, http.StatusNotFound, do(http.MethodPost, "/teams/424242/owner-balance", `{"balance":1,"operation":"add"}`).Code)
	require.Len(t, rec.calls, 2)

	// 站点管理员设置 / 取消团队管理员。
	adminPath := fmt.Sprintf("/teams/%d/members/%d/admin", summary.ID, member.ID)
	require.Equal(t, http.StatusOK, do(http.MethodPut, adminPath, `{"is_admin":true}`).Code)
	detail, err := teamSvc.GetAdminTeam(ctx, summary.ID)
	require.NoError(t, err)
	require.True(t, detail.Members[0].IsAdmin)
	require.Equal(t, http.StatusOK, do(http.MethodPut, adminPath, `{"is_admin":false}`).Code)
	require.Equal(t, http.StatusBadRequest, do(http.MethodPut, adminPath, `{}`).Code)
	require.Equal(t, http.StatusNotFound, do(http.MethodPut, "/teams/424242/members/1/admin", `{"is_admin":true}`).Code)

	// 站点管理员查看任意团队的成员花费统计。
	w := do(http.MethodGet, fmt.Sprintf("/teams/%d/usage/members?start_date=2026-10-01&end_date=2026-10-31", summary.ID), "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"members"`)
	require.Equal(t, http.StatusNotFound, do(http.MethodGet, "/teams/424242/usage/members", "").Code)
}
