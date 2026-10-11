/*
termScreen 是 AI 侧看终端用的那块"屏幕"。

ConPTY 发出来的**不是**纯文本,而是屏幕 diff——实测(120x30):

	echo hello-term\r\n\x1b[?25lhello-term\x1b[7;1HC:\core>\x1b[?25h

命令回显和输出是文本,但新提示符是靠「光标定位到第 7 行第 1 列」摆过去的,
中间没有换行;进度条靠 \r 原地覆写;擦行/清屏靠 \x1b[K / \x1b[J。把这些转义
序列直接剥掉,提示符就会粘在输出尾巴上(路径、指令、结果糊成一行,分不清)。
所以这里按终端语义重放字节流:H 行可视屏 + 回滚缓冲,渲染成一行一行。

只做"还原肉眼所见"需要的子集:CUP/A-H/G/E/F 光标、K/J 擦除、SGR 与模式序列
忽略、OSC 跳过。前端看的是原始流(xterm.js 自己渲染),这块只服务 AI 的读写。
*/
package service

import (
	"strings"
	"unicode/utf8"
)

const (
	termScreenCols = 120 // pty 初始尺寸;前端 Resize 会覆盖
	termScreenRows = 30
	termScrollMax  = 500 // 回滚行上限(够 read 取默认 100 行还有富余)
)

type termScreen struct {
	cols, rows int
	vis        [][]rune // 可视屏,长度恒为 rows
	scroll     [][]rune // 回滚(最旧在前)
	r, c       int      // 光标(0 基)
	hold       []byte   // 被分片截断的半个 rune / 半条转义序列,留给下次 write
}

func newTermScreen(cols, rows int) *termScreen {
	s := &termScreen{}
	s.resize(cols, rows)
	return s
}

/* resize 屏尺寸变化:高度差在回滚缓冲与可视屏之间挪,光标夹回屏内。 */
func (s *termScreen) resize(cols, rows int) {
	if cols >= 2 {
		s.cols = cols
	} else if s.cols < 2 {
		s.cols = termScreenCols
	}
	if rows < 2 {
		rows = termScreenRows
	}
	if s.vis == nil {
		s.vis = make([][]rune, rows)
	} else if rows > len(s.vis) { // 变高:底部补空行
		for i := len(s.vis); i < rows; i++ {
			s.vis = append(s.vis, nil)
		}
	} else if rows < len(s.vis) { // 变矮:顶部挪进回滚
		drop := len(s.vis) - rows
		for i := 0; i < drop; i++ {
			s.pushScroll(s.vis[i])
		}
		s.vis = append([][]rune(nil), s.vis[drop:]...)
	}
	s.rows = len(s.vis)
	s.clamp()
}

func (s *termScreen) pushScroll(row []rune) {
	s.scroll = append(s.scroll, row)
	if len(s.scroll) > termScrollMax {
		s.scroll = s.scroll[len(s.scroll)-termScrollMax:]
	}
}

func (s *termScreen) clamp() {
	if s.r < 0 {
		s.r = 0
	}
	if s.r >= s.rows {
		s.r = s.rows - 1
	}
	if s.c < 0 {
		s.c = 0
	}
	if s.c >= s.cols {
		s.c = s.cols - 1
	}
}

/*
write 重放一段字节流。PTY 读取会从任意位置切开,所以半个 rune / 半条转义

	序列都要留到下次(丢掉就会在输出里留下乱码或把 `1H` 当正文)。
*/
func (s *termScreen) write(p []byte) {
	if len(s.hold) > 0 {
		p = append(append([]byte(nil), s.hold...), p...)
		s.hold = s.hold[:0]
	}
	for i := 0; i < len(p); {
		b := p[i]
		switch {
		case b == 0x1b:
			n, ok := s.escape(p[i:])
			if !ok {
				s.hold = append(s.hold, p[i:]...)
				return
			}
			i += n
		case b == '\n':
			s.lineDown()
			i++
		case b == '\r':
			s.c = 0
			i++
		case b == '\b':
			if s.c > 0 {
				s.c--
			}
			i++
		case b == '\t':
			s.c = (s.c/8 + 1) * 8
			s.clamp()
			i++
		case b < 0x20 || b == 0x7f: // 其余控制字符(响铃/垂直制表…)不影响文本
			i++
		default:
			if !utf8.FullRune(p[i:]) { // rune 被切开:等下一次
				s.hold = append(s.hold, p[i:]...)
				return
			}
			ch, size := utf8.DecodeRune(p[i:])
			if ch == utf8.RuneError && size <= 1 { // 真的非法字节
				i++
				continue
			}
			s.put(ch)
			i += size
		}
	}
}

/* put 在光标处写一个字符(超出右边界先换行)。 */
func (s *termScreen) put(ch rune) {
	if s.c >= s.cols {
		s.lineDown()
	}
	row := s.vis[s.r]
	for len(row) < s.c {
		row = append(row, ' ')
	}
	if len(row) == s.c {
		row = append(row, ch)
	} else {
		row[s.c] = ch // 覆写:行尾更长的旧内容留着(与终端一致)
	}
	s.vis[s.r] = row
	s.c++
}

/* lineDown 下移一行(已在末行则滚屏),回到行首——ConPTY 的换行都是 CRLF。 */
func (s *termScreen) lineDown() {
	s.c = 0
	if s.r < s.rows-1 {
		s.r++
		return
	}
	s.pushScroll(s.vis[0])
	copy(s.vis, s.vis[1:])
	s.vis[s.rows-1] = nil
}

/* escape 解析一个转义序列,返回(消费字节数, 是否完整);不完整的交给调用方暂存。 */
func (s *termScreen) escape(p []byte) (int, bool) {
	if len(p) < 2 {
		return 0, false
	}
	switch p[1] {
	case '[': // CSI:ESC [ 参数 中间字节 终止字节
		i := 2
		var params []byte
		for i < len(p) && isCSIParam(p[i]) {
			params = append(params, p[i])
			i++
		}
		for i < len(p) && p[i] >= 0x20 && p[i] <= 0x2f {
			i++ // 中间字节(空格 ! " 等)
		}
		if i >= len(p) {
			return 0, false
		}
		s.csi(string(params), p[i])
		return i + 1, true
	case ']': // OSC(窗口标题、ConPTY 通知):跳到 BEL 或 ST
		for i := 2; i < len(p); i++ {
			if p[i] == 0x07 {
				return i + 1, true
			}
			if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
				return i + 2, true
			}
		}
		return 0, false
	case '(', ')', '*', '+', '#': // 字符集/行属性选择:多带一个字节
		if len(p) >= 3 {
			return 3, true
		}
		return 0, false
	default:
		return 2, true // 单字符转义
	}
}

func isCSIParam(b byte) bool {
	return (b >= '0' && b <= '9') || b == ';' || b == '?' || b == '>' || b == '<' || b == '=' || b == ':'
}

/* csi 处理会影响文本布局的 CSI;颜色/模式/查询一律忽略。 */
func (s *termScreen) csi(params string, final byte) {
	if params != "" && (params[0] == '?' || params[0] == '>' || params[0] == '<' || params[0] == '=') {
		return // 模式设置/私有查询
	}
	n := parseCSIParams(params)
	arg := func(i, def int) int {
		if i < len(n) && n[i] > 0 {
			return n[i]
		}
		return def
	}
	switch final {
	case 'H', 'f': // 绝对定位(行;列,1 基)
		s.r, s.c = arg(0, 1)-1, arg(1, 1)-1
		s.clamp()
	case 'A':
		s.r -= arg(0, 1)
		s.clamp()
	case 'B':
		s.r += arg(0, 1)
		s.clamp()
	case 'C':
		s.c += arg(0, 1)
		s.clamp()
	case 'D':
		s.c -= arg(0, 1)
		s.clamp()
	case 'E':
		s.r += arg(0, 1)
		s.c = 0
		s.clamp()
	case 'F':
		s.r -= arg(0, 1)
		s.c = 0
		s.clamp()
	case 'G': // 只移动列
		s.c = arg(0, 1) - 1
		s.clamp()
	case 'K':
		s.eraseLine(arg(0, 0))
	case 'J':
		s.eraseScreen(arg(0, 0))
	}
}

func parseCSIParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v := 0
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				v = 0
				break
			}
			v = v*10 + int(p[i]-'0')
		}
		out = append(out, v)
	}
	return out
}

/* eraseLine 擦除当前行:0=光标到行尾,1=行首到光标,2=整行。 */
func (s *termScreen) eraseLine(mode int) {
	row := s.vis[s.r]
	switch mode {
	case 1:
		for i := 0; i < s.c && i < len(row); i++ {
			row[i] = ' '
		}
	case 2:
		row = nil
	default:
		if s.c < len(row) {
			row = row[:s.c]
		}
	}
	s.vis[s.r] = row
}

/* eraseScreen 擦除:int 0=光标后,1=光标前,2=整屏(不挪光标)。 */
func (s *termScreen) eraseScreen(mode int) {
	switch mode {
	case 2:
		for i := range s.vis {
			s.vis[i] = nil
		}
	case 1:
		for i := 0; i < s.r; i++ {
			s.vis[i] = nil
		}
		s.eraseLine(1)
	default:
		s.eraseLine(0)
		for i := s.r + 1; i < s.rows; i++ {
			s.vis[i] = nil
		}
	}
}

/* visible 当前可视屏各行(去行尾空白),send 用它比对"这次命令从哪行起有变化"。 */
func (s *termScreen) visible() []string {
	out := make([]string, len(s.vis))
	for i, row := range s.vis {
		out[i] = trimRow(row)
	}
	return out
}

/*
looksIdle 末行像不像"回到提示符"了(cmd 的 >、bash 的 $、zsh 的 %、root 的 #)。

send 的静默窗只有几百毫秒,而 ping 这类命令每隔 1 秒才吐一行——只看静默会把
"暂时没输出"误判成"命令做完了"。提示符是命令收尾最可靠的信号,用它兜底。
*/
func (s *termScreen) looksIdle() bool {
	for i := len(s.vis) - 1; i >= 0; i-- {
		line := strings.TrimSpace(trimRow(s.vis[i]))
		if line == "" {
			continue
		}
		last := line[len(line)-1]
		return last == '>' || last == '$' || last == '%' || last == '#'
	}
	return false
}

/* text 回滚 + 可视 渲染成文本;n>0 只要末尾 n 行。 */
func (s *termScreen) text(n int) string {
	lines := make([]string, 0, len(s.scroll)+len(s.vis))
	for _, row := range s.scroll {
		lines = append(lines, trimRow(row))
	}
	for _, row := range s.vis {
		lines = append(lines, trimRow(row))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func trimRow(row []rune) string {
	end := len(row)
	for end > 0 && (row[end-1] == ' ' || row[end-1] == 0) {
		end--
	}
	return string(row[:end])
}
