<script lang="ts">
  import { isDesktop } from '../lib/desktop'

  /* 桌面壳标题栏：?desktop 参数时渲染（桌面窗口 URL 带 ?desktop=1）。
     三键/状态走 /api/window/*（Go 侧桥接原生窗口），拖拽与双击最大化由
     WebView2 原生非客户区支持处理（CSS app-region），浏览器访问不渲染 */
  const desktop = isDesktop

  let maximized = $state(false)

  /* 关闭询问（页面 modal）：后端未配置托盘时点 X 返回 prompt=true 弹出；
     勾选「以后最小化到托盘」由后端持久化，之后点 X 直接最小化 */
  let closePrompt = $state(false)
  let trayChoice = $state(false)

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

  async function onClose() {
    try {
      const r = await post('close')
      if (r.ok && (await r.json()).prompt) {
        trayChoice = false
        closePrompt = true
      }
    } catch {
      /* 后端不可达保持原样 */
    }
  }

  async function decideClose() {
    closePrompt = false
    try {
      await fetch('/api/window/close-decision', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tray: trayChoice, remember: trayChoice }),
      })
    } catch {
      /* 后端不可达保持原样 */
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
      <button class="tbtn close" onclick={() => void onClose()} title="关闭">
        <svg viewBox="0 0 12 12"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
    </div>
  </div>

  {#if closePrompt}
    <div class="close-mask" role="presentation" onclick={() => (closePrompt = false)}>
      <div class="close-dialog" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()}>
        <p class="close-q">关闭 ezharness？</p>
        <label class="close-opt">
          <input type="checkbox" bind:checked={trayChoice} />
          最小化到托盘（以后不再询问）
        </label>
        <div class="close-btns">
          <button class="cb cancel" onclick={() => (closePrompt = false)}>取消</button>
          <button class="cb ok" onclick={() => void decideClose()}>关闭</button>
        </div>
      </div>
    </div>
  {/if}
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
  /* 关闭询问：页面 modal（fixed 全屏遮罩 + 居中卡片），与产品 UI 同语言 */
  .close-mask {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: grid;
    place-items: center;
    background: rgb(0 0 0 / 32%);
  }
  .close-dialog {
    width: 320px;
    padding: 18px 20px 16px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .close-q {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }
  .close-opt {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
    user-select: none;
  }
  .close-opt input {
    accent-color: var(--accent);
  }
  .close-btns {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .cb {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    border-radius: 8px;
    padding: 6px 18px;
    font-size: 12px;
    font-weight: 550;
    cursor: pointer;
  }
  .cb.ok {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .cb:hover {
    opacity: 0.85;
  }
</style>
