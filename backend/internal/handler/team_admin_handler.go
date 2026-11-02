package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// SetMemberAdmin 主账号设置/取消团队管理员（仅主账号，管理员不能任命管理员）。
func (h *TeamHandler) SetMemberAdmin(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
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
	member, err := h.teamService.SetMemberAdmin(c.Request.Context(), userID, memberID, *req.IsAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

// MemberSpendStats 主账号/管理员按成员统计所选日期区间内的花费。
func (h *TeamHandler) MemberSpendStats(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	start, end, err := ParseTeamStatsRange(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	stats, err := h.teamService.GetMemberSpendStats(ctx, ownerID, start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

// ParseTeamStatsRange 解析成员花费统计的区间：start_date 起（含），end_date 止（YYYY-MM-DD 含当天，RFC3339 不含）。
// 两者都可省略，省略即不限制该侧。
func ParseTeamStatsRange(c *gin.Context) (*time.Time, *time.Time, error) {
	start, end, err := ParseTeamTimeRange(c)
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
