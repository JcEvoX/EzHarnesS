package hooks

import (
	"context"
	"strings"
	"testing"
)

/* parseDistillJSON：围栏/杂文剥除、坏输出报错。 */
func TestParseDistillJSON(t *testing.T) {
	ok := "```json\n{\"user\":{\"add\":[{\"text\":\"a\"}],\"update\":[{\"id\":\"u1\",\"text\":\"b\"}],\"remove\":[\"u2\"]},\"soul\":{\"add\":[],\"update\":[],\"remove\":[]},\"projects\":{\"save\":[],\"remove\":[]}}\n```"
	ch, err := parseDistillJSON(ok)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.User.Add) != 1 || ch.User.Add[0].Text != "a" || ch.User.Update[0].ID != "u1" || ch.User.Remove[0] != "u2" {
		t.Fatalf("user changes wrong: %+v", ch.User)
	}
	if _, err := parseDistillJSON("模型拒绝输出 JSON"); err == nil {
		t.Fatal("non-json output must fail")
	}
	if _, err := parseDistillJSON("{\"broken\": "); err == nil {
		t.Fatal("broken json must fail")
	}
}

/* distillToMemory：修改方案 JSON 落待确认区（不直接写记忆）；逐条确认
后应用——add 发新 id、update 改写、remove 删除、项目 upsert。 */
func TestDistillToMemory(t *testing.T) {
	ctx := context.Background()
	fsys := memFS{}
	_ = SaveItems(ctx, fsys, UserJson, []MemoryItem{
		{ID: "u1", Text: "偏好简洁回复"},
		{ID: "u2", Text: "过时条目"},
	})
	reply := "前后杂文\n" + `{"user":{"add":[{"text":"新偏好深色主题"}],"update":[{"id":"u1","text":"偏好极简回复"}],"remove":["u2"]},
	 "soul":{"add":[{"text":"改完跑 go build"}],"update":[],"remove":[]},
	 "projects":{"save":[{"id":"ezharness","name":"ezharness","description":"AI harness 桌面应用","content":"# ezharness\n\n记忆树已条目化。"}],"remove":[]}}`
	if err := distillToMemory(ctx, fakeProvider{reply}, fsys, nil, "C:/work/ezharness"); err != nil {
		t.Fatal(err)
	}
	// 未确认：记忆未被改动
	users, _ := LoadItems(ctx, fsys, UserJson)
	if len(users) != 2 || users[1].Text != "过时条目" {
		t.Fatalf("distill must not write memory directly: %+v", users)
	}
	ops, err := LoadPendingOps(ctx, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 5 { // add user + update u1 + remove u2 + soul add + project save
		t.Fatalf("want pending ops, got %d: %+v", len(ops), ops)
	}
	if ops[0].Op != PendingOpAdd || ops[0].Target != "user" || ops[0].Text != "新偏好深色主题" {
		t.Fatalf("first op wrong: %+v", ops[0])
	}
	// 逐条确认
	for _, op := range ops {
		if _, err := DecidePendingOp(ctx, fsys, op.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if ops2, _ := LoadPendingOps(ctx, fsys); len(ops2) != 0 {
		t.Fatalf("pending must be drained, got %d", len(ops2))
	}
	users, _ = LoadItems(ctx, fsys, UserJson)
	if len(users) != 2 {
		t.Fatalf("want 2 user items after apply, got %d: %+v", len(users), users)
	}
	if users[0].ID != "u1" || users[0].Text != "偏好极简回复" {
		t.Fatalf("update wrong: %+v", users[0])
	}
	if users[1].ID != "u3" || users[1].Text != "新偏好深色主题" {
		t.Fatalf("add must take next id u3: %+v", users[1])
	}
	souls, _ := LoadItems(ctx, fsys, SoulJson)
	if len(souls) != 1 || souls[0].Text != "改完跑 go build" {
		t.Fatalf("soul wrong: %+v", souls)
	}
	projects, _ := LoadProjects(ctx, fsys)
	if len(projects) != 1 || projects[0].ID != "ezharness" || projects[0].Description != "AI harness 桌面应用" {
		t.Fatalf("projects wrong: %+v", projects)
	}
	if content := string(fsys[ProjectContentPath("ezharness")]); !strings.HasPrefix(content, "# ezharness\n") {
		t.Fatalf("project content wrong: %q", content)
	}
	// 取消路径：再来一条后取消，记忆不变
	if err := distillToMemory(ctx, fakeProvider{`{"user":{"add":[{"text":"不该出现"}],"update":[],"remove":[]},"soul":{"add":[],"update":[],"remove":[]},"projects":{"save":[],"remove":[]}}`}, fsys, nil, "C:/work/ezharness"); err != nil {
		t.Fatal(err)
	}
	ops, _ = LoadPendingOps(ctx, fsys)
	if len(ops) != 1 {
		t.Fatalf("want 1 pending op, got %d", len(ops))
	}
	if _, err := DecidePendingOp(ctx, fsys, ops[0].ID, false); err != nil {
		t.Fatal(err)
	}
	users, _ = LoadItems(ctx, fsys, UserJson)
	if len(users) != 2 {
		t.Fatalf("cancelled op must not apply: %+v", users)
	}
}
