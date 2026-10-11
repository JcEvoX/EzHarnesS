package service

import (
	"strings"
	"testing"
)

/*
真机抓到的 ConPTY 片段(见 termscreen.go 头注释):命令回显 + 输出,

	新提示符靠 CUP 摆到下一行——回归点就是"提示符不许粘在输出尾巴上"。
*/
func TestScreenPromptNotGlued(t *testing.T) {
	s := newTermScreen(120, 30)
	s.write([]byte("echo hello-term\r\n\x1b[?25lhello-term\x1b[7;1HC:\\core>\x1b[?25h"))
	got := s.text(0)
	if strings.Contains(got, "hello-termC:") {
		t.Fatalf("提示符粘在输出上了:\n%s", got)
	}
	if !strings.Contains(got, "echo hello-term\nhello-term") {
		t.Fatalf("命令回显与输出应各占一行:\n%s", got)
	}
	if !strings.HasSuffix(got, "C:\\core>") {
		t.Fatalf("末行应是新提示符:\n%s", got)
	}
}

/* 报错场景:CUP 之后跟一长串文本,同样不许糊在一起。 */
func TestScreenCmdNotFound(t *testing.T) {
	s := newTermScreen(120, 30)
	s.write([]byte("nosuchcmd\r\n\x1b]9001;CmdNotFound;nosuchcmd\x1b\\\x1b[?25l" +
		"'nosuchcmd' 不是内部或外部命令，也不是可运行的程序\r\n或批处理文件。\x1b[11;1HC:\\core>\x1b[?25h"))
	got := s.text(0)
	for _, want := range []string{"nosuchcmd", "不是内部或外部命令", "或批处理文件。", "C:\\core>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("丢了 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "批处理文件。C:") {
		t.Fatalf("提示符粘在报错尾巴上:\n%s", got)
	}
	if strings.Contains(got, "9001") {
		t.Fatalf("OSC 序列没被跳过:\n%s", got)
	}
}

/* 普通多行输出(\r\n 分行)+ 滚动:超屏的行进回滚缓冲,顺序不能乱。 */
func TestScreenScrollback(t *testing.T) {
	s := newTermScreen(40, 4)
	for i := 1; i <= 6; i++ {
		s.write([]byte("line-" + string(rune('0'+i)) + "\r\n"))
	}
	got := s.text(0)
	want := "line-1\nline-2\nline-3\nline-4\nline-5\nline-6"
	if got != want {
		t.Fatalf("回滚顺序错:\n got %q\nwant %q", got, want)
	}
	if tail := s.text(2); tail != "line-5\nline-6" {
		t.Fatalf("取末尾 2 行 = %q", tail)
	}
}

/* \r 原地覆写(进度条形态):同一行只留最后一次的样子。 */
func TestScreenCarriageReturnOverwrite(t *testing.T) {
	s := newTermScreen(40, 4)
	s.write([]byte("progress   0%\rprogress  50%\rprogress 100%\r\n"))
	if got := s.text(0); got != "progress 100%" {
		t.Fatalf("\\r 覆写后 = %q", got)
	}
}

/* 擦行(CSI K):提示符重绘前会先擦掉旧内容,不留残余。 */
func TestScreenEraseLine(t *testing.T) {
	s := newTermScreen(40, 4)
	s.write([]byte("C:\\long\\old\\prompt>abc"))
	s.write([]byte("\r\x1b[KC:\\new>")) // 回到行首、擦到行尾、重写短提示符
	if got := s.text(0); got != "C:\\new>" {
		t.Fatalf("擦行后 = %q", got)
	}
}

/* SGR(颜色)与模式序列要丢掉,不能混进文本。 */
func TestScreenSGRStripped(t *testing.T) {
	s := newTermScreen(40, 4)
	s.write([]byte("\x1b[1;32mOK\x1b[0m\x1b[?25l done\x1b[?25h"))
	if got := s.text(0); got != "OK done" {
		t.Fatalf("SGR/模式序列没剥干净 = %q", got)
	}
}

/* 一半的 UTF-8 rune 跨分片到达也不能坏。 */
func TestScreenSplitRune(t *testing.T) {
	s := newTermScreen(40, 4)
	b := []byte("中文")
	s.write(b[:2]) // 半个"中"
	s.write(b[2:])
	if got := s.text(0); got != "中文" {
		t.Fatalf("跨分片 rune = %q", got)
	}
}

/* 可视屏比对:send 靠它判断"这次命令从哪一行起有变化"。 */
func TestScreenVisibleDiff(t *testing.T) {
	s := newTermScreen(40, 6)
	s.write([]byte("old\r\nC:\\>"))
	before := s.visible()
	s.write([]byte("dir\r\nC:\\>dir 的列表\r\nC:\\>"))
	after := s.visible()
	k := 0
	for k < len(before) && k < len(after) && before[k] == after[k] {
		k++
	}
	got := strings.Join(after[k:], "\n")
	if !strings.Contains(got, "dir 的列表") {
		t.Fatalf("diff 没覆盖新输出, k=%d:\n%s", k, got)
	}
	if strings.Contains(got, "old") {
		t.Fatalf("diff 把旧内容也算进来了:\n%s", got)
	}
}

/*
提示符判据:末行以 > $ % # 收尾才算"命令做完了"(ping 这类 1 秒一跳的输出

	靠它才不会把"暂时没输出"当成结束)。
*/
func TestScreenLooksIdle(t *testing.T) {
	s := newTermScreen(40, 4)
	s.write([]byte("C:\\>ping -n 3 127.0.0.1\r\n\r\n来自 127.0.0.1 的回复: 字节=32\r\n"))
	if s.looksIdle() {
		t.Fatal("输出中途不该判成空闲")
	}
	s.write([]byte("\r\nC:\\>"))
	if !s.looksIdle() {
		t.Fatal("回到提示符后应判成空闲")
	}
}

/* 变矮时顶部行进回滚,内容不丢。 */
func TestScreenResizeShrink(t *testing.T) {
	s := newTermScreen(40, 6)
	s.write([]byte("a\r\nb\r\nc\r\nd\r\n"))
	s.resize(40, 2)
	got := s.text(0)
	for _, want := range []string{"a", "b", "c", "d"} {
		if !strings.Contains(got, want) {
			t.Fatalf("变矮后丢了 %q:\n%s", want, got)
		}
	}
}
