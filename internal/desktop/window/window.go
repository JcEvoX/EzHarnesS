/*
Package window 只管 Wails 窗口：无边框主窗口创建、首屏显示时序、
尺寸钳制、关闭拦截钩子与快应用子窗口。

  - 页面一律走本进程 gin 的真实网络地址（http://127.0.0.1:<port>）：
    不经 wails 资产桥——该桥在 Windows 上缓冲整个响应，SSE 等流式无法
    工作；走网络后桌面端与浏览器访问行为完全一致。
  - 无边框拖拽/双击最大化走 WebView2 原生非客户区支持
    （NonClientRegionSupport + 前端 CSS app-region: drag），无需 JS 注入。

不涉及托盘与程序坞：关闭后是隐藏还是退出由 OnClosing 回调的上层决定。
*/
package window

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type Config struct {
	Title  string
	URL    string
	Width  int
	Height int
	Start  time.Time  // 进程启动时刻（启动耗时日志用）
	Port   func() int // 当前服务端口（快应用子窗口 URL 用，换代后会变）
}

type Window struct {
	app *application.App
	win *application.WebviewWindow
	cfg Config

	// OnClosing 在主窗口即将关闭时调用；返回 true = 已接管（取消关闭）。
	// 须在 App.Run 之前设置。
	OnClosing func() bool
}

var subWinSeq atomic.Int64 // 子窗口命名序号

/* clampToScreen 把窗口尺寸按主屏工作区等比钳制，返回可用尺寸。 */
func clampToScreen(win *application.WebviewWindow, w, h int) (int, int) {
	sc, err := win.GetScreen()
	if err != nil || sc == nil {
		return w, h
	}
	wa := sc.WorkArea
	if w <= wa.Width && h <= wa.Height {
		return w, h
	}
	scale := math.Min(float64(wa.Width)/float64(w), float64(wa.Height)/float64(h))
	return int(float64(w) * scale), int(float64(h) * scale)
}

/* New 创建主窗口（Hidden 起步，页面加载完成后钳制尺寸并显示）。 */
func New(app *application.App, cfg Config) *Window {
	opts := application.WebviewWindowOptions{
		Name:      "main",
		Title:     cfg.Title,
		URL:       cfg.URL,
		Width:     cfg.Width,
		Height:    cfg.Height,
		Hidden:    true, // 尺寸钳制后再显示，避免超大窗口闪现
		Frameless: true,
		// 组合宿主 + 非客户区支持：前者让 WndProc 接入宿主命中路由
		// （边缘缩放 resizeBorderHitTest + app-region 拖拽命中），后者开启
		// WebView2 对 CSS app-region 的解析。二者缺一则边缘无法缩放。
		Windows: application.WindowsWindow{
			NonClientRegionSupport:     true,
			WebView2CompositionHosting: true,
		},
	}
	w := &Window{app: app, win: app.Window.NewWithOptions(opts), cfg: cfg}

	// Run 之前 Show() 是静默 no-op（窗口 impl 尚未创建），必须在应用
	// 启动后的事件里显示：页面加载完成 → 按屏幕工作区钳制尺寸 → Show
	// （仅首次导航生效，刷新不重触发）
	var shown atomic.Bool
	show := func() {
		if !shown.CompareAndSwap(false, true) {
			return
		}
		if cw, ch := clampToScreen(w.win, cfg.Width, cfg.Height); cw != cfg.Width || ch != cfg.Height {
			w.win.SetSize(cw, ch)
		}
		w.win.Show()
		log.Printf("ezharness 窗口已显示（启动后 %.1fs）", time.Since(cfg.Start).Seconds())
	}
	w.win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { show() })
	// NavigationCompleted 偶发丢失（WebView2 时序）会让 Hidden 窗口永远
	// 不显示：超时兜底，与事件路径经 CAS 幂等合流。若显示恒比事件
	// 晚 ~2s 即兜底触发（事件丢失），其余慢在 WebView2 初始化/页面加载
	time.AfterFunc(2*time.Second, show)

	w.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if w.OnClosing != nil && w.OnClosing() {
			e.Cancel()
		}
	})
	return w
}

func (w *Window) Show()             { w.win.Show() }
func (w *Window) Hide()             { w.win.Hide() }
func (w *Window) Focus()            { w.win.Focus() }
func (w *Window) IsVisible() bool   { return w.win.IsVisible() }
func (w *Window) Minimise()         { w.win.Minimise() }
func (w *Window) ToggleMaximise()   { w.win.ToggleMaximise() }
func (w *Window) IsMaximised() bool { return w.win.IsMaximised() }

/*
OpenAppWindow 为快应用开独立子窗口：页面走本进程 gin 直出的绝对 URL
（wails 资产域只服务主窗口相对路径）。带系统标题栏。path 为完整
http(s) URL 时直接加载（看板浏览器的"独立窗口"，绕开 iframe 内嵌限制）。
*/
func (w *Window) OpenAppWindow(path, title string) {
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = fmt.Sprintf("http://127.0.0.1:%d%s", w.cfg.Port(), path)
	}
	opts := application.WebviewWindowOptions{
		Name:   fmt.Sprintf("app-%d", subWinSeq.Add(1)),
		Title:  title,
		URL:    url,
		Width:  1100,
		Height: 720,
		Hidden: true,
	}
	sub := w.app.Window.NewWithOptions(opts)
	if cw, ch := clampToScreen(sub, opts.Width, opts.Height); cw != opts.Width || ch != opts.Height {
		sub.SetSize(cw, ch)
	}
	sub.Show()
}
