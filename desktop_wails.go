/*
desktop 是桌面壳的装配层（Wails v3，跨平台）：把窗口、托盘、程序坞
三个互不依赖的子模块（internal/desktop/{window,tray,dock}）接到一起，
并持有"退出意图"。各模块职责：

- window：窗口本身（创建/显示时序/关闭钩子/子窗口）
- tray：托盘图标与菜单，行为全部以回调注入
- dock：macOS 程序坞图标显隐

组合规则（仅在此处）：
  - 窗口可见时程序坞 + 托盘并存；隐藏到托盘 = 隐藏窗口 + 收起程序坞图标；
    从托盘恢复 = 先恢复程序坞图标再显示并聚焦窗口。
  - 关闭行为实时读设置：CloseToTray 开 = 隐藏到托盘不弹窗；关 = 前端
    关闭询问（页面 modal，勾选「以后最小化到托盘」即持久化）。Alt+F4/
    任务栏关闭走系统惯例直接退出。
*/
package main

import (
	_ "embed"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"ezharness/internal/desktop/dock"
	"ezharness/internal/desktop/tray"
	"ezharness/internal/desktop/window"
)

//go:embed assets/icon.png
var trayIcon []byte

/*
	openWindow 装配桌面壳并阻塞至应用退出；返回后调用方收尾 server。

URL 走本进程真实网络地址（start 已完成 listen 后才开窗，无竞态）。
*/
func openWindow(a *app) {
	wailsApp := application.New(application.Options{Name: "ezharness"})
	win := window.New(wailsApp, window.Config{
		Title:  "ezharness",
		URL:    fmt.Sprintf("http://127.0.0.1:%d/?desktop=1", a.cfg.Port),
		Width:  a.cfg.WindowW,
		Height: a.cfg.WindowH,
		Start:  appStart,
		Port:   func() int { return a.snapshot().Port },
	})

	hideToTray := func() {
		win.Hide()
		dock.SetVisible(false)
	}
	showMain := func() {
		dock.SetVisible(true)
		win.Show()
		win.Focus()
	}

	// quitting 是退出意图（托盘退出/前端确认退出）：Quit() 会触发关窗流程，
	// 若仍走 CloseToTray 拦截会把退出取消掉（这是"托盘退出退不出"的另一
	// 半根因）。标题栏 X 的询问弹窗在前端（页面 modal），Alt+F4/任务栏
	// 关闭走系统惯例直接退出。
	var quitting atomic.Bool
	// shutdown 退出流程：置 quitting 让关窗放行 → 同步收尾（取消运行轮并
	// 落盘；无轮时毫秒级）→ Quit 正常走关窗退出；Quit 卡死时超时强退兜底
	shutdown := func() {
		quitting.Store(true)
		a.stop()
		wailsApp.Quit()
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
	win.OnClosing = func() bool {
		if quitting.Load() || !a.hub.SettingsSnapshot().CloseToTray {
			return false
		}
		hideToTray()
		return true
	}

	a.winCtl.Set(windowControl{win: win, hideToTray: hideToTray, quit: shutdown})

	tray.New(wailsApp, tray.Options{
		Icon:     trayIcon,
		Tooltip:  "ezharness",
		OpenText: "打开 ezharness",
		QuitText: "退出",
		OnClick: func() {
			if win.IsVisible() {
				hideToTray()
			} else {
				showMain()
			}
		},
		OnOpen: showMain,
		OnQuit: func() { go shutdown() },
	})

	_ = wailsApp.Run() // 阻塞主线程；退出（关窗/托盘退出）后返回
}

/*
	windowControl 适配到 controller.WindowControl：窗口操作委托 window，

"最小化到托盘"与退出走装配层闭包（含程序坞联动）。
*/
type windowControl struct {
	win        *window.Window
	hideToTray func()
	quit       func()
}

func (c windowControl) Minimise()         { c.win.Minimise() }
func (c windowControl) ToggleMaximise()   { c.win.ToggleMaximise() }
func (c windowControl) IsMaximised() bool { return c.win.IsMaximised() }
func (c windowControl) Hide()             { c.hideToTray() }

/*
	RequestQuit 真退出（前端关闭询问确认后调用）：quitting 置位让关窗

钩子放行，再走完整收尾退出流程。
*/
func (c windowControl) RequestQuit() { go c.quit() }

func (c windowControl) OpenAppWindow(path, title string) { c.win.OpenAppWindow(path, title) }
