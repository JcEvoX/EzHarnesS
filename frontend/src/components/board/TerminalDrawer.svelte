<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import TerminalTab from './TerminalTab.svelte'

  /*
  共享终端抽屉：推挤式右布局列（非悬浮 overlay）——打开时占布局宽度，
  聊天主列被挤窄不遮挡。收起仅宽度归零（width 0 + overflow hidden），
  TerminalTab 常驻挂载（WS/xterm 保活）。内层 .inner 固定宽度，过渡期间
  内容不重排；打开后 ResizeObserver 自动 fit。
  */
  const open = $derived(store.termDrawerOpen)
</script>

<aside class="drawer" class:open={open} role="complementary" aria-label="共享终端">
  <div class="inner">
    <header>
      <h3>共享终端</h3>
      <span class="hint">用户与 AI 共写 · 收起不中断</span>
      <button class="close" onclick={() => store.closeTermDrawer()} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>
    <div class="body">
      <TerminalTab active={open} />
    </div>
  </div>
</aside>

<style>
  .drawer {
    flex: none;
    width: 0;
    overflow: hidden;
    background: var(--bg);
    border-left: 1px solid var(--line);
    transition: width 0.28s var(--ease-out);
  }
  .drawer.open {
    width: min(620px, 42vw);
  }
  /* 内层固定宽度：外层宽度过渡时内容不挤压重排 */
  .inner {
    display: flex;
    flex-direction: column;
    width: min(620px, 42vw);
    height: 100%;
  }
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    padding: 14px 12px 8px 16px;
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
