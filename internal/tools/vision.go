/*
图片识别工具（image_recognize）：设置·模型·图片识别槽启用后装配。
主模型无视觉时图片会被存到工作目录（stripimage 装饰器），本工具让
模型按路径识别任意图片（含用户落盘的、终端/脚本产物）。
依赖倒置：tools 只依赖 RecognizeIO 接口，service 层用图片识别槽的
模型实现（service 已 import tools，反向引用会循环）。
*/
package tools

import (
	"context"
	"encoding/json"

	"github.com/xuanlv2002/ezloop/types"
)

/* ImageRecognizeTool 是图片识别工具的注册名。 */
const ImageRecognizeTool = "image_recognize"

/* RecognizeIO 是图片识别能力面（service 层实现：图片识别槽模型调用）。 */
type RecognizeIO interface {
	RecognizeImage(ctx context.Context, path string) (string, error)
}

/* ImageRecognize 返回图片识别工具集。 */
func ImageRecognize(io RecognizeIO) []types.Tool {
	return []types.Tool{recognizeTool{io}}
}

type recognizeTool struct{ io RecognizeIO }

func (recognizeTool) Name() string { return ImageRecognizeTool }
func (recognizeTool) Description() string {
	return "识别一张图片文件并返回详细文字描述（由图片识别模型驱动）。适用于以文件形式存在的图片：" +
		"本机任意路径的图片、终端/脚本产物、历史落盘图片（正文引导里给出路径时）。" +
		"注意：用户直接发送且已在你上下文里的图片无需调用本工具。"
}

func (recognizeTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "图片文件的完整绝对路径（png/jpg/webp/gif）"}
		},
		"required": ["path"]
	}`)
}

func (t recognizeTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	return t.io.RecognizeImage(ctx, a.Path)
}
