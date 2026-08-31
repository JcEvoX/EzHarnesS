/*
共享终端工具组(term_*):AI 与用户共写魔法看板里的同一批终端。
与 filetools 的 terminal(独立进程一次性命令)并存:需用户可见、交互式、
状态保留(长驻程序、跨命令 cd/环境变量)的场景用本组工具。
依赖倒置:tools 只依赖 TermIO 接口,由 service.TerminalService 实现
(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"
	"fmt"

	"github.com/xuanlv2002/ezloop/types"
)

/* TermIO 是共享终端服务的能力面(service.TerminalService 实现)。 */
type TermIO interface {
	/* RunIn 在终端执行命令并等输出静默;id 空则新建终端执行(保留,用户可接管) */
	RunIn(ctx context.Context, id, cmd string, quietMs, timeoutMs int) (string, error)
	/* ReadTail 读终端尾部输出(剥 ANSI 纯文本) */
	ReadTail(id string, chars int) string
	/* WriteAI 写原始键入(note 记入最近命令,便于清单展示) */
	WriteAI(id, note string, b []byte) error
	/* ListTermsJSON 终端清单(JSON 文本:id/名称/来源/状态/最近命令) */
	ListTermsJSON() string
}

type runArgs struct {
	Command   string `json:"command" desc:"要执行的 shell 命令"`
	TermID    string `json:"termId,omitempty" desc:"目标终端 id(term_list 查看)。省略=新建一个终端执行(推荐:命令对用户可见,终端保留,用户可接管续操作)"`
	QuietMs   int    `json:"quietMs,omitempty" desc:"输出静默多少毫秒后认为命令完成,默认 800"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"总等待上限毫秒,默认 30000,超时返回已得输出"`
}

type readArgs struct {
	TermID string `json:"termId,omitempty" desc:"目标终端 id,省略=最近使用的终端"`
	Chars  int    `json:"chars,omitempty" desc:"返回尾部字符数,默认 4000,上限 20000"`
}

type writeArgs struct {
	TermID string `json:"termId,omitempty" desc:"目标终端 id,省略=最近使用的终端"`
	Data   string `json:"data" desc:"要写入的原始键入,可含控制字符(如 \\u0003=Ctrl+C、方向键转义序列)"`
	Enter  bool   `json:"enter,omitempty" desc:"写入后追加回车(提交应答/执行输入行)"`
}

type interruptArgs struct {
	TermID string `json:"termId,omitempty" desc:"目标终端 id,省略=最近使用的终端"`
}

/* SharedTerm 构造 term_* 工具组(t 为 nil 返回 nil,测试装配可不注入)。 */
func SharedTerm(t TermIO) []types.Tool {
	if t == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("term_run",
			"在共享终端执行命令并等待输出静默后返回。与用户看板终端是同一会话:命令与输出对用户实时可见,"+
				"支持交互式程序与状态保留(cd/环境变量跨命令有效,长驻程序不阻塞)。termId 省略时新建一个终端执行,"+
				"终端会保留,用户可在看板接管;一次性无状态命令优先用 terminal 工具(更快)。返回首行含终端 id,续操作时用 termId 指定。",
			func(ctx context.Context, in *runArgs) (string, error) {
				return t.RunIn(ctx, in.TermID, in.Command, in.QuietMs, in.TimeoutMs)
			}),
		types.NewTool("term_list",
			"列出当前全部共享终端(id、名称、创建来源、运行状态、最近命令)。用户手动建的与 AI 新建的都在内。",
			func(ctx context.Context, _ *struct{}) (string, error) {
				return t.ListTermsJSON(), nil
			}),
		types.NewTool("term_read",
			"读取共享终端最近的输出(剥除颜色等控制序列的纯文本尾部)。命令仍在输出(如 tail -f、构建日志)或 term_run 超时后用本工具续读。",
			func(ctx context.Context, in *readArgs) (string, error) {
				return t.ReadTail(in.TermID, in.Chars), nil
			}),
		types.NewTool("term_write",
			"向共享终端写入原始键入:应答交互式程序(密码确认、分页器 y/n、菜单选择)、向 REPL 输入代码等。"+
				"回车提交用 enter=true;发送 Ctrl+C 用 term_interrupt 更直观。",
			func(ctx context.Context, in *writeArgs) (string, error) {
				data := []byte(in.Data)
				if in.Enter {
					data = append(data, '\r')
				}
				note := in.Data
				if r := []rune(note); len(r) > 40 {
					note = string(r[:40])
				}
				if err := t.WriteAI(in.TermID, note, data); err != nil {
					return "", err
				}
				return fmt.Sprintf("written %d bytes", len(data)), nil
			}),
		types.NewTool("term_interrupt",
			"向共享终端发送 Ctrl+C(中断当前命令/交互程序)。",
			func(ctx context.Context, in *interruptArgs) (string, error) {
				if err := t.WriteAI(in.TermID, "^C", []byte{0x03}); err != nil {
					return "", err
				}
				return "interrupt sent", nil
			}),
	}
}
