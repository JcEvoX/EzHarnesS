/*
快应用受限令牌：由主令牌与应用名经 HMAC 派生，无需存储。令牌只放行
模型调用与 MCP 调用/授权接口，页面即使被注入也无法读写文件、改设置、
操作终端或浏览器。应用名可含点，校验时按最后一个点切分签名。
*/
package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const appTokenPrefix = "ezapp."

/* issueAppToken 由主令牌与应用名派生受限令牌。 */
func issueAppToken(master, name string) string {
	mac := hmac.New(sha256.New, []byte(master))
	mac.Write([]byte("app:" + name))
	return appTokenPrefix + name + "." + hex.EncodeToString(mac.Sum(nil)[:16])
}

/* validAppToken 校验受限令牌（签名位置取最后一个点）。 */
func validAppToken(master, tok string) bool {
	rest, ok := strings.CutPrefix(tok, appTokenPrefix)
	if !ok {
		return false
	}
	i := strings.LastIndexByte(rest, '.')
	if i <= 0 {
		return false
	}
	return hmac.Equal([]byte(issueAppToken(master, rest[:i])), []byte(tok))
}

/* appAPIPath 是受限令牌可访问的接口：模型调用与 MCP 调用/授权。 */
func appAPIPath(path string) bool {
	switch path {
	case "/api/model/call",
		"/api/mcp/call",
		"/api/mcp/oauth/start",
		"/api/mcp/oauth/status",
		"/api/mcp/oauth/cancel":
		return true
	}
	return false
}
