package service

import (
	"context"
	"testing"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

// safeName 净化 server 名为文件名：去路径分隔符与上溯，空回落默认。
func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"notion":  "notion",
		"a/b\\c":  "a-b-c",
		"../evil": "-evil",
		"我的服务":    "----",
		"":        "server",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// 令牌落盘往返：空 store 返回 ErrNoToken；写入后可读回。每个 server 一份。
func TestFileTokenStoreRoundTrip(t *testing.T) {
	fsys := fs.NewLocal(t.TempDir())
	ctx := context.Background()
	st := &fileTokenStore{fsys: fsys, server: "notion"}

	if _, err := st.GetToken(ctx); err != mcp.ErrNoToken {
		t.Fatalf("empty store must return ErrNoToken, got %v", err)
	}
	tok := &mcp.Token{AccessToken: "abc", TokenType: "Bearer", RefreshToken: "r", ExpiresIn: 3600}
	if err := st.SaveToken(ctx, tok); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.GetToken(ctx)
	if err != nil || got.AccessToken != "abc" || got.RefreshToken != "r" {
		t.Fatalf("roundtrip: %+v err=%v", got, err)
	}
}
