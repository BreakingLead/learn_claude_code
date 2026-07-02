package agent

// 模块说明：
// 这个文件实现模型调用的恢复状态机。恢复层只处理“如何重新调用模型”，
// 不改变 agent loop 的业务语义：消息、工具和 hooks 仍由主循环管理。
//
// 恢复策略：
//   - max_tokens：第一次 stop_reason=max_tokens 时提升 token 上限并原样续跑。
//   - context overflow：400 错误时触发 reactive compact，然后重试。
//   - overload/rate limit：按指数退避重试，避免立即打爆上游。
//   - persistent failure：超过恢复预算后把错误返回给主循环。
//
// 状态边界：
// recoveryState 挂在 agentRuntime 上，配置来自 agentConfig，不使用包级变量。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type recoveryKind string

const (
	recoveryNone            recoveryKind = "none"
	recoveryContextOverflow recoveryKind = "context_overflow"
	recoveryRateLimited     recoveryKind = "rate_limited"
	recoveryOverloaded      recoveryKind = "overloaded"
	recoveryTransient       recoveryKind = "transient"
)

type recoveryState struct {
	model                 string
	maxTokens             int64
	escalatedMaxTokens    bool
	retries               int
	maxTokenContinuations int
}

// newRecoveryState 根据配置初始化恢复状态机。
func newRecoveryState(config agentConfig) recoveryState {
	return recoveryState{
		model:     config.Model,
		maxTokens: config.DefaultTokens,
	}
}

// continueAfterMaxTokens 插入内部续写请求，让主循环在 max_tokens 后继续生成一次。
func (rt *agentRuntime) continueAfterMaxTokens(messages *[]anthropic.MessageParam) bool {
	if rt.recovery.maxTokenContinuations >= rt.config.MaxTokenContinuations {
		return false
	}
	rt.recovery.maxTokenContinuations++
	*messages = append(*messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock("<system-reminder>Continue from the previous response. Do not repeat completed content.</system-reminder>"),
	))
	rt.emitLine("[recovery] continuing after max_tokens stop (%d/%d)", rt.recovery.maxTokenContinuations, rt.config.MaxTokenContinuations)
	return true
}

// callModelWithRecovery 包装 Anthropic 调用，并对可恢复错误执行压缩或重试。
func (rt *agentRuntime) callModelWithRecovery(ctx context.Context, client anthropic.Client, params anthropic.MessageNewParams, messages *[]anthropic.MessageParam) (*anthropic.Message, error) {
	params.Model = anthropic.Model(rt.recovery.model)
	params.MaxTokens = rt.recovery.maxTokens

	resp, err := client.Messages.New(ctx, params)
	if err == nil {
		rt.recovery.retries = 0
		if resp.StopReason == anthropic.StopReasonMaxTokens && !rt.recovery.escalatedMaxTokens {
			rt.recovery.escalatedMaxTokens = true
			rt.recovery.maxTokens = rt.config.EscalatedTokens
			rt.emitLine("[recovery] max_tokens reached, escalating output budget to %d", rt.recovery.maxTokens)
		}
		return resp, nil
	}

	kind := classifyRecovery(err)
	if kind == recoveryContextOverflow && messages != nil {
		rt.emitLine("%s", colorYellow("[recovery] context overflow, applying reactive compact"))
		*messages = rt.reactiveCompact(*messages)
		params.Messages = *messages
		return rt.retryModelCall(ctx, client, params)
	}
	if kind == recoveryRateLimited || kind == recoveryOverloaded || kind == recoveryTransient {
		return rt.retryModelCall(ctx, client, params)
	}
	return nil, err
}

// retryModelCall 用指数退避重试模型调用，直到成功或达到恢复预算。
func (rt *agentRuntime) retryModelCall(ctx context.Context, client anthropic.Client, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	var lastErr error
	for rt.recovery.retries < rt.config.MaxRecoveryRetries {
		rt.recovery.retries++
		delay := recoveryDelay(rt.recovery.retries, rt.config.RetryBaseDelay, rt.config.RetryMaxDelay)
		rt.emitLine("[recovery] retry %d/%d after %s", rt.recovery.retries, rt.config.MaxRecoveryRetries, delay)
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
		params.Model = anthropic.Model(rt.recovery.model)
		params.MaxTokens = rt.recovery.maxTokens
		resp, err := client.Messages.New(ctx, params)
		if err == nil {
			rt.recovery.retries = 0
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("recovery retry budget exhausted")
	}
	return nil, lastErr
}

// classifyRecovery 将 SDK/HTTP 错误归类为可恢复策略。
func classifyRecovery(err error) recoveryKind {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusBadRequest:
			return recoveryContextOverflow
		case http.StatusTooManyRequests:
			return recoveryRateLimited
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return recoveryTransient
		case 529:
			return recoveryOverloaded
		default:
			return recoveryNone
		}
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "overload") {
		return recoveryOverloaded
	}
	if strings.Contains(text, "rate limit") {
		return recoveryRateLimited
	}
	return recoveryNone
}

// recoveryDelay 计算有上限的指数退避时长。
func recoveryDelay(attempt int, base time.Duration, max time.Duration) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if max > 0 && delay > max {
			return max
		}
	}
	if max > 0 && delay > max {
		return max
	}
	return delay
}

// sleepContext 等待指定时间，并允许上层 context 取消等待。
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
