package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// resolveOpenAIForwardModel 解析 OpenAI 兼容转发使用的模型。
// messagesDispatchMappedModel 是调用方已为 /v1/messages 解析的显式调度结果；
// 普通 OpenAI 请求必须传空，避免将分组配置作为通用模型兜底。
func resolveOpenAIForwardModel(account *Account, requestedModel, messagesDispatchMappedModel string) string {
	messagesDispatchMappedModel = strings.TrimSpace(messagesDispatchMappedModel)
	if account == nil {
		if messagesDispatchMappedModel != "" {
			return messagesDispatchMappedModel
		}
		return requestedModel
	}

	mappedModel, matched := account.ResolveMappedModel(requestedModel)
	if !matched && messagesDispatchMappedModel != "" {
		return messagesDispatchMappedModel
	}
	return mappedModel
}

// openAIOAuthForeignModelPrefixes 列出明确属于其他厂商家族的模型名前缀。
// Codex 上游不可能服务这些模型：转发阶段 normalizeOpenAIModelForUpstream
// 对未知模型原样透传，上游必然返回不可重试的 400。
//
// 采用保守黑名单而非 Codex 模型白名单：未知/自定义别名保持「允许」，
// 以兼容渠道级模型映射等「账号选定之后才改写模型名」的部署方式
// （调度过滤看到的是改写前的原始模型名）。前缀分类的先例见
// ResolveThinkingProtocol（thinking_protocol.go）。
var openAIOAuthForeignModelPrefixes = []string{
	"deepseek-",
	"glm-",
	"kimi-",
	"moonshot-",
	"qwen-",
	"qwen2-",
	"qwen3-",
	"qwen4-",
	"qwq-",
	"minimax-",
	"gemini-",
	"gemma-",
	"grok-",
	"doubao-",
	"hunyuan-",
	"llama-",
	"llama2-",
	"llama3-",
	"meta-llama",
	"mistral-",
	"mixtral-",
	"baichuan-",
	"ernie-",
	"step-",
	"seed-",
	"yi-",
}

// isOpenAIOAuthServableModel 判断「空 model_mapping 的 OpenAI OAuth 账号」能否
// 服务请求模型。空映射默认仍是「允许」，仅排除明确属于其他厂商家族的模型
// （deepseek-*/glm-*、以及 Kimi Code 官方 bare ID k3 / k3-256k 等）——这类
// 请求原样透传必然被 Codex 上游以不可重试的 400 拒绝，且不触发 failover，
// 应在调度阶段就跳过该账号，把请求让给显式声明支持该模型的账号（#3662）。
// bare k3 仅精确匹配（取 last segment 后），不使用宽泛 k3- 前缀，以免误伤
// 自定义别名；显式 model_mapping 命中路径不经过本函数，语义不变。
func isOpenAIOAuthServableModel(requestedModel string) bool {
	model := strings.ToLower(lastOpenAIModelSegment(requestedModel))
	if model == "" {
		return true // 空模型交由上层必填校验处理
	}
	// Kimi Code 官方 bare model ID：无厂商前缀，prefix 黑名单挡不住。
	if model == "k3" || model == "k3-256k" {
		return false
	}
	for _, prefix := range openAIOAuthForeignModelPrefixes {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}
	return true
}

// deepseekServableModels 列出 DeepSeek 平台账号在「未配置 model_mapping」时
// 可服务的官方模型名（精确匹配，小写比较）。deepseek-flash 与 deepseek-v4-pro
// 为官方现行名；deepseek-v4-flash / deepseek-v4-flash-vision-exp 为处于兼容
// 路由期的旧名（上游仍接受并按 flash 价计费）；deepseek-v4-pro-0813 为与
// 计费 pro 档口径一致的版本化名。
var deepseekServableModels = []string{
	"deepseek-flash",
	"deepseek-v4-pro",
	"deepseek-v4-flash",
	"deepseek-v4-flash-vision-exp",
	"deepseek-v4-pro-0813",
}

// isDeepseekServableModel 报告 DeepSeek 平台账号在「未配置 model_mapping」时
// 可服务的模型名。官方现行模型为 deepseek-flash 与 deepseek-v4-pro；旧名
// deepseek-v4-flash / deepseek-v4-flash-vision-exp 处于兼容路由期（上游仍接受并
// 按 flash 价计费），deepseek-v4-pro-0813 为版本化名。其余一律拒绝：既避免把
// 未知模型透传给上游（上游 404/400 会触发 per-model 冷却并掩盖真实配置错误），
// 也避免上游把不认识的模型名静默兜底成 deepseek-flash。
// Source: https://api-docs.deepseek.com/quick_start/pricing
//
//	https://api-docs.deepseek.com/guides/anthropic_api
func isDeepseekServableModel(requestedModel string) bool {
	// 官方 Claude Code 接入文档要求 ANTHROPIC_MODEL=deepseek-flash[1m]：
	// [1m] 是客户端上下文选择器，先按同一规则归一化再比对白名单。
	model := strings.ToLower(normalizeClaudeCodeLongContextModel(strings.TrimSpace(requestedModel)))
	if model == "" {
		return true // 空模型交由上层必填校验处理
	}
	for _, servable := range deepseekServableModels {
		if model == servable {
			return true
		}
	}
	return false
}

// resolveOpenAICompactForwardModel determines the compact-only upstream model
// for /responses/compact requests. It never affects normal /responses traffic.
// When no compact-specific mapping matches, the input model is returned as-is.
func resolveOpenAICompactForwardModel(account *Account, model string) string {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" || account == nil {
		return trimmedModel
	}

	mappedModel, matched := account.ResolveCompactMappedModel(trimmedModel)
	if !matched {
		return trimmedModel
	}
	if trimmedMapped := strings.TrimSpace(mappedModel); trimmedMapped != "" {
		return trimmedMapped
	}
	return trimmedModel
}

// openAIClientBillingModel 返回 OpenAI 兼容链路的"计费模型"：客户端请求的模型
// （账号级 model_mapping 之前），而不是账号映射后的实际转发模型。
//
// 背景：各转发入口把 billingModel 设为 resolveOpenAIForwardModel 的结果（账号映射
// 后的模型）并写入 OpenAIForwardResult.BillingModel。运营者在账号上配置映射
// （如 gpt-6.1-sol → gpt-6-luna）只是为了换上游账号/规避上游限制，对用户而言
// 扣费应当仍按其请求的模型计。这里在 RecordUsage 汇聚点统一还原：
//   - BillingModel 为空：沿用 result.Model（请求模型）；
//   - BillingModel 恰好是账号映射对请求模型的映射结果：还原为映射前的模型
//     （messages 链路映射前模型是经过兼容归一化的名字，同时尝试归一化形式）；
//   - 其它情况（图片/视频/媒体的专用计费模型、messages 分组调度映射等显式设定）
//     原样保留——它们不是账号映射。
//
// 渠道级映射/计费来源（BillingModelSource）仍在调用方之后叠加，行为不变。
// 返回的 fallbacks 是后续兜底候选（请求原名、归一化名），用于请求模型无价时回退。
func openAIClientBillingModel(account *Account, result *OpenAIForwardResult) (billingModel string, fallbacks []string) {
	if result == nil {
		return "", nil
	}
	requested := strings.TrimSpace(result.Model)
	explicit := strings.TrimSpace(result.BillingModel)
	if explicit == "" {
		return forwardResultBillingModel(result.Model, result.UpstreamModel), nil
	}
	if account == nil || requested == "" || strings.EqualFold(explicit, requested) {
		return explicit, nil
	}
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return explicit, nil
	}
	candidates := make([]string, 0, 2)
	if normalized := openAICompatNormalizedModelName(requested); normalized != "" && normalized != requested {
		candidates = append(candidates, normalized)
	}
	candidates = append(candidates, requested)
	for _, candidate := range candidates {
		mapped, matched := resolveRequestedModelInMapping(mapping, candidate)
		if !matched {
			if normalized := normalizeRequestedModelForLookup(account.Platform, candidate); normalized != candidate {
				mapped, matched = resolveRequestedModelInMapping(mapping, normalized)
			}
		}
		if matched && strings.EqualFold(strings.TrimSpace(mapped), explicit) {
			return candidate, candidates
		}
	}
	return explicit, nil
}

// openAICompatNormalizedModelName 返回 /v1/messages 兼容链路对请求模型名做的归一化结果。
func openAICompatNormalizedModelName(model string) string {
	req := &apicompat.AnthropicRequest{Model: model}
	applyOpenAICompatModelNormalization(req)
	return strings.TrimSpace(req.Model)
}
