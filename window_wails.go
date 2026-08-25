/*
window 是桌面窗口壳（Wails v3，跨平台）：无边框窗口 + 系统托盘 +
关闭最小化到托盘。

- 前端由 wails 资产服务器服务：Assets.Handler 直通当前 gin engine
  （同进程同源直调，无代理层，SSE 流式直通）；页面经 /api/window/*
  控制三键。dev 模式不嵌前端，窗口直开 Vite dev server。
- 无边框拖拽/双击最大化走 WebView2 原生非客户区支持
  （NonClientRegionSupport + 前端 CSS app-region: drag），无需 JS 注入。
- 托盘常驻：左键切换窗口显示，右键菜单（打开/退出）。
- 关闭行为实时读设置：CloseToTray 开 = 隐藏到托盘，关 = 正常退出。
*/
package main

import (
	_ "embed"
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"ezharness/internal/domain"
	"ezharness/internal/osfs"
)

//go:embed assets/icon.png
var trayIcon []byte

// devURL dev 模式前端在 Vite dev server（proxy /api 到本进程）。
const devURL = "http://localhost:5173/?desktop=1"

/* openWindow 打开主窗口并阻塞至应用退出；返回后调用方收尾 server。 */
func openWindow(a *app) {
	appOpts := application.Options{Name: "ezharness"}
	opts := application.WebviewWindowOptions{
		Name:      "main",
		Title:     "ezharness",
		Width:     a.cfg.WindowW,
		Height:    a.cfg.WindowH,
		Frameless: true,
		Windows:   application.WindowsWindow{NonClientRegionSupport: true},
	}
	if distFS() != nil { // release：前端由 wails 资产服务器服务（直通 gin）
		appOpts.Assets = application.AssetOptions{Handler: http.HandlerFunc(a.serveHTTP)}
		opts.URL = "/?desktop=1"
	} else { // dev：前端在 Vite dev server
		opts.URL = devURL
	}
	wailsApp := application.New(appOpts)
	win := wailsApp.Window.NewWithOptions(opts)

	// 关闭拦截：实时读设置决定隐藏或放行（CloseToTray 运行时生效）
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if domain.LoadSettings(osfs.OS{}).CloseToTray {
			win.Hide()
			e.Cancel()
		}
	})
	a.winCtl.Set(wailsWindow{win})

	tray := wailsApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("ezharness")
	tray.OnClick(func() {
		if win.IsVisible() {
			win.Hide()
		} else {
			win.Show()
			win.Focus()
		}
	})
	menu := application.NewMenu()
	menu.Add("打开 ezharness").OnClick(func(*application.Context) {
		win.Show()
		win.Focus()
	})
	menu.Add("退出").OnClick(func(*application.Context) { wailsApp.Quit() })
	tray.SetMenu(menu)

	_ = wailsApp.Run() // 阻塞主线程；退出（关窗/托盘退出）后返回
}

/* wailsWindow 适配 wails Window 到 controller.WindowControl（剥掉返回值）。 */
type wailsWindow struct{ w *application.WebviewWindow }

func (a wailsWindow) Minimise()         { a.w.Minimise() }
func (a wailsWindow) ToggleMaximise()   { a.w.ToggleMaximise() }
func (a wailsWindow) IsMaximised() bool { return a.w.IsMaximised() }
func (a wailsWindow) Close()            { a.w.Close() }
