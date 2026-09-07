/*
uploadfile 是附件告知 hook：带附件的轮次在用户输入前插入一条
<upload_file> user 记录，列出附件的本地绝对路径。文件本体不进
上下文（base64 不入历史），模型按需用 read_file 读取；前端历史
重建按同一标签解析附件 chips。无附件轮次零开销（不插消息）。
*/
package hooks

import (
	"context"
	"slices"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/types"
)

/* UploadTag 是附件记录的包裹标签（前端历史重建按它识别）。 */
const UploadTag = "upload_file"

/* MetaUploadFiles 是 LoopState.Metadata 的附件路径键。 */
const MetaUploadFiles = "upload_files"

/* WithUploadFiles 把本轮附件路径放进 LoopState（Run 阶段单线程写，安全）。 */
func WithUploadFiles(paths []string) core.RunOption {
	return func(st *types.LoopState) {
		if len(paths) > 0 {
			st.Metadata[MetaUploadFiles] = paths
		}
	}
}

/* UploadFile 实现 <upload_file> 注入。 */
type UploadFile struct{}

/* NewUploadFile 创建附件告知 hook。 */
func NewUploadFile() *UploadFile { return &UploadFile{} }

func (h *UploadFile) Name() string { return "uploadfile" }

/* OnStart 在本轮输入前插入附件记录（startHooks 运行时末条必为本轮 input）。 */
func (h *UploadFile) OnStart(_ context.Context, state *types.LoopState) error {
	paths, _ := state.Metadata[MetaUploadFiles].([]string)
	if len(paths) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("<" + UploadTag + ">\n用户本轮上传了以下文件（已保存到本地，可用 read_file 读取）：\n")
	for _, p := range paths {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("</" + UploadTag + ">")
	msg := types.Message{Role: types.RoleUser, Content: b.String()}
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == types.RoleUser {
		state.Messages = slices.Insert(state.Messages, n-1, msg)
	} else {
		state.Messages = append(state.Messages, msg)
	}
	return nil
}
