/*
共享浏览器工具组(browser_tab / browser_action):AI 与用户操控魔法看板里
的同一批真实 Chromium 标签,全局共享(个人助手语义)。页面镜像到工作区
抽屉·浏览器页供用户实时共见;用户接管在抽屉页原生完成。检索、查资料、
操作网页用本组工具(与 terminal 互补:那是本机命令,这是真实浏览器);
截图经 image_loaded 标记转持久化图片消息(多模态模型直接看图)。

按 AI 心智分两面:tab 管标签生命周期;action 操作/观察单个标签页——
操作(navigate/click/type/key/scroll)、观察(screenshot/read)、
devtools(eval/console/network)。依赖倒置:tools 只依赖 BrowserIO 接口,
由 service.BrowserService 实现(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"
	"fmt"

	"github.com/xuanlv2002/ezloop/types"
)

/*
	BrowserIO 是共享浏览器服务的能力面(service.BrowserService 实现)。

ctx 透传本轮上下文:用户停止时打断在途的桥调用等待。
操作返回统一为标签状态 JSON:id/name/desc/origin/url/title/loading/note(read 正文放 output)。
*/
type BrowserIO interface {
	/* StartBrowser 新建标签(name 必填,desc 可选)并可选导航 */
	StartBrowser(ctx context.Context, name, desc, url string, timeoutMs int) (string, error)
	/* NavigateBrowser 跳转并等加载 */
	NavigateBrowser(ctx context.Context, tabID, url string, timeoutMs int) (string, error)
	/* ClickBrowser 点击(按 CSS 选择器定位) */
	ClickBrowser(ctx context.Context, tabID, selector string) (string, error)
	/* TypeBrowser 输入文本(selector 定位输入框,submit 回车提交) */
	TypeBrowser(ctx context.Context, tabID, selector, text string, submit bool) (string, error)
	/* PressBrowserKey 按键/组合键("Enter"、"Control+A") */
	PressBrowserKey(ctx context.Context, tabID, combo string) (string, error)
	/* ScrollBrowser 滚动(up|down) */
	ScrollBrowser(ctx context.Context, tabID, direction string, amountPx int) (string, error)
	/* HoverBrowser 悬停到元素(触发悬浮菜单/提示) */
	HoverBrowser(ctx context.Context, tabID, selector string) (string, error)
	/* SelectOptionBrowser 选 <select> 选项(按 value 或可见文本匹配) */
	SelectOptionBrowser(ctx context.Context, tabID, selector, value string) (string, error)
	/* UploadBrowser 给 <input type=file> 设置文件(files=本机绝对路径,逗号分隔) */
	UploadBrowser(ctx context.Context, tabID, selector, files string) (string, error)
	/* WaitBrowser 等条件成立(mode=selector|text|time|networkIdle) */
	WaitBrowser(ctx context.Context, tabID, mode, selector, text string, timeoutMs int) (string, error)
	/* DialogBrowser 给下一个原生对话框定应答(accept 确认,否则取消) */
	DialogBrowser(ctx context.Context, tabID string, accept bool) (string, error)
	/* ResetBrowser 复位 eval 留下的全局视觉变形(html/body 的 zoom/transform/filter) */
	ResetBrowser(ctx context.Context, tabID string) (string, error)
	/* ReadBrowser 读页面(modes 见工具说明;selector 供 html/element 定位) */
	ReadBrowser(ctx context.Context, tabID, mode, selector string, chars, timeoutMs int) (string, error)
	/* ScreenshotBrowser 截图(默认视口,fullPage=true 截整页;落盘+image_loaded 标记;timeoutMs=等首帧上限) */
	ScreenshotBrowser(ctx context.Context, tabID string, fullPage bool, selector string, timeoutMs int) (string, error)
	/* EvalBrowser 页面内执行 JS(改 DOM / 读状态;返回求值结果) */
	EvalBrowser(ctx context.Context, tabID, js string) (string, error)
	/* ConsoleBrowser 读/清控制台(errorsOnly 只留 error;mode=read|clear) */
	ConsoleBrowser(ctx context.Context, tabID, mode string, errorsOnly bool) (string, error)
	/* NetworkBrowser 读网络请求:index=0 返回清单,>0 返回该序号的详情(headers+响应体) */
	NetworkBrowser(ctx context.Context, tabID string, index int, filter string, errorsOnly bool) (string, error)
	/* CloseBrowserTab 关闭标签,返回 {"id":..,"closed":true} */
	CloseBrowserTab(ctx context.Context, tabID string) (string, error)
	/* ListBrowserTabsJSON 标签清单(JSON 数组,条目含 id/name/desc/origin/url/title/loading/active) */
	ListBrowserTabsJSON(ctx context.Context) string
	/* SelectTab 把某个标签切到前台(用户也会看到它) */
	SelectTab(ctx context.Context, tabID string) (string, error)
	/* ModelSeesImages 主模型是否多模态(截图返回形态裁决) */
	ModelSeesImages() bool
}

type browserTabArgs struct {
	Action    string `json:"action" desc:"open=新建标签(可带首跳网址);list=列出全部标签;select=切到前台(用户也会看到);close=关闭标签"`
	Name      string `json:"name,omitempty" desc:"open:标签名称,简短标识(如 查竞品、看文档),list 与看板中展示"`
	Desc      string `json:"desc,omitempty" desc:"open:标签描述,详细说明用途(可选)"`
	URL       string `json:"url,omitempty" desc:"open:创建后立即打开的网址(可选)"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"open:导航等待上限毫秒,默认 20000;首次使用会自动下载 Chromium,此时给 600000"`
	TabID     string `json:"tabId,omitempty" desc:"select/close:目标标签 id"`
}

type browserActionArgs struct {
	Action     string `json:"action" desc:"navigate=打开网址;click=点击;hover=悬停;type=输入文本;select=选下拉项;key=按键;scroll=滚动;upload=上传文件;screenshot=截图;read=读页面;wait=等条件;eval=页面内执行JS;console=读/清控制台;network=读网络请求;dialog=应答原生对话框;reset=复位eval改的全局样式"`
	TabID      string `json:"tabId" desc:"目标标签 id(browser_tab 的 list 可查)"`
	URL        string `json:"url,omitempty" desc:"navigate:要打开的网址"`
	TimeoutMs  int    `json:"timeoutMs,omitempty" desc:"navigate/screenshot/read/wait:等待上限毫秒(navigate 默认 20000,截图 10000,读取 8000,wait 15000;慢页可调大)"`
	Selector   string `json:"selector,omitempty" desc:"CSS 选择器(click/hover/type/select/upload/screenshot/read mode=element·html;支持穿透 shadow DOM 与同源 iframe)"`
	FullPage   bool   `json:"fullPage,omitempty" desc:"screenshot:截整页(默认只截当前视口;整页按视口高分段,段数超上限只截前几段并带说明)"`
	Mode       string `json:"mode,omitempty" desc:"read:text=正文(默认);html=DOM;elements=可交互元素清单(含选择器);links=链接;element=单元素详情;frames=子帧清单。wait:selector(默认)/text/time/networkIdle。console:read(默认)/clear"`
	Index      int    `json:"index,omitempty" desc:"network:清单里的序号 #n,给了就返回该条详情(请求/响应头 + 响应体)"`
	ErrorsOnly bool   `json:"errorsOnly,omitempty" desc:"console:只看 error;network:只看失败/4xx/5xx"`
	Filter     string `json:"filter,omitempty" desc:"network:URL 子串过滤"`
	Chars      int    `json:"chars,omitempty" desc:"read:text/html/elements/links:返回字符数上限,默认 4000,上限 20000"`
	JS         string `json:"js,omitempty" desc:"eval:在页面内执行的 JS(可改 DOM、读状态;返回求值结果。已注入 __ezOne/__ezAll 深查询辅助)"`
	Text       string `json:"text,omitempty" desc:"type:要输入的文本(支持中文,追加不覆盖);wait mode=text:要出现的文本"`
	Submit     bool   `json:"submit,omitempty" desc:"type:输入后提交所属表单(走 form.requestSubmit,等效回车隐式提交)"`
	Value      string `json:"value,omitempty" desc:"select:要选的 option 值或可见文本"`
	Files      string `json:"files,omitempty" desc:"upload:本机文件绝对路径,多个用逗号分隔"`
	Accept     bool   `json:"accept,omitempty" desc:"dialog:true=确认 false=取消(不调用 dialog 时默认取消,页面永不挂住)"`
	Combo      string `json:"combo,omitempty" desc:"key:按键,如 Enter、Escape、Tab、ArrowDown、Control+A、Shift+ArrowDown"`
	Direction  string `json:"direction,omitempty" desc:"scroll:方向 up/down/left/right"`
	AmountPx   int    `json:"amountPx,omitempty" desc:"scroll:像素量,默认 600"`
}

/* SharedBrowser 构造浏览器两工具(b 为 nil 返回 nil,测试装配可不注入)。 */
func SharedBrowser(b BrowserIO) []types.Tool {
	if b == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("browser_tab",
			"浏览器标签管理(真实 Chromium,画面镜像到工作区抽屉·浏览器页,用户实时共见可接管)。"+
				"action=open 新建标签并可选导航——name 是简短名称、desc 是详细描述,url 可选,"+
				"首次使用会自动下载 Chromium(约 150MB,timeoutMs 给 600000);"+
				"action=list 列出全部标签(用户手动开的与 AI 新开的都在内,所有会话共享);"+
				"action=close 关闭标签(用完及时关,释放资源)。"+
				"返回 JSON:id/name/desc/origin/url/title/loading/note(close 为 {id,closed})。",
			func(ctx context.Context, in *browserTabArgs) (string, error) {
				switch in.Action {
				case "open":
					return b.StartBrowser(ctx, in.Name, in.Desc, in.URL, in.TimeoutMs)
				case "list":
					return b.ListBrowserTabsJSON(ctx), nil
				case "select":
					return b.SelectTab(ctx, in.TabID)
				case "close":
					return b.CloseBrowserTab(ctx, in.TabID)
				}
				return "", fmt.Errorf("未知 action %q(open/list/select/close)", in.Action)
			}),
		types.NewTool("browser_action",
			"操作/观察一个浏览器标签页(tabId 必填,browser_tab list 可查)。"+
				"操作页:action=navigate 打开网址并等加载;click 点击——给 CSS 选择器(先 read mode=elements 拿可交互元素与选择器),"+
				"选择器拿不到时用 eval 在页面里按文本/结构找到元素直接 .click(),不要靠截图估像素坐标(不可靠);type 输入文本(selector 定位输入框,省略=当前焦点处;追加不覆盖,要提交给 submit=true——走 form.requestSubmit,合成回车触发不了提交);hover 悬停(触发悬浮菜单/提示);select 选下拉项(selector+value);"+
				"key 按键/组合键(合成按键不触发默认动作——不要用它提交表单);scroll 滚动(direction=up/down/left/right,横向溢出用 left/right);upload 给 <input type=file> 传本机文件(selector+files)。"+
				"观察页:action=screenshot 截图(默认当前视口——就是浏览器真实渲染的那一块,尺寸与用户所见一致;fullPage=true 截整页看整体结构,按视口高分段;给 selector 只截该元素。不做设备仿真,截的就是真实大小。经 image_loaded 转图片消息,多模态模型直接看图,只用于看外观/布局);"+
				"read 读页面内容(见 mode;跨域 iframe 读不到会用 frames 列出帧地址);wait 等条件成立再往下(mode=selector/text/time/networkIdle)——点完别急着 read,先 wait 免读到渲染前。"+
				"devtools(排障):action=eval 在页面内执行 JS(改 DOM、读状态;已注入 __ezOne/__ezAll 深查询辅助。改了 html/body 的 zoom/transform/filter 会持续污染后续所有截图与坐标,会当场警告,用 reset 还原);console 读控制台(含未捕获异常与浏览器日志如 CSP/混合内容,errorsOnly 只看 error,mode=clear 清空);network 读网络请求(序号 #n;index=n 取该条详情:请求/响应头+响应体;filter 过滤 URL;errorsOnly 只看失败)。首次读 console/network 才挂采集,在它之前的请求看不到。"+
				"操作类返回含页面实时 url/title/scrollY(以此判断是否生效);点击打开新标签时 note 会明示。",
			func(ctx context.Context, in *browserActionArgs) (string, error) {
				switch in.Action {
				case "navigate":
					return b.NavigateBrowser(ctx, in.TabID, in.URL, in.TimeoutMs)
				case "click":
					return b.ClickBrowser(ctx, in.TabID, in.Selector)
				case "hover":
					return b.HoverBrowser(ctx, in.TabID, in.Selector)
				case "type":
					return b.TypeBrowser(ctx, in.TabID, in.Selector, in.Text, in.Submit)
				case "select":
					return b.SelectOptionBrowser(ctx, in.TabID, in.Selector, in.Value)
				case "key":
					return b.PressBrowserKey(ctx, in.TabID, in.Combo)
				case "scroll":
					return b.ScrollBrowser(ctx, in.TabID, in.Direction, in.AmountPx)
				case "upload":
					return b.UploadBrowser(ctx, in.TabID, in.Selector, in.Files)
				case "screenshot":
					return b.ScreenshotBrowser(ctx, in.TabID, in.FullPage, in.Selector, in.TimeoutMs)
				case "read":
					return b.ReadBrowser(ctx, in.TabID, in.Mode, in.Selector, in.Chars, in.TimeoutMs)
				case "wait":
					return b.WaitBrowser(ctx, in.TabID, in.Mode, in.Selector, in.Text, in.TimeoutMs)
				case "eval":
					return b.EvalBrowser(ctx, in.TabID, in.JS)
				case "console":
					return b.ConsoleBrowser(ctx, in.TabID, in.Mode, in.ErrorsOnly)
				case "network":
					return b.NetworkBrowser(ctx, in.TabID, in.Index, in.Filter, in.ErrorsOnly)
				case "dialog":
					return b.DialogBrowser(ctx, in.TabID, in.Accept)
				case "reset":
					return b.ResetBrowser(ctx, in.TabID)
				}
				return "", fmt.Errorf("未知 action %q(navigate/click/hover/type/select/key/scroll/upload/screenshot/read/wait/eval/console/network/dialog/reset)", in.Action)
			}),
	}
}
