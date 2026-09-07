# Agent 装配：hooks / warps / 工具

一次会话（session）的运行时由 `internal/service/agent_service.go` 的 `Assemble` 装配：provider + model warp 链 + tool warp 链 + 静态工具 + hooks 数组。本文档是当前装配的完整清单。

## 一、引擎 hook 点（ezloop hook 包定义，7 个）

| hook 点 | 接口 | 触发时机 | 典型用途 |
|---|---|---|---|
| `OnStart` | StartHook | Run 组装期一次（用户输入已入史、startHooks 按数组顺序执行） | 注入工具、修理历史、插轮首消息 |
| `OnModelStart` | ModelStartHook | 每次调用模型前 | 改请求、可置 Stop 提前终止 |
| `OnModelEnd` | ModelEndHook | 每次模型调用后 | 记录用量 |
| `OnToolStart` | ToolStartHook | 每个工具调用前，返回 Action 可短路（Proceed / Skip(result) / Abort） | 审批、人机提问、工具拦截 |
| `OnToolEnd` | ToolEndHook | 工具调用后、结果入史前 | 结果改写（卸载/截断） |
| `OnLoop` | LoopHook | 每次迭代回边（工具批执行完 → 下次模型调用前） | 上下文整理、图片标记转换、配置热加载 |
| `OnEnd` | EndHook | 轮结束（引擎 defer 语义，成败/取消都跑） | 收尾记录、落盘 |

**并发契约**（ezloop 约定）：除 `OnToolStart` / `OnToolEnd` 在同轮多个工具调用之间并发外，其余 hook 点引擎串行调用。

一轮执行时序：

```
Run(input)
 ├─ runOpts（WithHistory / WithUploadFiles 附件路径入 Metadata）
 ├─ AppendMessage(input)                     ← 用户真实输入入史
 ├─ startHooks[].OnStart                     ← sys → contextfix → filetools → skilltool
 │                                              → status(插 agent_status) → uploadfile(插 upload_file)
 │                                              → approve → asker → task → mcp → …
 ├─ 迭代循环：
 │   ├─ 模型调用（model warp 链）
 │   │   ├─ OnModelStart（trace 等）
 │   │   ├─ provider（modeldump → modelretry → visionguard → 真实 API）
 │   │   └─ OnModelEnd
 │   ├─ 工具批（每个调用经 tool warp 链）
 │   │   ├─ OnToolStart（approve/skilltool/asker/task/trim 可短路）
 │   │   ├─ toolarg → limit → safetool → 工具本体
 │   │   └─ OnToolEnd（offload → guard → trace）
 │   └─ OnLoop（filetools 插图 → trim 水位 → mcp 热加载）
 └─ endHooks[].OnEnd（status → trace → endnote → sessionstore 落盘）
```

## 二、hooks 清单（装配顺序即执行顺序，16 个）

| # | hook | 来源 | hook 点 | 职责 |
|---|---|---|---|---|
| 1 | **sysprompt** | ezharness `hooks/sysprompt.go` | OnStart（必须首位） | system 唯一来源：base（人格/workspace/memory/skills/mcp）+ identity + compact summary |
| 2 | **contextfix** | ezloop `ext/hook/contextfix` | OnStart | 修理残缺历史（孤儿 tool_call 等协议不完整序列） |
| 3 | **filetools** | ezloop `ext/hook/filetools` | OnStart + **OnLoop** | OnStart 注册 read/write/edit/terminal + 注入系统环境段；OnLoop 把 read_file 图片标记转换为持久化 user 图片消息（见 context.md 第五节）；`WithImageHandler` 由 ezharness 注入（无视觉时返回 image_recognize 引导） |
| 4 | **skilltool** | ezharness `hooks/skilltool.go` | OnStart + OnToolStart | 注册 `load_skill`；OnToolStart 拦截执行：渲染 SKILL.md 指令集 + scripts 目录树作为工具结果（指令集免 offload 卸载） |
| 5 | **status** | ezharness `hooks/status.go` | OnStart + OnEnd | 轮首插 `<agent_status>` 快照（水位/资源变更，详见 context.md 第三节）+ 发 `status.snapshot` SSE；OnEnd 记 LastOutputAt（下轮"距上次输出"用） |
| 6 | **uploadfile** | ezharness `hooks/uploadfile.go` | OnStart | 带附件轮次在输入前插 `<upload_file>` 路径告知（详见 context.md 第四节） |
| 7 | **approve** | ezloop `ext/hook/approve` | OnStart + OnToolStart | 人机审批：按规则匹配工具调用（ezharness `needsApprove` 回调），挂起等决策（approve.request SSE）；拒绝即 Skip 短路 |
| 8 | **askuser** | ezloop `ext/hook/askuser` | OnStart + OnToolStart | `ask_user` 工具：模型向用户提问（askuser.request SSE），阻塞等回答（options/自由输入） |
| 9 | **task** | ezloop `ext/hook/task` | OnStart + OnToolStart | `task` 工具：fork 分身子循环（独立 LoopState，事件流 forkId 标记），answer 汇回主循环 |
| 10 | **mcp** | ezloop `ext/hook/mcp`（ezharness `service/mcp.go` 包装） | OnStart + OnLoop + OnEnd | 注册 `mcp_router`；OnLoop/OnEnd 配置热加载（mcp.json 变更即时生效，无需换 session） |
| 11 | **offload** | ezloop `ext/hook/offload` | OnToolEnd | >4096 字节工具结果卸载到 `.ezloop/offload/`，只留头部 512 字符 + 路径；豁免：ask_user/task/load_skill（后续行动依据）；read_file 是 ReplayTool（重放轮免卸载） |
| 12 | **guard** | ezharness `hooks/guard.go` | OnToolEnd | 窗口余量兜底：offload 豁免名单的大结果放不下时强制卸载，写入失败硬截断（须在 offload 之后） |
| 13 | **trim** | ezharness `hooks/trim.go` | OnLoop + OnToolStart | 上下文整理：OnLoop 水位自动（窗口 × TrimPercent%）；OnToolStart 拦模型主动 `trim_context`（登记 pending 返回 Skip，轮末执行）；就地截断 + `<context_trim>` marker（详见 context.md 第六节） |
| 14 | **trace** | ezharness `hooks/trace.go` | OnModelStart + OnModelEnd + OnToolStart + OnToolEnd + OnEnd | 调用链记录（trace 存档）：模型调用与工具执行的时序/耗时/参数，fork 前缀标记分身 |
| 15 | **endnote** | ezharness `hooks/endnote.go` | OnEnd | 轮末补 `<end_reason>`（轮次/时长/结束原因；compact 翻页自解释）；须在 sessionstore 落盘前 |
| 16 | **sessionstore** | ezharness `hooks/sessionstore.go`（`s.Sess`） | OnStart + OnEnd（最后） | 持久化：OnEnd 把 state.Messages/用量/资源基线落 session.json（内存与磁盘同源）；OnStart 恢复上下文衔接 |

**已实现未装配**：`hooks/recall.go`（recall_topic 话题回顾工具：查归档索引、按需加载旧 session 全文）——预留，不在 hooks 数组里。

## 三、warp 清单

warp 是节点装饰器（纵向封装，管节点内部；hook 是横向切面）。链式组装：**先注册的在外层**，调用依次经过 h1 → h2 → … → 节点。warp 实例 per-Run 独立（状态不跨 Run 共享）。

### 模型链（`core.WithModelWarp`，3 层）

| 层（外→内） | warp | 来源 | 职责 |
|---|---|---|---|
| 1 | **modeldump** | ezharness `warp/modeldump` | 每次模型调用把完整输入（Messages+Tools）打印控制台（调试上下文机制）；最外层，重试不重复打印 |
| 2 | **modelretry** | ezloop `ext/warp/model/modelretry` | 指数退避重试（默认 3 次，500ms 起翻倍 + 20% 抖动；流式已出 chunk 不重试）；发 `modelretry.retry` 事件 |
| 3 | **visionguard** | ezharness `warp/visionguard` | 无视觉模型的图片兜底：剥请求视图里全部 user Images、"已加载"文案改"已省略"（历史不动，换回多模态自动恢复）；最内层保证 retry 每次尝试都生效 |

### 工具链（`core.WithToolWarp`，3 层）

| 层（外→内） | warp | 来源 | 职责 |
|---|---|---|---|
| 1 | **toolarg** | ezharness `warp/toolarg` | `<@toolArg>路径</@toolArg>` 参数语法糖：执行前展开文件内容（200K 字符上限；入史参数保持原文） |
| 2 | **limit** | ezloop `ext/warp/tool/limit` | 同轮工具并发上限（4） |
| 3 | **safetool** | ezloop `ext/warp/tool/safetool` | panic 恢复为 error + 错误附带工具名（单个工具崩溃不炸 loop，错误回传模型自纠） |

## 四、工具清单（模型可见，16+1 个）

| 工具 | 提供方 | 说明 |
|---|---|---|
| `read_file` / `write_file` / `edit_file` / `terminal` | filetools hook | 文件三件套 + 原生终端（Windows cmd / POSIX sh）；read_file 图片走标记→OnLoop 图片消息 |
| `save_app` | ezharness `tools.SaveApp` | 生成快应用（html 落 apps/，可一键启动） |
| `term_start` / `term_send` / `term_read` / `term_list` / `term_close` | ezharness `tools.SharedTerm` | 共享终端（魔法看板）：与用户实时共见，多路复用 WS |
| `ask_user` | askuser hook | 向用户提问（选项/自由输入），阻塞等回答 |
| `task` | task hook | fork 分身执行子任务（独立上下文，answer 汇回） |
| `mcp_router` | mcp hook | 调用 mcp.json 配置的外部 MCP server 工具 |
| `trim_context` | trim hook | 模型主动整理上下文（登记 pending，轮末执行） |
| `load_skill` | skilltool hook | 加载技能完整指令集（SKILL.md + scripts 路径） |
| `image_recognize` | ezharness `tools.ImageRecognize` | 图片识别槽模型按路径识别图片（仅识别槽启用时装配） |

工具注册时序：`SetWarp`（tool warp 链）先于静态工具注册，两者都会被包装；hook 在 OnStart 里 `state.Tools.Register` 的工具同样过 warp 链（同名注册=覆盖）。

## 关键文件

`internal/service/agent_service.go`（装配中枢）、`ezloop/hook/hook.go`（hook 点接口与并发契约）、`ezloop/warp/warp.go`（装饰器链）、`ezloop/core/loop.go`（执行时序）
