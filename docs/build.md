# 构建方案设计

目标一句话：**一次构建产出一个自包含的 exe，放到哪里都能跑，数据就地在哪**。没有 dev server、没有热重载、没有环境变量开关——开发与发布走同一条路径。

## 单二进制形态

- `embed.go`：`//go:embed frontend/dist` 恒内嵌（无 build tag；dist 缺失 go build 直接报错），gin NoRoute 服务 SPA
- 纯 server 模式唯一例外：`EZHARNESS_NO_WINDOW=1` 不开窗口，浏览器访问

## 应用根与数据目录（internal/config/cfg.go）

两层分离，启动即确定：

| 层 | 文件 | 位置 |
|----|------|------|
| 结构配置 | `ezharness.json`（port/listen/dataDir/windowWidth/windowHeight） | 应用根 = **exe 所在目录** |
| 配置记录 | models.json / settings.json / toolRules.json / mcp.json | 数据目录 |
| 数据 | sessions/ topics.json memory/ workspace/ apps/ .ezloop/ | 数据目录 |

- 零配置可启动：缺失自动创建默认（端口 5260、监听 127.0.0.1、数据目录 `data/`）；损坏备份 .bak 重建
- 启动 `os.Chdir(DataDir)`：进程 cwd 即数据目录，ezloop hook 的相对路径自动落入
- 监听默认 127.0.0.1（不触发防火墙弹窗），设置页可与端口一起改

## 重启 = 换代（app.go）

设置页改端口/数据目录后进程内重启：收尾旧代运行轮落盘（OnEnd 才落盘，不等会丢整轮；SSE 永不 idle，优雅 Shutdown 必等满超时，故 `shutdownGeneration` 主动处理）→ chdir 新目录 → 重建 Hub/Router → 新端口。前端凭 boot 代际计数判断新服务就绪。

## 开发与发布（script/）

- **dev.bat**：`npm run build` → `go build -o build\dist\ezharness.exe .` → 直接运行。开发数据因此落在 `build/dist/`，与产品行为（数据在 exe 旁）完全一致
- **release.bat**：`wails3 task package` → 产物 `build\dist\ezharness.exe` + `bin\ezharness-amd64-installer.exe`

## 安装包（build/windows/）

- Taskfile 体系：根 Taskfile 派发 GOOS → build/windows/Taskfile（build:native = 前端构建 + generate:syso 图标与版本信息 → `go build -tags production -ldflags="-w -s -H windowsgui"`）
- **NSIS 默认 user 作用域**（装 `%LOCALAPPDATA%\Programs\ezharness`，无 UAC）：因「应用根 = exe 目录、数据就地生成」，Program Files 普通权限写不进去。per-machine 包：`wails3 task package INSTALL_SCOPE=machine`
- 安装组件「添加到 PATH」默认勾选（注册表 + WM_SETTINGCHANGE 广播）
- 元数据：`build/config.yml`（productIdentifier `com.ezharness.app`）→ info.json → syso，版权 (c) 2026, ezharness contributors

## 注意事项

- darwin/linux 交叉编译受 wails v3 beta CGO 限制，Windows 是当前一等平台
