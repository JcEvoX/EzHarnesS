/*
Package tray 只管系统托盘：图标、提示、左键与右键菜单。

不持有窗口或退出逻辑：点击/打开/退出均以回调交给上层装配。
*/
package tray

import "github.com/wailsapp/wails/v3/pkg/application"

type Options struct {
	Icon     []byte
	Tooltip  string
	OpenText string // 菜单「打开」文案
	QuitText string // 菜单「退出」文案
	OnClick  func() // 左键
	OnOpen   func() // 菜单：打开
	OnQuit   func() // 菜单：退出
}

/* New 创建托盘并挂上菜单。 */
func New(app *application.App, o Options) *application.SystemTray {
	t := app.SystemTray.New()
	t.SetIcon(o.Icon)
	t.SetTooltip(o.Tooltip)
	if o.OnClick != nil {
		t.OnClick(o.OnClick)
	}
	menu := application.NewMenu()
	if o.OnOpen != nil {
		menu.Add(o.OpenText).OnClick(func(*application.Context) { o.OnOpen() })
	}
	if o.OnQuit != nil {
		menu.Add(o.QuitText).OnClick(func(*application.Context) { o.OnQuit() })
	}
	t.SetMenu(menu)
	return t
}
