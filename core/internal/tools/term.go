/*
共享终端工具组(term_tab / term_action):AI 与用户共写魔法看板里的同一批
终端,全局共享——任何会话都能操作全部终端(个人助手语义)。

与 terminal(filetools 的独立进程一次性命令)的分工:terminal 是**唯一
阻塞**的执行工具(等到命令结束/超时);本组是持久、可见、可交互的共享
终端,term_action send **立即返回不等输出**。需要等待(下载/长任务)用
terminal 同步跑,不要往共享终端发 sleep 去"等"。

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

ctx 透传本轮上下文。返回统一为 termResult JSON:id/name/desc/origin/exited/lastCmd/output/note。
*/
type TermIO interface {
	/* NewTerm 新建终端(name 简短标识,desc 用途描述),返回终端状态 JSON */
	NewTerm(name, desc string) (string, error)
	/* SendTerm 发送命令/原始键入并立即返回(id 省略=最近使用的终端),不等输出 */
	SendTerm(ctx context.Context, id, command string) (string, error)
	/* ReadTerm 游标式读取新输出(读即消费) */
	ReadTerm(id string, chars int) (string, error)
	/* CloseTerm 关闭终端,返回 {"id":..,"closed":true} */
	CloseTerm(id string) (string, error)
	/* ListTermsJSON 终端清单(JSON 数组,条目含 id/name/desc/origin/exited/lastCmd) */
	ListTermsJSON() string
}

type tabArgs struct {
	Action string `json:"action" desc:"new=新建终端;list=列出全部终端;close=关闭终端"`
	Name   string `json:"name,omitempty" desc:"new:终端名称,简短标识(如 build-server、日志监控),term_tab list 与看板中展示"`
	Desc   string `json:"desc,omitempty" desc:"new:终端描述,详细说明用途(可选)"`
	TabID  string `json:"tabId,omitempty" desc:"close:要关闭的终端 id"`
}

type actionArgs struct {
	Action  string `json:"action" desc:"send=向终端发送命令/控制键(立即返回,不等输出);read=游标式读取新输出"`
	TabID   string `json:"tabId,omitempty" desc:"目标终端 id(term_tab 的 list 可查);省略=最近使用的终端"`
	Command string `json:"command,omitempty" desc:"send:要发送的命令或原始键入(\\u0003=Ctrl+C 中断当前命令;纯控制输入不会自动补回车)"`
	Chars   int    `json:"chars,omitempty" desc:"read:返回字符数上限,默认 4000,上限 20000"`
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
				"返回 JSON:{id,name,desc,origin,exited,lastCmd}(close 为 {id,closed})。",
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
			"对已存在的共享终端发命令或读输出。与用户看板是同一会话:输出对用户实时可见,cd/环境变量跨命令有效。"+
				"action=send 发送命令或原始键入(tabId 省略=最近使用的终端;\\u0003=Ctrl+C 中断当前命令,此时不自动补回车)"+
				"——**立即返回、不等待输出**;要拿结果稍后用 action=read 续读。需要等待的命令(下载/长任务)请改用 terminal 同步执行,"+
				"不要发 sleep 去等(send 不等,等于没等)。"+
				"action=read 游标式读取上次读取之后的新输出(读即消费,不重复)。"+
				"返回 JSON:{id,name,desc,origin,exited,lastCmd,output}(read 无新输出时 output=(无新输出))。",
			func(ctx context.Context, in *actionArgs) (string, error) {
				switch in.Action {
				case "send":
					return t.SendTerm(ctx, in.TabID, in.Command)
				case "read":
					return t.ReadTerm(in.TabID, in.Chars)
				}
				return "", fmt.Errorf("未知 action %q(send/read)", in.Action)
			}),
	}
}
