/* WorkspaceController：工作目录文件预览（附件 chips 缩略图与文件查看源）。 */
package controller

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* WorkspaceController 工作目录只读文件服务。 */
type WorkspaceController struct {
	Hub *domain.Hub
}

/*
File GET /api/workspace/file?path=<绝对路径>：输出工作目录内的文件
（inline 预览）。仅放行工作目录内路径——防路径穿越读取数据文件
（models.json/sessions/ 等在数据目录，不在工作目录内）。
*/
func (c *WorkspaceController) File(g *gin.Context) {
	workDir := service.ResolveWorkDir(c.Hub.SettingsSnapshot().WorkDir)
	abs, err := filepath.Abs(filepath.FromSlash(g.Query("path")))
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		g.JSON(http.StatusForbidden, gin.H{"error": "path outside workspace"})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		g.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(abs))
	if ct == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		ct = http.DetectContentType(buf[:n])
		if _, err := f.Seek(0, 0); err != nil {
			g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	g.Header("Content-Type", ct)
	g.Header("Content-Disposition", "inline")
	http.ServeContent(g.Writer, g.Request, filepath.Base(abs), time.Now(), f)
}
