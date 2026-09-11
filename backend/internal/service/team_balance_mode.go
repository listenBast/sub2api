package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// API key balance modes used by team members. Personal balance is funded by
// the member's own recharge or redemption; team balance is allocated by the
// team owner and is kept in a separate pool.
const (
	BalanceModeTeamFirst     = "team_first"
	BalanceModePersonalFirst = "personal_first"
	BalanceModeTeamOnly      = "team_only"
	BalanceModePersonalOnly  = "personal_only"

	DefaultBalanceMode = BalanceModeTeamFirst
)

var ErrAPIKeyBalanceModeInvalid = infraerrors.BadRequest("API_KEY_BALANCE_MODE_INVALID", "余额扣费方式无效")

func IsValidBalanceMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case BalanceModeTeamFirst, BalanceModePersonalFirst, BalanceModeTeamOnly, BalanceModePersonalOnly:
		return true
	default:
		return false
	}
}

// NormalizeBalanceMode maps an empty or unknown value to the default mode.
// The billing repository additionally treats team-only modes as personal-only
// for users who are not active members of a team.
func NormalizeBalanceMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if IsValidBalanceMode(mode) {
		return mode
	}
	return DefaultBalanceMode
}
