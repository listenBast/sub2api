package admin

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TeamHandler struct {
	teamService *service.TeamService
}

func NewTeamHandler(teamService *service.TeamService) *TeamHandler {
	return &TeamHandler{teamService: teamService}
}

func (h *TeamHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.teamService.ListAdminTeams(c.Request.Context(), page, pageSize, c.Query("status"), c.Query("search"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *TeamHandler) Overview(c *gin.Context) {
	item, err := h.teamService.GetAdminOverview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *TeamHandler) Create(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		OwnerEmail string `json:"owner_email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请输入团队名称和有效的主账号邮箱")
		return
	}
	item, err := h.teamService.AdminCreateTeam(c.Request.Context(), getAdminIDFromContext(c), req.OwnerEmail, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, item)
}

func (h *TeamHandler) Get(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	item, err := h.teamService.GetAdminTeam(c.Request.Context(), teamID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *TeamHandler) Delete(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	if err := h.teamService.AdminDeleteTeam(c.Request.Context(), getAdminIDFromContext(c), teamID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *TeamHandler) SetStatus(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	item, err := h.teamService.SetStatus(c.Request.Context(), getAdminIDFromContext(c), teamID, req.Status, req.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *TeamHandler) RemoveMember(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	memberID, err := strconv.ParseInt(c.Param("member_id"), 10, 64)
	if err != nil || memberID <= 0 {
		response.BadRequest(c, "成员 ID 无效")
		return
	}
	if err := h.teamService.AdminRemoveMember(c.Request.Context(), getAdminIDFromContext(c), teamID, memberID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"removed": true})
}

func (h *TeamHandler) AddMember(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	var req struct {
		Email  string `json:"email" binding:"required,email"`
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请输入有效的成员邮箱")
		return
	}
	member, err := h.teamService.AdminAddMember(c.Request.Context(), getAdminIDFromContext(c), teamID, req.Email, req.Remark)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, member)
}

func (h *TeamHandler) UpdateMemberRemark(c *gin.Context) {
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
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}
	member, err := h.teamService.AdminUpdateMemberRemark(c.Request.Context(), teamID, memberID, req.Remark)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, member)
}

func (h *TeamHandler) Usage(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	filter, err := parseAdminTeamUsageFilter(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, result, err := h.teamService.ListAdminUsage(c.Request.Context(), teamID, filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *TeamHandler) Dashboard(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	start, end, err := parseAdminTeamTimeRange(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	item, err := h.teamService.GetAdminDashboard(c.Request.Context(), teamID, start, end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *TeamHandler) Transactions(c *gin.Context) {
	teamID, ok := adminTeamID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.teamService.ListAdminTransactions(c.Request.Context(), teamID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func adminTeamID(c *gin.Context) (int64, bool) {
	teamID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || teamID <= 0 {
		response.BadRequest(c, "团队 ID 无效")
		return 0, false
	}
	return teamID, true
}

func parseAdminTeamUsageFilter(c *gin.Context) (service.TeamUsageFilter, error) {
	page, pageSize := response.ParsePagination(c)
	filter := service.TeamUsageFilter{Page: page, PageSize: pageSize, Model: c.Query("model")}
	if value := strings.TrimSpace(c.Query("member_id")); value != "" {
		memberID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || memberID <= 0 {
			return filter, fmt.Errorf("成员筛选参数无效")
		}
		filter.MemberID = memberID
	}
	start, end, err := parseAdminTeamTimeRange(c)
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

func parseAdminTeamTimeRange(c *gin.Context) (time.Time, time.Time, error) {
	start, err := parseAdminTeamDate(c.Query("start_date"), false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseAdminTeamDate(c.Query("end_date"), true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

func parseAdminTeamDate(value string, inclusiveEnd bool) (time.Time, error) {
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
