package hooks

import (
	"context"
	"strings"
	"testing"

	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* newTrimForTest 建 Trim；折叠段从 state.Metadata 断言。 */
func newTrimForTest(reply string, threshold int) *Trim {
	return NewTrim(fakeProvider{reply}, nil, threshold, 1000)
}

/* 水位自动路径：OnLoop 触发就地折叠，marker 衔接，折叠段移交档案。 */
func TestTrimAutoByWatermark(t *testing.T) {
	tr := newTrimForTest("整理摘要", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "base"},
		{Role: types.RoleUser, Content: "早期问题 1"},
		{Role: types.RoleAssistant, Content: "早期回答 1"},
		{Role: types.RoleUser, Content: "早期问题 2"},
		{Role: types.RoleAssistant, Content: "早期回答 2"},
		{Role: types.RoleUser, Content: "近期问题"},
		{Role: types.RoleAssistant, Content: "近期回答"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 800}}

	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(FoldedOf(state)) != 2 {
		t.Fatalf("handed-over = fold minus tail (2), got %d", len(FoldedOf(state)))
	}
	// [system, marker, tail 4]：立即生效，下一次模型调用即新上下文
	if len(state.Messages) != 6 {
		t.Fatalf("expect [system,marker,4 tail], got %d: %+v", len(state.Messages), state.Messages)
	}
	marker := state.Messages[1]
	if marker.Role != types.RoleUser || !IsTrimMarker(marker) ||
		!strings.Contains(marker.Content, "整理摘要") {
		t.Fatalf("marker wrong: %+v", marker)
	}
	if state.Messages[2].Content != "早期问题 2" || state.Messages[5].Content != "近期回答" {
		t.Fatalf("tail must keep recent messages: %+v", state.Messages)
	}
	if state.Metadata["trimmed"] != true {
		t.Fatal("must set trimmed flag for in-turn dedup")
	}

	// 防重入：同轮再次超水位不再折叠
	before := len(state.Messages)
	state.LastResponse.Usage.PromptTokens = 900
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != before || len(FoldedOf(state)) != 2 {
		t.Fatalf("second trim in same turn must be skipped, msgs=%d folded=%d",
			len(state.Messages), len(FoldedOf(state)))
	}
}

/* 低于阈值不触发；threshold<=0 禁用。 */
func TestTrimDisabledBelowThreshold(t *testing.T) {
	tr := newTrimForTest("s", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, Content: "a"},
	})
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 100}}
	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 3 || len(FoldedOf(state)) != 0 {
		t.Fatal("below threshold must not trim")
	}

	tr2 := newTrimForTest("s", 0) // 禁用
	tr2.OnLoop(context.Background(), state)
	if len(state.Messages) != 3 {
		t.Fatal("threshold<=0 must disable auto trim")
	}
}

/* 工具路径：末条 assistant(tool_calls) 保留，Skip 结果可接上。 */
func TestTrimToolPath(t *testing.T) {
	tr := newTrimForTest("工具摘要", 0)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "b"},
		{Role: types.RoleUser, Content: "q1"},
		{Role: types.RoleAssistant, Content: "a1"},
		{Role: types.RoleUser, Content: "q2"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", Name: TrimTool}}},
	})
	action, err := tr.OnToolStart(context.Background(), state, &types.ToolCall{ID: "c1", Name: TrimTool})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ezhook.KindSkip || action.Result == "" {
		t.Fatalf("expect skip with message, got %+v", action)
	}
	// [system, marker, 末条 assistant]：fold=[q1,a1,q2]
	if len(state.Messages) != 3 {
		t.Fatalf("expect 3 messages, got %d: %+v", len(state.Messages), state.Messages)
	}
	last := state.Messages[2]
	if last.Role != types.RoleAssistant || len(last.ToolCalls) != 1 {
		t.Fatalf("last assistant(tool_calls) must survive for result pairing: %+v", last)
	}
	if len(FoldedOf(state)) != 3 {
		t.Fatalf("folded 3 msgs, got %d", len(FoldedOf(state)))
	}
	// Skip 结果随后追加（引擎行为模拟）：序列协议完整
	state.Messages = append(state.Messages, types.Message{Role: types.RoleTool, ToolCallID: "c1", Content: action.Result})
	if state.Messages[3].ToolCallID != "c1" {
		t.Fatal("tool result must pair with surviving assistant")
	}
}

/* fork：折叠 SeedLen 之后的增量，SeedLen 重置对齐剥离偏移。 */
func TestTrimForkSeedLen(t *testing.T) {
	tr := newTrimForTest("fork 摘要", 500)
	state := newTestState([]types.Message{
		{Role: types.RoleSystem, Content: "seed system"},
		{Role: types.RoleUser, Content: "主上下文（归主库）"},
		{Role: types.RoleUser, Content: "分身任务"},
		{Role: types.RoleAssistant, Content: "分身过程 1"},
		{Role: types.RoleAssistant, Content: "分身过程 2"},
		{Role: types.RoleAssistant, Content: "分身过程 3"},
	})
	state.ForkID = "task-1"
	state.SeedLen = 2
	state.LastResponse = &types.ModelResponse{Usage: types.Usage{PromptTokens: 999}}

	if err := tr.OnLoop(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	// 完整 seed（system+主上下文）不动，marker 接 seed 后，增量折叠
	if state.Messages[0].Content != "seed system" || state.Messages[1].Content != "主上下文（归主库）" ||
		!IsTrimMarker(state.Messages[2]) {
		t.Fatalf("fork trim wrong head: %+v", state.Messages[:3])
	}
	// SeedLen 不变（seed 完整保留），sessionstore 剥离从 marker 起存增量
	if state.SeedLen != 2 {
		t.Fatalf("SeedLen must stay (seed intact), got %d", state.SeedLen)
	}
	// 只折叠增量（seed 后 4 条），tail 保留末 2 条过程
	if len(FoldedOf(state)) != 4 {
		t.Fatalf("fork fold = seed 后增量, got %d", len(FoldedOf(state)))
	}
}

/* tailStart：孤儿 tool 的配对 assistant 被截在窗口外时，起点前扩纳入配对。 */
func TestTailStartExpandsForOrphanTool(t *testing.T) {
	fold := []types.Message{
		{Role: types.RoleUser, Content: "q"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "1"}}},
		{Role: types.RoleTool, ToolCallID: "1"}, // 末窗口若含它则孤儿，前扩纳入 A1
		{Role: types.RoleUser, Content: "x"},
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "2"}}},
		{Role: types.RoleTool, ToolCallID: "2"},
	}
	// 末 2 条无孤儿：直接保留
	if s := tailStart(fold, 2); s != 4 {
		t.Fatalf("expect tail start 4, got %d", s)
	}
	// 末 4 条含 T1（配对 A1 在窗口外）：前扩到 A1，保留连续后缀
	if s := tailStart(fold, 4); s != 1 {
		t.Fatalf("expect tail start 1 (expand for pairing), got %d", s)
	}
	// 段不长于 K：全折叠
	if s := tailStart(fold, 6); s != 6 {
		t.Fatalf("short fold must collapse entirely, got %d", s)
	}
}
