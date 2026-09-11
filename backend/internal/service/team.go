package service

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	TeamStatusActive    = "active"
	TeamStatusSuspended = "suspended"

	TeamRoleIndividual = "individual"
	TeamRoleOwner      = "owner"
	TeamRoleMember     = "member"

	TeamMembershipInvited     = "invited"
	TeamMembershipActive      = "active"
	TeamMembershipExitPending = "exit_pending"

	TeamActionCreated              = "team_created"
	TeamActionRenamed              = "team_renamed"
	TeamActionInvited              = "member_invited"
	TeamActionInviteAccepted       = "invite_accepted"
	TeamActionInviteRejected       = "invite_rejected"
	TeamActionInviteCancelled      = "invite_cancelled"
	TeamActionAdminInviteCancelled = "admin_invite_cancelled"
	TeamActionBalanceAdded         = "balance_allocated"
	TeamActionBalanceRecovered     = "balance_recovered"
	TeamActionExitRequested        = "exit_requested"
	TeamActionExitRejected         = "exit_rejected"
	TeamActionExitApproved         = "exit_approved"
	TeamActionMemberRemoved        = "member_removed"
	TeamActionAdminRemoved         = "admin_member_removed"
	TeamActionAdminMemberAdded     = "admin_member_added"
	TeamActionMemberLimitsUpdated  = "member_limits_updated"
	TeamActionStatusChanged        = "status_changed"
	TeamActionOwnerTransferred     = "owner_transferred"
	TeamActionAdminOwnerTransfer   = "admin_owner_transferred"

	MaxTeamMembers = 100
)

var (
	ErrTeamNotFound                  = infraerrors.NotFound("TEAM_NOT_FOUND", "团队不存在")
	ErrTeamAlreadyExists             = infraerrors.Conflict("TEAM_ALREADY_EXISTS", "该用户已经创建或加入了其他团队")
	ErrTeamNotOwner                  = infraerrors.Forbidden("TEAM_OWNER_REQUIRED", "仅团队主账号可以执行此操作")
	ErrTeamSuspended                 = infraerrors.Forbidden("TEAM_SUSPENDED", "团队已被管理员冻结，暂时无法管理")
	ErrTeamMemberNotFound            = infraerrors.NotFound("TEAM_MEMBER_NOT_FOUND", "团队成员不存在")
	ErrTeamUserNotFound              = infraerrors.NotFound("TEAM_USER_NOT_FOUND", "没有找到对应用户")
	ErrTeamMemberExists              = infraerrors.Conflict("TEAM_MEMBER_EXISTS", "该用户已经创建、加入或收到其他团队的邀请")
	ErrTeamMemberLimit               = infraerrors.Conflict("TEAM_MEMBER_LIMIT", "团队成员数量已达到上限")
	ErrTeamInviteNotFound            = infraerrors.NotFound("TEAM_INVITE_NOT_FOUND", "没有找到待处理的团队邀请")
	ErrTeamPendingPayments           = infraerrors.Conflict("TEAM_PENDING_PAYMENTS", "加入团队前请先完成或取消待处理的支付、退款操作")
	ErrTeamInsufficientBalance       = infraerrors.Conflict("TEAM_INSUFFICIENT_BALANCE", "团队主账号余额不足")
	ErrTeamMemberInsufficientBalance = infraerrors.Conflict("TEAM_MEMBER_INSUFFICIENT_BALANCE", "团队成员的团队额度不足，无法收回该额度")
	ErrTeamExitAlreadyPending        = infraerrors.Conflict("TEAM_EXIT_ALREADY_PENDING", "退出团队申请已经提交，请等待主账号处理")
	ErrTeamOwnerTransferTarget       = infraerrors.BadRequest("TEAM_OWNER_TRANSFER_TARGET_INVALID", "只能把主账号转移给已加入团队的成员")
)

type TeamOwnerView struct {
	UserID   int64   `json:"user_id"`
	Email    string  `json:"email"`
	Username string  `json:"username"`
	// Balance 主账号余额，即团队资金池。
	Balance float64 `json:"balance"`
}

type TeamMemberView struct {
	MembershipID int64  `json:"membership_id"`
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	Remark       string `json:"remark"`
	Status       string `json:"status"`
	// Balance 成员个人余额（自己充值/兑换所得，不受团队管理）。
	Balance float64 `json:"balance"`
	// TeamBalance 主账号分配给该成员的团队额度。
	TeamBalance float64 `json:"team_balance"`
	// TotalBalance 成员当前可用总额 = 个人余额 + 团队额度。
	TotalBalance    float64    `json:"total_balance"`
	FrozenBalance   float64    `json:"frozen_balance"`
	Concurrency     int        `json:"concurrency"`
	RPMLimit        int        `json:"rpm_limit"`
	JoinedAt        *time.Time `json:"joined_at,omitempty"`
	ExitRequestedAt *time.Time `json:"exit_requested_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type TeamSummary struct {
	ID                 int64            `json:"id"`
	Name               string           `json:"name"`
	Status             string           `json:"status"`
	Owner              TeamOwnerView    `json:"owner"`
	Members            []TeamMemberView `json:"members,omitempty"`
	ActiveMemberCount  int              `json:"active_member_count"`
	PendingInviteCount int              `json:"pending_invite_count"`
	ExitPendingCount   int              `json:"exit_pending_count"`
	TotalBalance       float64          `json:"total_balance"`
	CreatedAt          time.Time        `json:"created_at"`
}

type TeamAdminOverview struct {
	TotalTeams     int     `json:"total_teams"`
	ActiveTeams    int     `json:"active_teams"`
	SuspendedTeams int     `json:"suspended_teams"`
	MemberCount    int     `json:"member_count"`
	PendingInvites int     `json:"pending_invites"`
	ExitPending    int     `json:"exit_pending"`
	TotalBalance   float64 `json:"total_balance"`
}

type TeamContext struct {
	Role             string `json:"role"`
	MembershipStatus string `json:"membership_status,omitempty"`
	// FinancialRestricted 历史字段：成员现在可以自行充值/兑换到个人余额，因此恒为 false。
	// 保留字段是为了兼容旧前端，避免旧版本隐藏兑换入口时读取到 undefined。
	FinancialRestricted bool            `json:"financial_restricted"`
	Team                *TeamSummary    `json:"team,omitempty"`
	CurrentMembership   *TeamMemberView `json:"current_membership,omitempty"`
}

type TeamTransactionView struct {
	ID                  int64     `json:"id"`
	TeamID              int64     `json:"team_id"`
	OperatorID          int64     `json:"operator_id"`
	MemberID            *int64    `json:"member_id,omitempty"`
	MemberEmail         string    `json:"member_email,omitempty"`
	MemberUsername      string    `json:"member_username,omitempty"`
	MemberRemark        string    `json:"member_remark,omitempty"`
	Action              string    `json:"action"`
	Amount              float64   `json:"amount"`
	OwnerBalanceBefore  float64   `json:"owner_balance_before"`
	OwnerBalanceAfter   float64   `json:"owner_balance_after"`
	MemberBalanceBefore *float64  `json:"member_balance_before,omitempty"`
	MemberBalanceAfter  *float64  `json:"member_balance_after,omitempty"`
	Note                string    `json:"note"`
	CreatedAt           time.Time `json:"created_at"`
}

type TeamUsageFilter struct {
	MemberID int64
	Model    string
	Start    *time.Time
	End      *time.Time
	Page     int
	PageSize int
}

type TeamUsageItem struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	MemberEmail  string    `json:"member_email"`
	RequestID    string    `json:"request_id"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CacheTokens  int       `json:"cache_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	ActualCost   float64   `json:"actual_cost"`
	DurationMS   *int      `json:"duration_ms,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type TeamTrendPoint struct {
	Date       string  `json:"date"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	ActualCost float64 `json:"actual_cost"`
}

type TeamMemberUsageStat struct {
	UserID     int64   `json:"user_id"`
	Email      string  `json:"email"`
	Username   string  `json:"username"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	ActualCost float64 `json:"actual_cost"`
}

// TeamMemberUsageSummary 团队用量按单个成员（或主账号）汇总的结果，
// 用量统计遵循与用量明细相同的筛选条件（模型、时间范围）。
type TeamMemberUsageSummary struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Remark   string `json:"remark,omitempty"`
	Role     string `json:"role"`
	// Balance 个人余额（主账号即团队资金池），TeamBalance 团队额度，TotalBalance 两者之和。
	Balance      float64 `json:"balance"`
	TeamBalance  float64 `json:"team_balance"`
	TotalBalance float64 `json:"total_balance"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	TotalCost    float64 `json:"total_cost"`
	ActualCost   float64 `json:"actual_cost"`
}

type TeamDashboard struct {
	TotalRequests int64                 `json:"total_requests"`
	TotalTokens   int64                 `json:"total_tokens"`
	TotalCost     float64               `json:"total_cost"`
	ActualCost    float64               `json:"actual_cost"`
	MemberCount   int                   `json:"member_count"`
	TeamBalance   float64               `json:"team_balance"`
	Trend         []TeamTrendPoint      `json:"trend"`
	Members       []TeamMemberUsageStat `json:"members"`
	Start         time.Time             `json:"start"`
	End           time.Time             `json:"end"`
}
