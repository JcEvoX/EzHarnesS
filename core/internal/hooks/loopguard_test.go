package hooks

import (
	"context"
	"strings"
	"testing"

	ezhook "github.com/xuanlv2002/ezloop/hook"
	"github.com/xuanlv2002/ezloop/types"
)

/* 同参重复第 4 次标记待提醒（仅一条），第 6 次起 Skip 拦截；不同参数各算各的。 */
func TestLoopGuardRepeat(t *testing.T) {
	ctx := context.Background()
	h := NewLoopGuard(64)
	state := newTestState([]types.Message{{Role: types.RoleSystem, Content: "base"}})
	call := &types.ToolCall{ID: "c1", Name: "browser_action", Args: []byte(`{"action":"scroll"}`)}
	tick := func() ezhook.Action { // 一次迭代：工具调用 → 模型调用前消费提醒
		act, _ := h.OnToolStart(ctx, state, call)
		if err := h.OnModelStart(ctx, state); err != nil {
			t.Fatal(err)
		}
		return act
	}

	for i := 0; i < 3; i++ {
		if a := tick(); a.Kind != ezhook.KindProceed {
			t.Fatalf("below block threshold must proceed, got %v", a.Kind)
		}
	}
	if n := countGuard(state.Messages); n != 0 {
		t.Fatalf("below threshold must not warn, got %d", n)
	}
	for i := 0; i < 10; i++ { // 第 4 次提醒、第 5 次仍放行，第 6 次起拦截
		a := tick()
		if i < 2 && a.Kind != ezhook.KindProceed {
			t.Fatalf("4th/5th call must proceed, got %v", a.Kind)
		}
		if i >= 2 && a.Kind != ezhook.KindSkip {
			t.Fatalf("at/after 6th call must skip, got %v", a.Kind)
		}
	}
	if n := countGuard(state.Messages); n != 1 {
		t.Fatalf("warn exactly once, got %d", n)
	}
	if !strings.Contains(lastGuard(state.Messages), "browser_action") {
		t.Fatalf("warn must name the tool: %q", lastGuard(state.Messages))
	}
	// 提醒消息只能出现在消息序列末尾（OnModelStart 时机），不得夹在 tool_use 与结果之间
	if state.Messages[len(state.Messages)-1].Content != lastGuard(state.Messages) {
		t.Fatal("guard message must be appended at sequence end")
	}

	// 不同参数各算各的
	other := &types.ToolCall{ID: "c2", Name: "browser_action", Args: []byte(`{"action":"click"}`)}
	for i := 0; i < 3; i++ {
		act, _ := h.OnToolStart(ctx, state, other)
		if act.Kind != ezhook.KindProceed {
			t.Fatalf("fresh signature must proceed, got %v", act.Kind)
		}
	}
	if err := h.OnModelStart(ctx, state); err != nil {
		t.Fatal(err)
	}
	if n := countGuard(state.Messages); n != 1 {
		t.Fatalf("different args count separately, got %d", n)
	}

	if err := h.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}
}

/* 轮数压力线：soft/hard 各注入一次收敛指令，短上限时 soft<hard 仍成立。 */
func TestLoopGuardPressure(t *testing.T) {
	ctx := context.Background()
	h := NewLoopGuard(64)
	state := newTestState([]types.Message{{Role: types.RoleSystem, Content: "base"}})
	state.MaxIterations = 64
	soft, hard := h.pressureLines()
	if soft != 24 || hard != 48 {
		t.Fatalf("default 64 pressure lines want 24/48, got %d/%d", soft, hard)
	}
	for i := 1; i <= hard+5; i++ {
		state.Iteration = i
		if err := h.OnModelStart(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	if n := countGuard(state.Messages); n != 2 {
		t.Fatalf("soft+hard each fire once, got %d", n)
	}
	if err := h.OnEnd(ctx, state); err != nil {
		t.Fatal(err)
	}

	h2 := NewLoopGuard(10)
	s2, h2line := h2.pressureLines()
	if s2 >= h2line {
		t.Fatalf("soft must stay below hard, got %d/%d", s2, h2line)
	}
}

func countGuard(msgs []types.Message) int {
	n := 0
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, "<"+LoopGuardTag+">") {
			n++
		}
	}
	return n
}

func lastGuard(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.HasPrefix(msgs[i].Content, "<"+LoopGuardTag+">") {
			return msgs[i].Content
		}
	}
	return ""
}
