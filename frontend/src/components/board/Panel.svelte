<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import MagicBoard from './MagicBoard.svelte'

  /*
  画板 overlay：大画布编辑面板（入口在输入框发送键旁与附件缩略图编辑）。
  overlay 用 CSS visibility 隐藏（keep-alive）：收起时画板元素保活；
  {#key boardSeq} 在每次"从关到开"时重建画布=新画布。终端在独立抽屉
  TerminalDrawer；浏览器暂未开放。
  */

  /* Escape：焦点不在输入框时关面板 */
  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !store.boardOpen) return
    const el = document.activeElement
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) return
    store.closeBoard()
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="overlay" class:off={!store.boardOpen} role="dialog" aria-label="画板">
  <div class="panel">
    <header>
      <h2>画板</h2>
      <button class="close" onclick={() => store.closeBoard()} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>
    <div class="body">
      {#key store.boardSeq}
        <MagicBoard
          active={store.boardOpen}
          source={store.boardSource}
          onDone={(f) => store.completeBoard(f)}
        />
      {/key}
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
  /* keep-alive 收起：DOM 保留（画板状态不丢），仅视觉隐藏 */
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
  /* 近全屏：屏幕多大画板多大 */
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
    justify-content: flex-end;
    gap: 14px;
    flex: none;
  }
  h2 {
    font-size: 15px;
    font-weight: 700;
    margin-right: auto;
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
</style>
