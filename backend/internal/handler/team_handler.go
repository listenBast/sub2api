package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TeamHandler struct {
	teamService *service.TeamService
}

func NewTeamHandler(teamService *service.TeamService) *TeamHandler {
	return &TeamHandler{teamService: teamService}
}

func (h *TeamHandler) GetContext(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	result, err := h.teamService.GetContext(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *TeamHandler) Upgrade(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	result, err := h.teamService.Upgrade(c.Request.Context(), userID, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, result)
}

func (h *TeamHandler) Rename(c *gin.Context) {
	userID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	result, err := h.teamService.Rename(ctx, userID, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *TeamHandler) Invite(c *gin.Context) {
	userID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请输入有效的成员邮箱")
		return
	}
	member, err := h.teamService.Invite(ctx, userID, req.Email)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, member)
}

func (h *TeamHandler) RespondInvitation(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	var req struct {
		Accept bool `json:"accept"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	result, err := h.teamService.RespondInvitation(c.Request.Context(), userID, req.Accept)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *TeamHandler) AllocateBalance(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	var req struct {
		Amount float64 `json:"amount" binding:"required,ne=0"`
		Note   string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请输入非零的额度调整值")
		return
	}
	member, err := h.teamService.AllocateBalance(ctx, ownerID, memberID, req.Amount, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

func (h *TeamHandler) RequestExit(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	result, err := h.teamService.RequestExit(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *TeamHandler) ReviewExit(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	if err := h.teamService.ReviewExit(ctx, ownerID, memberID, req.Approve); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"approved": req.Approve})
}

func (h *TeamHandler) RemoveMember(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	if err := h.teamService.RemoveMember(ctx, ownerID, memberID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"removed": true})
}

func (h *TeamHandler) UpdateMemberRemark(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || memberID <= 0 {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	member, err := h.teamService.UpdateMemberRemark(ctx, ownerID, memberID, req.Remark)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

func (h *TeamHandler) UpdateMemberLimits(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	var req struct {
		Concurrency *int `json:"concurrency" binding:"omitempty,min=1,max=1000"`
		RPMLimit    *int `json:"rpm_limit" binding:"omitempty,min=0,max=1000000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	member, err := h.teamService.UpdateMemberLimits(ctx, ownerID, memberID, req.Concurrency, req.RPMLimit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

// Dissolve 解散团队，仅主账号可用；团队管理员会收到 403 TEAM_OWNER_REQUIRED。
func (h *TeamHandler) Dissolve(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	owner, err := h.teamService.ResolveOwner(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.teamService.Dissolve(service.WithTeamOperator(c.Request.Context(), userID), owner.OwnerID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"dissolved": true})
}

// ListUsage 主账号 / 管理员看团队全部用量；普通成员只看自己的用量（与资金流水的口径一致）。
func (h *TeamHandler) ListUsage(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	filter, err := ParseTeamUsageFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	manager, err := h.teamService.ResolveManager(c.Request.Context(), userID)
	if errors.Is(err, service.ErrTeamManagerRequired) {
		items, pageResult, ownErr := h.teamService.ListOwnUsage(c.Request.Context(), userID, filter)
		if ownErr != nil {
			response.ErrorFrom(c, ownErr)
			return
		}
		response.Paginated(c, items, pageResult.Total, pageResult.Page, pageResult.PageSize)
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ctx := service.WithTeamOperator(c.Request.Context(), userID)
	items, pageResult, err := h.teamService.ListUsage(ctx, manager.OwnerID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, pageResult.Total, pageResult.Page, pageResult.PageSize)
}

func (h *TeamHandler) Dashboard(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	start, end, err := ParseTeamTimeRange(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	dashboard, err := h.teamService.GetDashboard(ctx, ownerID, start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dashboard)
}

func (h *TeamHandler) ListTransactions(c *gin.Context) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	// 主账号 / 管理员看团队全部流水；普通成员只看与自己相关的流水（沿用原有行为）。
	if manager, err := h.teamService.ResolveManager(ctx, userID); err == nil {
		userID = manager.OwnerID
		ctx = service.WithTeamOperator(ctx, manager.UserID)
	} else if !errors.Is(err, service.ErrTeamManagerRequired) && !errors.Is(err, service.ErrTeamNotFound) {
		response.ErrorFrom(c, err)
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.teamService.ListTransactions(ctx, userID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

// UsageSummary 主账号按成员汇总团队用量（余额、请求数、Token 数、实际消费）。
func (h *TeamHandler) UsageSummary(c *gin.Context) {
	ownerID, ctx, ok := currentTeamManager(c, h.teamService)
	if !ok {
		return
	}
	filter, err := ParseTeamUsageFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	summary, err := h.teamService.GetUsageSummary(ctx, ownerID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

// TransferOwnership 主账号把主账号身份转移给指定成员，自己降为普通成员。
func (h *TeamHandler) TransferOwnership(c *gin.Context) {
	ownerID, ok := currentTeamUserID(c)
	if !ok {
		return
	}
	var req struct {
		MemberID int64 `json:"member_id" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请选择要转移的成员")
		return
	}
	result, err := h.teamService.TransferOwnership(c.Request.Context(), ownerID, req.MemberID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// currentTeamManager 解析当前请求用户是否为团队管理者（主账号或管理员），每次请求读取数据库，
// 角色变更立即生效。返回的 ownerID 交给既有的主账号服务方法，ctx 携带真实操作人用于流水审计。
func currentTeamManager(c *gin.Context, teamService *service.TeamService) (int64, context.Context, bool) {
	userID, ok := currentTeamUserID(c)
	if !ok {
		return 0, nil, false
	}
	manager, err := teamService.ResolveManager(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return 0, nil, false
	}
	return manager.OwnerID, service.WithTeamOperator(c.Request.Context(), userID), true
}

func currentTeamUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "登录状态无效")
		return 0, false
	}
	return subject.UserID, true
}

func ParseTeamUsageFilter(c *gin.Context) (service.TeamUsageFilter, error) {
	page, pageSize := response.ParsePagination(c)
	filter := service.TeamUsageFilter{Page: page, PageSize: pageSize, Model: c.Query("model")}
	if member := strings.TrimSpace(c.Query("member_id")); member != "" {
		id, err := strconv.ParseInt(member, 10, 64)
		if err != nil || id <= 0 {
			return filter, fmt.Errorf("成员筛选参数无效")
		}
		filter.MemberID = id
	}
	start, end, err := ParseTeamTimeRange(c)
	if err != nil {
		return filter, err
	}
	if !start.IsZero() {
		filter.Start = &start
	}
	if !end.IsZero() {
		filter.End = &end
	}
	return filter, nil
}

func ParseTeamTimeRange(c *gin.Context) (time.Time, time.Time, error) {
	start, err := parseTeamDate(c.Query("start_date"), false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseTeamDate(c.Query("end_date"), true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

func parseTeamDate(value string, inclusiveEnd bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期格式无效")
	}
	if inclusiveEnd {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return parsed, nil
}
