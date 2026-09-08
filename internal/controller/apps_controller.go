/* AppsController：快应用列表与桌面端启动。 */
package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ezharness/internal/service"
)

/* AppsController 快应用表现层。 */
type AppsController struct {
	Svc *service.AppsService
	Win *WindowController // 桌面壳子窗口（浏览器访问时无窗口，回落新标签页）
}

/* List GET /api/apps。 */
func (c *AppsController) List(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"apps": c.Svc.List()})
}

/*
	Open POST /api/apps/open {name}：校验存在后交桌面壳开独立窗口。

浏览器访问（无窗口壳）返回 503，前端回落 window.open 新标签页。
*/
func (c *AppsController) Open(g *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, e := range c.Svc.List() { // 名单校验，杜绝路径穿越
		// 名单是带 .html 后缀的文件名；app:// 引用与 save_app 的 name 均
		// 为无后缀短名——两种写法都接受（仍限定名单内，不可穿越）
		if e.Name == req.Name || strings.TrimSuffix(e.Name, ".html") == req.Name {
			if c.Win.OpenApp("/apps/"+e.Name, e.Title+" · ezharness") {
				// 200+JSON（而非 204）：前端 post() 辅助函数固定解析 body，
				// 204 空 body 会被当成失败触发 window.open 回落（双开 bug）
				g.JSON(http.StatusOK, gin.H{"ok": true})
				return
			}
			g.Status(http.StatusServiceUnavailable)
			return
		}
	}
	g.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
}
