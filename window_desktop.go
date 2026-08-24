//go:build !windows

/*
window 是桌面窗口壳（macOS/Linux）：系统原生 WebView（WKWebView /
webkit2gtk），经 webview_go 驱动，构建需 CGO 与本机工具链。
（无边框/托盘为 Windows 专属；此平台保持系统标题栏。）
*/
package main

import (
	webview "github.com/webview/webview_go"

	"ezharness/internal/config"
)

/* openWindow 打开主窗口并阻塞至窗口关闭。 */
func openWindow(url string, cfg config.Config, closeToTray bool) {
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("ezharness")
	w.SetSize(cfg.WindowW, cfg.WindowH, webview.HintNone)
	w.Navigate(url)
	w.Run()
}
