/*
endnote 是轮终止记录 hook：非正常终止（手动取消/出错/达到迭代上限/
策略中止）时在消息历史尾部补一条 <end_reason> user 记录，让下轮模型
知道上一轮为何中断。必须排在 sessionstore 之前注册——落盘快照与内存
历史同源，重启恢复后结束原因仍在上下文里。fork 子循环不记。
*/
package hooks

import (
	"context"
	"fmt"
	"strconv"
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
	reason := FriendlyStop(string(state.StopReason))
	if reason == "" {
		return nil // 正常结束不记录
	}
	dur := time.Since(state.StartedAt) // EndedAt 在 endHooks 全部跑完后才设置
	state.AppendMessage(types.Message{Role: types.RoleUser,
		Content: "<" + EndReasonTag + ">\n运行 " + strconv.Itoa(state.Iteration) + " 轮、" +
			humanDur(dur) + "后，" + reason + "\n</" + EndReasonTag + ">"})
	return nil
}

/* humanDur 把轮耗时渲染为中文短语（"45 秒"、"3 分 12 秒"）。 */
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
