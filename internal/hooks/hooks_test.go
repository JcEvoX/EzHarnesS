package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* memFS 内存文件系统（测试用）。 */
type memFS map[string][]byte

func (m memFS) Read(_ context.Context, p string) ([]byte, error) {
	if d, ok := m[p]; ok {
		return d, nil
	}
	return nil, os.ErrNotExist
}
func (m memFS) Write(_ context.Context, p string, data []byte) error {
	m[p] = append([]byte(nil), data...)
	return nil
}
func (m memFS) List(_ context.Context, dir string) ([]fs.Entry, error) {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	seen := map[string]fs.Entry{}
	for p := range m {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		parts := strings.Split(rest, "/")
		if len(parts) == 1 {
			seen[rest] = fs.Entry{Name: rest}
		} else {
			seen[parts[0]] = fs.Entry{Name: parts[0], IsDir: true}
		}
	}
	out := make([]fs.Entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	return out, nil
}
func (m memFS) Edit(_ context.Context, p, oldText, newText string) (int, error) {
	d, ok := m[p]
	if !ok {
		return 0, os.ErrNotExist
	}
	n := strings.Count(string(d), oldText)
	if n == 0 {
		return 0, fmt.Errorf("not found in %s", p)
	}
	m[p] = []byte(strings.ReplaceAll(string(d), oldText, newText))
	return n, nil
}

/* fakeProvider 返回固定摘要文本。 */
type fakeProvider struct{ reply string }

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Invoke(_ context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	return &types.ModelResponse{Content: f.reply}, nil
}

func newTestState(msgs []types.Message) *types.LoopState {
	return &types.LoopState{Messages: msgs, Tools: types.NewToolRegistry(), Metadata: map[string]any{}}
}

/* 标题推导须跳过系统记录（agent_status/end_reason 都是 role=user 的注入消息） */
func TestFirstUserTitleSkipsSystemNotes(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "<agent_status>\n水位 50%\n</agent_status>"},
		{Role: types.RoleUser, Content: "<end_reason>\n（系统自动记录的轮次收尾信息，非用户发言，无需回应）\n</end_reason>"},
		{Role: types.RoleAssistant, Content: "答"},
		{Role: types.RoleUser, Content: "  真正的用户问题  "},
	}
	if got := FirstUserTitle(msgs); got != "真正的用户问题" {
		t.Fatalf("expect real user text, got %q", got)
	}
	if got := FirstUserTitle(msgs[:2]); got != "未命名话题" {
		t.Fatalf("all-system window should be untitled, got %q", got)
	}
}

/* endnote 错误轮须落错误详情（换行压平、超长截断），否则用户只见分类不知原因 */
func TestEndNoteErrorDetail(t *testing.T) {
	h := NewEndNote()
	long := strings.Repeat("错", 400)
	state := &types.LoopState{StopReason: types.StopError, LastError: fmt.Errorf("boom\nline2 %s", long)}
	if err := h.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	last := state.Messages[len(state.Messages)-1].Content
	if !strings.Contains(last, "结束原因：执行出错") {
		t.Fatalf("missing stop reason, got %q", last)
	}
	if !strings.Contains(last, "错误详情：boom line2") {
		t.Fatalf("newline not flattened, got %q", last)
	}
	if i := strings.Index(last, "错误详情："); i >= 0 && len([]rune(last[i:])) > 300+20 {
		t.Fatalf("detail not truncated to ~300 runes, got %d", len([]rune(last[i:])))
	}
}

func TestTopicsMatch(t *testing.T) {
	tp := NewTopics(memFS{})
	_ = tp.Add(TopicEntry{ID: "a1", Title: "Go 并发", Summary: "goroutine"})
	_ = tp.Add(TopicEntry{ID: "b2", Title: "数据库", Summary: "postgres"})
	if got := len(tp.Match("")); got != 2 {
		t.Fatalf("match all = %d", got)
	}
	if got := len(tp.Match("a1")); got != 1 {
		t.Fatalf("match id prefix = %d", got)
	}
	if got := len(tp.Match("数据库")); got != 1 {
		t.Fatalf("match title = %d", got)
	}
}

/* 旧扁平索引 → 分支索引一次性重建：compress 链归组为一条线，
LeafID=最新世代，各快照回填 LineRoot；已是新格式时幂等跳过。 */
func TestMigrateIndex(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	writeSnap := func(s *SessionSnap) {
		data, _ := json.Marshal(s)
		_ = fsys.Write(ctx, SessionsDir+"/"+s.ID+"/session.json", data)
	}
	// 线 1：gen1 → gen2(compress)，gen1 已归档；线 2：独立根
	writeSnap(&SessionSnap{ID: "gen1", CreatedAt: 100, Messages: []types.Message{{Role: types.RoleUser, Content: "话题一"}},
		Archived: true})
	writeSnap(&SessionSnap{ID: "gen2", CreatedAt: 200, PrevSession: "gen1", CompactSummary: "摘要"})
	writeSnap(&SessionSnap{ID: "root2", CreatedAt: 300, Messages: []types.Message{{Role: types.RoleUser, Content: "话题二"}}})
	// 旧格式索引：每世代一条、Kind=compact
	_ = fsys.Write(ctx, "topics.json", []byte(`[{"id":"gen1","title":"旧标题","kind":"compact"}]`))

	tp := NewTopics(fsys)
	tp.MigrateIndex(ctx)

	list := tp.Load()
	if len(list) != 2 {
		t.Fatalf("expect 2 lines, got %+v", list)
	}
	var l1, l2 TopicEntry
	for _, e := range list {
		switch e.ID {
		case "gen1":
			l1 = e
		case "root2":
			l2 = e
		}
	}
	if l1.LeafID != "gen2" || l1.Kind != "new" || l1.Title != "旧标题" {
		t.Fatalf("line1 wrong: %+v", l1)
	}
	if l2.LeafID != "root2" || l2.Kind != "new" {
		t.Fatalf("line2 wrong: %+v", l2)
	}
	// LineRoot 回填
	for id, want := range map[string]string{"gen1": "gen1", "gen2": "gen1", "root2": "root2"} {
		s, err := LoadSnap(ctx, fsys, id)
		if err != nil || s.LineRoot != want {
			t.Fatalf("lineRoot of %s = %v (want %s), err=%v", id, s.LineRoot, want, err)
		}
	}
	// 幂等：新格式重跑不再触发
	tp2 := NewTopics(fsys)
	tp2.MigrateIndex(ctx)
	if got := len(tp2.Load()); got != 2 {
		t.Fatalf("remigrate should be no-op, got %d", got)
	}
}

func TestSysPromptSingleSystem(t *testing.T) {
	sys := NewSysPrompt("base", "summary")
	state := newTestState([]types.Message{{Role: types.RoleUser, Content: "hi"}})
	if err := sys.OnStart(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 2 || state.Messages[0].Role != types.RoleSystem {
		t.Fatal("sysprompt must prepend single system message")
	}
	if state.Messages[0].Content != "base\n\nsummary" {
		t.Fatalf("render wrong: %q", state.Messages[0].Content)
	}
}

/* 三层加载的第 2 层：load_skill 返回 SKILL.md 全文 + 路径 + 目录结构。 */
func TestSkillToolLoad(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	_ = fsys.Write(ctx, "memory/skills/pdf/SKILL.md",
		[]byte("---\nname: pdf\ndescription: 提取 PDF\n---\n\n# PDF 处理\n步骤：pdfplumber"))
	_ = fsys.Write(ctx, "memory/skills/pdf/scripts/extract.py", []byte("print(1)"))
	_ = fsys.Write(ctx, "memory/skills/pdf/references/api.md", []byte("api 文档"))

	h := NewSkillTool(fsys, "memory/skills", nil)
	state := newTestState(nil)
	if err := h.OnStart(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Tools.Lookup(SkillTool); err != nil {
		t.Fatal("load_skill must be registered")
	}

	action, err := h.OnToolStart(ctx, state, &types.ToolCall{
		ID: "c1", Name: SkillTool, Args: []byte(`{"name":"pdf"}`),
	})
	if err != nil || action.Kind != ezhook.KindSkip {
		t.Fatalf("expect skip action, err=%v kind=%v", err, action.Kind)
	}
	r := action.Result
	if !strings.Contains(r, "memory/skills/pdf/SKILL.md") ||
		!strings.Contains(r, "步骤：pdfplumber") || // 全文（frontmatter 已剥离）
		!strings.Contains(r, "scripts/extract.py") || !strings.Contains(r, "references/api.md") {
		t.Fatalf("load result missing parts: %q", r)
	}
	if strings.Contains(r, "name: pdf") {
		t.Fatal("frontmatter must be stripped from instructions")
	}

	// 未知名：返回可用列表提示
	action, _ = h.OnToolStart(ctx, state, &types.ToolCall{
		ID: "c2", Name: SkillTool, Args: []byte(`{"name":"nope"}`),
	})
	if !strings.Contains(action.Result, "pdf") {
		t.Fatalf("unknown skill should list available: %q", action.Result)
	}
}
