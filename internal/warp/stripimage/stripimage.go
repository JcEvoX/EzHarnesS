/*
stripimage 是 provider 装饰器：主模型不支持视觉输入（ModelEntry.Vision
= false，配置驱动）时，把请求中的全部图片落盘到工作目录 images/，
正文替换为文件路径与引导说明。防"一次带图失败，图片留在历史里，
之后每轮纯文本也 400"的会话永久卡死——替换直接修改引擎消息（含落盘
历史），历史中的残留图片同样被清出。挂在 modelretry 内层，仅在主模型
未开视觉时装配。
*/
package stripimage

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
	"github.com/xuanlv2002/ezloop/warp"

	"ezharness/internal/osfs"
)

var seq atomic.Int64

/* Warp 返回图片落盘装饰器：workDir 是图片存放根目录（其下 images/），
hasRecognizer 决定占位文案是否引导使用 image_recognize 工具。 */
func Warp(fsys osfs.OS, workDir string, hasRecognizer bool) warp.ModelHandler {
	return func(_ event.Emitter, p provider.ModelProvider) provider.ModelProvider {
		return &stripProvider{inner: p, fsys: fsys, workDir: workDir, hasRecognizer: hasRecognizer}
	}
}

type stripProvider struct {
	inner        provider.ModelProvider
	fsys         osfs.OS
	workDir      string
	hasRecognizer bool
}

func (s *stripProvider) Invoke(ctx context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	s.saveImages(ctx, req)
	return s.inner.Invoke(ctx, req)
}

func (s *stripProvider) Stream(ctx context.Context, req *types.ModelRequest, onChunk provider.ModelChunkHandler) (*types.ModelResponse, error) {
	if sp, ok := s.inner.(provider.StreamProvider); ok {
		s.saveImages(ctx, req)
		return sp.Stream(ctx, req, onChunk)
	}
	return s.Invoke(ctx, req)
}

/* saveImages 把全部 user 消息的图片落盘并替换为路径占位；无图时原样
返回（落盘幂等：图片随消息替换被清出，不会重复写盘）。 */
func (s *stripProvider) saveImages(ctx context.Context, req *types.ModelRequest) {
	for i := range req.Messages {
		if len(req.Messages[i].Images) == 0 {
			continue
		}
		var paths []string
		for _, img := range req.Messages[i].Images {
			if p, err := s.saveOne(ctx, img); err == nil {
				paths = append(paths, p)
			}
		}
		note := "[图片附件已存至文件，当前模型不支持视觉输入：" + strings.Join(paths, "、") +
			"。可用文件工具查看；"
		if s.hasRecognizer {
			note += "如需识别内容请调用 image_recognize 工具（传图片路径）]\n"
		} else {
			note += "当前未启用图片识别模型，如需识别请在设置·模型启用]\n"
		}
		req.Messages[i].Content = note + req.Messages[i].Content
		req.Messages[i].Images = nil
	}
}

func (s *stripProvider) saveOne(ctx context.Context, img types.ImagePart) (string, error) {
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("img-%s-%d%s", time.Now().Format("20060102-150405"), seq.Add(1), extOf(img.MimeType))
	path := filepath.ToSlash(filepath.Join(s.workDir, "images", name))
	if err := s.fsys.Write(ctx, path, data); err != nil {
		return "", err
	}
	return path, nil
}

func extOf(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/jpeg", "":
		return ".jpg"
	}
	return ".bin"
}
