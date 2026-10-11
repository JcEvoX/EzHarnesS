package service

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	r := newRing(8)
	r.append([]byte("12345"))
	if got := string(r.snapshot()); got != "12345" {
		t.Fatalf("snapshot = %q", got)
	}
	r.append([]byte("678"))
	if got := string(r.snapshot()); got != "12345678" {
		t.Fatalf("append snapshot = %q", got)
	}
	r.append([]byte("90abcdef")) // 覆盖最旧,保留流式最新 8 字节
	if got := string(r.snapshot()); got != "90abcdef" {
		t.Fatalf("overflow snapshot = %q", got)
	}
}

/* send 的静默等待:新输出停了就返回;一直被输出顶着就到上限返回 timedOut。 */
func TestWaitQuiet(t *testing.T) {
	sess := &TermSession{screen: newTermScreen(40, 4), condCh: make(chan struct{})}
	sess.lastOut = time.Now()

	go func() {
		time.Sleep(30 * time.Millisecond)
		sess.mu.Lock()
		sess.screen.write([]byte("hello\r\n"))
		sess.lastOut = time.Now()
		close(sess.condCh)
		sess.condCh = make(chan struct{})
		sess.mu.Unlock()
	}()
	exited, timedOut := waitQuiet(context.Background(), sess, 40*time.Millisecond, 2*time.Second)
	if exited || timedOut {
		t.Fatalf("应等到静默返回, got exited=%v timedOut=%v", exited, timedOut)
	}
	if got := sess.screen.text(0); !strings.Contains(got, "hello") {
		t.Fatalf("输出没收进屏幕: %q", got)
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(15 * time.Millisecond):
				sess.mu.Lock()
				sess.screen.write([]byte("."))
				sess.lastOut = time.Now()
				close(sess.condCh)
				sess.condCh = make(chan struct{})
				sess.mu.Unlock()
			}
		}
	}()
	if _, timedOut2 := waitQuiet(context.Background(), sess, 60*time.Millisecond, 200*time.Millisecond); !timedOut2 {
		t.Fatal("持续输出应到上限返回 timedOut")
	}
}

/* send 取"这次命令改动的那几行":追加与原地改写都要落到这一段里。 */
func TestChangedSince(t *testing.T) {
	before := []string{"banner", "C:\\>", "", ""}
	after := []string{"banner", "C:\\>dir", "file-a", "file-b"}
	got := changedSince(before, after)
	if !strings.Contains(got, "dir") || !strings.Contains(got, "file-b") {
		t.Fatalf("漏了新内容: %q", got)
	}
	if strings.Contains(got, "banner") {
		t.Fatalf("把没变的行也算进来了: %q", got)
	}
	if got := changedSince(before, before); got != "" {
		t.Fatalf("无变化应返回空, got %q", got)
	}
}

func TestIsRawControl(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"\u0003", true},       // 纯 ^C
		{"\u0003\u0003", true}, // 多个控制符
		{"y\r", false},         // 含可打印内容
		{"echo hi", false},     // 普通命令
		{"", false},            // 空
		{"\ty", false},         // \t 不算控制输入
	}
	for _, c := range cases {
		if got := isRawControl(c.in); got != c.want {
			t.Errorf("isRawControl(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

/* 终端全局共享:Get 按 id 取,id 必填(没有"最近使用"兜底)。 */
func TestGetRequiresID(t *testing.T) {
	s := NewTerminalService("")
	s.sessions["t1"] = &TermSession{ID: "t1", Name: "build"}

	if _, err := s.Get("t1"); err != nil {
		t.Fatalf("按 id 访问应成功: %v", err)
	}
	if _, err := s.Get(""); err == nil {
		t.Fatal("省略 id 应报错(不再兜底到最近终端)")
	}
	if _, err := s.Get("t9"); err == nil {
		t.Fatal("不存在的终端应报错")
	}
}

/* remove 摘除会话并广播清单。 */
func TestRemoveDropsSession(t *testing.T) {
	s := NewTerminalService("")
	sess := &TermSession{ID: "t1", ring: newRing(16)}
	s.sessions["t1"] = sess

	s.remove(sess)
	if _, ok := s.sessions["t1"]; ok {
		t.Fatal("t1 应被移除")
	}
	if _, err := s.Get("t1"); err == nil {
		t.Fatal("移除后 Get 应报错")
	}
}
