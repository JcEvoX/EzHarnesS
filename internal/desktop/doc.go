/*
Package desktop 是桌面壳（Wails v3）的三个互不依赖的部件：

  - window.go：只管窗口（主窗口创建/首屏显示时序/尺寸钳制/关闭钩子/快应用子窗口）。
    页面一律走本进程 gin 的真实网络地址：不经 wails 资产桥（该桥在 Windows 上
    缓冲整个响应，SSE 无法工作）。无边框拖拽走 WebView2 原生非客户区支持。
  - tray.go：只管托盘（图标、提示、左键与菜单），行为全部以回调注入。
  - dock*.go：只管 macOS 程序坞图标显隐（NSApp 激活策略；非 macOS 空实现）。

部件之间不互相引用；组合规则（隐藏到托盘 = 隐藏窗口 + 收起程序坞）在装配层。
*/
package desktop
