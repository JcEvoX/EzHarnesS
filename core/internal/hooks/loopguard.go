/*
loopguard 是循环护栏 hook：防止"20 轮能完成的任务被拖到 100+ 轮"的双轨收敛机制。
  1) 同签名重复：第 4 次在下一次模型调用前插 <loop_guard> 提醒（模型自省），
     第 6 次起 Skip 拦截该次调用——同一手段重复必然无效，强制换路或收尾；
  2) 轮数压力线：轮数到 soft（约 24 轮，maxIters/2 孰小）与 hard（maxIters 的 3/4，
     上限 48）各注入一次收敛指令——针对视觉任务"截图-修-截图-修"签名各不相同、
     重复检测抓不住、却始终不收敛的典型形态：先提醒"主体达标即收尾"，
     再强令"停止迭代、总结交付"。

插入时机必须是 OnModelStart（消息序列稳定：tool_result 已全部 append）：
OnToolStart 时 append 会把消息夹在 assistant 的 tool_use 与 tool_result
之间，anthropic 协议要求两者紧邻，插中间直接 400。Skip 拦截是 OnToolStart
短路语义（与 approve 拒绝同机制），结果由引擎照常写入。
*/
package hooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/xuanlv2002/ezloop/event"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* LoopGuardTag 是循环护栏提醒的包裹标签。 */
const LoopGuardTag = "loop_guard"

/* EventLoopGuard 是护栏触发事件（Data 为 LoopGuardData，前端时间线实时渲染）。 */
const EventLoopGuard = event.EventType("loop.guard")

/* LoopGuardData 是护栏触发载荷（Note 非空时前端直接展示整句）。 */
type LoopGuardData struct {
	Tool  string `json:"tool,omitempty"`
	Count int    `json:"count,omitempty"`
	Note  string `json:"note,omitempty"`
}

const (
	guardRepeat = 4 // 同签名提醒阈值
	guardBlock  = 6 // 同签名拦截阈值（含）之后每次都拦
)

/* LoopGuard 实现循环护栏。 */
type LoopGuard struct {
	mu       sync.Mutex
	count    map[*types.LoopState]map[string]int
	pend     map[*types.LoopState]LoopGuardData // 达阈值的待提醒项（OnModelStart 消费）
	stage    map[*types.LoopState]int           // 轮数压力线进度（0 未触发 1 soft 2 hard）
	maxIters int
}

/* NewLoopGuard 创建护栏 hook（maxIterations 为本轮轮数上限，压力线按它推算）。 */
func NewLoopGuard(maxIterations int) *LoopGuard {
	if maxIterations <= 0 {
		maxIterations = 64
	}
	return &LoopGuard{
		count:    map[*types.LoopState]map[string]int{},
		pend:     map[*types.LoopState]LoopGuardData{},
		stage:    map[*types.LoopState]int{},
		maxIters: maxIterations,
	}
}

func (h *LoopGuard) Name() string { return "loopguard" }

/* OnToolStart 计数、标记待提醒；达到拦截阈值直接 Skip 该次调用。 */
func (h *LoopGuard) OnToolStart(_ context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	key := call.Name + "\x00" + string(call.Args)
	h.mu.Lock()
	if h.count[state] == nil {
		h.count[state] = map[string]int{}
	}
	h.count[state][key]++
	n := h.count[state][key]
	if n == guardRepeat {
		h.pend[state] = LoopGuardData{Tool: call.Name, Count: guardRepeat}
	}
	h.mu.Unlock()
	if n >= guardBlock {
		return ezhook.Skip(fmt.Sprintf(
			"（系统护栏）%s 已用相同参数调用 %d 次，本次调用被拦截。重复同一手段不会产生新结果——读上次返回确认状态，换手段，或 ask_user 请示，或就此总结收尾。",
			call.Name, n)), nil
	}
	return ezhook.Proceed, nil
}

/* OnModelStart 消费待提醒项并推进轮数压力线：消息序列稳定处插入、发事件。 */
func (h *LoopGuard) OnModelStart(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	d, ok := h.pend[state]
	delete(h.pend, state)
	stage := h.stage[state]
	h.mu.Unlock()

	if ok {
		msg := "<" + LoopGuardTag + ">\n（系统护栏）" + fmt.Sprintf("%s 已用相同参数调用 %d 次，多半此路不通。"+
			"停止重复：读返回结果确认状态，换手段，或 ask_user 向用户说明并请示（用户也可直接停止本轮）。\n</%s>",
			d.Tool, d.Count, LoopGuardTag)
		state.Messages = append(state.Messages, types.Message{Role: types.RoleUser, Content: msg})
		state.EmitEvent(EventLoopGuard, d)
	}

	soft, hard := h.pressureLines()
	it := state.Iteration
	if stage < 1 && it >= soft {
		h.markStage(state, 1)
		note := fmt.Sprintf("已进行 %d 轮。自检：主体目标是否已达成？视觉与细节的小瑕疵不阻塞交付——停止打磨，基于现有成果收尾；确有本质缺口就 ask_user 说明。", it)
		state.Messages = append(state.Messages, guardMsg(note))
		state.EmitEvent(EventLoopGuard, LoopGuardData{Note: note})
	} else if stage < 2 && it >= hard {
		h.markStage(state, 2)
		note := fmt.Sprintf("已进行 %d 轮，接近本轮轮数上限（%d）。立即停止迭代修复：不要再发起新的修改，总结现状、交付成果入口，剩余问题如实说明后结束本轮。", it, h.maxIters)
		state.Messages = append(state.Messages, guardMsg(note))
		state.EmitEvent(EventLoopGuard, LoopGuardData{Note: note})
	}
	return nil
}

func guardMsg(note string) types.Message {
	return types.Message{Role: types.RoleUser, Content: "<" + LoopGuardTag + ">\n（系统护栏）" + note + "\n</" + LoopGuardTag + ">"}
}

func (h *LoopGuard) markStage(state *types.LoopState, s int) {
	h.mu.Lock()
	h.stage[state] = s
	h.mu.Unlock()
}

/* pressureLines 轮数压力线：soft=min(24, max/2)、hard=min(48, max*3/4)，且 soft<hard。 */
func (h *LoopGuard) pressureLines() (int, int) {
	soft := h.maxIters / 2
	if soft > 24 {
		soft = 24
	}
	hard := h.maxIters * 3 / 4
	if hard > 48 {
		hard = 48
	}
	if soft >= hard {
		soft = hard - 1
	}
	return soft, hard
}

/* OnEnd 清计数（Run 结束即释放）。 */
func (h *LoopGuard) OnEnd(_ context.Context, state *types.LoopState) error {
	h.mu.Lock()
	delete(h.count, state)
	delete(h.pend, state)
	delete(h.stage, state)
	h.mu.Unlock()
	return nil
}
