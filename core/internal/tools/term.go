/*
共享终端工具组(term_tab / term_action):AI 与用户共写魔法看板里的同一批
终端,全局共享——任何会话都能操作全部终端(个人助手语义)。

与 terminal(filetools 的独立进程一次性命令)的分工:terminal 会**阻塞到
命令结束**(或超时),适合一次性的构建/下载;本组是持久、可见、可交互的
共享终端,term_action send 写完**等输出静默(最多 10 秒)就把这次命令的
输出带回**,长驻命令到点先返回、续读用 read。真要等一个长任务跑完,用
terminal,别往共享终端发 sleep 去"等"。

依赖倒置:tools 只依赖 TermIO 接口,由 service.TerminalService 实现
(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"
	"fmt"

	"github.com/xuanlv2002/ezloop/types"
)

/*
	TermIO 是共享终端服务的能力面(service.TerminalService 实现)。

ctx 透传本轮上下文。返回统一为 termResult JSON:id/name/desc/exited/output/note(清单另见 ListTermsJSON)。
*/
type TermIO interface {
	/* NewTerm 新建终端(name 简短标识,desc 用途描述),返回终端状态 JSON */
	NewTerm(name, desc string) (string, error)
	/* SendTerm 发送命令/原始键入(id 必填),等输出静默后把这次命令的输出一并返回 */
	SendTerm(ctx context.Context, id, command string) (string, error)
	/* ReadTerm 读终端末尾 lines 行(默认 100;命令回显与输出都在里面,非增量) */
	ReadTerm(id string, lines int) (string, error)
	/* CloseTerm 关闭终端,返回 {"id":..,"closed":true} */
	CloseTerm(id string) (string, error)
	/* ListTermsJSON 终端清单(JSON 数组,条目含 id/name/desc/origin/exited) */
	ListTermsJSON() string
}

type tabArgs struct {
	Action string `json:"action" desc:"new=新建终端;list=列出全部终端;close=关闭终端"`
	Name   string `json:"name,omitempty" desc:"new:终端名称,简短标识(如 build-server、日志监控),term_tab list 与看板中展示"`
	Desc   string `json:"desc,omitempty" desc:"new:终端描述,详细说明用途(可选)"`
	TabID  string `json:"tabId,omitempty" desc:"close:要关闭的终端 id"`
}

type actionArgs struct {
	Action  string `json:"action" desc:"send=发送命令/控制键并等输出静默返回结果;read=读取终端末尾 n 行"`
	TabID   string `json:"tabId" desc:"目标终端 id(term_tab 的 list 可查;必须显式指定,没有最近使用兜底)"`
	Command string `json:"command,omitempty" desc:"send:要发送的命令或原始键入(\\u0003=Ctrl+C 中断当前命令;纯控制输入不会自动补回车)"`
	Lines   int    `json:"lines,omitempty" desc:"read:返回末尾多少行,默认 100"`
}

/* SharedTerm 构造 term_tab / term_action(t 为 nil 返回 nil,测试装配可不注入)。 */
func SharedTerm(t TermIO) []types.Tool {
	if t == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("term_tab",
			"共享终端的标签管理(全局共享:任何会话都能操作全部终端,用户可在魔法看板实时看到并接管)。"+
				"action=new 新建终端(name 简短名称、desc 用途描述)并返回 id;action=list 列出全部终端"+
				"(用户手开的与 AI 新建的都在内,所有会话共享);action=close 关闭终端(tabId,结束 shell 进程树)。"+
				"返回 JSON:{id,name,desc,origin,exited}(list 为数组;close 为 {id,closed})。",
			func(_ context.Context, in *tabArgs) (string, error) {
				switch in.Action {
				case "new":
					return t.NewTerm(in.Name, in.Desc)
				case "list":
					return t.ListTermsJSON(), nil
				case "close":
					return t.CloseTerm(in.TabID)
				}
				return "", fmt.Errorf("未知 action %q(new/list/close)", in.Action)
			}),
		types.NewTool("term_action",
			"对已存在的共享终端发命令或读输出(tabId 必填,term_tab 的 list 可查)。与用户看板是同一会话:输出对用户实时可见,cd/环境变量跨命令有效。"+
				"action=send 发送命令(或原始键入:\\u0003=Ctrl+C 中断,此时不自动补回车)——**等输出静默后把这次命令的输出一并返回**(最多等 10 秒),"+
				"所以短命令一条调用就能拿到结果;命令还在跑(超时)会带说明,后续输出用 action=read 取。要等命令**跑完**的长任务(下载/构建)直接用 terminal;定时等待用 sleep 工具(别往这里发睡眠命令)。"+
				"action=read 读终端末尾 n 行(lines 默认 100)——命令回显和输出都在里面,就是肉眼看终端看到的那一块,适合看大段输出;"+
				"**不是增量**:重复读拿到的是同一份内容,读多少次都一样。"+
				"返回 JSON:{id,name,desc,exited,output,note}。output 是**终端屏幕还原后的多行文本**——命令回显、输出各占一行,提示符单独一行,跟人肉眼看终端一样。",
			func(ctx context.Context, in *actionArgs) (string, error) {
				switch in.Action {
				case "send":
					return t.SendTerm(ctx, in.TabID, in.Command)
				case "read":
					return t.ReadTerm(in.TabID, in.Lines)
				}
				return "", fmt.Errorf("未知 action %q(send/read)", in.Action)
			}),
	}
}
