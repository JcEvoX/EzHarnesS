/* 快应用启动：入口 URL 带上当前访问令牌（页面注入的桥会转交给页内 fetch），
   desktop 壳开子窗口、web 开新标签。 */
import { api } from './api'
import { store } from './store.svelte'
import { authUrl } from './auth'

export async function launchApp(name: string): Promise<void> {
  const r = await api.openApp(name)
  const url = authUrl(r.path)
  const desktopWindow = (window as any).ez?.window
  if (desktopWindow) desktopWindow.openApp(url, r.title)
  else window.open(url, '_blank', 'noopener') // 不带 opener：应用页拿不到主界面的 sessionStorage
  if (r.termId) store.openTermAt(r.termId)
}
