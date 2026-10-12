/*
mcp_auth.go 承载 MCP 的 OAuth 认证：令牌落盘、授权流程（DCR/授权 URL/换码）
与本地回调。一个进程级单例（McpAuthManager）跨代复用：它握一条固定端口的
loopback 监听作为 RedirectURI（DCR 只注册一次），并把令牌写到
<mcp-auth>/<server>.json——请求端（传输层）与授权端（本管理器）共享同一份。

流程由前端驱动：Start 返回授权 URL（前端用系统浏览器打开）→ 用户授权 →
授权服务器重定向回 loopback → 换码落盘 → 前端轮询 Status 见到已授权后
回传会话决策，阻塞中的工具调用随即重试。
*/
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/ext/hook/mcp"
)

const (
	mcpAuthDir    = "mcp-auth"
	mcpAuthScope  = "ezharness"
	mcpAuthPortFN = "mcp-auth/port"
)

/* McpAuthManager MCP OAuth 认证管理器（进程级单例）。 */
type McpAuthManager struct {
	Fsys   fs.FileSystem
	Router *mcp.Router // 授权成功后 Drop，强制下一次调用重建连接（带上登记的 client id 与新令牌）

	mu          sync.Mutex
	listener    net.Listener
	srv         *http.Server
	redirectURI string
	pending     map[string]pendingAuth // state → 待回调
	pendingURL  map[string]string      // server → 已生成的授权 URL（幂等复用）
	needs       map[string]bool        // server → 已判定需要 OAuth（自动检测结果）
	probed      map[string]bool        // server → 已探过落盘标记（免每轮重复读盘）
	netProbed   map[string]bool        // server → 已做过发现文档探测（含未命中，免每次裸 401 发请求）
	httpClient  *http.Client           // 发现文档探测用（短超时）
}

type pendingAuth struct {
	server   string
	verifier string
	flow     *mcp.OAuthFlow
}

/*
NewMcpAuthManager 构造并启动 loopback 回调监听（仅绑 127.0.0.1）。
监听失败不致命：回调不可用时 OAuth 流程报错，静态 header 的 server 不受影响。
*/
func NewMcpAuthManager(fsys fs.FileSystem) *McpAuthManager {
	m := &McpAuthManager{
		Fsys:       fsys,
		pending:    map[string]pendingAuth{},
		pendingURL: map[string]string{},
		needs:      map[string]bool{},
		probed:     map[string]bool{},
		netProbed:  map[string]bool{},
		httpClient: &http.Client{Timeout: 4 * time.Second},
	}
	// 端口落盘复用：redirect_uri 与 DCR 注册的 client_id 绑定，端口变了
	// 重新授权时会被授权服务器拒绝（redirect_uri 不匹配）。
	prev := 0
	if data, err := fsys.Read(context.Background(), mcpAuthPortFN); err == nil {
		prev, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", prev))
	if err != nil && prev != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // 端口被占：换随机端口
	}
	if err == nil {
		m.listener = ln
		m.redirectURI = fmt.Sprintf("http://%s/oauth/callback", ln.Addr().String())
		if p := ln.Addr().(*net.TCPAddr).Port; p != prev {
			_ = fsys.Write(context.Background(), mcpAuthPortFN, []byte(strconv.Itoa(p)))
		}
		m.srv = &http.Server{Handler: http.HandlerFunc(m.handleCallback)}
		go func() { _ = m.srv.Serve(ln) }()
	}
	return m
}

/* SetRouter 注入 router（授权成功后 Drop 用）；构造顺序使 router 晚于本管理器。 */
func (m *McpAuthManager) SetRouter(r *mcp.Router) { m.Router = r }

/* RedirectURI 返回回调地址（空表示监听不可用）。 */
func (m *McpAuthManager) RedirectURI() string { return m.redirectURI }

/* Close 关闭回调监听（换代/退出）。 */
func (m *McpAuthManager) Close() {
	if m.srv != nil {
		_ = m.srv.Close()
	}
}

/*
Start 为一个 server 生成授权 URL（幂等：已 pending 则复用）。**无需用户配置**——
对任意 http/sse server 都可发起；一旦发起即判定该 server 需 OAuth（落标记，
后续请求自动带 OAuth handler），并断开旧连接强制重连。metadataURL 非空时优先
用它发现授权服务器（来自 401 的 resource_metadata，最权威）。未登记 client id
时先走动态注册并落盘，供请求端传输复用；client/scope 覆盖来自 mcp.json 的 oauth 块。
*/
func (m *McpAuthManager) Start(ctx context.Context, name, metadataURL string) (string, error) {
	if m.redirectURI == "" {
		return "", fmt.Errorf("oauth callback listener unavailable")
	}
	f := loadMcpFileOrNil(ctx, m.Fsys)
	srv, ok := findServer(f, name)
	if !ok {
		return "", fmt.Errorf("unknown server: %s", name)
	}
	if srv.Type != "http" && srv.Type != "sse" {
		return "", fmt.Errorf("server %s: OAuth 仅适用于 http/sse", name)
	}
	m.mu.Lock()
	if u := m.pendingURL[name]; u != "" {
		m.mu.Unlock()
		return u, nil
	}
	m.mu.Unlock()

	oc := m.oauthConfig(ctx, srv)
	flow := mcp.NewOAuthFlow(srv.URL, *oc)
	flow.SetResourceMetadataURL(metadataURL)
	if flow.ClientID() == "" {
		if err := flow.RegisterClient(ctx, mcpAuthScope); err != nil {
			return "", fmt.Errorf("register client: %w", err)
		}
		if err := m.saveClientID(ctx, name, flow.ClientID()); err != nil {
			return "", err
		}
	}
	verifier, challenge, state, err := flow.GeneratePKCE()
	if err != nil {
		return "", err
	}
	url, err := flow.AuthorizationURL(ctx, state, challenge)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.pending[state] = pendingAuth{server: name, verifier: verifier, flow: flow}
	m.pendingURL[name] = url
	m.mu.Unlock()
	m.markNeedsOAuth(ctx, name)
	return url, nil
}

/*
NeedsOAuth 报告该 server 是否已被判定需要 OAuth（自动检测结果，落盘复用）。
供工厂决定是否给该 server 装 OAuth handler。
*/
func (m *McpAuthManager) NeedsOAuth(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.needs[name] {
		return true
	}
	if m.probed[name] {
		return false // 本轮已探过且未命中；后续只会经 markNeedsOAuth 置位
	}
	m.probed[name] = true
	if _, err := m.Fsys.Read(context.Background(), m.needsPath(name)); err == nil {
		m.needs[name] = true
		return true
	}
	return false
}

/* markNeedsOAuth 落"需 OAuth"标记并断开旧连接，使下次请求重建为带 handler 的传输。 */
func (m *McpAuthManager) markNeedsOAuth(ctx context.Context, name string) {
	m.mu.Lock()
	m.needs[name] = true
	m.mu.Unlock()
	_ = m.Fsys.Write(ctx, m.needsPath(name), []byte("1"))
	if m.Router != nil {
		m.Router.Drop(name)
	}
}

/*
DiscoverOAuth 兜底探测：底层 401 未带资源元数据时，按端点 origin 探 OAuth
发现文档——RFC9728 受保护资源元数据（`/.well-known/oauth-protected-resource[/<path>]`）
优先，其次 RFC8414 / OpenID 授权服务器元数据。命中即判定需 OAuth 并落标记。
结果按 server 缓存（含未命中），免每次裸 401 都发探测请求。
*/
func (m *McpAuthManager) DiscoverOAuth(ctx context.Context, srv McpServerFile) (string, bool) {
	if (srv.Type != "http" && srv.Type != "sse") || srv.URL == "" {
		return "", false
	}
	m.mu.Lock()
	if m.netProbed[srv.Name] {
		m.mu.Unlock()
		return "", false
	}
	m.netProbed[srv.Name] = true
	m.mu.Unlock()

	u, err := url.Parse(srv.URL)
	if err != nil || u.Host == "" {
		return "", false
	}
	origin := u.Scheme + "://" + u.Host
	cands := []string{origin + "/.well-known/oauth-protected-resource"}
	if p := strings.Trim(u.EscapedPath(), "/"); p != "" {
		cands = append(cands, origin+"/.well-known/oauth-protected-resource/"+p)
	}
	cands = append(cands,
		origin+"/.well-known/oauth-authorization-server",
		origin+"/.well-known/openid-configuration",
	)
	// 并行探候选：总体受一个 5s 预算约束（命中即返回，其余随 ctx 取消）。
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ch := make(chan string, len(cands))
	for _, c := range cands {
		go func(u string) {
			if m.probeOAuthMeta(pctx, u) {
				ch <- u
			} else {
				ch <- ""
			}
		}(c)
	}
	for range cands {
		if u := <-ch; u != "" {
			m.markNeedsOAuth(ctx, srv.Name)
			return u, true
		}
	}
	return "", false
}

/* probeOAuthMeta 拉取一个候选发现文档；含授权服务器线索（authorization_servers/authorization_endpoint）即命中。 */
func (m *McpAuthManager) probeOAuthMeta(ctx context.Context, target string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var doc struct {
		AuthorizationServers  []string `json:"authorization_servers"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc) != nil {
		return false
	}
	return len(doc.AuthorizationServers) > 0 || doc.AuthorizationEndpoint != ""
}

/*
Status 报告 server 的授权状态：authorized = 落盘令牌存在且未过期；
pending = 已有待回调的授权流程。
*/
func (m *McpAuthManager) Status(ctx context.Context, name string) (authorized, pending bool) {
	t, err := m.tokenStore(name).GetToken(ctx)
	if err == nil && t.AccessToken != "" {
		// access 有效即已授权；access 过期但有 refresh token 也算——请求端会自动刷新，
		// 不需要用户再介入（避免徽标误报未授权）。
		valid := t.ExpiresAt.IsZero() || time.Now().Before(t.ExpiresAt)
		authorized = valid || t.RefreshToken != ""
	}
	m.mu.Lock()
	_, pending = m.pendingURL[name]
	m.mu.Unlock()
	return
}

/* Cancel 取消一个待完成的授权流程（用户在弹框点取消）。 */
func (m *McpAuthManager) Cancel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pendingURL, name)
	for state, p := range m.pending {
		if p.server == name {
			delete(m.pending, state)
		}
	}
}

/* Logout 删除令牌并断开该 server（下次调用重新触发认证）。 */
func (m *McpAuthManager) Logout(ctx context.Context, name string) {
	_ = m.Fsys.Write(ctx, m.tokenPath(name), nil) // 置空令牌文件
	if m.Router != nil {
		m.Router.Drop(name)
	}
}

/*
oauthConfig 由 mcp.json 的 oauth 块（可空）与已登记 client id 组出请求端/授权端
共用的 OAuth 配置：无配置块也成立（client/scope 留空走 DCR 与默认发现）。
*/
func (m *McpAuthManager) oauthConfig(ctx context.Context, srv McpServerFile) *mcp.OAuthConfig {
	var clientID, clientSecret, metadataURL string
	var scopes []string
	if srv.OAuth != nil {
		clientID = srv.OAuth.ClientID
		clientSecret = srv.OAuth.ClientSecret
		scopes = srv.OAuth.Scopes
		metadataURL = srv.OAuth.MetadataURL
	}
	if clientID == "" {
		clientID = m.loadClientID(ctx, srv.Name)
	}
	return &mcp.OAuthConfig{
		ClientID:              clientID,
		ClientSecret:          clientSecret,
		RedirectURI:           m.redirectURI,
		Scopes:                scopes,
		AuthServerMetadataURL: metadataURL,
		PKCEEnabled:           true,
		TokenStore:            m.tokenStore(srv.Name),
	}
}

func (m *McpAuthManager) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	m.mu.Lock()
	p, ok := m.pending[state]
	if ok {
		delete(m.pending, state)
		delete(m.pendingURL, p.server)
	}
	m.mu.Unlock()
	if !ok {
		writeAuthHTML(w, false, "授权会话不存在或已过期，请重新发起认证。")
		return
	}
	if e := q.Get("error"); e != "" {
		writeAuthHTML(w, false, "授权被拒绝："+e)
		return
	}
	code := q.Get("code")
	if code == "" {
		writeAuthHTML(w, false, "缺少授权码。")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.flow.Exchange(ctx, code, state, p.verifier); err != nil {
		writeAuthHTML(w, false, "换取令牌失败："+err.Error())
		return
	}
	if err := m.saveClientID(ctx, p.server, p.flow.ClientID()); err != nil {
		writeAuthHTML(w, false, "保存客户端信息失败："+err.Error())
		return
	}
	if m.Router != nil {
		m.Router.Drop(p.server) // 下次调用重建传输，读取新令牌
	}
	writeAuthHTML(w, true, "授权成功，可关闭本页并回到 ezharness。")
}

func (m *McpAuthManager) tokenStore(server string) *fileTokenStore {
	return &fileTokenStore{fsys: m.Fsys, server: server}
}

func (m *McpAuthManager) tokenPath(server string) string {
	return mcpAuthDir + "/" + safeName(server) + ".json"
}

func (m *McpAuthManager) clientPath(server string) string {
	return mcpAuthDir + "/" + safeName(server) + ".client.json"
}

/* needsPath 是"该 server 需 OAuth"的落盘标记（存在即判定过，重启不重复探测）。 */
func (m *McpAuthManager) needsPath(server string) string {
	return mcpAuthDir + "/" + safeName(server) + ".needs"
}

func (m *McpAuthManager) loadClientID(ctx context.Context, server string) string {
	data, err := m.Fsys.Read(ctx, m.clientPath(server))
	if err != nil {
		return ""
	}
	var c struct {
		ClientID string `json:"client_id"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	return c.ClientID
}

func (m *McpAuthManager) saveClientID(ctx context.Context, server, id string) error {
	if id == "" {
		return nil
	}
	data, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: id})
	if err != nil {
		return err
	}
	return m.Fsys.Write(ctx, m.clientPath(server), data)
}

/* fileTokenStore 把单个 server 的 OAuth 令牌落盘（mcp.TokenStore）。 */
type fileTokenStore struct {
	fsys   fs.FileSystem
	server string
}

func (s *fileTokenStore) path() string { return mcpAuthDir + "/" + safeName(s.server) + ".json" }

func (s *fileTokenStore) GetToken(ctx context.Context) (*mcp.Token, error) {
	data, err := s.fsys.Read(ctx, s.path())
	if err != nil || len(data) == 0 {
		return nil, mcp.ErrNoToken
	}
	var t mcp.Token
	if json.Unmarshal(data, &t) != nil || t.AccessToken == "" {
		return nil, mcp.ErrNoToken
	}
	return &t, nil
}

func (s *fileTokenStore) SaveToken(ctx context.Context, t *mcp.Token) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return s.fsys.Write(ctx, s.path(), data)
}

/* findServer 在 mcp.json 中按名找 server。 */
func findServer(f *McpFile, name string) (McpServerFile, bool) {
	if f == nil {
		return McpServerFile{}, false
	}
	for _, s := range f.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return McpServerFile{}, false
}

/*
safeName 把 server 名净化为文件名：仅保留字母数字与 ._-，
其余替换为 -，并去掉前导点，防路径穿越。
*/
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if out == "" {
		return "server"
	}
	return out
}

func writeAuthHTML(w http.ResponseWriter, ok bool, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := "授权失败"
	if ok {
		title = "授权成功"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;color:#333}div{text-align:center}</style>
</head><body><div><h2>%s</h2><p>%s</p></div></body></html>`, title, title, msg)
}
