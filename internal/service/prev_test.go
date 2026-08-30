package service

import (
	"context"
	"os"
	"testing"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
	"ezharness/internal/osfs"
)

/* chdirTemp 切到临时目录（NewHub 读写 cwd 下的配置与索引，不得碰真实数据）。 */
func chdirTemp(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func newMsg(s string) types.Message {
	return types.Message{Role: types.RoleUser, Content: s}
}

/*
多代 compress 链上翻：gen1(new)→gen2→gen3，从 gen3 起应能连续翻到
gen2、gen1，gen1 之上无内容（204）。复现"只能翻到第二个会话"断链。
*/
func TestPrevChainToRoot(t *testing.T) {
	chdirTemp(t)
	svc := &SessionService{Hub: domain.NewHub()}
	ctx := context.Background()
	fsys := osfs.OS{}

	seedSnap := func(id string, msgs []string, target, kind string) {
		snap := &hooks.SessionSnap{ID: id, TargetID: target, SeedKind: kind, LineRoot: "gen1"}
		for _, m := range msgs {
			snap.Messages = append(snap.Messages, newMsg(m))
		}
		if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
			t.Fatal(err)
		}
	}
	seedSnap("gen1", []string{"gen1-early", "gen1-late"}, "", "new")
	seedSnap("gen2", []string{"gen2-msg"}, "gen1", "compress")
	seedSnap("gen3", []string{"gen3-msg"}, "gen2", "compress")

	// 第一次上翻：gen3 → gen2
	d, ok, err := svc.Prev(ctx, "gen3")
	if err != nil || !ok {
		t.Fatalf("first prev failed: ok=%v err=%v", ok, err)
	}
	if len(d.Messages) == 0 || d.Messages[0].Content != "gen2-msg" {
		t.Fatalf("expect gen2 content, got %+v", d.Messages)
	}
	if d.PrevSession != "gen1" {
		t.Fatalf("cursor must point gen1, got %q", d.PrevSession)
	}

	// 第二次上翻：gen2 → gen1（断链复现点）
	d2, ok2, err2 := svc.Prev(ctx, "gen2")
	if err2 != nil || !ok2 {
		t.Fatalf("second prev failed: ok=%v err=%v", ok2, err2)
	}
	if len(d2.Messages) == 0 || d2.Messages[0].Content != "gen1-early" {
		t.Fatalf("expect gen1 content, got %+v", d2.Messages)
	}
	if d2.PrevSession != "" {
		t.Fatalf("gen1 is root, no more prev, got %q", d2.PrevSession)
	}

	// 第三次：gen1 之上到底
	if _, ok3, err3 := svc.Prev(ctx, "gen1"); err3 != nil || ok3 {
		t.Fatalf("root must return 204, ok=%v err=%v", ok3, err3)
	}
}

/*
fork 上翻：体内含源 [0,anchor] 副本，上翻应从源的上一级继续——
fk1(fork→src1) + src1(compress→gen1) 时 Prev(fk1) 返回 gen1 内容；
源是根时到底。
*/
func TestPrevForkContinues(t *testing.T) {
	chdirTemp(t)
	svc := &SessionService{Hub: domain.NewHub()}
	ctx := context.Background()
	fsys := osfs.OS{}
	save := func(snap *hooks.SessionSnap) {
		if err := hooks.SaveSnap(ctx, fsys, snap); err != nil {
			t.Fatal(err)
		}
	}
	save(&hooks.SessionSnap{ID: "gen1", SeedKind: "new", Messages: []types.Message{newMsg("gen1-msg")}})
	save(&hooks.SessionSnap{ID: "src1", TargetID: "gen1", SeedKind: "compress"})
	save(&hooks.SessionSnap{ID: "fk1", TargetID: "src1", SeedKind: "fork"})

	d, ok, err := svc.Prev(ctx, "fk1")
	if err != nil || !ok {
		t.Fatalf("fork prev must continue to source's parent, ok=%v err=%v", ok, err)
	}
	if len(d.Messages) == 0 || d.Messages[0].Content != "gen1-msg" {
		t.Fatalf("expect gen1 content via fork, got %+v", d.Messages)
	}

	// 源是根：fork 上翻到底
	save(&hooks.SessionSnap{ID: "fk2", TargetID: "gen1", SeedKind: "fork"})
	if _, ok2, err2 := svc.Prev(ctx, "fk2"); err2 != nil || ok2 {
		t.Fatalf("fork of root must be terminal, ok=%v err=%v", ok2, err2)
	}
}
