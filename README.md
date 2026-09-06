# ezharness

> 开源的助手类 harness。核心理念：**为用户提供最简单的交互方案**。

## 理念

ezharness 认为一个 harness 只需做好三件事：

1. **上下文工程** —— 什么进上下文、何时折叠、如何让模型始终知道自己是谁、在哪、用户最近干了什么
2. **工具封装** —— 把系统能力（文件、终端、画板、MCP…）封装成安全可控、即取即用的工具
3. **产品交互方案设计** —— 人机协同的交互面：决策、通知、共享终端，对话之外的通道

一切设计都面向用户简化：零配置可启动、文件夹即存储、桌面窗口与浏览器同一份页面、agent 需要人时跨分支把通知送到眼前。

ezharness 与 [ezloop](https://github.com/xuanlv2002/ezloop) 分工：ezloop 是精简的 agent loop 框架，只负责核心循环；ezharness 在其上做**会话管理、上下文工程设计、工具封装、人机交互设计**。

## 快速开始（用户）

从 [Releases](https://github.com/xuanlv2002/ezharness/releases) 下载，两种方式任选其一（exe 在哪运行，配置与数据就在哪生成）：

**方式一：绿色版（下载 exe）**

1. 下载 `ezharness.exe`，放入任意文件夹（如 `D:\ezharness`）；
2. 将该文件夹加入 PATH 环境变量；
3. 任意终端输入 `ezharness` 启动。首次运行在同目录自动生成 `ezharness.json` 与 `data/`。

**方式二：安装包（下载 installer.exe）**

1. 下载安装包，双击运行；
2. 按引导选择安装目录，自动创建开始菜单与桌面快捷方式，可选「添加到 PATH」。

首次启动到「模型」页填 apiKey 后即可对话。

## 核心能力

| 模块 | 说明 |
|---|---|
| 会话 | 树状管理：多线并行、从任意消息分叉、上下文归档换代、分身并行子任务 |
| 对话 | SSE 流式时间线，跨分支全局通知，审批/询问决策卡 |
| 安全 | 四档审批策略（每次审批/黑名单/白名单/全部免审），即时生效 |
| 模型 | 四槽单选：main / vision / image / audio，各自带用量统计 |
| 记忆 | 长期记忆（索引入上下文，按需检索）/ 能力记忆（skill）/ 话题记忆（会话存档） |
| 终端 | 人机共享真实 shell：agent 干活你随时接管，你敲的命令 agent 知道 |
| 快应用 | agent 生成的小工具（html 等）一键启动为子窗口 |
| MCP | 外部工具服务器：http/stdio，探活、启停、热加载 |
| 魔法画板 | 对象模型白板：标注截图、白板创作，所见即所得回填为附件 |

## 开发

前置依赖：[Go](https://go.dev/dl/)、[Node.js](https://nodejs.org/)；ezloop 作为普通 Go 模块自动拉取。出安装包另需 wails3 CLI 与 NSIS：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

日常用两个脚本（[script/](script/)），从任意目录调用均可：

```sh
script\dev.bat           # 开发调试：npm run build -> go build -> 启动 exe
script\release.bat       # 发布：出 build\dist\ezharness.exe + bin\installer.exe
```

- 应用根 = exe 所在目录：首次启动自动创建 `ezharness.json` 与 `data/`，零配置可用；前端恒内嵌单二进制，无 dev server。
- 纯 server 形态（无窗口，浏览器访问）：`EZHARNESS_NO_WINDOW=1` 启动后访问 `http://127.0.0.1:<port>`。

## 架构

单二进制，一进程三角色：

```
ezharness.exe
├─ gin server        前端静态资源 + /api/* + SSE 事件流 + /apps/*
├─ wails 窗口壳      无边框主窗口 + 快应用子窗口 + 托盘
└─ agent 引擎        ezloop core，随会话装配
```

后端 Go + gin，三层 MVC（controller → service → domain）；前端 Svelte 5 + Vite + TypeScript；通信 REST + SSE。

## 文档

- [docs/sessions.md](docs/sessions.md) — 树状 session 管理
- [docs/context.md](docs/context.md) — 单上下文管理（trim / agent_status / 事件流）
- [docs/interaction.md](docs/interaction.md) — 以人为核心的人机交互设计
- [docs/build.md](docs/build.md) — 构建方案设计
- [AGENTS.md](AGENTS.md) — 开发备忘与踩坑清单

## License

Apache-2.0
