<script lang="ts">
  import EzAvatar from './EzAvatar.svelte'

  /*
  桌面宠物（pet=1 窗口）：透明置顶头像。
  整块 = app-region 拖拽区（与 TitleBar 同一套做法，纯 CSS、零 JS）；右上角
  一个 no-drag 小按钮打开主窗口。面板/会话/通知等一律移除，待重写。
  */

  const ez = (window as any).ez
</script>

<div class="pet">
  <EzAvatar size={72} />
  <button class="open" onclick={() => ez?.pet?.openMain()} title="打开主窗口">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
      <path d="M7 17L17 7M9 7h8v8" />
    </svg>
  </button>
</div>

<style>
  /* 透明窗口：html/body 背景必须重置为透明（app.css 给 body 涂了实色） */
  :global(html),
  :global(body) {
    background: transparent !important;
    overflow: hidden;
  }

  .pet {
    position: relative;
    width: 100vw;
    height: 100vh;
    display: grid;
    place-items: center;
    user-select: none;
    app-region: drag;
  }

  /* 打开主窗口：no-drag 小按钮（拖拽区收不到点击，交互元素必须独立出来） */
  .open {
    position: absolute;
    top: 1px;
    right: 1px;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: color-mix(in srgb, var(--bg) 72%, transparent);
    color: var(--muted);
    cursor: pointer;
    opacity: 0.55;
    app-region: no-drag;
  }
  .open:hover {
    opacity: 1;
    background: var(--bg);
    color: var(--fg);
  }
  .open svg {
    width: 11px;
    height: 11px;
  }
</style>
