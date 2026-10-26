/* 鉴权令牌注入：?token= 转 sessionStorage；REST 走全局 fetch 补丁，SSE/WS 走 authUrl。 */

let token = ''

export function initToken(): void {
  const params = new URLSearchParams(location.search)
  const q = params.get('token')
  if (q) {
    token = q
    sessionStorage.setItem('ez-token', q)
    /* 只摘 token，其余参数（desktop/popout）留给后读的组件（App 的 popout
       分支在 mount 时才取 location.search，全清会把弹窗渲染成主界面） */
    params.delete('token')
    const rest = params.toString()
    history.replaceState(null, '', rest ? `${location.pathname}?${rest}` : location.pathname)
  } else {
    token = sessionStorage.getItem('ez-token') || ''
  }
  if (!token) return
  const orig = window.fetch
  window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    let u: URL
    try {
      u = new URL(raw, location.href)
    } catch {
      return orig(input, init)
    }
    /* 只给本机 API 补令牌：data:/跨域请求不塞自定义头（会触发无谓的 CORS 预检） */
    if (u.origin !== location.origin || !u.pathname.startsWith('/api/')) return orig(input, init)
    const headers = new Headers(init?.headers)
    headers.set('X-EZ-Token', token)
    return orig(input, { ...init, headers })
  }
}

/* EventSource/WebSocket 的连接地址（token 进 query）。 */
export function authUrl(u: string): string {
  return token ? `${u}${u.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}` : u
}

/* 当前令牌。 */
export function getToken(): string {
  return token
}
