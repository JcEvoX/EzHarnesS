/* 快应用启动：首次打开先由用户明确授权；入口 URL 携带受限令牌
   （只放行模型调用与 MCP 接口），desktop 壳开子窗口、web 开新标签。 */
import { api } from './api'
import { store } from './store.svelte'

export async function launchApp(name: string): Promise<void> {
  let r = await api.openApp(name)
  if (r.needsTrust) {
    const ok = await store.askAppTrust(name, r.title || name)
    if (!ok) return
    await api.trustApp(name)
    r = await api.openApp(name)
  }
  const url = r.token ? `${r.path}?token=${encodeURIComponent(r.token)}` : r.path
  const desktopWindow = (window as any).ez?.window
  if (desktopWindow) desktopWindow.openApp(url, r.title)
  else window.open(url, '_blank', 'noopener') // 不带 opener：应用页拿不到主界面的 sessionStorage
  if (r.termId) store.openTermAt(r.termId)
}
