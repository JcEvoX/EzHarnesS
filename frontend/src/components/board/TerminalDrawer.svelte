<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import { isDesktop } from '../../lib/desktop'
  import TerminalTab from './TerminalTab.svelte'

  /*
  共享终端抽屉：与聊天并存的右侧滑入面板（不是全屏 overlay——终端是
  工作台性质，用户边聊边看，弹大面板会有中断感）。收起仅滑出屏幕，
  TerminalTab 常驻挂载（WS/xterm 保活）。桌面模式避开 34px 自绘标题栏。
  Escape 不拦：终端里 Esc 有 PTY 语义。z-index 90，看板 overlay(100)
  打开时盖住抽屉。
  */
  const open = $derived(store.termDrawerOpen)
</script>

<aside class="drawer" class:open={open} class:titled={isDesktop} role="complementary" aria-label="共享终端">
  <header>
    <h3>共享终端</h3>
    <span class="hint">用户与 AI 共写 · 关闭抽屉不中断</span>
    <button class="close" onclick={() => store.closeTermDrawer()} title="收起">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    </button>
  </header>
  <div class="body">
    <TerminalTab active={open} />
  </div>
</aside>

<style>
  .drawer {
    position: fixed;
    top: 14px;
    right: 14px;
    bottom: 14px;
    z-index: 90;
    display: flex;
    flex-direction: column;
    width: min(620px, 46vw);
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 16px;
    box-shadow: 0 18px 56px rgb(0 0 0 / 16%);
    transform: translateX(calc(100% + 28px));
    opacity: 0;
    transition:
      transform 0.32s var(--ease-out),
      opacity 0.2s var(--ease-out);
  }
  .drawer.titled {
    top: 48px; /* 桌面壳自绘标题栏(34px) + 悬浮边距 */
  }
  .drawer.open {
    transform: translateX(0);
    opacity: 1;
    transition:
      transform 0.3s var(--ease-out),
      opacity 0.24s var(--ease-out);
  }
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    padding: 10px 12px 8px 16px;
  }
  h3 {
    font-size: 13px;
    font-weight: 700;
  }
  .hint {
    flex: 1;
    font-size: 11px;
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .close {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
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
    width: 13px;
    height: 13px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 12px 12px 16px;
  }
</style>
