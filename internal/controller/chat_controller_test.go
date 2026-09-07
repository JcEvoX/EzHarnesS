package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/* 发送校验矩阵：text/files 至少其一、附件个数、单附件大小。 */
func TestSendMessageValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &ChatController{Svc: nil} // 校验在进 Svc 前完成，nil 不触发

	post := func(body string) int {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		g.Request.Header.Set("Content-Type", "application/json")
		c.SendMessage(g)
		return w.Code
	}

	if got := post(`{}`); got != http.StatusBadRequest {
		t.Fatalf("empty body: %d", got)
	}
	many, _ := json.Marshal(map[string]any{
		"files": func() []map[string]string {
			out := make([]map[string]string, 9)
			for i := range out {
				out[i] = map[string]string{"name": "a.txt", "mimeType": "text/plain", "data": "aGk="}
			}
			return out
		}(),
	})
	if got := post(string(many)); got != http.StatusBadRequest {
		t.Fatalf("too many files: %d", got)
	}
	big, _ := json.Marshal(map[string]any{
		"files": []map[string]string{{"name": "big.bin", "mimeType": "application/octet-stream", "data": strings.Repeat("a", maxAttachBase64+1)}},
	})
	if got := post(string(big)); got != http.StatusBadRequest {
		t.Fatalf("oversized file: %d", got)
	}
}
