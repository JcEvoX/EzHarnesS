---
name: quick-app
description: 构建/更新快应用——判断纯前端还是需要后端，app.quick 协议字段与后端约定（需要本机命令能力、MCP 也覆盖不了时用）
---

# 快应用构建

快应用 = `<数据目录>/apps/<名>/` 目录（数据目录绝对路径见 system 提示 `<workspace>` 段，apps 行给出的是同一个位置）。目录里有 `app.quick` 声明才算一个应用。

用户点击启动 = **打开应用窗口**；声明了 `backend` 就在 ezharness 内置终端里把后端跑起来。

## app.quick 协议

```json
{
  "title": "证书探测",
  "entry": "index.html",
  "backend": "python probe.py",
  "icon": "icon.svg"
}
```

- `title`：卡片标题；缺省取 entry 文件的 `<title>`，再缺省用目录名
- `entry`：前端入口（相对应用目录），缺省 `index.html`
- `backend`：后端启动命令，cwd 是应用目录；**省略 = 纯前端**，只开窗不起终端
- `icon`：卡片头像（相对应用目录的图片文件名，应用目录内、不可上溯）
- 未识别的字段一律忽略——协议向前兼容，加字段不会破旧版

## 先规划，再执行

多步应用（判断形态、选数据通道、页面结构、头像、落盘与验证）**先调用 plan 工具再动手**：

- `action=new` 提交方案：`plan` 按 目标/方案/步骤 写清「纯前端还是后端、数据从哪来、页面怎么组织」；`progress` 拆成 3–6 个可跟踪步骤。新方案会请用户审批，**批准后再写文件**，避免方向错了返工。
- 批准后按步骤执行，每完成一步 `action=update` 同步进度（progress 整表替换）。
- 用户改需求、原方案不再适用时重新 `new`，不要口头改方案；任务完成后保留全部 completed 即可。
- 一次 `save_app` 就能写完的纯静态小页面不用 plan。

## 应用文档与版本

- `save_app` 的 `description` / `version` / `document` 写入 `app.quick`；`description` 一句话，`version` 用语义化版本，`document` 缺省 `APP.md`。
- 可复用或要开放给 agent 的应用写应用文档（默认 `APP.md`）：frontmatter（name/description/version）+「能力 / Agent 调用 / 人类使用 / 限制」。
  **只有包含「Agent 调用」一节的应用才对 agent 开放**；这一节写清入口、输入、输出、副作用、失败语义、就绪条件——前端可以是一段 JS（如 `window.<app>.<method>()`），后端可以是 HTTP/CLI，规范不限定形态。
- 每次版本变化在应用目录追加 `CHANGELOG.md`（版本、日期、变更摘要）。
- 使用已有应用：清单在 system 的 `<apps>` 段（轮内新增/移除见 `<resource_change>`），用 `load_app` 读调用文档；不要凭 `app.quick` 猜调用方式，也不要给没有「Agent 调用」一节的应用臆造接口。

## 怎么建

**先判断形态，别一上来就写后端：**

1. **能用 MCP 就用 MCP**：本机已配的 MCP 服务可经页面同源直调（`POST /api/mcp/call`，见 `<mcp>` 段），不必起后端。
   用到大模型能力（总结/翻译/识别/生成）时用 `POST /api/model/call`（见下「大模型直调」），也不必起后端。
2. **纯前端**：`save_app`（name/title/html）一次写全 `app.quick` + `index.html`。
3. **需要后端**：`save_app` 带 `backend` 建骨架，再用 `write_file` 把后端脚本等文件写进同一目录（save_app 只写声明与前端，它引用的脚本要先写好）。

需要本机命令能力（TLS 证书探测、本地文件处理、调非 MCP 的本地程序）而 MCP 又覆盖不了时，才走第 3 条。

## 头像（icon）

**建应用时顺手给一张**——快应用卡片和桌面小助手钉选都显示它。

- 首选 **SVG**（纯代码可写）：`write_file` 写 `apps/<名>/icon.svg`，`save_app` 带 `"icon": "icon.svg"`；PNG 同理（需要二进制写出时才用）。
- 绘制要求：**128×128 视图框、透明底**；一个主题色大色块背景 + 简单几何图形或 1-2 个字母/字符表意（如证书探测 → 盾形 + "TLS"），风格干净扁平；**不要**复杂插画、照片、密集文字，缩小到 16px 仍可辨识。
- 色彩用应用自己的主题色（与页面主色一致最好）；没有想法就用中性色 + 首字母。
- 没给 icon 时卡片回落默认代码图标，不报错。

## 后端约定

- 命令在**应用目录**里执行（cwd = 应用目录），相对路径直接可用。
- **前台**运行：不要 `start /b`、不要 `&`——日志留在那个终端里，用户和你都看得到；停服务 = 在那个终端 Ctrl+C 或关掉它。后台化会让输出与退出都失控。
- 端口自己定一个高位端口，页面里用同一个值。
- 只依赖用户机器上已有的运行时（python/node 等），不要在脚本里装依赖。

## 页面与 core 的关系

- 前端由 core **同源**伺服（`/apps/<名>/`），所以 `fetch('/api/mcp/call')` 等 core 接口直接可用。
- 后端在另一个端口 = **跨源**：页面直接 fetch 后端时，后端必须返回 CORS 头（`Access-Control-Allow-Origin: *`），否则浏览器拦掉。
- **没有就绪握手**：点启动会立刻开窗，后端可能还没监听——页面首次请求自带重试（如 3 次 × 300ms），不要假设一开就有响应。

## 调 MCP（统一组件，别自己写 fetch）

快应用调 MCP 一律走组件——普通 server 直接返回结果；需 OAuth 的自动弹框、开浏览器、授权后重试，**不要自己写 fetch + 弹框 + 轮询**：

```html
<script src="/ez-mcp-auth.js"></script>
<script>
  // 需要授权时组件会自动在页面最外层居中弹出授权框（自带样式、隔离在 Shadow DOM），
  // 用户点「去授权」→ 系统浏览器打开 → 授权完成后自动重试并返回结果。
  const data = await ezMcp.call('notion', 'search', { q: 'hello' })
</script>
```

- `ezMcp.call(server, tool, args?)` → Promise，resolve 为工具结果（已层层 `JSON.parse` 解包）；失败/取消/超时以 `Error` reject（`message` 是原因）。
- `ezMcp.authorize(server)` → Promise：只发起授权、不调用工具（做「先授权再用」的入口）。
- 组件是**单文件零依赖、Shadow DOM 自带样式**，弹框 `position:fixed` 盖在最外层、不参与页面布局，页面的 CSS 不会污染它、它也不污染页面。
- 同一个 server 的并发调用共用一次授权；用户在应用外（会话弹框 / MCP 设置页 / 通知栏）授权同样有效（令牌全局共享）。

**底层契约（组件已封装，仅当你要自己实现时才需要知道）**：`POST /api/mcp/call` 遇未授权返回 **HTTP 401 + `{"needs_auth":true,"server":"…","auth_url":"…"}`**；`auth_url` 可能为空，则调 `POST /api/mcp/oauth/start {"name":"<server>"}` 现取；轮询 `GET /api/mcp/oauth/status?name=<server>` 返回 `{"authorized":bool,"pending":bool}`；取消用 `POST /api/mcp/oauth/cancel {"name":"<server>"}`。**授权必须由用户本人在浏览器完成，页面/后端都不要替用户跑授权流程。**

## 大模型直调（model call）

前端/后端都可以同源直调大模型，**无需任何密钥**（密钥由 core 持有）：

```js
const r = await fetch('/api/model/call', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ prompt: '把下面这段翻译成英文：…' })
})
const { result } = await r.json()   // result 是模型返回的文本
```

- body：`{prompt: string, model?: string, images?: string[]}`。`model` 省略 = 当前主模型；填模型名（system `<models>` 段清单里的条目）可选更合适的模型。
- `images` 是**本机图片绝对路径**列表（最多 4 张）——识别/描述图片时用，但目标模型必须支持视觉（模型页可见「支持视觉」标记），否则返回报错。
- 返回：`200 {"result": "模型输出文本"}`；失败非 2xx 且 `{"error":"原因"}`。
- 单次调用最长 5 分钟，失败直接报错，不重试。
- 用途：翻译、总结、分类、图片识别（把用户选的图片路径传进去）、内容生成等"一次问答"——需要多步推理或工具的活走 MCP/后端，别硬塞进一次 prompt。

## 重启与边界

- 同名应用的后端已在跑时，再点启动只开窗聚焦，不会重复起。要重启：先在终端里 Ctrl+C（或关掉那个终端），再点一次。
- 换代（改设置触发 core 重启）会**连带回收正在跑的后端**（终端随代际关闭），重启后重新点启动。
- 密钥不要写进页面（页面同源，能调 core 接口）；不手改其他快应用的文件。
