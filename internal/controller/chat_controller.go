/* ChatController：消息发送、取消、决策回传与 SSE 事件流。 */
package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/service"
)

/* 多模态输入限制（与前端压缩策略配合：压缩后单图远小于上限）。 */
const (
	maxInputImages = 8       // 单条消息图片数上限
	maxImageBase64 = 8 << 20 // 单图 base64 字符数上限（8MB）
)

/* ChatController 对话表现层。 */
type ChatController struct {
	Svc *service.ChatService
}

/*
	SendMessage POST /api/sessions/:id/messages（:id=分支根 ID）。

文本与图片至少其一；图片为内嵌 base64（多模态输入，主模型直收）。
*/
func (c *ChatController) SendMessage(g *gin.Context) {
	var body struct {
		Text   string `json:"text"`
		Images []struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		} `json:"images"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Text) == "" && len(body.Images) == 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}
	if len(body.Images) > maxInputImages {
		g.JSON(http.StatusBadRequest, gin.H{"error": "图片过多（单条最多 8 张）"})
		return
	}
	images := make([]types.ImagePart, 0, len(body.Images))
	for _, img := range body.Images {
		if !strings.HasPrefix(img.MimeType, "image/") {
			g.JSON(http.StatusBadRequest, gin.H{"error": "仅支持图片附件（image/*）"})
			return
		}
		if len(img.Data) > maxImageBase64 {
			g.JSON(http.StatusBadRequest, gin.H{"error": "单张图片过大（压缩后不超过 8MB）"})
			return
		}
		images = append(images, types.ImagePart{MimeType: img.MimeType, Data: img.Data})
	}
	if err := c.Svc.Send(g.Param("id"), body.Text, images); err != nil {
		if errors.Is(err, domain.ErrBusy) {
			g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, domain.ErrNoAPIKey) {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* CancelTurn POST /api/sessions/:id/cancel（:id=分支根 ID）。 */
func (c *ChatController) CancelTurn(g *gin.Context) {
	c.Svc.Cancel(g.Param("id"))
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/*
	Events GET /api/sessions/:id/events（SSE，:id=分支根 ID——按分支路由，

连接时重放该分支的回放帧与未决人机请求；compact 换代对象不变不断线）。
*/
func (c *ChatController) Events(g *gin.Context) {
	sess := c.Svc.Hub.SessionOf(g.Param("id"))
	if sess == nil {
		sess = c.Svc.Hub.Active
	}
	fl, ok := g.Writer.(interface{ Flush() })
	if !ok {
		g.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	fmt.Fprint(g.Writer, ": connected\n\n")

	/* 建连首帧：当前轮运行态（前端复位 busy / 截断本地本轮块，配合
	随后的整轮回放干净重建——防断线重连导致的重复块与卡死） */
	if data, err := json.Marshal(domain.ReplaySync(sess.TurnActive())); err == nil {
		fmt.Fprintf(g.Writer, "data: %s\n\n", data)
	}
	for _, frame := range sess.ReplayFrames() {
		fmt.Fprintf(g.Writer, "data: %s\n\n", frame)
	}
	fl.Flush()

	ch, unsub := sess.Subscribe()
	defer unsub()

	hb := g.Request.Context()
	// 心跳注释帧：SSE 经代理（Vite dev proxy 等）空闲约 200s 被断，
	// 周期性写入保持连接活性（注释行不触发前端 onmessage）。
	hbTick := time.NewTicker(20 * time.Second)
	defer hbTick.Stop()
	for {
		select {
		case <-hb.Done():
			return
		case <-hbTick.C:
			if _, err := fmt.Fprint(g.Writer, ": hb\n\n"); err != nil {
				return
			}
			fl.Flush()
		case frame := <-ch:
			if _, err := fmt.Fprintf(g.Writer, "data: %s\n\n", frame); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

/* DecideApprove POST /api/sessions/:id/decisions/approve（:id=分支根 ID）。 */
func (c *ChatController) DecideApprove(g *gin.Context) {
	var body struct {
		CallID  string `json:"callId" binding:"required"`
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecideApprove(g.Param("id"), body.CallID, body.Approve, body.Reason)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}

/* Notifications GET /api/notifications：全分支未决人机请求汇总（通知栏
全局轮询数据源；纯读无状态，后台分支的请求也在此可达）。 */
func (c *ChatController) Notifications(g *gin.Context) {
	type noticeGroup struct {
		RootID string                 `json:"rootId"`
		Items  []domain.PendingNotice `json:"items"`
	}
	out := []noticeGroup{}
	for _, s := range c.Svc.Hub.Sessions() {
		if items := s.PendingNotices(); len(items) > 0 {
			out = append(out, noticeGroup{RootID: s.Root(), Items: items})
		}
	}
	g.JSON(http.StatusOK, out)
}

/* DecideAnswer POST /api/sessions/:id/decisions/answer（:id=分支根 ID）。 */
func (c *ChatController) DecideAnswer(g *gin.Context) {
	var body struct {
		CallID string `json:"callId" binding:"required"`
		Input  string `json:"input"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Svc.DecideAnswer(g.Param("id"), body.CallID, body.Input)
	g.JSON(http.StatusOK, gin.H{"ok": true})
}
