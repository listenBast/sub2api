package admin

// 站点管理员的团队管理员 / 主账号余额 / 成员花费统计接口（fork-only，独立文件以降低与上游的合并冲突）。

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SetMemberAdmin 站点管理员设置 / 取消团队管理员。
// PUT /api/v1/admin/teams/:id/members/:member_id/admin
func (h *TeamHandler) SetMemberAdmin(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("member_id"), 10, 64)
	if err != nil || memberID <= 0 {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	var req struct {
		IsAdmin *bool `json:"is_admin" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.IsAdmin == nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	member, err := h.teamService.AdminSetMemberAdmin(c.Request.Context(), getAdminIDFromContext(c), teamID, memberID, *req.IsAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

// MemberSpendStats 站点管理员查看任意团队各成员在所选区间内的花费。
// GET /api/v1/admin/teams/:id/usage/members?start_date=&end_date=
func (h *TeamHandler) MemberSpendStats(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	start, end, err := parseAdminTeamStatsRange(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	stats, err := h.teamService.GetAdminMemberSpendStats(c.Request.Context(), teamID, start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

// AdjustOwnerBalanceRequest 与用户管理“调整余额”完全一致：set / add / subtract。
type AdjustOwnerBalanceRequest = UpdateBalanceRequest

// AdjustOwnerBalance 在团队管理里直接增减团队主账号余额，复用用户管理的余额调整服务
// （adminService.UpdateUserBalance：同样的校验、审计与兑换码调整记录、缓存失效）。
// POST /api/v1/admin/teams/:id/owner-balance
func (h *TeamHandler) AdjustOwnerBalance(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	var req AdjustOwnerBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ownerID, err := h.teamService.AdminOwnerID(c.Request.Context(), teamID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	idempotencyPayload := struct {
		TeamID  int64                `json:"team_id"`
		OwnerID int64                `json:"owner_id"`
		Body    UpdateBalanceRequest `json:"body"`
	}{TeamID: teamID, OwnerID: ownerID, Body: req}
	executeAdminIdempotentJSONWithResponse(c, "admin.teams.owner_balance.update", idempotencyPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		user, execErr := h.adminService.UpdateUserBalance(ctx, ownerID, req.Balance, req.Operation, req.Notes)
		if execErr != nil {
			return nil, execErr
		}
		return dto.UserFromServiceAdmin(user), nil
	}, func(data any) any {
		if adminaccess.FromContext(c.Request.Context()) != nil {
			return gin.H{"id": ownerID}
		}
		return data
	})
}

func parseAdminTeamStatsRange(c *gin.Context) (*time.Time, *time.Time, error) {
	start, end, err := parseAdminTeamTimeRange(c)
	if err != nil {
		return nil, nil, err
	}
	var startPtr, endPtr *time.Time
	if !start.IsZero() {
		startPtr = &start
	}
	if !end.IsZero() {
		endPtr = &end
	}
	return startPtr, endPtr, nil
}
