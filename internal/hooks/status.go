/*
status 是 agent 状态栏 hook：每轮 chat 在用户输入前插入一条
<agent_status> user 记录。systemPrompt 每 session 固定，轮内发生的
资源变更（页面新增 mcp、agent 自建 skill）不进 system——模型靠这条
记录获知当前水位与增删变更，直到下个 session 才并入 system。
同时发 status.snapshot 事件供前端渲染状态卡；变更对比靠 Store 里的
资源基线快照（持久化，重启可续），不依赖历史里的旧 status 记录。
*/
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/types"
)

/* StatusTag 是状态记录的包裹标签。 */
const StatusTag = "agent_status"

/* EventStatus 是状态快照事件（Data 为 StatusData）。 */
const EventStatus = event.EventType("status.snapshot")

/* StatusMcp 是状态栏里的单个 MCP 条目（描述前 8 字）。 */
type StatusMcp struct {
	Name string `json:"name"`
	Desc string `json:"desc,omitempty"`
}

/* StatusTerm 是状态栏的终端条目（多终端清单 diff 用）。 */
type StatusTerm struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Exited bool   `json:"exited"`
	Origin string `json:"origin"` // "用户" / "AI" / "AI·<会话名>"：新增时报来源，模型可区分手动/自建终端
}

/* UserAction 是用户在终端的一次手动输入（agent_status 注入，AI 感知
人为操作）。 */
type UserAction struct {
	ID   string `json:"id"`
	Line string `json:"line"`
}

/* TermReport 是 status 向服务层要的终端状态面。 */
type TermReport struct {
	Terms []StatusTerm
	Lines []UserAction
}

/* StatusData 是状态快照内容（SSE 事件给前端渲染；注入给模型的正文
由 render 转成中文语义化文本）。skill/mcp 全量清单在 system
（<skills>/<mcp> 块），这里只注入变更。 */
type StatusData struct {
	Now                string   `json:"now"`
	SinceLastOutputMin int64    `json:"sinceLastOutputMin"` // 0 = 无记录
	CtxTokens          int      `json:"ctxTokens"`
	CtxWindow          int      `json:"ctxWindow"`
	SuggestCompact     bool     `json:"suggestCompact"`
	Changes            []string `json:"changes,omitempty"`
}

/* Status 实现状态栏注入。 */
type Status struct {
	fsys      fs.FileSystem
	store     *Store
	ctxTokens func() int
	ctxWindow int
	mcpList   func() []StatusMcp
	termRep   func() TermReport // 可空：无共享终端服务时不注入
}

/*
NewStatus 创建状态栏 hook。ctxTokens 返回最近一次模型调用的 prompt
tokens；ctxWindow 是主模型上下文窗口（<=0 由调用方兜底默认）；
mcpList 返回启用的 server 清单（描述截断由调用方完成）；
termRep 返回共享终端清单与用户手动输入（可空）。
*/
func NewStatus(fsys fs.FileSystem, store *Store, ctxTokens func() int, ctxWindow int,
	mcpList func() []StatusMcp, termRep func() TermReport) *Status {
	return &Status{fsys: fsys, store: store, ctxTokens: ctxTokens, ctxWindow: ctxWindow, mcpList: mcpList, termRep: termRep}
}

func (h *Status) Name() string { return "status" }

/* OnStart 组装状态并在用户输入前插入（startHooks 运行时末条必为本轮 input）。 */
func (h *Status) OnStart(ctx context.Context, state *types.LoopState) error {
	data := h.build(ctx)
	content := "<" + StatusTag + ">\n" + renderStatus(data) + "\n</" + StatusTag + ">"

	msg := types.Message{Role: types.RoleUser, Content: content}
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == types.RoleUser {
		state.Messages = slices.Insert(state.Messages, n-1, msg)
	} else {
		state.Messages = append(state.Messages, msg)
	}
	state.EmitEvent(EventStatus, data)
	return nil
}

/*
renderStatus 把状态数据渲染成模型可读的中文文本（裸 JSON 的键名与
"+ mcp" 之类缩写对模型不友好）。前端历史重建按关键词识别异常行
（建议整理/资源变更），普通轮次不进时间线。
*/
func renderStatus(d StatusData) string {
	var b strings.Builder
	b.WriteString("当前时间：" + d.Now)
	if d.CtxWindow > 0 {
		fmt.Fprintf(&b, "\n上下文水位：%d / %d tokens", d.CtxTokens, d.CtxWindow)
		if d.SuggestCompact {
			b.WriteString("（已超窗口 70%，建议调用 trim_context 整理上下文）")
		}
	}
	if d.SinceLastOutputMin > 0 {
		fmt.Fprintf(&b, "\n距上次输出：%d 分钟", d.SinceLastOutputMin)
	}
	if len(d.Changes) > 0 {
		b.WriteString("\n本轮资源变更：" + strings.Join(d.Changes, "；"))
	}
	return b.String()
}

/* OnEnd 记录最近输出时间（下轮"距上次输出"用）。 */
func (h *Status) OnEnd(_ context.Context, _ *types.LoopState) error {
	h.store.SetLastOutputAt(time.Now().UnixMilli())
	return nil
}

/* build 组装状态数据并推进资源基线。 */
func (h *Status) build(ctx context.Context) StatusData {
	now := time.Now()
	data := StatusData{
		Now:       now.Format("2006-01-02 15:04"),
		CtxTokens: h.ctxTokens(),
		CtxWindow: h.ctxWindow,
	}
	if last := h.store.LastOutputAt(); last > 0 {
		data.SinceLastOutputMin = (now.UnixMilli() - last) / 60000
	}
	if h.ctxWindow > 0 && data.CtxTokens > h.ctxWindow*7/10 {
		data.SuggestCompact = true
	}

	var curSkills, curMcps []string
	if skills, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range skills {
			curSkills = append(curSkills, s.Name)
		}
		sort.Strings(curSkills)
	}
	for _, m := range h.mcpList() { // 全量仅作变更基线，不进状态记录
		curMcps = append(curMcps, m.Name)
	}
	sort.Strings(curMcps)

	/* 终端：清单变更走基线 diff（新增/退出/关闭）；用户手动输入收割即
	注入（服务侧队列取走即清，AI 写入不记录） */
	var curTerms []string
	var userChanges []string
	if h.termRep != nil {
		rep := h.termRep()
		for _, t := range rep.Terms {
			curTerms = append(curTerms, termKey(t))
		}
		for _, a := range rep.Lines {
			userChanges = append(userChanges, fmt.Sprintf("用户在终端 %s 执行：%s", a.ID, a.Line))
		}
	}

	if prev := h.store.ResSnap(); prev != nil {
		data.Changes = append(diffNames(prev.Skills, curSkills, "skill"),
			diffNames(prev.Mcps, curMcps, "mcp")...)
		data.Changes = append(data.Changes, diffTerms(prev.Terms, curTerms)...)
	}
	data.Changes = append(data.Changes, userChanges...)
	h.store.SetResSnap(&ResSnapshot{Skills: curSkills, Mcps: curMcps, Terms: curTerms})
	return data
}

/* termKey 终端基线编码（id|名称|是否退出|来源）。 */
func termKey(t StatusTerm) string {
	return fmt.Sprintf("%s|%s|%v|%s", t.ID, t.Name, t.Exited, t.Origin)
}

/* diffTerms 对比终端基线产出变更（同 id 退出态变化报"已退出"，
消失报"已关闭"，AI 可感知用户关掉了自己开的终端；新增时报创建
来源——用户手动开的终端对模型是未知状态，须显式区分）。 */
func diffTerms(oldS, newS []string) []string {
	parse := func(s string) (id, name, origin string, exited bool) {
		parts := strings.SplitN(s, "|", 4)
		if len(parts) < 3 {
			return s, s, "", false
		}
		exited = parts[2] == "true"
		if len(parts) == 4 {
			return parts[0], parts[1], parts[3], exited
		}
		return parts[0], parts[1], "", exited // 旧 3 段基线（无来源）
	}
	originLabel := func(origin string) string {
		switch {
		case origin == "用户":
			return "，用户手动创建"
		case strings.HasPrefix(origin, "AI"):
			return "，AI 创建"
		}
		return ""
	}
	prev := map[string]string{}
	for _, s := range oldS {
		id, _, _, _ := parse(s)
		prev[id] = s
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range newS {
		id, name, origin, exited := parse(s)
		seen[id] = true
		old, had := prev[id]
		if !had {
			out = append(out, fmt.Sprintf("新增终端 %s(%s)%s", id, name, originLabel(origin)))
			continue
		}
		if _, _, _, wasExited := parse(old); exited && !wasExited {
			out = append(out, fmt.Sprintf("终端 %s(%s) 已退出", id, name))
		}
	}
	for _, s := range oldS {
		id, name, _, _ := parse(s)
		if !seen[id] {
			out = append(out, fmt.Sprintf("终端 %s(%s) 已关闭", id, name))
		}
	}
	return out
}

/* diffNames 对比新旧名单产出变更记录（中文完整短语，模型可读）。 */
func diffNames(oldS, newS []string, kind string) []string {
	label := map[string]string{"skill": "技能", "mcp": "MCP 服务"}[kind]
	var out []string
	for _, n := range newS {
		if !slices.Contains(oldS, n) {
			out = append(out, "新增"+label+" "+n)
		}
	}
	for _, n := range oldS {
		if !slices.Contains(newS, n) {
			out = append(out, "移除"+label+" "+n)
		}
	}
	return out
}

/* FriendlyStop 把轮停止原因映射为终止分类（end_reason 记录用）。 */
func FriendlyStop(reason string) string {
	switch reason {
	case "completed":
		return "正常结束"
	case "cancelled":
		return "手动停止"
	case "max_iterations":
		return "达到迭代上限"
	case "aborted":
		return "策略中止"
	case "error":
		return "执行出错"
	case "":
		return "正常结束"
	}
	return "本轮结束（" + reason + "）"
}

/* ParseStatusTag 从消息内容解析 <agent_status> 载荷（非状态记录返回 nil）。 */
func ParseStatusTag(content string) *StatusData {
	open, close := "<"+StatusTag+">", "</"+StatusTag+">"
	i := strings.Index(content, open)
	if i < 0 {
		return nil
	}
	rest := content[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return nil
	}
	var d StatusData
	if json.Unmarshal([]byte(strings.TrimSpace(rest[:j])), &d) != nil {
		return nil
	}
	return &d
}

/* LastCtxTokens 从历史尾部找最近一条状态记录的水位（旧快照无
ctxTokens 字段时的恢复兜底；找不到返回 0）。 */
func LastCtxTokens(msgs []types.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser {
			continue
		}
		if d := ParseStatusTag(msgs[i].Content); d != nil && d.CtxTokens > 0 {
			return d.CtxTokens
		}
	}
	return 0
}
