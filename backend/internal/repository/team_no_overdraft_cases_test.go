//go:build unit || integration

package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

// 不透支扣费规则的用例表（sub2api-custom: team billing）。
// 单元测试（sqlmock）用它校验 Go 侧对 SQL 结果的处理；集成测试在真实 Postgres 上逐条执行
// teamUsageBillingDeductSQL，校验 SQL 本身的拆分与写回结果。
//
// membership 为空表示没有团队关系；"active"/"exit_pending" 视为团队成员，其余状态（如 "invited"）不是。
type noOverdraftCase struct {
	name       string
	membership string
	mode       string
	personal   float64 // 扣费前个人余额
	team       float64 // 扣费前团队额度
	amount     float64

	wantPersonalPart float64
	wantTeamPart     float64
	wantUncovered    float64
	wantBalance      float64 // 扣费后个人余额
	wantTeamBalance  float64 // 扣费后团队额度
}

func (c noOverdraftCase) wantSufficient() bool { return c.wantUncovered <= 0 }

var noOverdraftCases = []noOverdraftCase{
	// team_first（含未设置）：先团队，再个人，都不够的部分不扣。
	{"team_first/enough_team", "active", service.BalanceModeTeamFirst, 5, 15, 10, 0, 10, 0, 5, 5},
	{"team_first/unset_mode_is_team_first", "active", "", 0, 15, 3, 0, 3, 0, 0, 12},
	{"team_first/spill_to_personal", "active", service.BalanceModeTeamFirst, 8, 4, 10, 6, 4, 0, 2, 0},
	{"team_first/both_short", "active", service.BalanceModeTeamFirst, 3, 4, 10, 3, 4, 3, 0, 0},
	{"team_first/both_empty", "active", service.BalanceModeTeamFirst, 0, 0, 1, 0, 0, 1, 0, 0},
	{"team_first/existing_negative_personal_team_enough", "active", service.BalanceModeTeamFirst, -1.73, 38.94, 1, 0, 1, 0, -1.73, 37.94},
	{"team_first/existing_negative_personal_team_short", "active", service.BalanceModeTeamFirst, -1.73, 0.5, 1, 0, 0.5, 0.5, -1.73, 0},
	{"team_first/exit_pending_is_member", "exit_pending", service.BalanceModeTeamFirst, 0, 15, 3, 0, 3, 0, 0, 12},
	{"team_first/precision_exact", "active", service.BalanceModeTeamFirst, 1.12345678, 3.87654321, 4.99999999, 1.12345678, 3.87654321, 0, 0, 0},
	{"team_first/precision_short", "active", service.BalanceModeTeamFirst, 1.12345678, 3.87654321, 10, 1.12345678, 3.87654321, 5.00000001, 0, 0},

	// personal_first：先个人，再团队。
	{"personal_first/enough_personal", "active", service.BalanceModePersonalFirst, 15, 5, 10, 10, 0, 0, 5, 5},
	{"personal_first/spill_to_team", "active", service.BalanceModePersonalFirst, 4, 8, 10, 4, 6, 0, 0, 2},
	{"personal_first/both_short", "active", service.BalanceModePersonalFirst, 4, 3, 10, 4, 3, 3, 0, 0},
	{"personal_first/existing_negative_personal", "active", service.BalanceModePersonalFirst, -2, 5, 3, 0, 3, 0, -2, 2},

	// team_only：只扣团队额度，不够也不动个人余额（尊重显式选择）。
	{"team_only/enough_team", "active", service.BalanceModeTeamOnly, 5, 15, 10, 0, 10, 0, 5, 15 - 10},
	{"team_only/team_short_no_spill", "active", service.BalanceModeTeamOnly, 50, 4, 10, 0, 4, 6, 50, 0},
	{"team_only/team_empty", "active", service.BalanceModeTeamOnly, 5, 0, 10, 0, 0, 10, 5, 0},
	{"team_only/existing_negative_both", "active", service.BalanceModeTeamOnly, -2, -1, 1, 0, 0, 1, -2, -1},

	// personal_only：只扣个人余额，不够也不动团队额度。
	{"personal_only/enough_personal", "active", service.BalanceModePersonalOnly, 15, 5, 10, 10, 0, 0, 5, 5},
	{"personal_only/personal_short_no_spill", "active", service.BalanceModePersonalOnly, 4, 50, 10, 4, 0, 6, 0, 50},
	{"personal_only/existing_negative_personal", "active", service.BalanceModePersonalOnly, -1, 50, 3, 0, 0, 3, -1, 50},

	// 非团队成员：任何模式都只扣个人余额。
	{"non_member/team_first_enough", "", service.BalanceModeTeamFirst, 15, 0, 10, 10, 0, 0, 5, 0},
	{"non_member/team_first_short", "", service.BalanceModeTeamFirst, 4, 0, 10, 4, 0, 6, 0, 0},
	{"non_member/personal_first_short", "", service.BalanceModePersonalFirst, 4, 0, 10, 4, 0, 6, 0, 0},
	{"non_member/team_only_uses_personal", "", service.BalanceModeTeamOnly, 4, 0, 10, 4, 0, 6, 0, 0},
	{"non_member/personal_only_enough", "", service.BalanceModePersonalOnly, 20, 0, 10, 10, 0, 0, 10, 0},
	{"non_member/existing_negative_personal", "", service.BalanceModePersonalFirst, -3, 0, 1, 0, 0, 1, -3, 0},
	{"non_member/invited_with_leftover_team_quota", "invited", service.BalanceModeTeamFirst, 2, 7, 5, 2, 0, 3, 0, 7},
}
