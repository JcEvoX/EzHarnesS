# 单上下文管理

一条线同一时刻只有一个活跃上下文（system + 消息库视图）。本文档覆盖：消息构成（谁往上下文里放了什么）、system prompt 组装、agent_status / upload_file 详解、附件与图片处理、trim 压缩、事件流。

## 一、上下文里有哪些消息、从哪来

### user 消息（5 种，只有 1 种是用户亲手写的）

| 种类 | 生成者 | 时机与位置 | 作用 |
|---|---|---|---|
| **用户真实输入** | 引擎 `loop.go`（`AppendMessage(input)`） | 每轮末尾 | 本轮指令（纯文本；附件不在消息里，见第五节） |
| `<agent_status>` | status hook（`internal/hooks/status.go`）`OnStart` | 每轮、插在真实输入**前** | 轮首状态快照：时间/水位/资源变更（详见第三节） |
| `<upload_file>` | uploadfile hook（`internal/hooks/uploadfile.go`）`OnStart` | 仅带附件的轮次、插在真实输入**前**（agent_status 之后） | 附件落盘路径告知（详见第四节） |
| `<end_reason>` | endnote hook（`internal/hooks/endnote.go`）`OnEnd` | 每轮结束追加尾部 | 轮次收尾记录：轮次/时长/结束时间/原因分类；compact 翻页后落新会话开头需自解释 |
| `<context_trim kept="N">` | trim hook（`internal/hooks/trim.go`） | 截断发生时尾插（就地生效） | 整理 marker：替代被折叠的早期消息，内含摘要文本 |

一轮完整序列示例（带附件）：

```
[被折叠的早期消息 ...] <context_trim kept="4">摘要…</context_trim>
[近几轮消息 ...]
<agent_status>…</agent_status>          ← status hook
<upload_file>…</upload_file>            ← uploadfile hook（无附件轮次没有这条）
看下我发的截图                            ← 用户真实输入
（assistant 正文 / tool_use …
 tool 结果 …）*
<end_reason>…</end_reason>              ← 轮末收尾
```

标题推导（`hooks.FirstUserTitle`）与 fork/归档的历史遍历均按标签识别跳过这 4 种系统注入消息。

### 其他角色

- **system**：每 session 固定（组装见下节）+ ezloop filetools 追加的系统环境段；轮内不变
- **assistant**：模型正文与 tool_use（`ToolCalls`）
- **tool**：工具结果（纯 string；图片不走消息内容，见第五节）

## 二、system prompt 组装（hooks/sysprompt.go + agent_service.go）

两段式 + 身份块，每 session 创建时组装一次并固定（重启从快照还原不重组，skill/mcp/记忆变更下个 session 生效）：

- **base**：人格 + Settings.SystemExtra + `<workspace>` 目录架构（含 tmp/ 附件暂存说明与 toolArg 语法糖）+ `<memory>` 长期记忆索引（`memory/longterm/harness.md`）+ `<skills>` 清单 + `<mcp>` 清单
- **identity**：会话 ID + 存档绝对路径 —— trim 折叠后模型的回忆入口
- **summary**：compact 摘要段（换代时注入）

sysprompt hook 必须是 startHooks 首位：ezloop 契约 agent 构建后只读，compact 需运行中换 system。

## 三、agent_status 详解（internal/hooks/status.go）

**不是状态机枚举，是逐轮注入的快照记录**——system 每 session 固定，轮内发生的资源变更（新增 skill/mcp、终端操作）靠这条告知模型。

### 数据结构（StatusData，同构两个出口）

```go
type StatusData struct {
    Now                string   `json:"now"`                // 当前时间
    SinceLastOutputMin int64    `json:"sinceLastOutputMin"` // 距上次输出分钟（0=无记录）
    CtxTokens          int      `json:"ctxTokens"`          // 最近一次调用 prompt tokens
    CtxWindow          int      `json:"ctxWindow"`          // 主模型窗口
    SuggestCompact     bool     `json:"suggestCompact"`     // 水位超 70%
    Changes            []string `json:"changes,omitempty"`  // 资源基线 diff
}
```

### 出口 1：模型侧（中文语义化文本，注入 user 消息）

`renderStatus` 渲染。行格式是**前后端契约**（改格式须同步 `store.svelte.ts` / `StatusTagCard.svelte` 的解析）：

```
<agent_status>
当前时间：2026-09-07 23:43
上下文水位：89000 / 128000 tokens（已超窗口 70%，建议调用 trim_context 整理上下文）
距上次输出：5 分钟
本轮资源变更：新增技能 pdf-export；终端 t1 已关闭；用户在终端 t2 执行：python serve.py
</agent_status>
```

- 水位行仅 `CtxWindow > 0` 时有；括号提示仅 `SuggestCompact` 时有
- "距上次输出"仅 >0 时有；"本轮资源变更"仅非空时有，多项以 `；` 分隔
- `Changes` 来自资源基线（skills/mcps/terms）diff，基线每会话持久化（重启可续）；终端变更细分新增（报来源 用户/AI）/已退出/已修改/已关闭

### 出口 2：前端侧（SSE `status.snapshot` 事件，Data 为 StatusData JSON）

随每轮 OnStart 发出。前端 `store.apply` 归约：更新右上角水位条与 `status.contextTokens`；仅异常时（suggestCompact 或 changes 非空）在时间线插 `status` 块（插到本轮 user 块之前）。

### 前端解析（两条路径）

- **实时**：SSE 事件直接给 JSON，无需解析文本
- **历史重建**（`buildBlocks`）：user 消息 content 含 `<agent_status>` → 旧格式先试 `parseStatus`（JSON.parse 标签内文本）；新格式按关键词识别——含"整理上下文"或"资源变更"才入时间线，普通轮次只进右上角。`StatusTagCard.svelte` 渲染时逐行 `startsWith` 前缀匹配 + 局部正则提取字段

## 四、upload_file 详解（internal/hooks/uploadfile.go）

仅带附件的轮次注入，格式固定（行前缀 `- ` 是前端解析契约）：

```
<upload_file>
用户本轮上传了以下文件（已保存到本地，可用 read_file 读取）：
- C:/…/workspace/tmp/att-20260907-234324-1-画板-1788795801055.png
- C:/…/workspace/tmp/att-20260907-234324-2-需求.docx
</upload_file>
```

- 路径一律**绝对路径**（正斜杠风格），模型可直接 read_file
- 无附件轮次零开销（不插消息）
- 前端历史重建：`buildBlocks` 用 `/^- (.+)$/gm` 取路径列表暂存，挂到**紧跟其后的真实 user 块**渲染附件 chips；实时路径由 send 响应 `files` 字段回填

## 五、附件与图片处理

### 原则

- **发送的文件不进上下文**：附件落盘 tmp/，进上下文的只有 `<upload_file>` 里的路径
- **read_file 读到的图片进上下文**：作为一条**持久化的 user 图片消息**存在——历史里有什么，上下文就是什么，无运行时推断

### 机制（全协议通用：user+Images 是三家 provider 共同支持的通道）

| 阶段 | 机制 |
|---|---|
| **发送** | 前端任意类型附件（≤8 个/单文件 base64 ≤20MB）POST → 后端落盘 `<工作目录>/tmp/att-<时间戳>-<序号>-<净化名>` → `<upload_file>` 告知路径。base64 **不**入会话历史 |
| **read_file 读图** | 魔数判定（jpeg/png/gif/webp；>8MB 拒载）。主模型开视觉 → 工具结果为机器标记 `<image_loaded path="…"/>`（ezloop filetools 内置格式）；未开视觉 → `WithImageHandler` 回调返回引导文案（可调 `image_recognize`；识别槽未启用则引导设置） |
| **OnLoop 转换** | filetools hook 的 OnLoop（迭代回边、下次模型调用前）把**本轮工具批**的标记（从尾部回扫 tool 消息、至第一条 assistant 止——批边界天然划定，免状态）转换为：tool 结果文本改为"已加载"说明 + **批末插入一条 user 图片消息** `{Content:"[图片已加载: 路径…]}", Images:[base64…]}`（批内多图合并）。消息随历史**落盘 session.json**。增量语义：assistant 边界之前的残留（取消轮落盘）不补偿（模型看到标记文本无害，重新 read_file 即可） |
| **visionguard 兜底** | `internal/warp/visionguard` 挂模型链最内层：主模型无视觉时每次实际请求（含 retry）剥掉请求视图里全部 user Images、"[图片已加载: …]"改写为"[图片已省略（当前模型可能已切换，不支持图片输入）：…]"。**只改请求副本，落盘不动**——换回多模态图片自动恢复 |
| **image_recognize** | 识别槽模型的独立工具：按路径识别任意图片返回文字描述（无视觉模型的替代通道） |

消息序列（多模态模型读图一轮）：

```
assistant: [tool_use read_file {path: tmp/att-…png}]
tool:      [图片已作为视觉内容加载，见相邻消息]
user:      [图片已加载: C:/…/att-…png]  ← Images 带 base64，随历史落盘
assistant: 这张图是…
```

anthropic 侧 tool_result 与相邻 user 聚合为同一条消息（协议合法）；openai 侧 tool 后跟 user 图片 parts（协议合法）。

### 由此得到的语义

| 场景 | 行为 |
|---|---|
| 重启应用 | 图片消息在历史里，provider 直接带图 |
| 换非多模态模型 | visionguard 剥请求视图里的图；模型看到"[图片已省略…路径]"文本 → 可 read_file → 得到 image_recognize 引导，闭环 |
| 换回多模态模型 | 历史未动，图片自动恢复可见 |
| trim 折叠 | 图片消息是普通 user 消息，折叠即退出上下文 |
| 前端渲染 | 图片消息走历史接口 `images` 字段，MessageItem 现有图片网格自动渲染，**前端零特判** |
| 旧会话 | 早期"发送直传"时期的历史 Images 同样被 visionguard 覆盖（此前无兜底会 400 的场景已修复） |

### toolArg 参数语法糖（internal/warp/toolarg）

工具参数字符串值里的 `<@toolArg>绝对路径</@toolArg>` 在**执行前**展开为文件内容（递归遍历 JSON 字符串值、Marshal 回填保证转义安全、单文件 200K 字符上限、读不到报错回传模型自纠）。只影响执行：入史参数与工具卡展示均为标签原文。

## 六、trim 压缩（hooks/trim.go）

两条触发路径，统一在 OnLoop 串行区执行：

- **水位自动**：PromptTokens > 窗口 × TrimPercent%（settings.json，默认 75，0 禁用）
- **模型主动**：`trim_context` 工具（OnToolStart 只登记 pending 返回 Skip，轮末执行）

执行：摘要折叠段（独立 2 分钟预算）→ 截断为 `[head, tail, marker]`（保留末 4 条；孤儿 tool 前移保证配对完整）→ 尾插 `<context_trim kept="N">` marker（内含摘要文本）。fork 只折叠 SeedLen 之后的增量。

**追加式档案与不变量**：折叠段挂 `state.Metadata["trim_folded"]`；`ViewStart`（视图起点回溯）与 `MergeFull`（全量 = 已知全量截到视图起点 + 折叠段 + 当前消息）是 modelView、落盘、FinishRun 三处共用的同一套判断——保证「marker 前档案从未进过本轮视图的必须保留，跨轮覆盖不丢档」。

兜底：`hooks/guard.go` 按窗口余量动态卸载放不下的工具结果到 `.ezloop/offload/`（4096 字节以下豁免），写入失败硬截断。

## 七、事件流（domain/event.go）

- `Event{Type, Ts, Iter, ForkID, Data}` SSE 帧；`MapEvent` 把 ezloop 事件归一（tool_start/tool_end/model_end/approve.request/askuser.request/task.*/session.compact/session.trim/status.snapshot/model_chunk/...）
- 合成帧：`turn_end`（stopReason/usage）、`replay.sync`（SSE 建连首帧，前端权威复位 busy）
- 轮停止原因 `FriendlyStop` 映射中文分类（completed/cancelled/max_iterations/aborted/error）
- **回放分层**：人机请求登记 `pending`（断线重放）；聚合帧进 `turnFrames`（轮进行中整轮回放）；`replayable` 名单排除高频增量与瞬态帧——**turn_end 不可回放**（防 busy 卡死的关键设计）
- 慢消费者丢帧，前端 turn_end 校正兜底；`model_end` 更新水位

## 关键文件

`internal/hooks/sysprompt.go`、`internal/hooks/status.go`、`internal/hooks/uploadfile.go`、`internal/hooks/endnote.go`、`internal/hooks/trim.go`、`internal/hooks/summarize.go`、`internal/hooks/guard.go`、`internal/warp/visionguard/visionguard.go`、`internal/warp/toolarg/toolarg.go`、`internal/domain/event.go`、`internal/domain/session.go`（ViewStart/MergeFull/FinishRun）、`ezloop/ext/hook/filetools`（read_file 图片分支 + OnLoop 插图）
