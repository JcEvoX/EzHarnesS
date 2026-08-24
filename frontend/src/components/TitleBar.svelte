<script lang="ts">
  /* 桌面壳自绘标题栏：win_* 由 Go 侧 webview Bind 注入，浏览器访问时不存在则不渲染 */
  const w = window as unknown as Record<string, ((...a: unknown[]) => Promise<unknown>) | undefined>
  const desktop = typeof w.win_min === 'function'

  let maximized = $state(false)

  async function toggleMax() {
    await w.win_max?.()
    maximized = (await w.win_is_max?.()) === true
  }

  function drag(e: MouseEvent) {
    if (e.button !== 0 || (e.target as HTMLElement).closest('.tbtn')) return
    void w.win_drag?.()
  }

  /* 最大化状态跟随：拖拽还原/系统快捷键改变窗口态时同步按钮图标 */
  $effect(() => {
    if (!desktop) return
    const sync = () => void w.win_is_max?.().then((v) => (maximized = v === true))
    sync()
    window.addEventListener('resize', sync)
    return () => window.removeEventListener('resize', sync)
  })
</script>

{#if desktop}
  <div class="titlebar" onmousedown={drag} ondblclick={toggleMax}>
    <span class="name">ezharness</span>
    <div class="btns">
      <button class="tbtn" onclick={() => void w.win_min?.()} title="最小化">
        <svg viewBox="0 0 12 12"><path d="M1 6h10" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
      <button class="tbtn" onclick={toggleMax} title={maximized ? '还原' : '最大化'}>
        {#if maximized}
          <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2">
            <rect x="1.5" y="3.5" width="7" height="7" />
            <path d="M3.5 3.5v-2h7v7h-2" />
          </svg>
        {:else}
          <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2">
            <rect x="1.5" y="1.5" width="9" height="9" />
          </svg>
        {/if}
      </button>
      <button class="tbtn close" onclick={() => void w.win_close?.()} title="关闭">
        <svg viewBox="0 0 12 12"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
    </div>
  </div>
{/if}

<style>
  .titlebar {
    display: flex;
    align-items: center;
    height: 34px;
    flex: none;
    background: var(--bg);
    user-select: none;
  }
  .name {
    margin-left: 14px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    pointer-events: none;
  }
  .btns {
    margin-left: auto;
    display: flex;
    height: 100%;
  }
  .tbtn {
    display: grid;
    place-items: center;
    width: 44px;
    height: 100%;
    color: var(--muted);
    border-radius: 0;
    transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
  }
  .tbtn svg {
    width: 12px;
    height: 12px;
  }
  .tbtn:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .tbtn.close:hover {
    background: #e81123;
    color: #fff;
  }
</style>
