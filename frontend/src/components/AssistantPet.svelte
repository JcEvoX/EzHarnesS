<script lang="ts">
  import { onMount } from 'svelte'
  import EzAvatar from './EzAvatar.svelte'

  /*
  桌面宠物（pet=1 窗口）：透明置顶头像 + 状态感知 + 钉选快应用 dock。
  整块 = app-region 拖拽区（与 TitleBar 同一套做法，纯 CSS、零 JS）；右上角
  一个 no-drag 小按钮打开主窗口。轮询 core 只读端点（本窗口唯一的
  core 依赖，无 bootstrap/SSE/store）：
  /api/topics 有 running → 头像换 spin 转圈（执行中）；
  /api/notifications 有待审批（approve/ask）→ 右上角红点带条数（优先），
  只有完成通知（info）→ 绿点；点角标开主窗口去处理；
  /api/apps 的 pinned → 底部一排快应用图标，点击启动（开子窗）。
  core 不可达静默重试。
  */

  const ez = (window as any).ez

  let running = $state(false)
  let pending = $state(0)
  let done = $state(false)
  let pinned = $state<{ name: string; title: string; icon?: string }[]>([])

  onMount(() => {
    const poll = async () => {
      try {
        const [topics, notices, apps] = await Promise.all([
          fetch('/api/topics').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/notifications').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/apps').then((r) => (r.ok ? r.json() : { apps: [] })),
        ])
        running = (topics as { running?: boolean }[]).some((t) => t.running)
        const items = (notices as { items?: { kind?: string }[] }[]).flatMap((g) => g.items ?? [])
        pending = items.filter((it) => it.kind === 'approve' || it.kind === 'ask').length
        done = items.some((it) => it.kind === 'info')
        pinned = ((apps as { apps?: { name: string; title: string; icon?: string; pinned?: boolean }[] }).apps ?? [])
          .filter((a) => a.pinned)
      } catch {
        /* core 不可达：静默，下轮再试 */
      }
    }
    void poll()
    const t = setInterval(poll, 3000)
    return () => clearInterval(t)
  })

  /* 启动钉选的快应用：同快应用页——core 校验返回入口路径，desktop 壳开子窗 */
  async function launchApp(a: { name: string; title: string }) {
    try {
      const r = await fetch('/api/apps/open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: a.name }),
      }).then((r) => (r.ok ? r.json() : null))
      if (!r) return
      ez?.window?.openApp?.(r.path, r.title)
    } catch {
      /* core 不可达：忽略，用户可从快应用页启动 */
    }
  }
</script>

<div class="pet">
  <div class="avatar-zone">
    <div class="avatar-wrap">
      <EzAvatar size={72} run={running} />
      {#if pending > 0}
        <span class="badge red" role="button" tabindex="-1" onclick={() => ez?.pet?.openMain()} title={`有 ${pending} 条待审批，点击打开主窗口`}>
          {pending > 9 ? '9+' : pending}
        </span>
      {:else if done}
        <span class="badge done" role="button" tabindex="-1" onclick={() => ez?.pet?.openMain()} title="有执行完成的通知，点击打开主窗口"></span>
      {/if}
      <button class="open" onclick={() => ez?.pet?.openMain()} title="打开主窗口">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
          <path d="M7 17L17 7M9 7h8v8" />
        </svg>
      </button>
    </div>
  </div>
  {#if pinned.length}
    <div class="dock">
      {#each pinned as a (a.name)}
        <button class="app" onclick={() => void launchApp(a)} title={`${a.title}（点击启动）`}>
          {#if a.icon}
            <img src={a.icon} alt="" loading="lazy" onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')} />
          {:else}
            <span class="letter">{a.title.slice(0, 1)}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
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
    display: flex;
    flex-direction: column;
    user-select: none;
    app-region: drag;
  }
  .avatar-zone {
    flex: 1;
    display: grid;
    place-items: center;
    min-height: 0;
  }
  .avatar-wrap {
    position: relative;
  }

  /* 状态角标：审批（红，带条数）优先于完成（绿点）；no-drag 才收得到点击 */
  .badge {
    position: absolute;
    top: 5px;
    right: 5px;
    display: grid;
    place-items: center;
    min-width: 18px;
    height: 18px;
    padding: 0 4px;
    border-radius: 9px;
    font-size: 11px;
    font-weight: 700;
    color: #fff;
    cursor: pointer;
    app-region: no-drag;
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--bg) 65%, transparent);
  }
  .badge.red {
    background: #e74c3c;
  }
  .badge.done {
    min-width: 12px;
    width: 12px;
    height: 12px;
    padding: 0;
    border-radius: 50%;
    background: #2ecc71;
  }

  /* 钉选快应用 dock：头像下方一排小图标，no-drag 可点 */
  .dock {
    flex: none;
    display: flex;
    justify-content: center;
    gap: 6px;
    padding: 0 0 2px;
  }
  .app {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid color-mix(in srgb, var(--line) 70%, transparent);
    border-radius: 7px;
    background: color-mix(in srgb, var(--bg) 55%, transparent);
    cursor: pointer;
    app-region: no-drag;
  }
  .app:hover {
    background: var(--bg);
    border-color: var(--line-strong);
  }
  .app img {
    width: 16px;
    height: 16px;
    object-fit: cover;
    border-radius: 4px;
  }
  .app .letter {
    font-size: 11px;
    font-weight: 700;
    color: var(--muted);
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
