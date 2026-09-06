# 单上下文管理

一条线同一时刻只有一个活跃上下文（system + 消息库视图）。本文档覆盖：system prompt 组装、trim 压缩、agent_status 轮首快照、事件流。

## system prompt 组装（hooks/sysprompt.go + agent_service.go）

两段式 + 身份块，每 session 创建时组装一次并固定（重启从快照还原不重组，skill/mcp/记忆变更下个 session 生效）：

- **base**：人格 + Settings.SystemExtra + `<workspace>` 目录架构 + `<memory>` 长期记忆索引（`memory/longterm/harness.md`）+ `<skills>` 清单 + `<mcp>` 清单
- **identity**：会话 ID + 存档绝对路径 —— trim 折叠后模型的回忆入口
- **summary**：compact 摘要段（换代时注入）

sysprompt hook 必须是 startHooks 首位：ezloop 契约 agent 构建后只读，compact 需运行中换 system。

## trim 压缩（hooks/trim.go）

两条触发路径，统一在 OnLoop 串行区执行：

- **水位自动**：PromptTokens > 窗口 × TrimPercent%（settings.json，默认 75，0 禁用）
- **模型主动**：`trim_context` 工具（OnToolStart 只登记 pending 返回 Skip，轮末执行）

执行：摘要折叠段（独立 2 分钟预算）→ 截断为 `[head, tail, marker]`（保留末 4 条；孤儿 tool 前移保证配对完整）→ 尾插 `<context_trim kept="N">` marker。fork 只折叠 SeedLen 之后的增量。

**追加式档案与不变量**：折叠段挂 `state.Metadata["trim_folded"]`；`ViewStart`（视图起点回溯）与 `MergeFull`（全量 = 已知全量截到视图起点 + 折叠段 + 当前消息）是 modelView、落盘、FinishRun 三处共用的同一套判断——保证「marker 前档案从未进过本轮视图的必须保留，跨轮覆盖不丢档」。

兜底：`hooks/guard.go` 按窗口余量动态卸载放不下的工具结果到 `.ezloop/offload/`（4096 字节以下豁免），写入失败硬截断。

## agent_status（hooks/status.go）

**不是状态机枚举，是逐轮注入的快照记录**：

- 每轮 OnStart 在用户输入前插一条 `<agent_status>` user 消息（system 每 session 固定，轮内资源变更靠这条告知模型）
- `StatusData{Now, SinceLastOutputMin, CtxTokens, CtxWindow, SuggestCompact, Changes[]}` 渲染为中文语义文本；同时发 `status.snapshot` SSE 事件供前端状态卡/水位条
- `Changes` 来自资源基线（skills/mcps/terms）diff，每会话独立持久化；终端变更细分新增（报来源 用户/AI）/已退出/已修改/已关闭
- 轮停止原因 `FriendlyStop` 映射中文分类（completed/cancelled/max_iterations/aborted/error）

## 事件流（domain/event.go）

- `Event{Type, Ts, Iter, ForkID, Data}` SSE 帧；`MapEvent` 把 ezloop 事件归一（tool_start/tool_end/model_end/approve.request/askuser.request/task.*/session.compact/session.trim/status.snapshot/model_chunk/...）
- 合成帧：`turn_end`（stopReason/usage）、`replay.sync`（SSE 建连首帧，前端权威复位 busy）
- **回放分层**：人机请求登记 `pending`（断线重放）；聚合帧进 `turnFrames`（轮进行中整轮回放）；`replayable` 名单排除高频增量与瞬态帧——**turn_end 不可回放**（防 busy 卡死的关键设计）
- 慢消费者丢帧，前端 turn_end 校正兜底；`model_end` 更新水位

## 关键文件

`internal/hooks/sysprompt.go`、`internal/hooks/trim.go`、`internal/hooks/summarize.go`、`internal/hooks/status.go`、`internal/hooks/guard.go`、`internal/domain/event.go`、`internal/domain/session.go`（ViewStart/MergeFull/FinishRun）
