package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/* 发送校验矩阵：text/images 至少其一、图片张数、MIME 前缀、单图大小。 */
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
	if got := post(`{"images":[{"mimeType":"text/plain","data":"aGk="}]}`); got != http.StatusBadRequest {
		t.Fatalf("non-image mime: %d", got)
	}
	many, _ := json.Marshal(map[string]any{
		"images": func() []map[string]string {
			out := make([]map[string]string, 9)
			for i := range out {
				out[i] = map[string]string{"mimeType": "image/png", "data": "aGk="}
			}
			return out
		}(),
	})
	if got := post(string(many)); got != http.StatusBadRequest {
		t.Fatalf("too many images: %d", got)
	}
	big, _ := json.Marshal(map[string]any{
		"images": []map[string]string{{"mimeType": "image/png", "data": strings.Repeat("a", maxImageBase64+1)}},
	})
	if got := post(string(big)); got != http.StatusBadRequest {
		t.Fatalf("oversized image: %d", got)
	}
}
