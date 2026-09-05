package stripimage

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/osfs"
)

/* fakeProvider 记录每次收到的请求图片数。 */
type fakeProvider struct {
	calls    int
	lastImgs int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Invoke(_ context.Context, req *types.ModelRequest) (*types.ModelResponse, error) {
	f.calls++
	imgs := 0
	for _, m := range req.Messages {
		imgs += len(m.Images)
	}
	f.lastImgs = imgs
	return &types.ModelResponse{Content: "ok"}, nil
}

func imgReq() *types.ModelRequest {
	return &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: "看图", Images: []types.ImagePart{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("pngdata"))},
		}},
	}}
}

/* 图片落盘到 workDir/images/ 并在正文留下绝对路径与工具引导。 */
func TestSaveImagesToDisk(t *testing.T) {
	dir := t.TempDir()
	fsys := osfs.OS{}
	fp := &fakeProvider{}
	w := Warp(fsys, dir, true)(nil, fp)
	req := imgReq()
	if _, err := w.Invoke(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if fp.lastImgs != 0 {
		t.Fatalf("images must be stripped, got %d", fp.lastImgs)
	}
	content := req.Messages[0].Content
	if !strings.Contains(content, "image_recognize") {
		t.Fatalf("recognize guide expected, got %q", content)
	}
	dirSlash := filepath.ToSlash(dir)
	i := strings.Index(content, dirSlash)
	if i < 0 {
		t.Fatalf("saved path expected in content, got %q", content)
	}
	// 从占位文本里抠出路径（dir 起到 ".png"）
	seg := content[i:]
	end := strings.Index(seg, ".png")
	if end < 0 {
		t.Fatalf(".png path expected, got %q", seg)
	}
	saved := filepath.FromSlash(seg[:end+len(".png")])
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("image file must exist: %v", err)
	}
	if string(data) != "pngdata" {
		t.Fatalf("image content mismatch: %q", data)
	}
	if filepath.Dir(saved) != filepath.Join(dir, "images") {
		t.Fatalf("image must be under images/, got %s", saved)
	}
}

/* 未启用识别模型时占位文案引导到设置页。 */
func TestGuideWithoutRecognizer(t *testing.T) {
	fp := &fakeProvider{}
	w := Warp(osfs.OS{}, t.TempDir(), false)(nil, fp)
	req := imgReq()
	if _, err := w.Invoke(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req.Messages[0].Content, "image_recognize") {
		t.Fatal("should not mention image_recognize when disabled")
	}
	if !strings.Contains(req.Messages[0].Content, "未启用图片识别模型") {
		t.Fatalf("settings guide expected, got %q", req.Messages[0].Content)
	}
}

/* 无图请求原样透传（Content 不被改写，不写盘）。 */
func TestNoopWithoutImages(t *testing.T) {
	fp := &fakeProvider{}
	w := Warp(osfs.OS{}, t.TempDir(), true)(nil, fp)
	req := &types.ModelRequest{Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}}}
	if _, err := w.Invoke(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.Messages[0].Content != "hi" {
		t.Fatalf("content must be untouched, got %q", req.Messages[0].Content)
	}
}
