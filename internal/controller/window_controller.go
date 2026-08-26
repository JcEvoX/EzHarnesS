/*
窗口壳桥接：/api/window/* 供桌面端自绘标题栏调用（wails 资产域同源
或 Vite proxy 转发）。浏览器访问时无窗口，一律 503。窗口句柄由 main
侧装配（wails Window 的适配器），换代重启跨代保留。
*/
package controller

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

/* WindowControl 是桌面窗口的最小控制面（main 侧适配原生窗口）。 */
type WindowControl interface {
	Minimise()
	ToggleMaximise()
	IsMaximised() bool
	Close()
	OpenAppWindow(path, title string) // 快应用独立子窗口
}

type WindowController struct {
	mu  sync.RWMutex
	win WindowControl
}

/* Set 注入当前窗口（openWindow 时）。 */
func (c *WindowController) Set(w WindowControl) {
	c.mu.Lock()
	c.win = w
	c.mu.Unlock()
}

func (c *WindowController) current() WindowControl {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.win
}

func (c *WindowController) Minimise(g *gin.Context) {
	if w := c.current(); w != nil {
		w.Minimise()
		g.Status(http.StatusNoContent)
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

func (c *WindowController) ToggleMaximise(g *gin.Context) {
	if w := c.current(); w != nil {
		w.ToggleMaximise()
		g.JSON(http.StatusOK, gin.H{"maximized": w.IsMaximised()})
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

func (c *WindowController) State(g *gin.Context) {
	if w := c.current(); w != nil {
		g.JSON(http.StatusOK, gin.H{"maximized": w.IsMaximised()})
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

func (c *WindowController) Close(g *gin.Context) {
	if w := c.current(); w != nil {
		w.Close() // 关闭行为（托盘隐藏/退出）由窗口壳的 WindowClosing 钩子统一裁决
		g.Status(http.StatusNoContent)
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

/* OpenApp POST /api/apps/open：桌面壳为快应用开子窗口（path 形如 /apps/x.html）。 */
func (c *WindowController) OpenApp(path, title string) bool {
	if w := c.current(); w != nil {
		w.OpenAppWindow(path, title)
		return true
	}
	return false
}
