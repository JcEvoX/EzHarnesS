/*
ez-mcp-auth.js —— 快应用调用 MCP 的统一 OAuth 组件。

用法（快应用页面，同源）：

    <script src="/ez-mcp-auth.js"></script>
    <script>
      const data = await ezMcp.call('notion', 'search', { q: 'hello' })
    </script>

`ezMcp.call` 调 POST /api/mcp/call；若目标 server 需要 OAuth 且未授权（后端
回 HTTP 401 + {needs_auth:true, auth_url}），组件会自动在最外层居中弹出一个
授权弹框（Shadow DOM 自带样式，位于页面所有内容之上，不参与页面的任何布局）：

  - 「去授权」→ 取授权 URL（必要时现取）→ 系统默认浏览器打开 → 轮询授权状态
  - 授权完成后弹框自动关闭，**同一次调用自动重试**并返回结果
  - 「取消」→ 取消后端流程，call 以 Error('cancelled') 拒绝
  - 轮询超时（默认 5 分钟）→ 以 Error('timeout') 拒绝

同一个 server 的并发授权请求会复用同一个弹框/流程。用户也完全可以在应用外
（会话里的待认证弹框、MCP 设置页、通知栏）完成授权——令牌全局共享，轮询同样
能感知到。

无需构建、无依赖；令牌与流程全在 core 侧，本文件只负责弹框与重试。
*/
;(function () {
  'use strict'

  var API_CALL = '/api/mcp/call'
  var API_START = '/api/mcp/oauth/start'
  var API_STATUS = '/api/mcp/oauth/status'
  var API_CANCEL = '/api/mcp/oauth/cancel'
  var POLL_MS = 1500
  var TIMEOUT_MS = 5 * 60 * 1000

  /* ── 弹框（Shadow DOM，隔离样式，固定居中于最外层）── */
  var host = null
  var shadow = null
  var els = null
  var cur = null // { server, authUrl, resolve, reject, poller, timer }

  var CSS =
    ':host{all:initial}' +
    '.mask{position:fixed;inset:0;display:flex;align-items:center;justify-content:center;' +
    'background:rgba(15,18,24,.42);-webkit-backdrop-filter:blur(3px);backdrop-filter:blur(3px);' +
    "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif}" +
    '.card{box-sizing:border-box;width:min(440px,88vw);padding:22px 24px;border-radius:16px;' +
    'background:#fff;color:#1a1d21;border:1px solid rgba(0,0,0,.08);box-shadow:0 18px 60px rgba(0,0,0,.28);' +
    'animation:in .16s ease-out}' +
    '@keyframes in{from{opacity:0;transform:translateY(6px) scale(.98)}to{opacity:1;transform:none}}' +
    '.icon{width:36px;height:36px;border-radius:10px;display:flex;align-items:center;justify-content:center;' +
    'background:rgba(59,130,246,.12);color:#3b82f6;margin-bottom:12px}' +
    '.icon svg{width:20px;height:20px}' +
    'h2{margin:0 0 8px;font-size:16px;font-weight:600;color:#1a1d21}' +
    '.msg{margin:0;font-size:13px;line-height:1.6;color:#4b5563}' +
    '.msg b{color:#1a1d21;font-weight:600}' +
    '.hint{margin:10px 0 0;font-size:12px;color:#3b82f6}' +
    '.err{margin:10px 0 0;font-size:12px;color:#dc2626;word-break:break-word}' +
    '.ops{display:flex;gap:10px;margin-top:20px}' +
    'button{font:inherit;font-size:13px;font-weight:500;border-radius:9px;padding:9px 18px;cursor:pointer;border:1px solid transparent}' +
    '.go{background:#3b82f6;color:#fff}' +
    '.go:hover{background:#2f74e0}' +
    '.go[disabled]{opacity:.55;cursor:default}' +
    '.cancel{background:transparent;color:#6b7280;border-color:rgba(0,0,0,.14)}' +
    '.cancel:hover{background:rgba(0,0,0,.04)}' +
    '@media (prefers-color-scheme:dark){' +
    '.mask{background:rgba(0,0,0,.55)}' +
    '.card{background:#1c1f24;color:#e6e8ea;border-color:rgba(255,255,255,.1)}' +
    'h2,.msg b{color:#fff}.msg{color:#b6bcc4}' +
    '.cancel{color:#9aa2ac;border-color:rgba(255,255,255,.16)}' +
    '.cancel:hover{background:rgba(255,255,255,.06)}}'

  function ensureHost() {
    if (host) return
    host = document.createElement('div')
    host.setAttribute('data-ez-mcp-auth', '')
    host.style.cssText = 'position:fixed;inset:0;z-index:2147483000;display:none'
    shadow = host.attachShadow({ mode: 'open' })
    shadow.innerHTML =
      '<style>' +
      CSS +
      '</style>' +
      '<div class="mask"><div class="card" role="dialog" aria-modal="true">' +
      '<div class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
      'stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.6 7.6a5 5 0 1 1-7.1 7.1 5 5 0 0 1 7.1-7.1zm0 0L15 8m0 0l3 3 3-3-3-3"/></svg></div>' +
      '<h2>MCP 需要授权</h2>' +
      '<p class="msg"></p>' +
      '<p class="hint" hidden>已打开浏览器，请在浏览器中完成授权…</p>' +
      '<p class="err" hidden></p>' +
      '<div class="ops"><button class="go" type="button">去授权</button>' +
      '<button class="cancel" type="button">取消</button></div>' +
      '</div></div>'
    document.body.appendChild(host)
    els = {
      msg: shadow.querySelector('.msg'),
      hint: shadow.querySelector('.hint'),
      err: shadow.querySelector('.err'),
      go: shadow.querySelector('.go'),
      cancel: shadow.querySelector('.cancel'),
    }
    els.go.addEventListener('click', onGo)
    els.cancel.addEventListener('click', function () {
      settle(new Error('cancelled'))
    })
  }

  function open(server, authUrl, metadataUrl) {
    ensureHost()
    cur = { server: server, authUrl: authUrl, metadataUrl: metadataUrl, poller: 0, timer: 0 }
    els.msg.innerHTML = '服务 <b></b> 需要 OAuth 认证才能调用工具。点「去授权」在浏览器中完成授权，完成后会自动重试本次调用。'
    els.msg.querySelector('b').textContent = server
    els.hint.hidden = true
    els.err.hidden = true
    els.go.disabled = false
    els.go.textContent = '去授权'
    host.style.display = 'block'
  }

  function setError(text) {
    els.err.textContent = text
    els.err.hidden = false
  }

  function settle(err) {
    var c = cur
    if (!c) return
    cur = null
    if (c.poller) clearInterval(c.poller)
    if (c.timer) clearTimeout(c.timer)
    if (host) host.style.display = 'none'
    if (err) {
      // 取消后端流程（幂等；授权已完成时也无害）
      fetch(API_CANCEL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: c.server }),
      }).catch(function () {})
      c.reject(err)
    } else {
      c.resolve()
    }
  }

  function onGo() {
    if (!cur) return
    var server = cur.server
    els.go.disabled = true
    els.go.textContent = '打开中…'
    els.err.hidden = true

    startAuth(server, cur.authUrl, cur.metadataUrl)
      .then(function (url) {
        if (!cur) return
        els.hint.hidden = false
        els.go.textContent = '重新打开授权页'
        els.go.disabled = false
        try {
          window.open(url, '_blank') // 桌面壳 → 系统浏览器；web 端 → 新标签
        } catch (e) {
          setError('无法打开浏览器，请手动访问：' + url)
        }
        beginPolling(server)
      })
      .catch(function (e) {
        if (!cur) return
        els.go.disabled = false
        els.go.textContent = '去授权'
        setError((e && e.message) || String(e))
      })
  }

  function beginPolling(server) {
    if (!cur || cur.poller) return
    var started = Date.now()
    cur.poller = setInterval(function () {
      if (!cur) return
      if (Date.now() - started > TIMEOUT_MS) {
        settle(new Error('timeout'))
        return
      }
      fetch(API_STATUS + '?name=' + encodeURIComponent(server))
        .then(function (r) {
          return r.json()
        })
        .then(function (s) {
          if (cur && s && s.authorized) settle(null)
        })
        .catch(function () {
          /* 后端不可达：下一轮重试 */
        })
    }, POLL_MS)
  }

  function startAuth(server, authUrl, metadataUrl) {
    if (authUrl) return Promise.resolve(authUrl)
    var body = { name: server }
    if (metadataUrl) body.metadata_url = metadataUrl
    return fetch(API_START, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(function (r) {
      return r.json().then(function (b) {
        if (!r.ok || !b.url) throw new Error(b.error || '无法获取授权地址')
        return b.url
      })
    })
  }

  /* ── 对外 API ── */

  var inflight = {} // server → Promise（同一 server 的并发授权复用）

  function ensureAuthorized(server, authUrl, metadataUrl) {
    if (inflight[server]) return inflight[server]
    var p = new Promise(function (resolve, reject) {
      if (cur) {
        // 已有别的 server 的弹框：等它结束后重试本 server
        var wait = setInterval(function () {
          if (!cur) {
            clearInterval(wait)
            ensureAuthorized(server, authUrl, metadataUrl).then(resolve, reject)
          }
        }, 200)
        return
      }
      open(server, authUrl, metadataUrl)
      cur.resolve = resolve
      cur.reject = reject
      cur.timer = setTimeout(function () {
        settle(new Error('timeout'))
      }, TIMEOUT_MS)
      // 自动进入授权（等价于点击「去授权」，省一步）
      onGo()
    })
    inflight[server] = p
    var clear = function () {
      delete inflight[server]
    }
    p.then(clear, clear)
    return p
  }

  /* deepParse 层层 JSON.parse 直到不能再解析（MCP result 常是嵌套的 JSON 文本） */
  function deepParse(v, depth) {
    depth = depth || 0
    if (typeof v !== 'string' || depth > 8) return v
    var s = v.trim()
    if (!s) return v
    var c = s.charAt(0)
    if (c !== '{' && c !== '[' && c !== '"') return v
    try {
      return deepParse(JSON.parse(s), depth + 1)
    } catch (e) {
      return v
    }
  }

  /*
  call(server, tool, args?) → Promise<result>
    调用 MCP 工具。需要授权时弹框、授权后自动重试一次。
    成功返回 result（已 deepParse）；失败/取消/超时以 Error 拒绝（message 为后端原因）。
  */
  function call(server, tool, args, _retried) {
    var body = { server: server, tool: tool, args: args == null ? {} : args }
    return fetch(API_CALL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(function (r) {
      if (r.status === 401) {
        return r
          .json()
          .catch(function () {
            return {}
          })
          .then(function (b) {
            if (!b.needs_auth) throw new Error(b.error || '未授权')
            return ensureAuthorized(b.server || server, b.auth_url || '', b.metadata_url || '').then(function () {
              if (_retried) throw new Error('授权后仍无法调用')
              return call(server, tool, args, true)
            })
          })
      }
      return r.text().then(function (text) {
        var parsed = deepParse(text)
        if (!r.ok) {
          var msg = parsed && parsed.error ? parsed.error : 'HTTP ' + r.status
          throw new Error(msg)
        }
        if (parsed && typeof parsed === 'object' && 'result' in parsed) {
          return deepParse(parsed.result)
        }
        return parsed
      })
    })
  }

  /* authorize(server) → Promise<void>：仅发起授权（不调用工具），供预授权用 */
  function authorize(server) {
    return ensureAuthorized(server, '')
  }

  window.ezMcp = { call: call, authorize: authorize, deepParse: deepParse }
})()
