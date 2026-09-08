package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
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

/* 标题推导须跳过系统记录（agent_status/res_change/end_reason 都是 role=user 的注入消息） */
func TestFirstUserTitleSkipsSystemNotes(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "<agent_status>\n水位 50%\n</agent_status>"},
		{Role: types.RoleUser, Content: "<res_change>\n- 新增技能 x\n</res_change>"},
		{Role: types.RoleUser, Content: "<end_reason>\n（系统自动记录的轮次收尾信息，非用户发言，无需回应）\n</end_reason>"},
		{Role: types.RoleAssistant, Content: "答"},
		{Role: types.RoleUser, Content: "  真正的用户问题  "},
	}
	if got := FirstUserTitle(msgs); got != "真正的用户问题" {
		t.Fatalf("expect real user text, got %q", got)
	}
	if got := FirstUserTitle(msgs[:3]); got != "未命名话题" {
		t.Fatalf("all-system window should be untitled, got %q", got)
	}
}

/* remind 收尾段：错误轮须落错误详情（换行压平、超长截断） */
func TestRemindEndNoteErrorDetail(t *testing.T) {
	h := NewRemind(memFS{}, NewStore(memFS{}, "t1"), func() int { return 0 }, 1000, nil, nil, nil)
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

/* 三层加载的第 2 层（load_skill）已下沉 ezloop ext/hook/skilltool（测试随迁）。 */

/* remind 变更段：首轮只建基线零消息；用户终端操作是事件型，首轮也报。 */

/* remind 变更段：首轮只建基线零消息；用户终端操作是事件型，首轮也报。 */
func TestRemindFirstRoundBaselineOnly(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	_ = fsys.Write(ctx, "memory/skills/pdf/SKILL.md", []byte("---\nname: pdf---\n步骤"))
	store := NewStore(fsys, "t1")
	// termRep 报一条用户操作 + 空终端基线
	h := NewRemind(fsys, store, func() int { return 0 }, 1000,
		func() []StatusMcp { return nil },
		func() TermReport {
			return TermReport{Lines: []UserAction{{ID: "t2", Line: "go run ."}}}
		}, nil)
	state := newTestState([]types.Message{{Role: types.RoleUser, Content: "q"}})
	if err := h.OnStart(ctx, state); err != nil {
		t.Fatal(err)
	}
	resChange, status := 0, 0
	for _, m := range state.Messages {
		if strings.Contains(m.Content, "<"+ResChangeTag+">") {
			resChange++
			if !strings.Contains(m.Content, "用户在终端 t2 执行：go run .") {
				t.Fatalf("user action missing: %q", m.Content)
			}
		}
		if strings.Contains(m.Content, "<"+StatusTag+">") {
			status++
		}
	}
	if resChange != 1 {
		t.Fatalf("first round: exactly 1 res_change (user action only), got %d", resChange)
	}
	if status != 1 {
		t.Fatalf("snapshot always present, got %d", status)
	}
	if store.ResSnap() == nil {
		t.Fatal("baseline must be established on first round")
	}
}

/* remind 变更段：二轮检测到技能新增 → 恰一条 res_change 在 agent_status 前。 */
func TestRemindResChangeInsertedBeforeStatus(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	store := NewStore(fsys, "t1")
	mcpList := func() []StatusMcp { return nil }
	h := NewRemind(fsys, store, func() int { return 0 }, 1000, mcpList, nil, nil)
	state1 := newTestState([]types.Message{{Role: types.RoleUser, Content: "q1"}})
	if err := h.OnStart(ctx, state1); err != nil {
		t.Fatal(err)
	} // 首轮建基线

	_ = fsys.Write(ctx, "memory/skills/pdf/SKILL.md", []byte("---\nname: pdf---\n步骤"))
	state2 := newTestState([]types.Message{
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleUser, Content: "q2"},
	})
	if err := h.OnStart(ctx, state2); err != nil {
		t.Fatal(err)
	}
	// 序列应为 [q1, res_change, agent_status, q2]
	if len(state2.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(state2.Messages), state2.Messages)
	}
	if !strings.Contains(state2.Messages[1].Content, "新增技能 pdf") {
		t.Fatalf("res_change missing: %q", state2.Messages[1].Content)
	}
	if !strings.Contains(state2.Messages[2].Content, "<"+StatusTag+">") {
		t.Fatalf("snapshot not after res_change: %q", state2.Messages[2].Content)
	}
}

/* 无变更轮次零 res_change（不浮夸）。 */
func TestRemindNoChangeNoMessage(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	_ = fsys.Write(ctx, "memory/skills/pdf/SKILL.md", []byte("---\nname: pdf---\n步骤"))
	store := NewStore(fsys, "t1")
	h := NewRemind(fsys, store, func() int { return 0 }, 1000,
		func() []StatusMcp { return nil }, nil, nil)
	s1 := newTestState([]types.Message{{Role: types.RoleUser, Content: "q1"}})
	_ = h.OnStart(ctx, s1) // 建基线
	s2 := newTestState([]types.Message{{Role: types.RoleUser, Content: "q2"}})
	if err := h.OnStart(ctx, s2); err != nil {
		t.Fatal(err)
	}
	for _, m := range s2.Messages {
		if strings.Contains(m.Content, "<"+ResChangeTag+">") {
			t.Fatalf("no-change round must be silent: %q", m.Content)
		}
	}
}

/* OnEnd：fork 不记收尾但 LastOutputAt 仍更新。 */
func TestRemindOnEndForkSkip(t *testing.T) {
	store := NewStore(memFS{}, "t1")
	h := NewRemind(memFS{}, store, func() int { return 0 }, 1000, nil, nil, nil)
	state := &types.LoopState{ForkID: "f1", Messages: []types.Message{}}
	if err := h.OnEnd(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 0 {
		t.Fatalf("fork must not record end_reason, got %d msgs", len(state.Messages))
	}
	if store.LastOutputAt() == 0 {
		t.Fatal("LastOutputAt must be recorded even for fork")
	}
}

/* 终端变更 diff:新增(报来源)/退出/修改(名称·来源)/关闭。 */
func TestDiffTerms(t *testing.T) {
	oldS := []string{
		"t1|build|false|用户",
		"t2|logs|false|AI",
		"t3|watch|true|AI",
	}
	newS := []string{
		"t1|build-server|false|用户", // 名称变化 → 修改
		"t2|logs|false|AI",          // 不变
		"t4|deploy|false|AI",        // 新增
	}
	got := diffTerms(oldS, newS)
	joined := strings.Join(got, ";")
	for _, want := range []string{
		"终端 t1 已修改（名称 \"build\"→\"build-server\"）",
		"新增终端 t4(deploy)，AI 创建",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("diffTerms 缺少 %q, got %q", want, joined)
		}
	}
	// t3 消失 → 已关闭
	if !strings.Contains(joined, "终端 t3(watch) 已关闭") {
		t.Errorf("diffTerms 缺少 t3 已关闭, got %q", joined)
	}
	// t2 无任何变更记录
	if strings.Contains(joined, "t2") {
		t.Errorf("t2 无变化不应出现, got %q", joined)
	}
}

/* 退出态翻转报"已退出"(修改的特例,专项文案)。 */
func TestDiffTermsExited(t *testing.T) {
	got := diffTerms([]string{"t1|build|false|AI"}, []string{"t1|build|true|AI"})
	joined := strings.Join(got, ";")
	if !strings.Contains(joined, "终端 t1(build) 已退出") {
		t.Errorf("缺少退出记录, got %q", joined)
	}
	if strings.Contains(joined, "已修改") {
		t.Errorf("纯退出不应报修改, got %q", joined)
	}
}
