<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import { isDesktop } from '../../lib/desktop'

  /*
  魔法看板·浏览器 tab(第一期:iframe 简版)。地址栏导航 + 后退/前进/
  刷新;iframe 跨域拿不到内部跳转,历史只记录本栏发起的导航。
  部分站点(GitHub/Google 等)响应 X-Frame-Options/CSP 禁止内嵌——
  iframe 无法可靠探测,常驻提示条 +「独立窗口」(Wails 子窗口 WebView,
  能力完整)兜底;AI 共览/操控留待下期 CDP 方案。
  */

  let addr = $state('') // 地址栏编辑值
  let hist = $state<string[]>([])
  let idx = $state(-1)
  let frameKey = $state(0) // 刷新用:key 重挂 iframe

  $effect(() => {
    /* 外部入口(store.browserURL,如消息链接)→ 导航;重复点同链接仍刷新 */
    const u = store.browserURL
    if (u) navigate(u, true)
  })

  function normalize(input: string): string {
    const t = input.trim()
    if (!t) return ''
    if (/^https?:\/\//i.test(t)) return t
    if (/^[\w-]+(\.[\w-]+)+([/?#].*)?$/.test(t)) return 'https://' + t
    return 'https://www.bing.com/search?q=' + encodeURIComponent(t)
  }

  function navigate(u: string, push: boolean) {
    addr = u
    if (push) {
      hist = [...hist.slice(0, idx + 1), u]
      idx = hist.length - 1
    }
    frameKey++
  }

  function go() {
    const u = normalize(addr)
    if (u) navigate(u, true)
  }

  function back() {
    if (idx > 0) {
      idx--
      addr = hist[idx]
      frameKey++
    }
  }

  function forward() {
    if (idx < hist.length - 1) {
      idx++
      addr = hist[idx]
      frameKey++
    }
  }

  /* 独立窗口:桌面壳开 WebView 子窗口(无内嵌限制);浏览器模式退化为新标签 */
  async function openExternal() {
    const u = hist[idx]
    if (!u) return
    if (isDesktop) {
      const r = await fetch('/api/window/open-web', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: u }),
      })
      if (!r.ok) window.open(u, '_blank', 'noreferrer')
    } else {
      window.open(u, '_blank', 'noreferrer')
    }
  }
</script>

<div class="browser">
  <div class="bar">
    <button class="nav" disabled={idx <= 0} onclick={back} title="后退">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6" /></svg>
    </button>
    <button class="nav" disabled={idx >= hist.length - 1} onclick={forward} title="前进">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6" /></svg>
    </button>
    <button class="nav" onclick={() => (frameKey++)} title="刷新">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36" /><path d="M21 3v6h-6" /></svg>
    </button>
    <input
      class="addr"
      placeholder="输入网址或搜索…"
      value={addr}
      oninput={(e) => (addr = (e.currentTarget as HTMLInputElement).value)}
      onkeydown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          go()
        }
      }}
    />
    <button class="ext" onclick={() => void openExternal()} disabled={idx < 0} title="独立窗口打开(不受内嵌限制)">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
        <path d="M15 3h6v6" />
        <path d="M10 14L21 3" />
      </svg>
      独立窗口
    </button>
  </div>
  <p class="hint">部分网站(GitHub、Google 等)禁止内嵌,页面空白时请点「独立窗口」打开。</p>

  <div class="view">
    {#key frameKey}
      {#if idx >= 0}
        <iframe src={hist[idx]} title="看板浏览器" referrerpolicy="no-referrer-when-downgrade"></iframe>
      {/if}
    {/key}
    {#if idx < 0}
      <div class="empty">
        <p>输入网址,或点击消息里的链接在此打开</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .browser {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 8px;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .nav {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 8px;
    color: var(--muted);
  }
  .nav:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .nav svg {
    width: 13px;
    height: 13px;
  }
  .addr {
    flex: 1;
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg);
    padding: 6px 12px;
    font-size: 12.5px;
    font-family: var(--font-mono);
    color: var(--fg);
    outline: none;
  }
  .addr:focus {
    border-color: var(--line-strong);
  }
  .ext {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 9px;
    padding: 6px 12px;
    font-size: 12px;
    color: var(--muted);
    flex: none;
  }
  .ext:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .ext:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .ext svg {
    width: 12px;
    height: 12px;
  }
  .hint {
    flex: none;
    font-size: 11px;
    color: var(--faint);
  }
  .view {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: hidden;
  }
  iframe {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: none;
  }
  .empty {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: 13px;
  }
</style>
