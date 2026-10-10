/*
魔法看板·共享浏览器——core 侧桥。browser_* 工具的端无关定义在 core
(tools 层),真实浏览器是 desktop 壳的资产(Electron WebContentsView):
core 经 /api/browser/bridge WS 把工具调用(JSON-RPC 式)转发给 desktop
执行并等回执;截图由 desktop 回传 base64、core 落盘并走 image_loaded
通道转持久化图片消息。桥未连接(web 端直连、desktop 未跑)时工具报
"仅桌面端可用"。服务无页面状态,换代不重建;桥断线由 desktop 自动重连。
*/
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

/* 桥调用等待上限分档：交互动作可等页面响应，快操作秒级，轮首快照不能拖住轮。 */
const (
	bridgeWriteTimeout = 5 * time.Second  // WS 帧发送
	bridgeOpTimeout    = 30 * time.Second // click/type/key/scroll 与 read/screenshot 默认
	bridgeQuickTimeout = 15 * time.Second // close/list
	bridgeBriefTimeout = 2 * time.Second  // 轮首 TabsBrief 快照
)

/* bridgeRequest 是 core → desktop 的调用帧。 */
type bridgeRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"` // start|navigate|click|type|key|scroll|read|screenshot|list|close
	Params map[string]any `json:"params"`
}

/* bridgeReply 是 desktop → core 的回执帧。 */
type bridgeReply struct {
	ID     int64    `json:"id"`
	OK     bool     `json:"ok"`
	Result string   `json:"result"` // 工具返回文本(含头行)
	Error  string   `json:"error"`
	Images []string `json:"images,omitempty"` // screenshot 的分屏 PNG base64(整页分段)
	Note   string   `json:"note,omitempty"`   // 附加说明(如整页超出屏数上限)
}

/* BrowserService 是浏览器桥(单连接:新 desktop 连接顶替旧连接)。 */
type BrowserService struct {
	mu         sync.Mutex
	writeMu    sync.Mutex // WS 单写者
	seq        int64
	conn       *websocket.Conn
	pending    map[int64]chan bridgeReply
	shotDir    string
	seesImages func() bool
}

/* NewBrowserService 构造(workDir 为 agent 工作区,截图落 workspace/browser)。 */
func NewBrowserService(workDir string) *BrowserService {
	return &BrowserService{
		pending: map[int64]chan bridgeReply{},
		shotDir: filepath.Join(workDir, "browser"),
	}
}

/* SetVisionProbe 注入主模型视觉判定(Assemble 时调用,截图返回形态实时裁决)。 */
func (s *BrowserService) SetVisionProbe(fn func() bool) {
	s.mu.Lock()
	s.seesImages = fn
	s.mu.Unlock()
}

/* ModelSeesImages 主模型是否多模态(browser_screenshot 返回形态裁决)。 */
func (s *BrowserService) ModelSeesImages() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seesImages != nil && s.seesImages()
}

/* AttachBridge desktop 桥连入(顶替旧连接并唤醒其挂起调用);此后 pump 读回执。 */
func (s *BrowserService) AttachBridge(conn *websocket.Conn) {
	// 截图回执是整帧 base64(可达数 MB),默认 32KB 读限会读爆断桥
	conn.SetReadLimit(64 << 20)
	s.mu.Lock()
	old := s.conn
	s.conn = conn
	s.mu.Unlock()
	if old != nil {
		_ = old.CloseNow() // 旧连接读泵退出,其 pending 由 detach 统一失败
	}
	s.pumpBridge(conn)
	s.mu.Lock()
	if s.conn == conn { // 只摘自己(可能已被更新连接顶替)
		s.conn = nil
	}
	// 挂起调用全部失败(桥已断,永无回执)
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()
}

/* pumpBridge 读泵:回执按 id 分发到等待者。 */
func (s *BrowserService) pumpBridge(conn *websocket.Conn) {
	for {
		_, r, err := conn.Reader(context.Background())
		if err != nil {
			return
		}
		var reply bridgeReply
		if err := json.NewDecoder(r).Decode(&reply); err != nil {
			continue
		}
		s.mu.Lock()
		ch, ok := s.pending[reply.ID]
		delete(s.pending, reply.ID)
		s.mu.Unlock()
		if ok {
			ch <- reply
		}
	}
}

/*
call 转发一次工具调用并等回执。桥未连接立即报错;超时由调用方语义决定
(导航类用 timeoutMs,其余 30s);ctx 取消立即返回(用户停止本轮时打断在途
等待)。desktop 侧已在执行的操作不远程中断——回执迟到时 pumpBridge 查不到
pending 直接丢弃,在途的 capturePage/CDP 自然跑完。
*/
func (s *BrowserService) call(ctx context.Context, method string, params map[string]any, timeout time.Duration) (bridgeReply, error) {
	s.mu.Lock()
	if s.conn == nil {
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器不可用:需要 desktop 桌面端(web 端直连无内嵌浏览器)")
	}
	conn := s.conn
	s.seq++
	id := s.seq
	ch := make(chan bridgeReply, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	payload, err := json.Marshal(bridgeRequest{ID: id, Method: method, Params: params})
	if err == nil {
		s.writeMu.Lock()
		wctx, wcancel := context.WithTimeout(context.Background(), bridgeWriteTimeout)
		err = conn.Write(wctx, websocket.MessageText, payload)
		wcancel()
		s.writeMu.Unlock()
	}
	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器桥发送失败: %w", err)
	}

	select {
	case reply, ok := <-ch:
		if !ok { // 桥断开,close 的 ch 返回零值
			return bridgeReply{}, fmt.Errorf("浏览器桥已断开")
		}
		if !reply.OK {
			return bridgeReply{}, fmt.Errorf("%s", reply.Error)
		}
		return reply, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器操作已取消: %w", ctx.Err())
	case <-time.After(timeout):
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return bridgeReply{}, fmt.Errorf("浏览器操作超时(%v)", timeout)
	}
}

func bridgeTimeout(timeoutMs, fallbackMs, maxMs int) time.Duration {
	if timeoutMs <= 0 {
		timeoutMs = fallbackMs
	}
	if timeoutMs > maxMs {
		timeoutMs = maxMs
	}
	return time.Duration(timeoutMs) * time.Millisecond
}

/* ── AI 工具后端(BrowserIO 实现,签名与 tools 层契约一致) ── */

/* StartBrowser 新建标签(name 必填,desc 可选)并可选导航,返回标签状态 JSON。 */
func (s *BrowserService) StartBrowser(ctx context.Context, name, desc, rawURL string, timeoutMs int) (string, error) {
	reply, err := s.call(ctx, "start", map[string]any{
		"name": name, "desc": desc, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 600000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 600000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* NavigateBrowser 跳转并等加载,返回新页面标题。 */
func (s *BrowserService) NavigateBrowser(ctx context.Context, tabID, rawURL string, timeoutMs int) (string, error) {
	reply, err := s.call(ctx, "navigate", map[string]any{
		"tabId": tabID, "url": rawURL,
		"timeoutMs": int(bridgeTimeout(timeoutMs, 20000, 120000).Milliseconds()),
	}, bridgeTimeout(timeoutMs, 25000, 120000))
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ClickBrowser 点击(按 CSS 选择器定位,页面内原生点击)。 */
func (s *BrowserService) ClickBrowser(ctx context.Context, tabID, selector string) (string, error) {
	reply, err := s.call(ctx, "click", map[string]any{
		"tabId": tabID, "selector": selector,
	}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* TypeBrowser 输入文本:selector 定位输入框(省略=当前焦点处),submit 回车提交。 */
func (s *BrowserService) TypeBrowser(ctx context.Context, tabID, selector, text string, submit bool) (string, error) {
	reply, err := s.call(ctx, "type", map[string]any{
		"tabId": tabID, "selector": selector, "text": text, "submit": submit,
	}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* PressBrowserKey 按键/组合键("Enter"、"Control+A")。 */
func (s *BrowserService) PressBrowserKey(ctx context.Context, tabID, combo string) (string, error) {
	reply, err := s.call(ctx, "key", map[string]any{"tabId": tabID, "combo": combo}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ScrollBrowser 页面滚动(direction=up|down,amountPx 默认 600)。 */
func (s *BrowserService) ScrollBrowser(ctx context.Context, tabID, direction string, amountPx int) (string, error) {
	if amountPx <= 0 {
		amountPx = 600
	}
	reply, err := s.call(ctx, "scroll", map[string]any{
		"tabId": tabID, "direction": direction, "amountPx": amountPx,
	}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* HoverBrowser 悬停到元素（触发悬浮菜单/提示）。 */
func (s *BrowserService) HoverBrowser(ctx context.Context, tabID, selector string) (string, error) {
	reply, err := s.call(ctx, "hover", map[string]any{"tabId": tabID, "selector": selector}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* SelectOptionBrowser 选 <select> 选项（按 value 或可见文本匹配）。 */
func (s *BrowserService) SelectOptionBrowser(ctx context.Context, tabID, selector, value string) (string, error) {
	reply, err := s.call(ctx, "selectOption", map[string]any{"tabId": tabID, "selector": selector, "value": value}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* UploadBrowser 给 <input type=file> 设置文件（files 为本机绝对路径,逗号分隔）。 */
func (s *BrowserService) UploadBrowser(ctx context.Context, tabID, selector, files string) (string, error) {
	reply, err := s.call(ctx, "upload", map[string]any{"tabId": tabID, "selector": selector, "files": files}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* DragBrowser 指针拖拽（源→目标元素中心;HTML5 DnD 不适用）。 */
func (s *BrowserService) DragBrowser(ctx context.Context, tabID, selector, to string) (string, error) {
	reply, err := s.call(ctx, "drag", map[string]any{"tabId": tabID, "selector": selector, "to": to}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* WaitBrowser 等条件成立:mode=selector|text|time|networkIdle。 */
func (s *BrowserService) WaitBrowser(ctx context.Context, tabID, mode, selector, text string, timeoutMs int) (string, error) {
	timeout := bridgeOpTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs+5000) * time.Millisecond
	}
	reply, err := s.call(ctx, "wait", map[string]any{
		"tabId": tabID, "mode": mode, "selector": selector, "text": text, "timeoutMs": timeoutMs,
	}, timeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* DialogBrowser 给下一个原生对话框定应答（accept 确认,否则取消;text=prompt 输入）。 */
func (s *BrowserService) DialogBrowser(ctx context.Context, tabID string, accept bool, text string) (string, error) {
	reply, err := s.call(ctx, "dialog", map[string]any{"tabId": tabID, "accept": accept, "text": text}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* SelectTab 把某个标签切到前台（用户也会看到它）。 */
func (s *BrowserService) SelectTab(ctx context.Context, tabID string) (string, error) {
	reply, err := s.call(ctx, "activate", map[string]any{"tabId": tabID}, bridgeQuickTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/*
	ReadBrowser 读页面内容:mode=text 正文、html DOM、elements 可交互元素、
	links 链接、element 单元素(selector 定位)、frames 子帧清单(跨域帧够不着)。

timeoutMs 透传 desktop——页面仍在加载时先等收尾(默认 8000ms)再读,
免读到半页;桥等待上限随之放宽(超时+10s 余量给正文提取)。
*/
func (s *BrowserService) ReadBrowser(ctx context.Context, tabID, mode, selector string, chars, timeoutMs int) (string, error) {
	if chars <= 0 {
		chars = 4000
	}
	if chars > 20000 {
		chars = 20000
	}
	timeout := bridgeOpTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs+10000) * time.Millisecond
	}
	reply, err := s.call(ctx, "read", map[string]any{
		"tabId": tabID, "mode": mode, "selector": selector, "chars": chars, "timeoutMs": timeoutMs,
	}, timeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* EvalBrowser 在页面内执行 JS,返回求值结果(可改 DOM、读状态)。 */
func (s *BrowserService) EvalBrowser(ctx context.Context, tabID, js string) (string, error) {
	reply, err := s.call(ctx, "eval", map[string]any{"tabId": tabID, "js": js}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ConsoleBrowser 读/清控制台(errorsOnly 只留 error;mode=read|clear)。 */
func (s *BrowserService) ConsoleBrowser(ctx context.Context, tabID, mode string, errorsOnly bool) (string, error) {
	reply, err := s.call(ctx, "console", map[string]any{
		"tabId": tabID, "mode": mode, "errorsOnly": errorsOnly,
	}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* NetworkBrowser 读网络请求:index=0 清单,>0 该序号详情。 */
func (s *BrowserService) NetworkBrowser(ctx context.Context, tabID string, index int, filter string, errorsOnly bool) (string, error) {
	reply, err := s.call(ctx, "network", map[string]any{"tabId": tabID, "index": index, "filter": filter, "errorsOnly": errorsOnly}, bridgeOpTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/*
ScreenshotBrowser 截图:desktop 回分屏 PNG base64(整页按视口高切片,每片
原生分辨率——一张大图会被视觉模型压缩到看不清),core 逐张落盘 workspace/
browser/<tabID>-<时间戳>-<序号>.png(每张一个新文件——图片按路径进上下文与
时间线,同名覆写会让历史截图全被最后一张顶掉)。主模型有视觉时每张给一个
image_loaded 标记(filetools 的 OnLoop 会把批内全部标记合并成一条多图 user
消息),无视觉返回路径文字引导。
*/
func (s *BrowserService) ScreenshotBrowser(ctx context.Context, tabID string, fullPage bool, selector string, scale float64, timeoutMs int) (string, error) {
	/* timeoutMs 透传:desktop 先等加载收尾(默认 10000ms)再截,消除
	"返回即截图"撞首帧未合成的竞态;桥上限 = 等待 + 截图余量 */
	timeout := bridgeOpTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs+20000) * time.Millisecond
	}
	reply, err := s.call(ctx, "screenshot", map[string]any{
		"tabId": tabID, "fullPage": fullPage, "selector": selector, "scale": scale, "timeoutMs": timeoutMs,
	}, timeout)
	if err != nil {
		return "", err
	}
	if len(reply.Images) == 0 {
		return "", fmt.Errorf("截图失败:desktop 未回传图像数据")
	}
	if err := os.MkdirAll(s.shotDir, 0o755); err != nil {
		return "", fmt.Errorf("创建截图目录失败: %w", err)
	}
	base := time.Now().UnixMilli()
	var paths []string
	for i, b64 := range reply.Images {
		png, derr := base64.StdEncoding.DecodeString(b64)
		if derr != nil || len(png) == 0 {
			continue
		}
		path := filepath.Join(s.shotDir, fmt.Sprintf("%s-%d-%d.png", tabID, base, i+1))
		if werr := os.WriteFile(path, png, 0o644); werr != nil {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("截图数据解码失败")
	}
	head := fmt.Sprintf("[浏览器标签 %s 截图已生成(%d 张)]", tabID, len(paths))
	if reply.Note != "" {
		head += "\n" + reply.Note
	}
	if !s.ModelSeesImages() {
		return head + "\n已保存至 " + strings.Join(paths, "、") + ";当前模型无多模态能力,如需识别请在设置·模型为主模型勾选\"支持视觉\"", nil
	}
	markers := make([]string, 0, len(paths))
	for _, p := range paths {
		markers = append(markers, `<image_loaded path="`+p+`"/>`)
	}
	return head + "\n" + strings.Join(markers, "\n"), nil
}

/* CloseBrowserTab 关闭标签;幂等,返回 desktop 回执的 closed JSON。 */
func (s *BrowserService) CloseBrowserTab(ctx context.Context, tabID string) (string, error) {
	reply, err := s.call(ctx, "close", map[string]any{"tabId": tabID}, bridgeQuickTimeout)
	if err != nil {
		return "", err
	}
	return reply.Result, nil
}

/* ListBrowserTabsJSON 标签清单 JSON 文本(browser_list 工具直接返回)。 */
func (s *BrowserService) ListBrowserTabsJSON(ctx context.Context) string {
	reply, err := s.call(ctx, "list", nil, bridgeQuickTimeout)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return reply.Result
}

/*
TabsBrief 返回开启中标签的展示名清单("name（页面标题）[tabId]",
agent_status 快照每轮现查用;id 供模型直接 browser_action/read 与
交付 browser:// 入口,免一次 list 发现)。桥未连接/查询失败/超时
返回 nil(渲染为"（无）",不视为错误——web 端直连本就无浏览器)。
自带 2 秒短超时:每轮 OnStart 同步调用,desktop 卡死不能拖住轮首。
*/
func (s *BrowserService) TabsBrief() []string {
	ctx, cancel := context.WithTimeout(context.Background(), bridgeBriefTimeout)
	defer cancel()
	reply, err := s.call(ctx, "list", nil, bridgeBriefTimeout)
	if err != nil {
		return nil
	}
	var tabs []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Desc  string `json:"desc"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if json.Unmarshal([]byte(reply.Result), &tabs) != nil {
		return nil
	}
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		label := t.Name
		seg := briefSeg(t.Title, 60)
		if seg == "" {
			seg = briefSeg(t.Desc, 60)
		}
		if seg == "" {
			seg = briefSeg(t.URL, 60)
		}
		if seg != "" {
			label += "（" + seg + "）"
		}
		out = append(out, label+"["+t.ID+"]")
	}
	return out
}
