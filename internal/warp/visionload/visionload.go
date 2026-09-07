/*
Package visionload 是图片动态加载装饰器：模型调用前扫描消息里的
"图片已加载"路径标记（read_file 读图成功的工具结果），主模型开视觉
时从磁盘读出图片注入请求（挂最后一条 user 消息的 Images），否则原样
放行（标记文本自解释，不注入）。

历史里永远只有路径标记——base64 不落盘（session.json 轻量），换非
多模态模型不注入即无缝继续；trim 折叠标记消息后图片自动退出上下文；
标记对应的文件被删时跳过该图（优雅降级，标记文本仍在）。

注入只发生在请求视图上：req.Messages 换新切片、目标消息用副本，
引擎 state.Messages（落盘历史）不受影响。
*/
package visionload

import (
	"context"
	"encoding/base64"
	"regexp"
	"slices"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"
)

/* markPrefix/markSuffix 是路径标记的文本契约（read_file 回调生成，
本包扫描消费；两侧同源对齐，见 MarkLoaded）。 */
const markPrefix = "[图片已加载到上下文: "
const markSuffix = "]"

var markRe = regexp.MustCompile(`\[图片已加载到上下文: (.+?)\]`)

/* MarkLoaded 生成"图片已加载"路径标记（filetools 图片回调与扫描正则的
单一来源：改格式只动这里）。 */
func MarkLoaded(path string) string {
	return markPrefix + path + markSuffix
}

/* Warp 包装模型节点：visionOn 实时判断主模型视觉能力（闭包读设置）。 */
func Warp(fsys fs.FileSystem, visionOn func() bool) warp.ModelHandler {
	return func(_ event.Emitter, inner provider.ModelProvider) provider.ModelProvider {
		return &loadProvider{inner: inner, fsys: fsys, visionOn: visionOn}
	}
}

type loadProvider struct {
	inner    provider.ModelProvider
	fsys     fs.FileSystem
	visionOn func() bool
}

var _ provider.ModelProvider = (*loadProvider)(nil)
var _ provider.StreamProvider = (*loadProvider)(nil)

func (p *loadProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	p.inject(req)
	return p.inner.Invoke(ctx, req)
}

func (p *loadProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	p.inject(req)
	if sp, ok := p.inner.(provider.StreamProvider); ok {
		return sp.Stream(ctx, req, onChunk)
	}
	return p.inner.Invoke(ctx, req)
}

/* inject 就地把标记图片挂进请求视图（无标记或无视觉能力时不动）。 */
func (p *loadProvider) inject(req *types.ModelRequest) {
	if !p.visionOn() || len(req.Messages) == 0 {
		return
	}
	paths := collectMarks(req.Messages)
	if len(paths) == 0 {
		return
	}
	imgs := make([]types.ImagePart, 0, len(paths))
	for _, path := range paths {
		data, err := p.fsys.Read(context.Background(), path)
		if err != nil {
			continue // 文件被删：跳过，标记文本仍在（模型可感知并告知）
		}
		mime := sniffMime(data)
		if mime == "" {
			continue
		}
		imgs = append(imgs, types.ImagePart{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)})
	}
	if len(imgs) == 0 {
		return
	}
	// 换新切片 + 目标消息副本：不污染引擎 state（落盘历史无 base64）
	msgs := slices.Clone(req.Messages)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser {
			continue
		}
		cp := msgs[i]
		cp.Images = append(slices.Clone(cp.Images), imgs...)
		msgs[i] = cp
		req.Messages = msgs
		return
	}
}

/* collectMarks 收集消息里全部路径标记（保序去重）。 */
func collectMarks(messages []types.Message) []string {
	var out []string
	for i := range messages {
		for _, m := range markRe.FindAllStringSubmatch(messages[i].Content, -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	return out
}

/* sniffMime 魔数推图片 MIME（与 ezloop filetools.imageMime 同集）。 */
func sniffMime(b []byte) string {
	switch {
	case len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) > 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}
