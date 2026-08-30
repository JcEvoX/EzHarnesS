/*
trim 是上下文整理 hook：水位超阈值（OnLoop，每次迭代回边）或模型
主动调 trim_context 工具（OnToolStart）时就地折叠上下文——早期消息
总结为摘要，以 <context_trim> marker 消息衔接，立即生效（下一次模型
调用即新上下文）。会话身份不变：不换库、不切 trace、不动话题线。

存储为追加式档案：折叠段经 onFold 交给 domain 层（内存拼接 + 落盘
session.json 全量保留），渲染时间线可见整理位置，发给模型的上下文
只取最后一个 marker（含）之后的消息（Session.modelView 过滤）。
fork 同样支持（折叠 SeedLen 之后的增量，SeedLen 重置对齐剥离逻辑）。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xuanlv2002/ezloop/event"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* TrimTool 是模型主动整理上下文的注册工具名。 */
const TrimTool = "trim_context"

/* TrimTag 是整理 marker 的包裹标签（渲染/过滤据此识别）。 */
const TrimTag = "context_trim"

/* EventTrim 是上下文整理完成事件（Data 为 TrimInfo）。 */
const EventTrim = event.EventType("session.trim")

/* EventTrimming 是整理开始事件（摘要最多 2 分钟，静默会被当成卡死）。 */
const EventTrimming = event.EventType("session.trimming")

/* TrimInfo 描述一次上下文整理。 */
type TrimInfo struct {
	Tokens int  `json:"tokens"` // 触发时的水位（自动路径；工具路径为 0）
	Folded int  `json:"folded"` // 折叠的早期消息条数
	Kept   int  `json:"kept"`   // 保留的近期消息条数（不含 marker）
	Fork   bool `json:"fork"`
}

const trimSummaryPrompt = "整理当前对话的早期上下文以释放窗口空间：保留延续" +
	"当前任务所需的关键事实、已达成的决定、未完成的待办与近期文件操作；" +
	"忽略过程细节与状态栏记录；简洁自包含，200 字以内。"

/* trimKeepTail 是折叠后保留的近期消息条数（维持任务衔接的最小视野）。 */
const trimKeepTail = 4

/* trimFoldedKey 是 state.Metadata 里折叠段的键（sessionstore 与 domain 共读）。 */
const trimFoldedKey = "trim_folded"

/* FoldedOf 取 state 上累积的折叠段（无则空）。 */
func FoldedOf(state *types.LoopState) []types.Message {
	if state == nil {
		return nil
	}
	if m, ok := state.Metadata[trimFoldedKey].([]types.Message); ok {
		return m
	}
	return nil
}

/*
MergeFull 合成全量历史：已知全量（上一轮末）中最后一个 marker 之前的
部分 + 本轮折叠段 + 当前消息。marker 起的旧视图已被本轮重新折叠进
折叠段，直接拼接不重复；marker 之前的档案从未进过本轮视图，必须保留
——否则跨轮覆盖丢档。落盘（store.OnEnd，last=盘上快照）与内存
（FinishRun，last=history）共用此规则。
*/
func MergeFull(last []types.Message, state *types.LoopState) []types.Message {
	cut := len(last)
	for i := len(last) - 1; i >= 0; i-- {
		if IsTrimMarker(last[i]) {
			cut = i
			break
		}
	}
	folded, msgs := FoldedOf(state), []types.Message{}
	if state != nil {
		msgs = state.Messages
	}
	out := make([]types.Message, 0, cut+len(folded)+len(msgs))
	out = append(out, last[:cut]...)
	out = append(out, folded...)
	out = append(out, msgs...)
	return out
}

/* Trim 提供水位自动与模型主动两条整理路径（就地、立即生效）。 */
type Trim struct {
	provider  provider.ModelProvider
	trace     *Trace
	threshold int // 水位阈值（prompt tokens），<=0 禁用自动整理
	window    int // 模型窗口（提示展示水位比例用）

	mu sync.Mutex // OnToolStart 同轮并发契约：整理临界区互斥
}

/* NewTrim 创建整理 hook。 */
func NewTrim(p provider.ModelProvider, trace *Trace, threshold, window int) *Trim {
	return &Trim{provider: p, trace: trace, threshold: threshold, window: window}
}

func (t *Trim) Name() string { return "trim" }

/* OnStart 注册 trim_context 工具并注入使用说明（sys 首位重写 base 后追加，幂等）。 */
func (t *Trim) OnStart(_ context.Context, state *types.LoopState) error {
	state.Tools.Register(trimTool{})
	if len(state.Messages) > 0 && state.Messages[0].Role == types.RoleSystem {
		state.Messages[0].Content += "\n\n<tool-guide>\ntrim_context：把早期对话就地折叠为摘要，" +
			"上下文立即变小（会话与历史档案不变）。感觉上下文过长影响专注或质量时主动调用，无需用户同意。\n</tool-guide>"
	}
	return nil
}

/*
OnLoop 水位自动整理：迭代回边（工具结果已全部入史、消息序列协议
完整）检查 PromptTokens，超阈值就地折叠。Metadata 防同轮重复（整理
后 LastResponse 用量仍是旧值，须显式标记；下一轮 state 新建自动复位）。
*/
func (t *Trim) OnLoop(ctx context.Context, state *types.LoopState) error {
	if t.threshold <= 0 || state.LastResponse == nil || state.Metadata["trimmed"] == true {
		return nil
	}
	tokens := state.LastResponse.Usage.PromptTokens
	if tokens <= t.threshold {
		return nil
	}
	msg := fmt.Sprintf("上下文水位 %d tokens，达到阈值，正在整理…", tokens)
	if t.window > 0 {
		msg = fmt.Sprintf("上下文水位 %d / %d tokens（%d%%），达到阈值，正在整理…",
			tokens, t.window, tokens*100/t.window)
	}
	state.EmitEvent(EventTrimming, msg)
	if _, err := t.doTrim(ctx, state, true, tokens); err != nil {
		state.EmitEvent(event.EventError, "auto trim failed: "+err.Error())
	}
	return nil
}

/*
OnToolStart 拦截模型主动整理。折叠范围截至末条 assistant（它携带本轮
tool_calls、Skip 结果随后由引擎追加——保留它消息序列才协议完整）。
*/
func (t *Trim) OnToolStart(ctx context.Context, state *types.LoopState, call *types.ToolCall) (ezhook.Action, error) {
	if call.Name != TrimTool {
		return ezhook.Proceed, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if state.Metadata["trimmed"] == true {
		return ezhook.Skip("本轮已整理过上下文，无需重复调用，请继续当前任务。"), nil
	}
	info, err := t.doTrim(ctx, state, false, 0)
	if err != nil {
		return ezhook.Skip("trim failed: " + err.Error()), nil
	}
	return ezhook.Skip(fmt.Sprintf("早期上下文已折叠为摘要（%d 条消息整理，保留最近 %d 条），"+
		"请继续当前任务，不要重述已折叠的过程细节。", info.Folded, info.Kept)), nil
}

/*
doTrim 执行整理：摘要折叠段 → 构造 marker → 截断为 [system, marker, tail]。
失败不改变上下文（放弃本轮，模型继续原上下文作答）。折叠段在截断前
经 onFold 移交（内存渲染全量 + 落盘档案全量）。
*/
func (t *Trim) doTrim(ctx context.Context, state *types.LoopState, auto bool, tokens int) (TrimInfo, error) {
	msgs := state.Messages
	if len(msgs) == 0 {
		return TrimInfo{}, errors.New("nothing to trim")
	}
	fork := state.ForkID != ""
	start := 1 // 主循环跳过 system
	if fork {
		if state.SeedLen <= 0 || state.SeedLen >= len(msgs) {
			return TrimInfo{}, errors.New("fork has no incremental context")
		}
		start = state.SeedLen // seed 归主库，只折叠 fork 增量
	}
	end := len(msgs)
	if !auto && msgs[end-1].Role == types.RoleAssistant {
		end-- // 工具路径：末条 assistant 留给 Skip 结果配对
	}
	if end-start <= 0 {
		return TrimInfo{}, errors.New("nothing to trim")
	}
	fold := msgs[start:end]

	var sp *Span
	if t.trace != nil {
		sp = t.trace.StartSpan(state, "trim", "trim", map[string]any{
			"fork": fork, "tokens": tokens, "fold": len(fold),
		})
	}
	summaryText, err := summarizeMsgs(ctx, t.provider, trimSummaryPrompt, fold)
	if err != nil {
		if t.trace != nil {
			t.trace.EndSpan(sp, map[string]any{"err": err.Error()})
		}
		return TrimInfo{}, err
	}
	if t.trace != nil {
		t.trace.EndSpan(sp, map[string]any{"summary": truncStr(summaryText, 2048)})
	}

	marker := types.Message{Role: types.RoleUser, Content: "<" + TrimTag + ">" +
		"\n（系统自动整理，非用户发言，无需回应）" +
		"\n此标记之前的对话已折叠出模型上下文（原始记录仍完整保留在会话档案中），摘要：\n" +
		summaryText + "\n</" + TrimTag + ">"}
	keptFrom := tailStart(fold, trimKeepTail)
	tail := fold[keptFrom:]

	if keptFrom > 0 {
		// 折叠段挂 state：sessionstore 落盘（全量档案）与 domain FinishRun
		// （渲染全量历史）都从此取——单一来源，与回调顺序无关
		if state.Metadata == nil {
			state.Metadata = map[string]any{}
		}
		state.Metadata[trimFoldedKey] = append(FoldedOf(state), fold[:keptFrom]...)
	}
	// 头部不动（主循环 system / fork 完整 seed），marker 接后，tail 承前启后
	next := make([]types.Message, 0, start+1+len(tail)+len(msgs[end:]))
	next = append(next, msgs[:start]...)
	next = append(next, marker)
	next = append(next, tail...)
	next = append(next, msgs[end:]...) // 工具路径的末条 assistant（如有）
	state.Messages = next
	if state.Metadata == nil {
		state.Metadata = map[string]any{}
	}
	state.Metadata["trimmed"] = true

	info := TrimInfo{Tokens: tokens, Folded: keptFrom, Kept: len(tail), Fork: fork}
	state.EmitEvent(EventTrim, info)
	return info, nil
}

/*
tailStart 选折叠段的保留起点：末 K 条作衔接视野，窗口内出现孤儿
tool（配对 assistant 被截在窗口外）则起点前移纳入其配对——保留段
恒为连续无孤儿后缀，消息序列协议完整（OnLoop 时点结果已全入史，
配对静态可判定）。段不长于 K 时全折叠（起点=段长）。
*/
func tailStart(fold []types.Message, k int) int {
	if len(fold) <= k {
		return len(fold) // 段太短：全折叠，避免"保留比折叠多"的无意义整理
	}
	s := len(fold) - k
	for s > 0 && hasOrphanTool(fold[s:]) {
		s--
	}
	return s
}

/* hasOrphanTool 报告窗口内是否有 tool 消息的配对 assistant 不在窗口内。 */
func hasOrphanTool(win []types.Message) bool {
	ids := map[string]bool{}
	for _, m := range win {
		if m.Role == types.RoleAssistant {
			for _, c := range m.ToolCalls {
				ids[c.ID] = true
			}
		}
	}
	for _, m := range win {
		if m.Role == types.RoleTool && !ids[m.ToolCallID] {
			return true
		}
	}
	return false
}

/* trimTool 是 trim_context 壳工具（拦截式，Invoke 不可达）。 */
type trimTool struct{}

func (trimTool) Name() string { return TrimTool }
func (trimTool) Description() string {
	return "整理上下文：把早期对话就地折叠为摘要，上下文立即变小，会话与历史档案不变。" +
		"当感觉上下文过长影响专注、或状态栏提示水位过高时调用。"
}
func (trimTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}
func (trimTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("trim: hook not registered")
}

/* IsTrimMarker 报告消息是否为整理 marker（域层过滤/前端识别共用）。 */
func IsTrimMarker(m types.Message) bool {
	return strings.HasPrefix(m.Content, "<"+TrimTag+">")
}
