package middleware

// 团队计费的鉴权层判定（sub2api 二开，fork-only）。
// api_key_auth.go / api_key_auth_google.go 只保留带 `// sub2api-custom: team billing`
// 标记的调用。合并上游后请运行 service/team_billing.go 顶部列出的守护测试。

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// apiKeyBalanceBelowAuthThresholdForMode keeps the lightweight authentication
// gate compatible with team members whose personal wallet is empty but whose
// API key can spend an allocated team quota. The handler-level billing check
// performs the precise (fresh) balance and reserve validation afterward.
func apiKeyBalanceBelowAuthThresholdForMode(user *service.User, apiKey *service.APIKey, cfg *config.Config) bool {
	if user == nil {
		return true
	}
	if !apiKeyBalanceBelowAuthThreshold(user.Balance, cfg) {
		return false
	}
	mode := service.DefaultBalanceMode
	if apiKey != nil {
		mode = service.NormalizeBalanceMode(apiKey.BalanceMode)
	}
	if mode == service.BalanceModePersonalOnly {
		return true
	}
	return user.TeamBalance <= 0
}
