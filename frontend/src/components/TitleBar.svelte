<script lang="ts">
  /* 桌面壳标题栏：wails 资产域（wails.localhost）或 ?desktop 参数时渲染。
     三键/状态走 /api/window/*（Go 侧桥接原生窗口），拖拽与双击最大化由
     WebView2 原生非客户区支持处理（CSS app-region），浏览器访问不渲染 */
  const desktop =
    location.hostname === 'wails.localhost' ||
    new URLSearchParams(location.search).has('desktop')

  let maximized = $state(false)

  function post(action: string): Promise<Response> {
    return fetch(`/api/window/${action}`, { method: 'POST' })
  }

  async function syncMax() {
    try {
      const r = await fetch('/api/window/state')
      if (r.ok) maximized = (await r.json()).maximized === true
    } catch {
      /* 状态获取失败保持原样 */
    }
  }

  async function toggleMax() {
    try {
      const r = await post('max')
      if (r.ok) maximized = (await r.json()).maximized === true
    } catch {
      void syncMax()
    }
  }

  /* 最大化状态跟随：拖拽还原/系统快捷键改变窗口态时同步按钮图标 */
  $effect(() => {
    if (!desktop) return
    void syncMax()
    window.addEventListener('resize', syncMax)
    return () => window.removeEventListener('resize', syncMax)
  })
</script>

{#if desktop}
  <div class="titlebar">
    <span class="name">ezharness</span>
    <div class="btns">
      <button class="tbtn" onclick={() => void post('min')} title="最小化">
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
      <button class="tbtn close" onclick={() => void post('close')} title="关闭">
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
    app-region: drag;
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
    app-region: no-drag;
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
