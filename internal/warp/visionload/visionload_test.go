package visionload

import (
	"context"
	"errors"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/types"
)

/* 内存 fs：预置文件，缺失路径返回 not found */
type fakeFS struct{ files map[string][]byte }

func (f fakeFS) Read(_ context.Context, p string) ([]byte, error) {
	if d, ok := f.files[p]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}
func (f fakeFS) Write(_ context.Context, _ string, _ []byte) error { return nil }
func (f fakeFS) List(_ context.Context, _ string) ([]fs.Entry, error) {
	return nil, nil
}
func (f fakeFS) Edit(_ context.Context, _, _, _ string) (int, error) { return 0, nil }

var png1x1 = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}

/* mkReq 构造带图片标记的请求；返回首条 user 消息的原始引用（模拟引擎历史） */
func mkReq(content string) (*types.ModelRequest, *types.Message) {
	userMsg := &types.Message{Role: types.RoleUser, Content: "hello"}
	req := &types.ModelRequest{Messages: []types.Message{
		*userMsg,
		{Role: types.RoleAssistant, Content: "x"},
		{Role: types.RoleTool, Content: content},
	}}
	return req, userMsg
}

func TestInjectMarksDedupAndClone(t *testing.T) {
	p := &loadProvider{fsys: fakeFS{files: map[string][]byte{"/tmp/a.png": png1x1}}, visionOn: func() bool { return true }}
	req, userMsg := mkReq(MarkLoaded("/tmp/a.png") + "\n" + MarkLoaded("/tmp/a.png"))
	p.inject(req)
	got := req.Messages[0]
	if len(got.Images) != 1 {
		t.Fatalf("images = %d, want 1 (duplicate marks deduped)", len(got.Images))
	}
	if got.Images[0].MimeType != "image/png" {
		t.Fatalf("mime = %s", got.Images[0].MimeType)
	}
	if len(userMsg.Images) != 0 {
		t.Fatalf("original message mutated: %d images leaked into history", len(userMsg.Images))
	}
}

func TestInjectSkipsWhenNoVision(t *testing.T) {
	p := &loadProvider{fsys: fakeFS{files: map[string][]byte{"/tmp/a.png": png1x1}}, visionOn: func() bool { return false }}
	req, _ := mkReq(MarkLoaded("/tmp/a.png"))
	p.inject(req)
	if len(req.Messages[0].Images) != 0 {
		t.Fatalf("should not inject without vision")
	}
}

func TestInjectSkipsMissingAndNonImage(t *testing.T) {
	p := &loadProvider{fsys: fakeFS{files: map[string][]byte{"/tmp/b.txt": []byte("hi")}}, visionOn: func() bool { return true }}
	req, _ := mkReq(MarkLoaded("/tmp/missing.png") + MarkLoaded("/tmp/b.txt"))
	p.inject(req)
	if len(req.Messages[0].Images) != 0 {
		t.Fatalf("missing/non-image marks should not inject")
	}
}
