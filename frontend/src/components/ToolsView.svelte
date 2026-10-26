<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type AppEntry } from '../lib/api'
  import { store } from '../lib/store.svelte'
  import { launchApp } from '../lib/apps'

  /* 快应用 = apps/<名>/ 目录应用（app.quick 声明）。desktop 壳 = IPC 开独立
  子窗口；浏览器访问 = 新标签页直开。声明了后端的应用由后端把终端拉进侧边栏。 */
  let apps = $state<AppEntry[]>([])
  let loaded = $state(false)

  onMount(async () => {
    try {
      const { apps: list } = await api.getApps()
      apps = list ?? []
    } catch {
      /* 后端不可达时空列表 */
    }
    loaded = true
  })

  /* 启动：desktop 壳子窗口（IPC），web 端新标签页；带后端的应用把它的
  终端拉到侧边栏（后端可能起不来，失败要出声） */
  async function launch(a: AppEntry) {
    try {
      await launchApp(a.name)
    } catch (err) {
      store.lastStatus = `启动快应用失败：${(err as Error).message}`
    }
  }

  /* 钉到桌面小助手/取消（图标常驻宠物窗，点击即启动）。core 钉满 3 会
  顶掉最早那颗——重拉整表让所有卡片的 pinned 标记跟服务端一致（只改被点
  的那张会留下"多于 3 个都钉着"的假象，切页才自愈） */
  async function togglePin(a: AppEntry) {
    try {
      await api.pinApp(a.name)
      const { apps: list } = await api.getApps()
      apps = list ?? []
    } catch (err) {
      store.lastStatus = `操作失败：${(err as Error).message}`
    }
  }
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>快应用</h1>
      <p class="lead">agent 构建的快应用都在这里，一键启动；带后端的会在侧边栏终端里跑起来。</p>
    </div>
  </div>

  <p class="dir"><svg class="dirico" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>apps/</p>

  <div class="grid">
    {#if !loaded}
      <p class="lead">加载中…</p>
    {:else if apps.length}
      {#each apps as a (a.name)}
        <div class="card" role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && launch(a)}>
          <div class="top">
            {#if a.icon}
              <img class="glyph avatar" src={a.icon} alt="" loading="lazy"
                onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')} />
            {:else}
              <div class="glyph live">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M8 6L3 12l5 6" />
                  <path d="M16 6l5 6-5 6" />
                </svg>
              </div>
            {/if}
            <span class="kind">{a.kind}</span>
          </div>
          <h2>{a.title}</h2>
          <p class="desc">{a.name}</p>
          <div class="foot">
            <span class="time">{a.mtime}</span>
            <span class="footbtns">
              <button class="pin" class:on={a.pinned} onclick={() => void togglePin(a)}
                title={a.pinned ? '已钉到桌面小助手，点击取消' : '添加到桌面小助手（图标常驻，点击即启动）'}>
                <svg viewBox="0 0 24 24" fill={a.pinned ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 17v5" />
                  <path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6z" />
                </svg>
              </button>
              <button class="run" onclick={() => launch(a)}>
                启动
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M7 17L17 7" />
                  <path d="M8 7h9v9" />
                </svg>
              </button>
            </span>
          </div>
        </div>
      {/each}
    {:else}
      <div class="card ghost">
        <div class="top">
          <div class="glyph ghost-glyph">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
          </div>
        </div>
        <h2>暂无工具</h2>
        <p class="desc">让 agent 做一个——「帮我写个 xx 的快应用」，生成后自动出现在这里。</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .head h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    font-size: var(--font-size-ui);
    color: var(--muted);
    margin-top: 4px;
  }
  .dir {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
    gap: 14px;
    margin-top: 4px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 14px;
    background: var(--bg);
    padding: 16px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }
  .card:not(.ghost):hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 7%);
    transform: translateY(-2px);
  }
  .top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  .glyph {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 10px;
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .glyph.live {
    background: var(--bg-invert);
    border-color: var(--bg-invert);
    color: var(--fg-invert);
  }
  .glyph svg {
    width: 16px;
    height: 16px;
  }
  /* 声明了 icon 的应用：卡片头像直接用图 */
  .glyph.avatar {
    width: 34px;
    height: 34px;
    border-radius: 10px;
    object-fit: cover;
  }
  .kind {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    font-weight: 600;
    text-transform: uppercase;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 2px 6px;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
    margin-top: 2px;
  }
  .desc {
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    flex: 1;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 2px;
  }
  .time {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .footbtns {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  /* 图钉：钉住后实心并点亮 */
  .pin {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
  }
  .pin:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .pin.on {
    color: #f0883e;
    border-color: color-mix(in srgb, #f0883e 40%, var(--line));
  }
  .pin svg {
    width: 13px;
    height: 13px;
  }
  .run {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 5px 12px;
    font-size: var(--font-size-ui);
    font-weight: 500;
  }
  .run svg {
    width: 11px;
    height: 11px;
  }
  .run:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .card.ghost {
    border-style: dashed;
    color: var(--faint);
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h2 {
    color: var(--muted);
  }
</style>
