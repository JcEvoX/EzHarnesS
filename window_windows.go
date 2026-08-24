//go:build windows

/*
window 是桌面窗口壳（Windows）：go-webview2 纯 syscall 驱动 WebView2，
无需 CGO——本机 go build 即可出包（Win11 自带 WebView2 运行时）。

无边框 + 自绘标题栏 + 托盘：

- 子类化主窗口过程：WM_NCCALCSIZE 返回 0 让客户区占满窗口（系统标题栏
  消失），保留 WS_THICKFRAME；边缘缩放由前端 mousemove 检测（WebView2
  子窗口盖住客户区，主窗口收不到 NCHITTEST），mousedown 时经 Bind 发
  WM_NCLBUTTONDOWN+HT* 交给系统模态循环；WM_GETMINMAXINFO 修正最大化
  范围（无边框窗口最大化默认会溢出屏幕一个边框宽）。
- 前端经 Bind 注入的 window.win_* 函数控制窗口：拖拽 =
  WM_NCLBUTTONDOWN+HTCAPTION；关闭按配置（closeToTray）隐藏或退出。
- 托盘：独立消息窗口（自有 WndProc + GetMessage 循环）挂
  Shell_NotifyIcon，左键/双击恢复主窗口，右键菜单（打开/退出）。
*/
package main

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"

	"ezharness/internal/config"
)

/* ── Win32 常量 ── */
const (
	swHide      = 0
	swShow      = 5
	swRestore   = 9
	swMaximize  = 3
	swMinimize  = 6
	swpNoSize   = 0x0001
	swpNoMove   = 0x0002
	swpFrameChg = 0x0020

	// GWL/GWLP 索引是负数，以 uintptr 补码形式表示（^n = -(n+1)）
	gwlStyle     = ^uintptr(15) // -16
	gwlpWndproc  = ^uintptr(3)  // -4
	gwlpUserdata = ^uintptr(20) // -21

	wsCaption = 0x00C00000

	wmClose  = 0x0010
	wmApp    = 0x8000
	wmCommand = 0x0111

	htCaption = 2
	htLeft    = 10
	htRight   = 11
	htTop     = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2
	nifMessage = 1
	nifIcon    = 2
	nifTip     = 4
	nifInfo    = 0x10

	wmLbuttonup     = 0x0202
	wmLbuttondblclk = 0x0203
	wmRbuttonup     = 0x0205

	mfString     = 0
	tpmRightBtn  = 2
	tpmRetTrue   = 0x0004

	idiApplication = 32512
	monDefault     = 0
	hwndMessage    = ^uintptr(2) // -3
)

/* ── Win32 结构 ── */
type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type minmaxInfo struct {
	PtReserved, PtMaxSize, PtMaxPosition, PtMinTrackSize, PtMaxTrackSize point
}

type monitorInfo struct {
	CbSize   uint32
	RcMon    rect
	RcWork   rect
	DwFlags  uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	WndProc       uintptr
	ClsExtra      int32
	WndExtra      int32
	Instance      syscall.Handle
	Icon          syscall.Handle
	Cursor        syscall.Handle
	Background    syscall.Handle
	MenuName      *uint16
	ClassName     *uint16
	IconSm        syscall.Handle
}

type msgStruct struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [256]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     syscall.Handle
}

/* ── Win32 调用 ── */
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pDefWindowProc     = user32.NewProc("DefWindowProcW")
	pCallWindowProc    = user32.NewProc("CallWindowProcW")
	pGetWindowLongPtr  = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtr  = user32.NewProc("SetWindowLongPtrW")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pShowWindow        = user32.NewProc("ShowWindow")
	pIsZoomed          = user32.NewProc("IsZoomed")
	pReleaseCapture    = user32.NewProc("ReleaseCapture")
	pSendMessage       = user32.NewProc("SendMessageW")
	pPostMessage       = user32.NewProc("PostMessageW")
	pMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfo    = user32.NewProc("GetMonitorInfoW")
	pGetWindowRect     = user32.NewProc("GetWindowRect")
	pRegisterClassEx   = user32.NewProc("RegisterClassExW")
	pCreateWindowEx    = user32.NewProc("CreateWindowExW")
	pGetMessage        = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessage   = user32.NewProc("DispatchMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pSetForeground     = user32.NewProc("SetForegroundWindow")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenu        = user32.NewProc("AppendMenuW")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pDestroyMenu       = user32.NewProc("DestroyMenu")
	pLoadIcon          = user32.NewProc("LoadIconW")
	pGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
	pSetProp           = user32.NewProc("SetPropW")
	pGetProp           = user32.NewProc("GetPropW")
	pRemoveProp        = user32.NewProc("RemovePropW")
	pShellNotifyIcon   = shell32.NewProc("Shell_NotifyIconW")
)

func call(p *syscall.LazyProc, a ...uintptr) uintptr {
	r, _, _ := p.Call(a...)
	return r
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return (*uint16)(unsafe.Pointer(p))
}

func loWord(v uintptr) uint32 { return uint32(v) & 0xFFFF }

/* ── 托盘 ── */

type tray struct {
	main  syscall.Handle
	hwnd  syscall.Handle
	icon  syscall.Handle
	added bool
	ready chan struct{}
}

/* newTray 在独立线程建消息窗口并挂托盘图标（阻塞至就绪）。 */
func newTray(mainHwnd syscall.Handle) *tray {
	t := &tray{main: mainHwnd, ready: make(chan struct{})}
	go t.loop()
	<-t.ready
	return t
}

func (t *tray) loop() {
	runtime.LockOSThread()
	proc := syscall.NewCallback(trayWndProc)
	inst := call(pGetModuleHandle, 0)
	wc := wndClassEx{
		CbSize:    uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   proc,
		Instance:  syscall.Handle(inst),
		ClassName: utf16("ezharness-tray"),
	}
	call(pRegisterClassEx, uintptr(unsafe.Pointer(&wc)))
	t.hwnd = syscall.Handle(call(pCreateWindowEx, 0,
		uintptr(unsafe.Pointer(wc.ClassName)), uintptr(unsafe.Pointer(wc.ClassName)),
		0, 0, 0, 0, 0, uintptr(hwndMessage), inst, 0, 0))
	call(pSetWindowLongPtr, uintptr(t.hwnd), gwlpUserdata, uintptr(unsafe.Pointer(t)))
	t.icon = syscall.Handle(call(pLoadIcon, 0, uintptr(idiApplication)))
	t.add()
	close(t.ready)

	var m msgStruct
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		call(pTranslateMessage, uintptr(unsafe.Pointer(&m)))
		call(pDispatchMessage, uintptr(unsafe.Pointer(&m)))
	}
}

func (t *tray) add() {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmApp + 1
	nid.HIcon = t.icon
	tip, _ := syscall.UTF16FromString("ezharness")
	copy(nid.SzTip[:], tip)
	call(pShellNotifyIcon, uintptr(nimAdd), uintptr(unsafe.Pointer(&nid)))
	t.added = true
}

/* balloon 弹托盘气泡通知（隐藏窗口后告知用户去哪找回）。 */
func (t *tray) balloon(title, text string) {
	if !t.added {
		return
	}
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UFlags = nifInfo
	titleU, _ := syscall.UTF16FromString(title)
	textU, _ := syscall.UTF16FromString(text)
	copy(nid.SzInfoTitle[:], titleU)
	copy(nid.SzInfo[:], textU)
	call(pShellNotifyIcon, uintptr(nimModify), uintptr(unsafe.Pointer(&nid)))
}

func (t *tray) remove() {
	if !t.added {
		return
	}
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UFlags = nifMessage
	call(pShellNotifyIcon, uintptr(nimDelete), uintptr(unsafe.Pointer(&nid)))
	t.added = false
}

func (t *tray) restore() {
	call(pShowWindow, uintptr(t.main), uintptr(swRestore))
	call(pSetForeground, uintptr(t.main))
}

func (t *tray) menu() {
	m := call(pCreatePopupMenu)
	call(pAppendMenu, m, uintptr(mfString), 1, uintptr(unsafe.Pointer(utf16("打开 ezharness"))))
	call(pAppendMenu, m, uintptr(mfString), 2, uintptr(unsafe.Pointer(utf16("退出"))))
	call(pSetForeground, uintptr(t.hwnd)) // 让菜单在失焦时正确收起
	var p point
	call(pGetCursorPos, uintptr(unsafe.Pointer(&p)))
	call(pTrackPopupMenu, m, uintptr(tpmRightBtn|tpmRetTrue), uintptr(p.X), uintptr(p.Y), 0, uintptr(t.hwnd), 0)
	call(pDestroyMenu, m)
}

func trayWndProc(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
	t := (*tray)(unsafe.Pointer(call(pGetWindowLongPtr, uintptr(hwnd), gwlpUserdata)))
	switch msg {
	case wmApp + 1: // Shell_NotifyIcon 回调：lp 低 16 位是鼠标事件
		if t == nil {
			break
		}
		switch loWord(lp) {
		case wmLbuttonup, wmLbuttondblclk:
			t.restore()
		case wmRbuttonup:
			t.menu()
		}
	case wmCommand: // 菜单项：1=打开 2=退出
		switch loWord(wp) {
		case 1:
			if t != nil {
				t.restore()
			}
		case 2:
			if t != nil {
				call(pPostMessage, uintptr(t.main), uintptr(wmClose), 1, 0) // wp=1 强制退出，绕过托盘隐藏
			}
		}
	}
	return call(pDefWindowProc, uintptr(hwnd), uintptr(msg), wp, lp)
}

/* ── 主窗口：无边框子类化 ── */

/*
winState 挂在窗口属性（SetProp）上——不能用 GWLP_USERDATA，那是
go-webview2 库自己存实例指针的槽位，覆盖会让库的 WndProc 拿不到实例：
WM_SIZE 不再驱动 browser.Resize（最大化黑边）、WM_DESTROY 不再
Terminate（退出挂死）。
*/
type winState struct {
	proc        uintptr // 原窗口过程（库实例指针在 USERDATA，经 CallWindowProc 保留）
	tray        *tray
	closeToTray bool
}

const propName = "ez_state"

func setState(hwnd syscall.Handle, st *winState) {
	call(pSetProp, uintptr(hwnd), uintptr(unsafe.Pointer(utf16(propName))), uintptr(unsafe.Pointer(st)))
}

func getState(hwnd syscall.Handle) *winState {
	return (*winState)(unsafe.Pointer(call(pGetProp, uintptr(hwnd), uintptr(unsafe.Pointer(utf16(propName))))))
}

func mainWndProc(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
	st := getState(hwnd)
	switch msg {
	case 0x0083 + 1: // WM_NCCALCSIZE：客户区占满窗口矩形 = 无边框
		if wp != 0 && st != nil {
			return 0
		}
	case 0x0024: // WM_GETMINMAXINFO：最大化限制到显示器工作区（无边框默认溢出）
		mmi := (*minmaxInfo)(unsafe.Pointer(lp))
		mon := call(pMonitorFromWindow, uintptr(hwnd), uintptr(monDefault))
		var mi monitorInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if mon != 0 && call(pGetMonitorInfo, mon, uintptr(unsafe.Pointer(&mi))) != 0 {
			mmi.PtMaxSize.X = mi.RcWork.Right - mi.RcWork.Left
			mmi.PtMaxSize.Y = mi.RcWork.Bottom - mi.RcWork.Top
			mmi.PtMaxPosition.X = mi.RcWork.Left
			mmi.PtMaxPosition.Y = mi.RcWork.Top
			return 0
		}
	case wmClose: // 关闭：托盘模式且非强制（wp==0）时隐藏，否则销毁（托盘退出发 wp==1）
		if st != nil && st.closeToTray && wp == 0 {
			call(pShowWindow, uintptr(hwnd), uintptr(swHide))
			return 0
		}
	case 0x0002: // WM_DESTROY：清托盘图标与窗口属性
		if st != nil {
			if st.tray != nil {
				st.tray.remove()
			}
			call(pRemoveProp, uintptr(hwnd), uintptr(unsafe.Pointer(utf16(propName))))
		}
	}
	if st == nil || st.proc == 0 {
		return call(pDefWindowProc, uintptr(hwnd), uintptr(msg), wp, lp)
	}
	return call(pCallWindowProc, st.proc, uintptr(hwnd), uintptr(msg), wp, lp)
}

/* openWindow 打开主窗口并阻塞至窗口关闭。 */
func openWindow(url string, cfg config.Config, closeToTray bool) {
	enablePerMonitorDPI()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "ezharness",
			Width:  uint(cfg.WindowW),
			Height: uint(cfg.WindowH),
			Center: true,
		},
	})
	if w == nil {
		fmt.Println("WebView2 初始化失败（需要 Windows 10+ 与 WebView2 运行时）")
		return
	}
	defer w.Destroy()
	w.SetSize(cfg.WindowW, cfg.WindowH, webview2.HintNone)

	hwnd := syscall.Handle(w.Window())
	st := &winState{closeToTray: closeToTray}
	// 去系统标题栏，子类化窗口过程（库实例指针在 USERDATA，CallWindowProc 保留其处理）
	style := call(pGetWindowLongPtr, uintptr(hwnd), gwlStyle)
	call(pSetWindowLongPtr, uintptr(hwnd), gwlStyle, style&^uintptr(wsCaption))
	st.proc = call(pSetWindowLongPtr, uintptr(hwnd), gwlpWndproc,
		syscall.NewCallback(mainWndProc))
	setState(hwnd, st)
	call(pSetWindowPos, uintptr(hwnd), 0, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpFrameChg))

	bindWindow(w, hwnd, st)
	if closeToTray {
		st.tray = newTray(hwnd)
	}

	w.Navigate(url)
	w.Run()
}

/* bindWindow 注入 window.win_* 函数（回调在 UI 线程执行，可同步 SendMessage）。 */
func bindWindow(w webview2.WebView, hwnd syscall.Handle, st *winState) {
	_ = w.Bind("win_min", func() error {
		call(pShowWindow, uintptr(hwnd), uintptr(swMinimize))
		return nil
	})
	_ = w.Bind("win_max", func() error {
		if call(pIsZoomed, uintptr(hwnd)) != 0 {
			call(pShowWindow, uintptr(hwnd), uintptr(swRestore))
		} else {
			call(pShowWindow, uintptr(hwnd), uintptr(swMaximize))
		}
		return nil
	})
	_ = w.Bind("win_is_max", func() (bool, error) {
		return call(pIsZoomed, uintptr(hwnd)) != 0, nil
	})
	_ = w.Bind("win_drag", func() error {
		if call(pIsZoomed, uintptr(hwnd)) != 0 {
			return nil // 最大化时不拖拽（拖拽会被系统还原且状态不同步）
		}
		// 交出 WebView2 的鼠标捕获后进入系统模态拖拽循环
		call(pReleaseCapture)
		call(pSendMessage, uintptr(hwnd), uintptr(0x00A1), uintptr(htCaption), 0) // WM_NCLBUTTONDOWN
		return nil
	})
	_ = w.Bind("win_close", func() error {
		if !st.closeToTray {
			call(pPostMessage, uintptr(hwnd), uintptr(wmClose), 0, 0)
			return nil
		}
		if st.tray == nil {
			st.tray = newTray(hwnd) // 设置页中途开启：首次关闭时补建托盘
		}
		call(pShowWindow, uintptr(hwnd), uintptr(swHide))
		st.tray.balloon("ezharness 仍在运行", "已最小化到托盘，点击图标恢复窗口")
		return nil
	})
}

/* enablePerMonitorDPI 声明逐显示器 DPI 感知（PER_MONITOR_AWARE_V2）——
否则高分屏缩放下 WebView2 内容被系统位图拉伸，整页发糊。
重复声明/老系统失败时静默（无副作用）。 */
func enablePerMonitorDPI() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SetProcessDpiAwarenessContext")
	_, _, _ = proc.Call(^uintptr(3)) // -4 = DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
}
