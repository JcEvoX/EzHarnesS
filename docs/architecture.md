# ezharness 程序架构

## 总览：单二进制，一进程三角色

```
ezharness.exe（单进程）
├─ gin server（goroutine，真实 TCP，默认 :5260）
│    ├─ 服务内嵌前端（frontend/dist，go:embed）
│    ├─ /api/* 全部业务接口
│    ├─ /api/sessions/:id/events —— SSE 事件流（流式）
│    └─ /apps/* —— 快应用静态页
├─ wails 窗口壳（主线程）
│    ├─ 主窗口：WebView2 直开 http://127.0.0.1:<port>/?desktop=1
│    ├─ 快应用子窗口：按需创建，直开 /apps/<name>
│    └─ 托盘（左键切换窗口，右键 打开/退出）
└─ agent 引擎（ezloop core，随会话装配）
```

关键定调（2026-08-26）：**页面一律走本进程真实网络地址，不经 wails 资产桥**。
wails 桥（Assets.Handler / 虚拟域 wails.localhost）在 Windows 上缓冲整个响应、
无法流式，SSE 必死；且 ezharness 本就常驻 gin（浏览器访问是功能），桥零收益。
wails 的职责收缩为纯窗口壳：无边框窗口、托盘、子窗口。

## 两种运行形态

| 形态 | 前端来源 | 说明 |
|------|----------|------|
| release（`make build`，-tags release） | gin 直出内嵌 dist | 窗口开 `http://127.0.0.1:<port>/?desktop=1` |
| dev（`make dev`） | Vite :5173 | 窗口开 Vite（proxy /api、/apps 到 5260），HMR |
| 浏览器访问 | 同上两者 | 无窗口壳，`?desktop=1` 缺省即浏览器形态 |

前端判定桌面形态：URL 带 `?desktop=1`（渲染自绘标题栏、快应用走子窗口）。

## 请求链路（桌面窗口与浏览器完全一致）

```
页面 fetch('/api/...')  ─┐
EventSource('/api/...') ─┼─→ HTTP(TCP) → gin → controller → service → domain
子窗口 /apps/x.html     ─┘
```

- 启动顺序：gin 完成 listen 之后才开窗口，无竞态（main.go）。
- SSE 是长连接，gin 日志在 handler 返回（连接结束）时才落一行，连接存续期不落日志。
- 窗口三键/最大化状态走 `/api/window/{min,max,state,close}`（controller.WindowController
  持 wails 窗口适配器，浏览器形态无窗口返回 503）。
- 快应用启动：`POST /api/apps/open {name}`（名单校验防路径穿越）→ 后端创建
  wails 子窗口（带系统标题栏，960x640，钳制到屏幕）；浏览器形态前端回落 window.open。
  成功一律 200+JSON（204 空 body 会被前端 post() 的 json 解析当失败）。

## 后端分层（gin + 三层 MVC）

- `controller/`：gin handler 只做绑定/校验/响应。
- `service/`：用例（agent/chat/session/topic/settings/memory/mcp/apps）。
- `domain/`：Session 聚合 + Hub + 事件帧，零 HTTP 依赖。
- `main.go` 只做装配；`app.go` 生命周期（换代重启）；`window_wails.go` wails 壳。
- 装配蓝本＝ezloop/examples/chat/main.go；ezloop 缺的 hook 能力在 internal/hooks 自建。
- 工具集＝ezloop filetools（read/write/edit/bash）+ 自建 save_app。

## 配置与数据（文件夹即存储，不用数据库）

- 应用根 `ezharness.json`：{port, dataDir, windowWidth/Height}（结构配置，损坏自动备份重建）。
- 数据目录（进程 cwd）＝一切记录与数据：models.json（主模型+能力槽）、settings.json、
  toolRules.json（审批策略）、mcp.json、sessions/、memory/ 三文件夹、topics.json、
  apps/（快应用 html）。
- 零配置可启动：apiKey 空照常起服务，UI 引导去模型页。

## 已知约束与踩坑备忘

- wails beta.12 Windows：`WebviewWindow.Show()` 在 `wailsApp.Run()` 之前是静默 no-op
  （impl 未创建）——窗口显示必须挂在启动后的事件（WebViewNavigationCompleted）里。
- 同机两个 ezharness 实例（同 exe）并发时，WebView2 控制器初始化可能 30s 超时
  （用户数据目录争抢）——测试多实例需用不同路径的 exe 副本。
- WebView2 里 `window.open` 相对路径会被丢给系统默认浏览器（无 NewWindowRequested 处理），
  桌面形态一律走后端子窗口。
- Windows 下 `go run .` 单文件会报 undefined（main 包多文件），一律 `go run .` 或 make 目标。
