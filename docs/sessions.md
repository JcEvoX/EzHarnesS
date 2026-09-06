# 树状 Session 管理

会话数据组织成一颗树：**节点是 session（一次换代内的完整对话库），线是用户视角的"会话"**。侧栏、路由、SSE 订阅都挂在线上，树的对用户只露两个操作：分叉（fork）与归档（compact）。

## 核心概念

| 概念 | 含义 | 载体 |
|------|------|------|
| session（节点） | 一次换代内的消息库 + 系统上下文 | `sessions/<id>/session.json` |
| 线（branch/topic） | 叶节点到根的 compress 链，用户视角的一个"会话" | `topics.json` 索引 |
| RootID | 线根 ID，稳定不变。前端路由 `:id`、SSE 订阅键、注册表键 | `Session.RootID` |
| 叶 ID | 当前叶 session ID，compact 换代时更新 | `TopicEntry.LeafID` |
| 向上边 | 节点到上一代的链接 `SnapEdge{TargetID, Anchor, SeedKind, ForkedFrom}` | session.json 内 |

SeedKind 三种：

- `new` —— 全新线（NewBranch）
- `fork` —— 从某消息处分叉出的拷贝（Fork）
- `compress` —— 归档换代（compact）产生的世代更替

## 三种"分叉"语义（勿混淆）

1. **fork 分叉（copy 语义）**：`TopicService.Fork` 复制源会话 `[0, anchor]` 前缀为完整副本开新线，system 与水位承源，`ForkOrigin` 仅作展示元数据。删源不伤分叉内容。
2. **fork 分身（task 子循环）**：ezloop `task.New()` 起的子 agent loop，事件带 `ForkID`，存档在 `sessions/<主ID>/forks/<forkID>/`（只存增量）。前端分身抽屉（ForkPanel）展示。
3. **compact 换代**：见下。

## compact（归档换代）

用户按键触发（TopicService.Compact），流程（`hooks.ArchiveSession`）：

1. 摘要模型生成前情摘要（2 分钟预算，期间持 `BeginArchive` 原子锁：锁发消息/切分支/防二次）
2. 旧库标记 `Archived=true` 封存，不动内容
3. 写新库：空 messages + 新 system（base 全量重载 + `<compact-summary>` 摘要段 + 旧库路径引用），compress 边指回旧库
4. 内存热切换：`sys.Set` 换 system、`sess.SetID` 换叶、话题线索引 `UpdateLeaf` 换叶不换线

与 trim 的分工：**trim 是模型侧上下文整理（就地折叠不换库），archive 是用户侧会话树管理（换代封存）**。

## 恢复与切换

- **启动**：`Hub.bootstrap()` 按 mtime 取最近未封存快照恢复；无则新建。
- **切换**：`TopicService.Switch` 先查注册表（`Hub.branches` map[rootID]*Session），命中直接激活——**后台分支的运行轮不取消**，线间可并发，切回时 `ReplayFrames` 重建现场。
- **删除**：清线上全部世代目录 + 索引 + 注册表；运行中拒绝；删活动线先自动开新线。

## 前端对应（store.svelte.ts）

- `activeId`（线根，稳定）/ `leafId`（当前叶）
- `branches = BranchView[]`：线索引 + Running/Waiting/Archiving/Active 运行态合成标志
- **上翻懒加载**：`loadPrev` 沿 compress 链一次拉一个旧世代，插入「⇪ 上下文已压缩归档」分割块；`canPrev` 后端判定（fork 换源后从源的上一级继续翻）
- Block 带 `owner`/`msgIdx`（所属 session + 下标）供分叉定位

## 关键文件

`internal/domain/session.go`（Session/Hub/注册表）、`internal/hooks/topics.go`（线索引）、`internal/hooks/sessionstore.go`（落盘/恢复/分身存档）、`internal/hooks/archive.go`（归档换代）、`internal/service/topic_service.go`（线操作用例）
