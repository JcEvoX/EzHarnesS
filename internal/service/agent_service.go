/*
AgentService 负责 agent 的装配与重建：provider、hooks、warp、工具集。
文件与终端工具复用 ezloop 的 filetools hook（原生 shell，Windows 为 cmd，
模型适配环境），ezharness 只增补 save_app。

上下文机制：system 由 sysprompt hook 每轮注入（session 创建时组装一次：
人格+SystemExtra+长期记忆+skill/mcp 列表；重启从快照还原不重组）。
contextfix 修理残缺历史，offload 卸载大工具结果；压缩（compact）、
状态栏（status）、调用链（trace）见 internal/hooks 各文件。
*/
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuanlv2002/ezloop/core"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/ext/hook/contextfix"
	"github.com/xuanlv2002/ezloop/ext/hook/filetools"
	"github.com/xuanlv2002/ezloop/ext/hook/offload"
	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/ext/hook/task"
	"github.com/xuanlv2002/ezloop/ext/provider/openai"
	"github.com/xuanlv2002/ezloop/ext/warp/model/modelretry"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/limit"
	"github.com/xuanlv2002/ezloop/ext/warp/tool/safetool"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
	"ezharness/internal/tools"
	"ezharness/internal/warp/modeldump"
)

/* AgentService 装配领域会话的运行时。 */
type AgentService struct {
	Hub *domain.Hub
}

/* Assemble 按配置装配 agent 并注入会话（主模型取 models 四槽 main 启用条目）。 */
func (a *AgentService) Assemble(s *domain.Session, st domain.Settings) {
	ctx := context.Background()

	main := a.Hub.ModelsSnapshot().ActiveMain()
	if main == nil {
		main = &domain.ModelEntry{}
	}
	provider := openai.New(openai.Options{
		BaseURL: main.BaseURL,
		APIKey:  main.APIKey,
		Headers: main.Headers,
		Model:   main.Name,
	})

	// system 两段式：每 session 固定——已有 SysPrompt 直接复用（Resume/
	// compact 热更过的状态是本 session 的真相，重建不得回退到旧快照）；
	// 恢复的会话从快照还原（记忆/skill/mcp 变更等下个 session），新会话
	// 组装一次后固定，直到 compact 创建新 session。
	var sys *hooks.SysPrompt
	if sp := s.SysPromptRef(); sp != nil {
		sys = sp
	} else if snap := s.Snapshot(); snap != nil {
		sys = hooks.NewSysPrompt(snap.SystemBase, snap.SummaryBlock)
	} else {
		sys = hooks.NewSysPrompt(buildSystemBase(ctx, st, s.Fsys), "")
	}
	s.SetSysP(sys)
	s.Sess.BindSys(sys, main.Name)

	approver, approveCh := approve.New(a.needsApprove)
	asker, answerCh := askuser.New()

	window := main.ContextWindow
	if window <= 0 {
		window = 128000 // 旧 models.json 无 contextWindow 字段的兜底
	}
	s.Sess.BindCtx(func() (int, int) { return s.CtxTokens(), window })
	statusHook := hooks.NewStatus(s.Fsys, s.Sess,
		func() int { return s.CtxTokens() },
		window,
		func() []hooks.StatusMcp { return mcpStatusList(s.Fsys) },
	)
	traceHook := hooks.NewTrace(s.Fsys, s.Sess, func() string { return main.Name })
	compactHook := hooks.NewCompact(provider, s.Fsys, s.Sess, sys, a.Hub.Topics, traceHook,
		window*st.CompactPercent/100, // 水位=窗口百分比，随模型自适应（换模型 Reassemble 重算）
		window, // 模型窗口（压缩提示展示水位比例用）
		func() string { return buildSystemBase(ctx, st, s.Fsys) }, // compact 即新 session：全量重载
		func(info hooks.CompactInfo) { s.SetIdentity(info.NewID) }, // 绑定本会话：后台分支压缩不串线
	)

	agent := core.NewAgent(provider,
		core.WithModelWarp(modeldump.Warp(), modelretry.Warp()),
		core.WithToolWarp(limit.Warp(4), safetool.Warp()),
		core.WithTools(tools.SaveApp(s.Fsys)...),
		core.WithHooks(
			sys, // startHooks 首位：system base 唯一来源；后续 hook 在其 OnStart 里追加 tool-guide 说明段
			contextfix.New(),
			filetools.New(s.Fsys, filetools.WithWorkDir(resolveWorkDir(st.WorkDir))),
			hooks.NewSkillTool(s.Fsys, hooks.SkillsDir),
			statusHook,
			approver,
			asker,
			task.New(),
			NewMcpHook(s.Fsys),
			offload.New(s.Fsys, offload.WithSkip(askuser.ToolName, task.ToolName), offload.WithReplayTool("read_file")),
			hooks.NewGuard(s.Fsys, window), // 窗口余量兜底：offload 豁免名单（read_file 等）的大结果放不下时卸载，须在 offload 之后
			compactHook, // OnEnd 在 trace/store 之前：截断+换库先发生
			traceHook,
			hooks.NewEndNote(), // 每轮收尾补 <end_reason>（轮次/时长/结束时间/原因），须在 sessionstore 落盘前
			s.Sess, // 最后落盘
		),
		core.WithLoopParams(core.LoopParams{MaxIterations: 12}),
		core.WithStreaming(true),
	)

	s.Attach(domain.Wiring{
		Agent:     agent,
		Provider:  provider,
		ApproveCh: approveCh,
		AnswerCh:  answerCh,
		Trace:     traceHook,
		ToolNames: []string{
			"read_file", "write_file", "edit_file", "bash", "save_app",
			askuser.ToolName, task.ToolName,
			"mcp_router", hooks.CompactTool, hooks.SkillTool,
		},
	})
}

/* Reassemble 重建全部存活分支的 agent（配置变更后；运行中的分支
跳过——保留旧 wiring 到其轮结束，下次变更追平）。活动分支 busy
仍返回 ErrBusy 保持前端提示语义。 */
func (a *AgentService) Reassemble(st domain.Settings) error {
	if a.Hub.Active.Busy() {
		return domain.ErrBusy
	}
	for _, s := range a.Hub.Sessions() {
		if s.Busy() {
			continue
		}
		a.Assemble(s, st)
	}
	return nil
}

/*
	needsApprove 按审批策略判定（安全页四档）。

人机交互与内部工具恒免审；未知工具默认审批。运行时读设置快照，
改策略即时生效（无需重建 agent）。
*/
func (a *AgentService) needsApprove(c *types.ToolCall) bool {
	switch c.Name {
	case askuser.ToolName, hooks.SkillTool, hooks.CompactTool:
		return false // 交互与内部工具不属用户管控面（加载技能/压缩均为只读元操作）
	}
	name := c.Name
	if name == "mcp_router" {
		// ezloop mcp 是单一 router 工具，二段式（action/server/tool 在 args）。
		// 发现类（mcp_list/tool_list）只读无副作用，四档下一律免审。
		var a struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(c.Args, &a)
		if a.Action == "mcp_list" || a.Action == "tool_list" {
			return false
		}
		name = "mcp.*" // tool_call 按 server.tool 名单走四档
	}
	rules := a.Hub.SettingsSnapshot().ToolRules
	var rule *domain.ToolRule
	for i := range rules {
		if rules[i].Tool == name {
			rule = &rules[i]
			break
		}
	}
	if rule == nil {
		return true
	}
	switch rule.Level {
	case domain.LevelAuto:
		return false
	case domain.LevelAsk:
		return true
	case domain.LevelWhite:
		return !matchRuleList(rule.List, name, c.Args)
	case domain.LevelBlack:
		return matchRuleList(rule.List, name, c.Args)
	}
	return true
}

/*
	matchRuleList 判定工具调用是否命中名单：bash 匹配命令（相等或词边界前缀）、

文件工具匹配路径前缀、mcp 匹配 server 或 server.tool。
*/
func matchRuleList(list []string, ruleTool string, args json.RawMessage) bool {
	key := ""
	switch ruleTool {
	case "terminal":
		var a struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Command
	case "read_file", "write_file", "edit_file":
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Path
	case "mcp.*":
		var a struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
		}
		_ = json.Unmarshal(args, &a)
		key = a.Server
		if a.Tool != "" {
			key = a.Server + "." + a.Tool
		}
	}
	if key == "" {
		return false
	}
	for _, e := range list {
		if key == e {
			return true
		}
		if ruleTool == "terminal" && strings.HasPrefix(key, e+" ") {
			return true // 命令词边界
		}
		if ruleTool == "mcp.*" && strings.HasPrefix(key, e+".") {
			return true // server 前缀放行整站（点边界：time 不误命中 timeX）
		}
		if ruleTool != "terminal" && ruleTool != "mcp.*" && strings.HasPrefix(key, e) {
			return true // 路径前缀
		}
	}
	return false
}

/*
resolveWorkDir 把工作目录配置解析为绝对路径：空 = 数据目录下 workspace/
（模型草稿与命令产物落这里，不与 models.json/sessions/ 等数据文件混放），
相对 = 相对数据目录；目录不存在则创建（terminal 的执行目录必须存在）。
*/
func resolveWorkDir(spec string) string {
	wd, err := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	if err != nil {
		wd = "."
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = filepath.Join(wd, "workspace")
	} else if !filepath.IsAbs(spec) {
		spec = filepath.Join(wd, spec) // 相对路径按数据目录解析
	}
	abs, err := filepath.Abs(spec)
	if err != nil {
		return spec
	}
	_ = os.MkdirAll(abs, 0o755)
	return abs
}

/*
buildSystemBase 组装 session 的 system 基础段：人格 + SystemExtra +
标签化注入块（<memory> 长期记忆结构+索引 / <skills> 技能列表 /
<mcp> MCP 列表）。只在 session 创建时调用一次（同 session 不变）；
skill 全文与记忆细节不注入（模型按需用文件工具读取），列表变更要等
下个 session 才进 system，过渡期靠 agent_status 状态栏告知模型。
*/
func buildSystemBase(ctx context.Context, st domain.Settings, fsys osfs.OS) string {
	var b strings.Builder
	b.WriteString("你是 ezharness——一个持续陪伴用户的设备级 agent，可全权操作本机文件与命令。" +
		"能用工具就用工具，回答简洁。" +
		"用户需要小工具或网页时用 save_app 生成为快应用，用户可一键启动。" +
		"重要的用户偏好与事实可写入长期记忆（结构见 <memory> 块）。")
	if st.SystemExtra != "" {
		b.WriteString("\n\n" + st.SystemExtra)
	}
	dataDir, _ := os.Getwd() // 进程 cwd 即数据目录（启动时 chdir）
	workDir := resolveWorkDir(st.WorkDir)
	p := func(rel string) string { return filepath.ToSlash(filepath.Join(dataDir, rel)) }
	b.WriteString("\n\n<workspace>\n" +
		"# 目录架构与读写权限（下列均为完整绝对路径，直接使用，不要自行拼接）：\n" +
		"# " + p("workspace") + "          工作目录，草稿/脚本/命令产物放这里，自由读写（terminal 默认执行目录：" + filepath.ToSlash(workDir) + "）\n" +
		"# " + p("memory/longterm") + "    长期记忆，可写：harness.md 是索引（已注入上下文），主题文件按需新建，沉淀用户偏好与重要事实\n" +
		"# " + p("memory/skills") + "      技能库，可写：每技能一个子目录（SKILL.md 指令 + scripts/ 脚本），新建后下个 session 进清单\n" +
		"# " + p("apps") + "               快应用目录，由 save_app 工具写入，一般不手动改\n" +
		"# " + p("mcp.json") + "           MCP 服务配置，可写：新增/修改 server 后经 mcp_router 调用（资源变更会出现在状态栏）\n" +
		"# " + p("sessions") + "           历史会话存档，只读：上下文与回忆来源（compact 摘要引用其路径），改写会破坏会话链\n" +
		"# " + p(".ezloop/offload") + "    大工具结果的卸载区，按需读取，不手动管理\n" +
		"# " + p("settings.json") + " / " + p("models.json") + " / " + p("stats.json") + " / " + p("topics.json") + "：应用配置与索引，由设置页和应用自身管理，不要直接改写\n" +
		"# 规则：terminal 每条命令是独立进程（cd 不跨命令保留）；所有文件读写与命令一律绝对路径，不要依赖当前目录；\n" +
		"# 工作目录之外的临时文件不要随手乱放。\n" +
		"</workspace>")
	memRoot := filepath.ToSlash(filepath.Join(dataDir, "memory"))
	b.WriteString("\n\n<memory>\n" +
		"# 长期记忆（下列均为完整绝对路径，直接使用，不要自行拼接）\n" +
		"- 索引 " + memRoot + "/longterm/harness.md：长期记忆入口，全文见下方，可用文件工具直接更新\n" +
		"- 主题记忆 " + memRoot + "/longterm/：按主题的记忆文件（如 user.md），按需创建，不进上下文，用 findstr/grep 检索\n" +
		"- 技能 " + memRoot + "/skills/：沉淀的技能，每技能一个子目录（清单见 <skills>）\n" +
		"- 话题存档 " + filepath.ToSlash(filepath.Join(dataDir, "sessions")) + "/：历史会话全文（compact 后的旧库；在数据目录下，不在 memory 里）\n" +
		"# 索引 harness.md 全文\n" +
		hooks.EnsureHarnessMd(ctx, fsys) +
		"\n</memory>")
	if skills, err := skill.LoadDir(ctx, fsys, hooks.SkillsDir); err == nil && len(skills) > 0 {
		b.WriteString("\n\n<skills>\n（本清单由系统运行时生成，不在任何文件里；技能正文在 " +
			memRoot+"/skills/<名>/SKILL.md，可用文件工具编辑，改动下个 session 生效；"+
			"使用前先调用 load_skill 获取完整指令与脚本路径）")
		for _, sk := range skills {
			fmt.Fprintf(&b, "\n- %s: %s", sk.Name, sk.Description)
		}
		b.WriteString("\n</skills>")
	}
	if lines := mcpListLines(fsys); len(lines) > 0 {
		b.WriteString("\n\n<mcp>\n（经 mcp_router 工具调用，先用 mcp_list/tool_list 发现服务与工具）")
		for _, l := range lines {
			b.WriteString("\n" + l)
		}
		b.WriteString("\n</mcp>")
	}
	return b.String()
}

/* mcpListLines 返回启用 server 的"名: 描述"清单。 */
func mcpListLines(fsys osfs.OS) []string {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	var out []string
	for _, srv := range f.Servers {
		if srv.IsEnabled() {
			out = append(out, "- "+srv.Name+": "+srv.Description)
		}
	}
	return out
}

/* mcpStatusList 返回状态栏 MCP 清单（描述前 8 字）。 */
func mcpStatusList(fsys osfs.OS) []hooks.StatusMcp {
	f := loadMcpFileOrNil(fsys)
	if f == nil {
		return nil
	}
	out := make([]hooks.StatusMcp, 0, len(f.Servers))
	for _, srv := range f.Servers {
		if !srv.IsEnabled() {
			continue
		}
		desc := []rune(srv.Description)
		if len(desc) > 8 {
			desc = desc[:8]
		}
		out = append(out, hooks.StatusMcp{Name: srv.Name, Desc: string(desc)})
	}
	return out
}
