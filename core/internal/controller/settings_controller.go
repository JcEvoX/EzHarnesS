/* SettingsController：设置与长期记忆。 */
package controller

import (
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"

	"ezharness/core/internal/domain"
	"ezharness/core/internal/service"
)

/* SettingsController 设置表现层。 */
type SettingsController struct {
	Settings *service.SettingsService
	Memory   *service.MemoryService
}

/* GetSettings GET /api/settings。 */
func (c *SettingsController) GetSettings(g *gin.Context) {
	g.JSON(http.StatusOK, c.Settings.Get())
}

/* UpdateSettings POST /api/settings。 */
func (c *SettingsController) UpdateSettings(g *gin.Context) {
	var st service.SettingsView
	if err := g.ShouldBindJSON(&st); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.Update(g.Request.Context(), st); err != nil {
		if err.Error() == "model and baseUrl required" {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetModels GET /api/models。 */
func (c *SettingsController) GetModels(g *gin.Context) {
	g.JSON(http.StatusOK, c.Settings.GetModels())
}

/* UpdateModels POST /api/models（每槽至多一条启用，保存后重建 agent）。 */
func (c *SettingsController) UpdateModels(g *gin.Context) {
	var m domain.ModelsConfig
	if err := g.ShouldBindJSON(&m); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.UpdateModels(g.Request.Context(), m); err != nil {
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetSecurity GET /api/security。 */
func (c *SettingsController) GetSecurity(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"rules": c.Settings.SecurityRules()})
}

/* UpdateSecurity POST /api/security。 */
func (c *SettingsController) UpdateSecurity(g *gin.Context) {
	var body struct {
		Rules []domain.ToolRule `json:"rules"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.UpdateSecurity(g.Request.Context(), body.Rules); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* GetMemoryConfig GET /api/memory/config（记忆页三文件夹数据）。 */
func (c *SettingsController) GetMemoryConfig(g *gin.Context) {
	g.JSON(http.StatusOK, c.Memory.Config(g.Request.Context()))
}

/* GetMemory GET /api/memory。 */
func (c *SettingsController) GetMemory(g *gin.Context) {
	g.JSON(http.StatusOK, gin.H{"content": c.Memory.GetMemory(g.Request.Context())})
}

/* SaveMemory POST /api/memory。 */
func (c *SettingsController) SaveMemory(g *gin.Context) {
	var body struct {
		Content string `json:"content"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Memory.SaveMemory(g.Request.Context(), body.Content); err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* CreateSkill POST /api/memory/skills（zip base64 新建技能，名称自动推导）。 */
func (c *SettingsController) CreateSkill(g *gin.Context) {
	var body struct {
		Data string `json:"data"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "data 不是有效的 base64"})
		return
	}
	if err := c.Memory.CreateSkill(g.Request.Context(), data); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* DeleteSkill DELETE /api/memory/skills/:id。 */
func (c *SettingsController) DeleteSkill(g *gin.Context) {
	if err := c.Memory.DeleteSkill(g.Request.Context(), g.Param("id")); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* ToggleSkill POST /api/memory/skills/:id/enabled（启停技能）。 */
func (c *SettingsController) ToggleSkill(g *gin.Context) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Memory.ToggleSkill(g.Request.Context(), g.Param("id"), body.Enabled); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* ActivateModel POST /api/models/activate：主模型切换（输入框选择器；下一次输入生效）。 */
func (c *SettingsController) ActivateModel(g *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := g.ShouldBindJSON(&req); err != nil || req.Name == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	e, err := c.Settings.ActivateMainModel(g.Request.Context(), req.Name)
	if err != nil {
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "model": e})
}

/* SetReasoning POST /api/settings/reasoning：思考强度档位（输入框下拉；请求侧实时读取，即改即生效）。 */
func (c *SettingsController) SetReasoning(g *gin.Context) {
	var req struct {
		Effort string `json:"effort"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.Settings.SetReasoningEffort(g.Request.Context(), req.Effort); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "effort": req.Effort})
}

/*
CallModel POST /api/model/call：快应用/脚本直调大模型（与 /api/mcp/call 同级的
系统内置能力）。body {model?, prompt, images?: [路径]}；返回 {result}。
*/
func (c *SettingsController) CallModel(g *gin.Context) {
	var req struct {
		Model  string   `json:"model"`
		Prompt string   `json:"prompt"`
		Images []string `json:"images"`
	}
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := c.Settings.ModelCall(g.Request.Context(), req.Model, req.Prompt, req.Images)
	if err != nil {
		g.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"result": result})
}
