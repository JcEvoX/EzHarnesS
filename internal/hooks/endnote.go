/*
endnote 是轮终止记录 hook：每轮结束（含正常结束）在消息历史尾部补一条
<end_reason> user 记录，含轮次/时长/结束时间/结束原因分类；本轮发生过
话题压缩时附说明（压缩翻页后本记录落在新会话开头，需自解释，否则模型
莫名收到一条来历不明的消息）。必须排在 sessionstore 之前注册——落盘
快照与内存历史同源，重启恢复后结束原因仍在上下文里。fork 子循环不记。
*/
package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"
)

/* EndReasonTag 是终止记录的包裹标签。 */
const EndReasonTag = "end_reason"

type endNote struct{}

/* NewEndNote 创建终止记录 hook（EndHook，注册于 sessionstore 之前）。 */
func NewEndNote() *endNote { return &endNote{} }

func (h *endNote) Name() string { return "endnote" }

func (h *endNote) OnEnd(_ context.Context, state *types.LoopState) error {
	if state.ForkID != "" {
		return nil // fork 历史只存增量，终止语义归主循环
	}
	var b strings.Builder
	b.WriteString("<" + EndReasonTag + ">")
	b.WriteString("\n（系统自动记录的轮次收尾信息，非用户发言，无需回应）")
	fmt.Fprintf(&b, "\n运行轮次：%d", state.Iteration)
	fmt.Fprintf(&b, "\n运行时长：%s", humanDur(time.Since(state.StartedAt))) // EndedAt 在 endHooks 全部跑完后才设置
	fmt.Fprintf(&b, "\n结束时间：%s", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "\n结束原因：%s", FriendlyStop(string(state.StopReason)))
	if state.Metadata["compacted"] == true {
		b.WriteString("\n话题压缩：本轮结束时上下文水位达到阈值，上一会话已压缩归档并开启新会话，" +
			"本条记录随翻页落在新会话开头，仅用于说明上一会话的收尾情况。")
	}
	b.WriteString("\n</" + EndReasonTag + ">")
	state.AppendMessage(types.Message{Role: types.RoleUser, Content: b.String()})
	return nil
}

/* humanDur 把轮耗时渲染成中文短语（"45 秒"、"3 分 12 秒"）。 */
func humanDur(d time.Duration) string {
	switch {
	case d <= 0:
		return "不足 1 秒"
	case d < time.Minute:
		return fmt.Sprintf("%.0f 秒", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%d 小时 %d 分钟", int(d.Hours()), int(d.Minutes())%60)
	}
}
