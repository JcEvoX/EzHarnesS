# ezharness 会话管理与上下文管理

> 版本：2026-08-30 · 描述当前实现。两大主题：**Session 管理**（会话树、话题归档、
> 分支切换）与**同一 session 内的上下文管理**（system 组装、每轮注入、按需加载、
> 窗口防线、trim 折叠）。取代 `contextdesign.md` 的过时描述。

---

## 第一部分：Session 管理

### 1.1 会话树模型

三种用户操作构成节点树：**new = 顶层根，archive = 沿 compress 边向下换代（纵深），fork = 挂在源会话的父下（兄弟位）**。每个叶子节点是一个可进入的分支。

```mermaid
flowchart TD
    R1["gen1（new，根）<br/>聊聊 Go 并发"]
    R2["gen2（compress）<br/>归档换代：摘要交接"] --> R1
    R3["gen3（compress）<br/>再次归档"] --> R2
    F1["fork-A（fork）<br/>从 gen2 第 3 条消息分叉"] -.兄弟位.-> R1
    N2["genN（new，另一条线）"]

    style R2 fill:#e8f0fe
    style R3 fill:#e8f0fe
    style F1 fill:#fef3e8
```

- **归档链**：gen2 的 `TargetID=gen1`、`SeedKind=compress`，新库 system 注入 `<compact-summary>` 摘要段。
- **fork 的父**：源会话是 compress 产物则挂其父下（与源同级）；源是根则 fork 也是顶层。fork 体内自带复制前缀（快照隔离）。
- **线的身份**：线根 ID（rootID）稳定——前端路由、SSE 订阅、分支注册表都用它；归档换代只换叶（LeafID），线不新增。

### 1.2 存储形态

```mermaid
flowchart LR
    T["topics.json<br/>线索引（可重建缓存）"] -- "LeafID 指向当前叶" --> S2
    subgraph sessions["sessions/ 目录（内容真相）"]
        S1["gen1/session.json<br/>Archived=true"]
        S2["gen2/session.json<br/>TargetID=gen1, SeedKind=compress"]
        S3["gen2/forks/task-1/<br/>fork 增量"]
    end
    S2 --- S3
```

- `session.json`：会话完整快照——消息**全量**（含 trim 折叠档案）、system 两段、用量水位、向上边、fork 摘要。
- `topics.json`：条目身份=线根 ID，记录 LeafID/Title/Msgs/Kind/Origin。`MigrateIndex` 可沿 compress 边从 sessions 全量重建（含 LineRoot 回填），索引丢失不丢数据。

### 1.3 一次用户输入的生命周期

```mermaid
sequenceDiagram
    participant U as 用户
    participant D as domain.Session
    participant E as ezloop 引擎
    participant S as sessionstore

    U->>D: Send(text)
    D->>D: StartRun：modelView() 过滤历史<br/>（ViewStart 起，见 2.6）
    D->>E: RunAsync(WithHistory(视图))
    loop 迭代（≤12）
        E->>E: 模型调用（system 注入 + 历史 + tools）
        E->>E: 工具执行（OnLoop 回边可触发 trim）
    end
    E->>S: OnEnd 落盘（MergeFull 全量合成）
    E->>D: FinishRun：history = 全量（渲染视图）
```

- **双视图**：`s.history` 全量（前端渲染）；`modelView()` 是发给模型的子集。
- 落盘（endHooks）先于 FinishRun，两处共用 `hooks.MergeFull`，与回调顺序无关。

### 1.4 归档（archive）

```mermaid
sequenceDiagram
    participant UI as 分支列表 ⇪ 按钮
    participant C as TopicService.Compact
    participant A as ArchiveSession
    participant FS as 文件系统

    UI->>C: POST /api/topics/compact {rootId}
    C->>C: BeginArchive（原子锁）
    Note over C: 摘要期间（≤2 分钟）锁：<br/>发消息、切分支、二次归档
    C->>A: History() 全量 + ModelView() 视图
    A->>A: 摘要 = summarize(视图)（链式浓缩）
    A->>FS: 旧库翻 Archived 位封存
    A->>FS: 新库初始快照（空 messages + rebuildBase<br/>+ compact-summary + compress 边）
    A->>A: sys 热更 / SetID / SetPrev / trace 换库
    A->>FS: topics.UpdateLeaf（同线换代）
    C->>C: RotateTo(newID)：清历史/水位
```

- 摘要输入用**模型视图**（marker 摘要链已覆盖更早内容，避免重复浓缩）。
- 新库先落盘再切内存——任一时刻重启都是可恢复形态。
- 归档即新 session：system base 全量重载（记忆/skill/mcp 变更此刻生效）。

---

## 第二部分：同一 session 的上下文管理

### 2.1 一次模型调用看到的上下文（解剖）

```mermaid
flowchart TB
    subgraph REQ["ModelRequest"]
        subgraph SYS["system（SysPrompt hook 每轮注入）"]
            B1["base：人格 + SystemExtra"] --- B2["<workspace> 目录与权限"]
            B2 --- B3["<memory> harness.md 全文"]
            B3 --- B4["<skills> 名称清单"]
            B4 --- B5["<mcp> server 清单"]
            B5 --- B6["<compact-summary> 摘要段<br/>（归档产物，可空）"]
        end
        subgraph MSGS["消息历史"]
            M0["<agent_status> 状态栏<br/>（每轮用户输入前注入）"]
            M0 --- M1["用户输入 / assistant / tool 对"]
            M1 --- M2["[trim marker + 保留段]<br/>（整理后，见 2.6）"]
            M2 --- M3["<end_reason> 轮次收尾<br/>（上一轮遗留）"]
        end
        TOOLS["tools 定义：文件/终端/save_app/<br/>trim_context / load_skill / recall_topic / mcp_router…"]
    end
```

### 2.2 system 的两段式与固定性

`SysPrompt`（`internal/hooks/sysprompt.go`）是 system 唯一来源：`base + summary` 两段。
**内容在 session 创建时组装一次，同 session 内不变**（模型对开局的认知稳定）；轮内变化走状态栏（2.4）。

| 块 | 内容 | 来源 | 变化时机 |
|----|------|------|----------|
| 人格 + SystemExtra | ezharness 定位 + 用户自定义补丁 | `buildSystemBase`（agent_service） | 设置变更 + 下个 session |
| `<workspace>` | 数据目录架构、读写权限、绝对路径 | 同上 | 目录迁移 + 下个 session |
| `<memory>` | 长期记忆索引 harness.md **全文** | `memory/longterm/harness.md` | agent 编辑文件后，下个 session 生效 |
| `<skills>` | 技能名称+描述清单 | `memory/skills/*/SKILL.md` | 新建/关闭技能后，下个 session |
| `<mcp>` | 启用 server 清单 | `mcp.json` | 配置变更后，下个 session |
| `<compact-summary>` | 上一会话归档摘要 | ArchiveSession 写入 | 归档时 |

重启恢复从快照还原两段，不重新组装（记忆/skill/mcp 变更等归档或新线才并入）。

### 2.3 按需加载的三层设计（不进上下文的上下文）

全量内容不塞 system，靠"清单 → 详情 → 资源"分层拉取：

```mermaid
flowchart LR
    subgraph L1["① 清单（system，开局可见）"]
        SK["<skills> 名称+描述"] & MCP["<mcp> server 名"] & MEM["<memory> harness.md 索引"]
    end
    subgraph L2["② 详情（工具按需取）"]
        LS["load_skill → SKILL.md 全文+目录"] & RT["recall_topic → 归档 session 全文"] & GR["grep/findstr → 主题记忆文件"]
    end
    subgraph L3["③ 资源（文件工具执行）"]
        SC["scripts/ 脚本"] & ARC["sessions/ 存档"]
    end
    SK --> LS --> SC
    MEM --> GR
    MCP --> RT --> ARC
```

- **skill 三层**：名称进 system（知道有什么）→ `load_skill` 取全文（知道怎么用）→ 文件工具跑 scripts（实际执行）。
- **记忆两层**：harness.md 索引全文进 system；主题记忆文件按需 grep。
- **归档回忆**：`recall_topic` 查看话题索引、加载旧 session 全文——归档不等于遗忘。

### 2.4 每轮动态注入

| 注入 | 时机 | 内容 | hook |
|------|------|------|------|
| `<agent_status>` | 每轮用户输入**前** | 当前时间、上下文水位、距上次输出、本轮资源变更（skill/mcp 增删）、超 70% 建议整理 | `status.go` |
| `<end_reason>` | 每轮结束（落盘前） | 轮次/时长/结束时间/原因分类 | `endnote.go` |
| tool-guide 段 | OnStart 幂等追加 | trim_context 等内部工具用法说明 | `trim.go` 等 |

system 每 session 固定，轮内资源变更靠状态栏传递——直到下个 session 才并入 system。

### 2.5 工具结果的窗口防线

结果进入消息历史前有两道防线（`internal/hooks/guard.go` + ezloop ext）：

```mermaid
flowchart LR
    R["工具结果"] --> O{"offload：<br/>超 4096 字节？"}
    O -->|是| OF["卸载到 .ezloop/offload/<br/>上下文留摘要+路径"]
    O -->|"否（含豁免：read_file 等）"| G{"Guard：<br/>窗口余量够吗？"}
    G -->|不够| GF["同样卸载；写失败硬截断<br/>（保会话优先于保内容）"]
    G -->|够| H["原样进消息历史"]
    OF --> H2["模型按路径回读（replay）"]
```

`contextfix`（ezloop ext）另负责修理残缺历史（孤儿 tool 消息等恢复场景）。

### 2.6 trim：消息历史维度的整理

system 固定 + 工具结果有防线后，历史本身仍会涨——trim 负责折叠：

```mermaid
flowchart TD
    A{"触发"} -->|"水位：PromptTokens > 窗口×trimPercent"| L
    A -->|"模型调 trim_context<br/>（OnToolStart 并发区）"| Q["锁内登记 pending，零 state 写"]
    Q --> L["OnLoop（引擎串行区）<br/>Skip 结果已入史"]
    L --> D["doTrim：摘要折叠段"]
    D --> E["截断为 [head, tail(≤4条，配对完整),<br/>marker 尾插 kept=N]"]
```

- **立即生效**：下一次模型调用即新上下文；session 身份不变（不换库、不动话题线）。
- **追加式档案**：折叠段经 `MergeFull`（不变量：`全量 = last 截到视图起点 + 本轮折叠段 + 当前消息`）保留在 session.json——渲染时间线可见 ✂️ 分割线，历史原文不丢。
- **恢复视图**：`ViewStart` 按最后 marker 的 `kept` 回溯保留段（clamp 不跨上一 marker）；`modelView()` 与落盘共用。
- fork 同样支持（折叠 SeedLen 之后的增量，seed 归主库）。

### 2.7 各机制的窗口分工

| 机制 | 层面 | 动作 | 生效 |
|------|------|------|------|
| offload / Guard | 单条工具结果 | 卸载到文件，留路径 | 即时 |
| trim | 消息历史 | 折叠早期消息为 marker 摘要 | 下次模型调用 |
| archive | 整个 session | 换库 + system 重载 + 摘要交接 | 用户触发 |

---

## 代码索引

| 关注点 | 位置 |
|--------|------|
| system 组装（base 各块） | `internal/service/agent_service.go` buildSystemBase |
| system 唯一来源（两段式） | `internal/hooks/sysprompt.go` |
| 状态栏 / 轮次收尾 | `internal/hooks/status.go` / `endnote.go` |
| skill 三层 / 记忆 / 归档回忆 | `internal/hooks/skilltool.go` / `memory.go` / `recall.go` |
| 窗口防线 | `internal/hooks/guard.go` + ezloop ext offload |
| trim（marker/ViewStart/MergeFull） | `internal/hooks/trim.go` |
| 归档核心 | `internal/hooks/archive.go` |
| 落盘 / 线索引 | `internal/hooks/sessionstore.go` / `topics.go` |
| 双视图 / 归档锁 | `internal/domain/session.go` |
| 归档用例 | `internal/service/topic_service.go` Compact |
