# 快应用规范（草案 v0.1）

快应用是 AI 构建、快速迭代、人与 agent 共用的应用形态。它不是固定技术栈：可以是一个纯 HTML/JS 页面，也可以带本机后端。

本规范只约定**如何被发现、启动、描述自己**，不限定应用内部怎么实现、用什么框架、暴露什么形态的接口。

## 1. 硬契约（必须满足）

```
apps/<app-id>/
├── app.quick          # 必须有：应用声明
└── <entry>            # entry 指向的入口，默认 index.html
```

- `app-id` 就是目录名：全局唯一、创建后不改（它是应用身份，不是标题）。
- 目录里有 `app.quick` 才算快应用；没有该文件的其他目录一律忽略。
- `entry` 指向的文件必须位于应用目录内，不允许上溯到目录外。
- 应用自包含：所有页面、脚本、素材、数据默认放在自己的目录里，不修改其他快应用的文件。
- 未声明的内部文件（脚本、组件、后端程序、资源）完全由应用自己组织，规范不干预。

## 2. app.quick

JSON 文件。未识别的字段一律忽略——这是协议向前兼容的基础：新规范加字段不会破旧内核，旧应用也不会因为内核升级而失效。

| 字段 | 必填 | 说明 |
|------|------|------|
| `title` | 否 | 人类可读名称；缺省取 entry 的 `<title>`，再缺省用目录名 |
| `description` | 否 | 一句话描述（建议写），用于应用列表与 agent 发现 |
| `version` | 否 | 语义化版本（建议写），如 `1.3.0` |
| `entry` | 否 | 前端入口，相对应用目录，默认 `index.html` |
| `backend` | 否 | 后端启动命令，cwd 为应用目录；省略 = 纯前端，只开窗不起终端 |
| `icon` | 否 | 卡片头像（应用目录内的图片文件名） |
| `document` | 否 | 应用文档路径，默认 `APP.md`（见第 3 节） |

```json
{
  "title": "证书探测",
  "description": "探测域名 HTTPS 证书链与到期时间",
  "version": "1.3.0",
  "entry": "index.html",
  "backend": "python server.py",
  "icon": "icon.svg",
  "document": "APP.md"
}
```

## 3. 应用文档：agent 可调用性的唯一依据

是否允许 agent 调用，**不由内核开关决定，也不由 app.quick 的字段决定**，只取决于应用是否提供了自己的调用文档。

- 文档路径由 `app.quick.document` 指定，默认 `APP.md`。
- 系统提示的 `<apps>` 段常驻应用清单（与技能同构）；轮内新增/更新/移除由 `<resource_change>` 报告，`load_app(name)` 读取本文档与版本历史。
- 文档存在且包含「Agent 调用」一节 → 应用对 agent 开放。
- 没有文档，或文档没有「Agent 调用」一节 → 应用只面向人类；agent 仍可按用户要求用浏览器操作页面，但那只是代操作，不是应用接口。

### 3.1 文档格式

建议用 Markdown，frontmatter 供内核索引（没有 frontmatter 也能读，只是不利于发现）：

```markdown
---
name: certprobe
description: 探测域名 HTTPS 证书链与到期时间
version: 1.3.0
---

# 能力
说明应用能做什么。

# Agent 调用
说明 agent 怎么调用；这一节存在才代表应用对 agent 开放。

# 人类使用
界面入口与操作说明。

# 限制
依赖、端口、数据位置、已知边界。
```

### 3.2 「Agent 调用」写什么

规范不限定调用方式。前端 JS、HTTP、CLI、文件、DOM 操作都可以，但必须写清：

- 入口：从哪里调用（全局对象 / URL / 命令 / 文件）
- 输入：参数名、类型、必填项
- 输出：返回结构或可观察结果
- 副作用：会改什么、会不会启动进程、会不会写文件
- 失败语义：抛错 / 非 2xx / 返回错误字段
- 就绪条件：后端是否必须先启动、首次调用是否需要重试

### 3.3 示例：纯前端（一段 JS）

```markdown
# Agent 调用

页面加载完成后，在页面上下文执行：

    window.certprobe.probe({ domain: "example.com" })
      → Promise<{ subject: string, expiresAt: string }>

无副作用，只读。失败以 Promise reject 返回 Error(message)。
```

### 3.4 示例：带后端（HTTP）

```markdown
# Agent 调用

POST http://127.0.0.1:18731/api/probe
Content-Type: application/json

{ "domain": "example.com" }

200 → { "subject": "...", "expiresAt": "..." }
非 2xx → { "error": "原因" }

后端由应用启动命令拉起；首次请求可能未就绪，按 3 次 × 300ms 重试。
```

## 4. 版本与变更

- 每次交付更新 `app.quick.version`。
- 建议在应用目录维护 `CHANGELOG.md`，每个版本一条：版本号、日期、变更摘要。这是约定，不是内核强校验。
- 兼容性原则：新增字段不破坏旧内核；破坏性变更升 major；`entry` 路径尽量保持稳定，避免用户书签/外部调用失效。

## 5. 运行契约

- 前端由 core 同源伺服在 `/apps/<app-id>/`，因此可直接调用 core 暴露的本机 API（MCP、模型调用等），无需密钥。
- 声明了 `backend` 的应用，用户点击启动时命令在应用目录中前台执行；不要后台化（`start /b`、`&`），否则日志与生命周期都失控。
- 后端是独立端口 = 跨源：页面直接请求后端时，后端必须返回 CORS 头。
- 没有就绪握手：窗口先开、后端后起，页面首次请求要自带重试。
- 页面里不要写密钥；需要 core 能力时走同源接口。
- 需要持久化的数据由应用自己定义位置，并在文档里写明；不要默认写到应用目录之外。

## 6. 作者检查清单

- [ ] `app.quick` 存在，`entry` 在目录内
- [ ] `description` / `version` 已填
- [ ] 需要 agent 调用：`APP.md`（或 `document` 指向的文件）含「Agent 调用」一节，且入口/输入/输出/副作用/失败语义齐全
- [ ] 不需要 agent 调用：不要写「Agent 调用」，应用自然保持人类专属
- [ ] `CHANGELOG.md` 已追加本次变更
- [ ] 有图标时给出 128×128 的 SVG/PNG
- [ ] 后端前台运行，端口与页面一致（或文档写明）

## 7. 当前实现

- `app.quick` 读取 `title`、`description`、`version`、`entry`、`backend`、`icon`、`document`；`save_app` 可直接写入 `description` / `version` / `document`。
- 系统提示的 `<apps>` 段列出应用清单（name、标题、版本、描述、是否可调用），与技能清单同构。
- 应用新增/更新/移除进 `<resource_change>`（变更后附完整 `available_app` 清单与描述）。
- `load_app(name)` 读取元数据、应用文档与 `CHANGELOG.md`（对应技能的 `load_skill`）。
- 是否可调用只看文档里有没有「Agent 调用」标题。具体调用方式由文档定义，agent 用 `browser_action`（前端 JS）或 `terminal`（HTTP/CLI）按文档执行；内核不提供统一的 `app_call` 转发——这正是为了不把应用形态写死。
