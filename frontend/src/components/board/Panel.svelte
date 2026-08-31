<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import MagicBoard from './MagicBoard.svelte'
  import BrowserTab from './BrowserTab.svelte'
  import TerminalTab from './TerminalTab.svelte'

  /*
  魔法看板容器：右上角常显入口按钮（fab），点击展开大 overlay。
  overlay 用 CSS visibility 隐藏（keep-alive）：面板收起时画板元素、
  终端连接都保活，重开无重建。三个 tab 内容惰性首挂（visited），
  激活过的 tab 切走只 display:none 不销毁。
  */

  const tabs = [
    { key: 'board', label: '画板' },
    { key: 'browser', label: '浏览器' },
    { key: 'terminal', label: '终端' },
  ] as const

  type TabKey = (typeof tabs)[number]['key']
  let visited = $state<Record<string, boolean>>({})

  /* 打开期间激活过的 tab 记为已访问（keep-alive 标记；用户点击路径
     由 setTab 同步置位，AI 触发的自动切换由本 effect 补位） */
  $effect(() => {
    if (store.boardOpen) visited[store.boardTab] = true
  })

  function setTab(t: TabKey) {
    visited[t] = true
    store.boardTab = t
  }

  /* Escape：仅画板 tab 且焦点不在输入框时关面板（终端 tab 的 Esc 有
     PTY 语义，浏览器 tab 不拦截） */
  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !store.boardOpen) return
    if (store.boardTab !== 'board') return
    const el = document.activeElement
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) return
    store.closeBoard()
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="overlay" class:off={!store.boardOpen} role="dialog" aria-label="魔法看板">
  <div class="panel">
    <header>
      <h2>魔法看板</h2>
      <nav class="tabs">
        {#each tabs as t (t.key)}
          <button class="tab" class:active={store.boardTab === t.key} onclick={() => setTab(t.key)}>{t.label}</button>
        {/each}
      </nav>
      <button class="close" onclick={() => store.closeBoard()} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>

    <div class="body">
      {#if visited.board || (store.boardOpen && store.boardTab === 'board')}
        <div class="tabpane" class:hidden={store.boardTab !== 'board'}>
          {#key store.boardSeq}
            <MagicBoard
              active={store.boardOpen && store.boardTab === 'board'}
              source={store.boardSource}
              onDone={(f) => store.completeBoard(f)}
            />
          {/key}
        </div>
      {/if}
      {#if visited.browser || (store.boardOpen && store.boardTab === 'browser')}
        <div class="tabpane" class:hidden={store.boardTab !== 'browser'}>
          <BrowserTab />
        </div>
      {/if}
      {#if visited.terminal || (store.boardOpen && store.boardTab === 'terminal')}
        <div class="tabpane" class:hidden={store.boardTab !== 'terminal'}>
          <TerminalTab active={store.boardOpen && store.boardTab === 'terminal'} />
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    place-items: center;
    /* 不用 backdrop-filter:WebView2 下近全屏 blur 叠加画板每帧重绘会明显掉帧 */
    background: rgb(0 0 0 / 52%);
    animation: fade var(--dur-fast) var(--ease-out) both;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
  /* keep-alive 收起：DOM 保留（终端连接/画板状态不丢），仅视觉隐藏 */
  .overlay.off {
    visibility: hidden;
    opacity: 0;
    pointer-events: none;
    animation: none;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      visibility var(--dur-fast);
  }
  .overlay.off .panel {
    animation: none;
  }
  /* 近全屏：屏幕多大看板多大 */
  .panel {
    display: flex;
    flex-direction: column;
    width: calc(100vw - 48px);
    height: calc(100vh - 48px);
    max-width: 1920px;
    background: var(--bg);
    border-radius: 18px;
    padding: 18px 18px 16px;
    box-shadow: 0 24px 64px rgb(0 0 0 / 24%);
    animation: pop var(--dur-in) var(--ease-out) both;
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
  header {
    display: flex;
    align-items: center;
    gap: 14px;
    flex: none;
  }
  h2 {
    font-size: 15px;
    font-weight: 700;
  }
  .tabs {
    display: flex;
    gap: 4px;
    flex: 1;
  }
  .tab {
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: 12.5px;
    font-weight: 550;
    padding: 6px 14px;
    border-radius: 9px;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .tab:hover {
    background: var(--line);
    color: var(--fg);
  }
  .tab.active {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .close {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
  }
  .close:hover {
    background: var(--line);
    color: var(--fg);
  }
  .close svg {
    width: 14px;
    height: 14px;
  }
  .body {
    position: relative;
    flex: 1;
    min-height: 0;
    margin-top: 12px;
  }
  .tabpane {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
  }
  .tabpane.hidden {
    display: none;
  }
  .term-pending {
    flex: 1;
    display: grid;
    place-items: center;
    font-size: 13px;
    color: var(--faint);
  }
</style>
