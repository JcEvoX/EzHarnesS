# AGENTS.md — ezharness 开发备忘

变更时的连带检查清单与易踩的坑（按主题）。改动相关模块前先扫对应小节，
这里的每一条都是实际踩过的。

## agent_status（状态注入）变更清单

格式或文案改动牵一发动全身，以下全部要对齐：

- **生成**：`internal/hooks/status.go` 的 `renderStatus`（中文语义化文本：当前时间 /
  上下文水位 / 距上次输出 / 本轮资源变更）
- **历史重建识别**：`frontend/src/lib/store.svelte.ts` 的 `buildBlocks`——按关键词
  （`整理上下文` / `资源变更`）决定状态消息是否进时间线。**改 renderStatus 文案必须
  同步关键词**，否则消息被整个吞掉（建议整理被忽略）或普通轮次状态全量入时间线
- **状态卡渲染**：`frontend/src/components/StatusTagCard.svelte`——两条路径（旧 JSON
  `data` / 新中文文本 `raw`），新格式解析（当前时间/上下文水位/本轮资源变更行）要与
  renderStatus 文案严格一致，否则历史重建时全文铺开（已修过一次）
- **标题推导**：`FirstUserTitle` 按 `<agent_status>` tag 跳过系统记录
- **基线**：`ResSnapshot{Skills, Mcps, Terms}` 每会话独立持久化（sessionstore），
  资源清单对比 = 全局实时清单 vs 本会话基线

## 工具面增删/改名清单（term_run→term_send 的教训）

- `internal/service/agent_service.go`：**ToolNames 硬编码清单**；
  `needsApprove`/`matchRuleList` 的工具名匹配与命令词边界分支
- **工具实名以注册处为准**（如 ezloop filetools 的 `terminal`，tools.go:152），
  ToolNames 曾残留旧名 `bash` 导致右上角显示不存在 的工具——改名时
  grep 旧名逐一核对（settings.go LoadSettings 里的 bash→terminal 迁移是
  历史档兼容，属故意保留）
- `internal/domain/settings.go`：`DefaultToolRules` + `LoadSettings` 旧档迁移
  （参照 bash→terminal、term_run→term_send 的写法）。注意 **ToolRules 是全量覆盖
  语义**：用户档里没有的新工具默认 ask——不迁移就是能力倒退
- system prompt 工具指南（`buildSystemBase` 的 workspace 段文案）
- ezloop offload：`WithSkip` 名单（askuser / task / load_skill）
- 前端：grep 新旧工具名，确认 SettingsView / ToolGroup / api.ts 无硬编码残留

## skill 消费点（改 skill 相关须全查）

`skill.LoadDir` 共 **5 处**消费，每处都要按 `DisabledSkills` 过滤：
`settings_service.Config` / `agent_service.buildSystemBase` / `hooks/skilltool.go` /
`hooks/status.go` / `session_service.Status`。

- 技能身份用**目录名**（`hooks.SkillDirOf(s.Path)`），frontmatter name 仅作显示名——
  开关/删除按目录名定位，别混用
- skilltool/status 是 hook（不能 import service，会循环），禁用名单经闭包
  `func() []string` 注入

## 终端模块

- **readMark 单游标**：`term_send` 与 `term_read` 共用 `TermSession.readMark`，
  send 返回增量后必须推进，否则 read 会重复输出
- 终端**全局共享**（个人助手语义）；agent_status 的终端基线每会话独立——
  A 会话首轮见到 B 会话开的终端报"新增"是**设计**（各会话模型知悉全局水位）
- WS 帧结构（`wsTermSessionOut`）改动要同步 `frontend/src/lib/term.ts` 的 `TermInfo`
- `killTree(nil)` 会 panic——手工构造 TermSession 的测试场景需 nil 防护（已加）

## SSE 时序（断线重连的坑）

- **回放帧语义**：SSE 建连时后端回放整轮聚合帧（`ReplayFrames`，含 loop_start/
  model_end/tool 卡），但前端 blocks 里可能已有半轮内容——**所有回放帧的 apply
  必须幂等**。曾踩坑：loop_start 去重只看"最后一个块"，重连时尾部已是模型输出
  → user 块重复 push（消息显示两次）
- **turn_end 不可回放**（`replayable` 名单排除）：轮在断线窗口内结束则前端永远
  收不到 → busy 卡死。修复：建连首帧 `replay.sync {turnActive}`（前端权威复位
  busy / 截断本轮块再收重放）+ `Cancel` 无运行轮时补发合成 turn_end
- **改帧类型/事件协议时**检查三处：后端 `Publish`/`replayable` 名单（session.go）、
  `MapEvent`（event.go）、前端 `apply` 的对应 case（含幂等性）

## 模型 warp 链

链序（先注册 = 外层）：`modeldump → modelretry → [stripimage] → provider`。
- 模型层错误**必须**在 provider 装饰器里处理（引擎在模型节点失败即终止
  loop，hook 拦截不到）
- `stripimage`（internal/warp/stripimage）：主模型 `ModelEntry.Vision=false`
  时装配（配置驱动），请求前把全部图片**落盘到工作目录 images/** 并把
  正文替换为路径与引导（识别槽启用时引导 image_recognize，否则引导去
  设置启用）——防"一次带图失败、图片留历史、之后每轮 400 卡死"；替换
  直接改引擎消息（落盘自愈语义）。用户偏好显式配置而非 400 自适应降级
  （曾实现过 novision 自适应方案被否）

## 模型槽语义（四槽）

main 是唯一对话模型（Vision 开关决定图片进上下文还是落盘）；vision 槽
= 图片识别（启用 → 暴露 `image_recognize` 工具，service 层实时读槽配置
调 buildProvider，无需重建 agent）；image/audio 槽 = 预留（工具后续版本）。
改槽语义/工具暴露记得：ToolNames 动态清单、DefaultToolRules、ModelsView
槽文案三处同步。

## 构建/运行陷阱

- `go run .` 会把 exe 放 go-build 临时目录，config 按 exe 位置找不到
  ezharness.json → 回落默认端口 5260 + 空数据目录。**验证须 `go build -o xxx.exe .`
  后在 ezharness/ 目录下运行**
- 进程 CWD = 数据目录（启动时 chdir），data 下文件用相对路径直接操作
- ezloop 是本地 replace（`../ezloop`），能不动就不动
- `fs.FileSystem` 接口无删除能力：删目录用 `os.RemoveAll`（service 层有 os 先例）
- 验证链：`go build ./... && go vet ./... && go test ./...` +
  `cd frontend && npm run build`；起服务 `EZHARNESS_NO_WINDOW=1`（端口见
  ezharness.json，当前 5262），测完删自编译 exe

## 前端杂项

- API 错误形态是 `400: {"error":"xx"}`，展示给用户前用 errText 提取 error 字段
- 原生 `confirm()` 在 Wails WebView 里贴顶难看——确认弹窗用自制居中 modal
  （参考 MemoryView 的 skill 删除确认）
