# ezharness

> 基于 [ezloop](https://github.com/xuanlv2002/ezloop) 内核的产品级 AI agent 桌面应用。
> Go 单二进制 + WebView2 原生窗口，也支持浏览器访问。

## 理念

- **harness 提供机制与管控，ezloop 提供引擎**：平台守事件流契约，产品负责把 agent 的每次输出、每个决策呈现给人。
- **全权限设备 agent**：启动即对本机全权（文件不限目录 + shell），在哪启动操作哪台设备；权限通过四档审批策略管控而非默认阉割。
- **文件夹即存储**：不用数据库。结构配置（`ezharness.json`：端口/数据目录）与配置记录（模型/设置/记忆等）分离，后者全部落在数据目录，结构随数据建模独立演进。
- **零配置可启动**：apiKey 为空照常起服务，UI 引导补配。
- **前端纯渲染**：一切数据与默认值由后端下发，前端不造数据、不含业务逻辑。

## 核心能力

| 模块 | 说明 |
|---|---|
| 对话 | agent 循环 + 工具调用（文件读写/终端/画板等），SSE 流式时间线，fork 分身并行 |
| 安全 | 四档审批策略（每次审批/黑名单/白名单/全部免审），名单按命令前缀/路径/工具名配置，即时生效 |
| 模型 | 四槽单选：main 主模型 / vision 图片识别兜底 / image 文生图 / audio 语音合成，各自带用量统计 |
| 记忆 | 三个文件夹：长期记忆（索引文件初始入上下文，其余按需检索）/ 能力记忆（skill）/ 话题记忆（会话存档） |
| 知识库 | 成体系文档库：上传或 agent 存入，自动索引 + 摘要，检索 |
| 快应用 | agent 生成的小工具（html 等）一键启动为子窗口 |
| MCP | 外部工具服务器：http/stdio，探活、启停、热加载 |
| 魔法画板 | 对象模型白板：标注截图、白板创作，所见即所得回填为附件发给 AI |

## 架构

单二进制，一进程三角色：

```
ezharness.exe
├─ gin server        前端静态资源 + /api/* + SSE 事件流 + /apps/*
├─ wails 窗口壳      无边框主窗口 + 快应用子窗口 + 托盘
└─ agent 引擎        ezloop core，随会话装配
```

- 后端 Go + gin，三层 MVC（controller → service → domain）；前端 Svelte 5 + Vite + TypeScript。
- 页面一律走本进程真实网络地址（不经 wails 资产桥），桌面窗口与浏览器行为完全一致。
- 通信 REST + SSE：事件流单向推送，决策（审批/回答）POST 回传。

## 构建

前置依赖：[Go](https://go.dev/dl/)、[Node.js](https://nodejs.org/)（npm）、wails3 CLI：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

构建（产物在 `build/dist/`，图标与版本信息自动嵌入 exe）：

```sh
wails3 task build        # 当前平台
wails3 task build GOOS=darwin GOARCH=arm64   # 交叉编译
wails3 package           # 出安装包（Windows NSIS / macOS .app）
```

## 快速启动

```sh
wails3 task run          # 构建产物运行（自动设 EZHARNESS_ROOT 指回项目根）
wails3 dev               # 开发模式：热重载 + 前端 Vite dev server
```

- 首次启动自动创建 `ezharness.json`（结构配置：端口/数据目录）与 `data/` 数据目录，零配置可用；到「模型」页填 apiKey 后即可对话。
- 手动运行 `build/dist/` 里的二进制时需设 `EZHARNESS_ROOT` 指向项目根，否则回落默认配置（端口 5260 + 空数据目录）。
- 纯 server 形态（无窗口，浏览器访问）：`EZHARNESS_NO_WINDOW=1` 启动后访问 `http://127.0.0.1:<port>`。

## 文档

- [docs/design.md](docs/design.md) — 产品与页面设计
- [docs/architecture.md](docs/architecture.md) — 程序架构
- [AGENTS.md](AGENTS.md) — 开发备忘与踩坑清单

## License

Apache-2.0
