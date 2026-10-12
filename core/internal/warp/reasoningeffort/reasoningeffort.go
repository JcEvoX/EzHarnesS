/*
reasoningeffort 是 model warp：按全局设置把思考强度档位写进每次请求
（改请求副本，落盘历史无关）。档位空 = 不控制；none/low/high/max 由
各协议在请求侧自行映射（openai reasoning_effort / anthropic
thinking / responses output_config.effort）。闭包实时读设置快照，
输入框切换即改即生效，无需 Reassemble。
*/
package reasoningeffort

import (
	"context"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

/* Warp 包装模型节点：effort 实时返回当前思考档位（闭包读设置）。 */
func Warp(effort func() string) warp.ModelHandler {
	return func(_ event.Emitter, inner provider.ModelProvider) provider.ModelProvider {
		return &effortProvider{inner: inner, effort: effort}
	}
}

type effortProvider struct {
	inner  provider.ModelProvider
	effort func() string
}

var _ provider.ModelProvider = (*effortProvider)(nil)
var _ provider.StreamProvider = (*effortProvider)(nil)


func (p *effortProvider) with(req *types.ModelRequest) *types.ModelRequest {
	if e := p.effort(); e != "" {
		cp := *req
		cp.ReasoningEffort = e
		return &cp
	}
	return req
}

func (p *effortProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	return p.inner.Invoke(ctx, p.with(req))
}

func (p *effortProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	if sp, ok := p.inner.(provider.StreamProvider); ok {
		return sp.Stream(ctx, p.with(req), onChunk)
	}
	return p.Invoke(ctx, req)
}
