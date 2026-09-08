package hooks

import (
	"context"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"
)

func TestUploadFileOnStart(t *testing.T) {
	h := NewUploadFile()
	state := &types.LoopState{Metadata: map[string]any{}, Messages: []types.Message{
		{Role: types.RoleUser, Content: "<agent_status>…</agent_status>"},
		{Role: types.RoleUser, Content: "看下我发的文件"},
	}}
	WithUploadFiles([]string{"C:/data/workspace/tmp/att-x.png"})(state)
	if err := h.OnStart(context.Background(), state); err != nil {
		t.Fatalf("onstart: %v", err)
	}
	if len(state.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(state.Messages))
	}
	got := state.Messages[1].Content // 插在末条 user（本轮 input）之前
	if !strings.HasPrefix(got, "<"+UploadTag+">") || !strings.HasSuffix(got, "</"+UploadTag+">") {
		t.Fatalf("bad wrap: %q", got)
	}
	if !strings.Contains(got, "- C:/data/workspace/tmp/att-x.png") {
		t.Fatalf("path line missing: %q", got)
	}
}

func TestUploadFileNoAttachments(t *testing.T) {
	h := NewUploadFile()
	state := &types.LoopState{Metadata: map[string]any{}, Messages: []types.Message{
		{Role: types.RoleUser, Content: "hi"},
	}}
	if err := h.OnStart(context.Background(), state); err != nil {
		t.Fatalf("onstart: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Fatalf("no-attachment turn should not inject, got %d", len(state.Messages))
	}
}
