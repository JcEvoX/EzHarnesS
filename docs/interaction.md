# 以人为核心的人机交互设计

agent 的能力边界再大，落到用户手里的也只是几个交互面。ezharness 把人机协同交互工具**外置**——不塞进对话流里硬凑，而是做成独立的产品面，让用户在 agent 工作时能看、能改、能答。

## 设计原则

1. **对话不是唯一的通道**。决策、观察、操作各有专门的界面；对话流只承载自然语言。
2. **通知全局，数据不绑视图**。无论用户停在哪个分支哪个页面，agent 需要人时一定能找到人。
3. **外置工具是"共享"的**。终端、（未来的）浏览器是人和 agent 共用的同一个实例，不是 agent 私有的副本——用户手敲的命令 agent 看得见，agent 的操作用户也看得见。

## 决策链路（approve / ask）

- 后端按 `toolRules.json` 四档判定（ask/black/white/auto，人机与内部工具恒免审），悬置时登记 pending（断线可重放）
- 前端 **DecisionCard**（approve/ask 两型）嵌在时间线；已决卡移除，结果以工具卡徽标呈现；刷新后由 `decisions.jsonl` 重建
- 决策回传 `POST /api/sessions/:id/decisions/approve|answer`；轮已结束的过期决策丢弃

## 全局通知栏（NoticePanel）

- 3s 轮询 `GET /api/notifications` 汇总**所有分支**（含后台分支与分身）的未决请求，全量替换（服务端为唯一真相，内容不变不换引用防抖动）
- 通知内联可直接决策（乐观移除，下轮轮询自洽）；「查看」跳转：跨分支先切线，分身请求开分身抽屉定位决策卡，主时间线锚点滚动
- 顺序稳定：后端双重排序（分支内按时间降序 + 组间排序）

## 共享终端（当前唯一实现的外置协同工具）

设计定位：**人机共用同一个真实 shell**。agent 的 workDir 与终端一致，用户可以随时接管、演示、纠错。

- 后端 `service/terminal.go`：Windows ConPTY 全局公共池（跨会话共享），环形缓冲 256KB（WS 重连恢复快照）
- `readMark` 单游标：term_send/term_read 共用，读即消费——agent 拿到的是用户上次阅读位置之后的新输出
- 用户手动输入聚合（转义序列过滤）进入 agent_status 的 Changes，agent 每轮知道用户在终端干了什么
- 前端 `lib/term.ts`：**单条 WebSocket 多路复用全部终端**（帧带 id 路由，hello/terminals 帧同步清单，指数退避重连）；TerminalDrawer 收起仅滑出，WS 与 xterm 常驻保活

## 路线图

共享终端是第一员。同样的模式——真实实例 + 单游标消费 + 人机双写——后续扩展到：

- **浏览器**：人机共用同一个浏览器实例，用户看着 agent 操作页面，可随时接管
- 更多外置工具按此标准工程化（每个工具回答三个问题：人和 agent 各看到什么、写入如何仲裁、状态如何进入 agent_status）

## 桌面壳（window_wails.go）

- 无边框主窗口（WebView2 NonClientRegionSupport + CSS app-region 拖拽）；页面走本进程 gin 真实网络地址（不经 wails 资产桥，SSE 可用，桌面端与浏览器行为一致）
- 托盘常驻：左键切换显示，右键菜单
- 关闭拦截：实时读设置。CloseToTray 开 = 隐藏到托盘；关 = 前端页面 modal 询问（可勾选「以后最小化到托盘」持久化）；Alt+F4/任务栏走系统惯例直接退出
- 快应用子窗口：`/apps/*` 页面或外部 URL 开独立窗口

## 关键文件

`internal/service/terminal.go`、`frontend/src/lib/term.ts`、`frontend/src/components/TerminalDrawer.svelte`、`frontend/src/components/NoticePanel.svelte`、`frontend/src/components/DecisionCard.svelte`、`frontend/src/components/ForkPanel.svelte`、`window_wails.go`
