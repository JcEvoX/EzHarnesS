<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type HistoryMessage, type MemoryConfig, type MemoryTopicEntry } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /*
  记忆 = 三个文件夹（数据由 GET /api/memory/config 下发）：
    长期记忆——一套文件系统：harness.md 是索引文件，初始加载进上下文，
      其余文件由 agent 按需检索（grep 等）。
    能力记忆——沉淀的 skill，agent 按需调用。
    话题记忆——历史 session 存档，可回顾/删除。
  */
  let { onNavigate }: { onNavigate?: (v: string) => void } = $props()

  let cfg = $state<MemoryConfig | null>(null)
  let message = $state('')
  let openTopic = $state('') // 展开回顾的 topic id
  let topicMsgs = $state<HistoryMessage[]>([])

  onMount(async () => {
    try {
      cfg = await api.getMemoryConfig()
    } catch {
      message = '记忆数据加载失败（后端不可达）'
    }
  })

  async function removeTopic(t: MemoryTopicEntry) {
    try {
      await api.deleteTopic(t.id)
      if (cfg) cfg.topics.items = cfg.topics.items.filter((x) => x.id !== t.id)
    } catch (e) {
      message = `删除失败：${(e as Error).message}`
    }
  }

  /* 回顾：展开只读全文（再点收起） */
  async function reviewTopic(t: MemoryTopicEntry) {
    if (openTopic === t.id) {
      openTopic = ''
      return
    }
    try {
      const d = await api.getTopic(t.id)
      topicMsgs = d.messages || []
      openTopic = t.id
    } catch (e) {
      message = `回顾失败：${(e as Error).message}`
    }
  }

  /* 回到话题：恢复为活动会话并跳转对话页 */
  async function resumeTopic(t: MemoryTopicEntry) {
    try {
      await store.resumeTopic(t.id)
      onNavigate?.('chat')
    } catch (e) {
      message = `回到话题失败：${(e as Error).message}`
    }
  }

  function fmtSize(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1024 / 1024).toFixed(1)} MB`
  }

  /* 分支列表：条目=线（根→叶），按最近活动排序；fork 线带分叉徽标 */
  const branchRows = $derived.by(() => {
    if (!cfg) return [] as MemoryTopicEntry[]
    return [...cfg.topics.items].sort(
      (a, b) => (b.updatedAt || b.createdAt) - (a.updatedAt || a.createdAt),
    )
  })

  function fmtDate(ts: number): string {
    if (!ts) return ''
    const d = new Date(ts)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }
</script>

<div class="page">
  <h1>记忆</h1>
  <p class="lead">agent 的记忆由三个文件夹构成，路径可在设置中配置。</p>
  {#if message}
    <p class="lead err">{message}</p>
  {/if}

  <!-- ── 长期记忆 ── -->
  <section>
    <header>
      <div>
        <h2>长期记忆</h2>
        <p class="hint">harness.md 索引随上下文初始加载，其余文件由 agent 按需检索。</p>
      </div>
      <button class="new" disabled>+ 新建文件</button>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.longterm.dir : '—'}</p>
    <div class="list">
      {#if cfg}
        {#if cfg.longterm.harnessMd}
          <div class="file index">
            <span class="name">harness.md</span>
            <span class="badge">初始加载</span>
            <span class="stat">{fmtSize(cfg.longterm.harnessMd.size)} · {cfg.longterm.harnessMd.mtime}</span>
          </div>
        {/if}
        {#each cfg.longterm.files as f (f.name)}
          <div class="file">
            <span class="name">{f.name}</span>
            <span class="stat">{fmtSize(f.size)} · {f.mtime}</span>
          </div>
        {/each}
        {:else}
        <div class="empty">暂无文件——agent 会把重要的用户偏好与事实沉淀到这里。</div>
      {/if}
    </div>
  </section>

  <!-- ── 能力记忆 ── -->
  <section>
    <header>
      <div>
        <h2>能力记忆</h2>
        <p class="hint">沉淀的 skill——做过一次的复杂操作固化为可复用的能力。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.skills.dir : '—'}</p>
    <div class="grid">
      {#if cfg}
        {#each cfg.skills.items as s (s.id)}
          <div class="card" class:off={!s.enabled}>
            <div class="card-top">
              <div class="glyph">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 3l1.9 5.6L20 10l-5 3.6L16.5 20 12 16.6 7.5 20 9 13.6 4 10l6.1-1.4L12 3z" />
                </svg>
              </div>
              <span class="toggle" class:on={s.enabled} role="switch" aria-checked={s.enabled} tabindex="0">
                <i></i>
              </span>
            </div>
            <h3>{s.name}</h3>
            <p class="card-desc">{s.desc}</p>
          </div>
        {/each}
        <div class="card ghost">
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3>新建技能</h3>
          <p class="card-desc">把重复性工作流沉淀为可复用的能力。</p>
        </div>
      {:else}
        <div class="card ghost">
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3>暂无技能</h3>
          <p class="card-desc">重复性工作流可沉淀为 skill。</p>
        </div>
      {/if}
    </div>
  </section>

  <!-- ── 话题记忆 ── -->
  <section>
    <header>
      <div>
        <h2>话题记忆</h2>
        <p class="hint">历史会话存档，可回顾或删除。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.topics.dir : '—'}</p>
    <div class="list">
      {#if cfg}
        {#each branchRows as t, i (`${t.id}-${i}`)}
          <div class="branch">
          <div class="topic" class:cur={store.activeId === t.id}>
            <div class="info">
              <span class="name"
                >{t.title}{#if t.kind === 'fork'}<span class="kbadge" title={t.origin?.title ? `分叉自：${t.origin.title}` : '分叉产生的分支'}>⑂ 分叉</span>{/if}{#if store.activeId === t.id}<span class="kbadge live">进行中</span>{/if}</span
              >
              <span class="desc">{fmtDate(t.updatedAt || t.createdAt)} · {t.msgs} 条消息{t.origin?.title ? ` · 分叉自「${t.origin.title}」` : ''}</span>
              {#if t.summary}
                <span class="summary">{t.summary}</span>
              {/if}
            </div>
            <div class="ops">
              <button class="op" onclick={() => reviewTopic(t)}>
                {openTopic === t.id ? '收起' : '回顾'}
              </button>
              <button class="op" onclick={() => resumeTopic(t)} title="切换到该分支继续">切到分支</button>
              <button class="del" onclick={() => removeTopic(t)} title="删除整条线（全部世代）">删除</button>
            </div>
          </div>
          {#if openTopic === t.id}
            <div class="review">
              {#each topicMsgs as m, i (i)}
                <div class="rv" class:me={m.role === 'user'}>
                  <span class="rrole">{m.role === 'assistant' ? 'agent' : m.role === 'tool' ? 'tool' : m.role}</span>
                  <span class="rtext">{(m.content || (m.tool_calls ? JSON.stringify(m.tool_calls) : '')).slice(0, 300)}</span>
                </div>
              {/each}
              {#if topicMsgs.length === 0}
                <div class="rv">（无消息）</div>
              {/if}
            </div>
          {/if}
          </div>
        {/each}
        {#if cfg.topics.items.length === 0}
          <div class="empty">暂无分支——在对话页新建或从任意消息分叉。</div>
        {/if}
      {:else}
        <div class="empty">暂无分支——在对话页新建或从任意消息分叉。</div>
      {/if}
    </div>
  </section>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 34px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  .lead {
    font-size: 12.5px;
    color: var(--muted);
    margin-top: -24px;
  }
  .lead.err {
    color: #c0392b;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: 2px;
  }
  .new {
    flex: none;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    padding: 5px 12px;
    font-size: 11.5px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .new:not(:disabled):hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .new:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .dir {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .dir span {
    font-size: 11px;
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .empty {
    padding: 22px 14px;
    text-align: center;
    font-size: 12px;
    color: var(--faint);
  }

  /* 长期记忆：文件行 */
  .file {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .file + .file {
    border-top: 1px solid var(--line);
  }
  .file:hover {
    background: var(--bg-soft);
  }
  .file .name {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .file .badge {
    flex: none;
    font-size: 10px;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
  }
  .file .stat {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    white-space: nowrap;
  }
  .file.index {
    background: color-mix(in srgb, var(--accent) 4%, var(--bg));
  }

  /* 能力记忆：技能卡片 */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
    gap: 14px;
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
  .card.off {
    opacity: 0.55;
  }
  .card-top {
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
  .glyph svg {
    width: 16px;
    height: 16px;
  }
  .card h3 {
    font-size: 13.5px;
    font-weight: 650;
    color: var(--fg);
    margin-top: 2px;
  }
  .card-desc {
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    flex: 1;
  }
  .card.ghost {
    border-style: dashed;
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h3 {
    color: var(--muted);
  }
  .toggle {
    position: relative;
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    transition: background var(--dur-fast) var(--ease-out);
  }
  .toggle i {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--bg);
    transition: transform var(--dur-fast) var(--ease-out);
    box-shadow: 0 1px 2px rgb(0 0 0 / 20%);
  }
  .toggle.on {
    background: var(--accent);
  }
  .toggle.on i {
    transform: translateX(13px);
  }

  /* 话题记忆：会话行 */
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .skill .name {
    font-size: 12.5px;
    color: var(--fg);
    font-weight: 550;
  }
  .desc {
    font-size: 11px;
    color: var(--faint);
    overflow-wrap: anywhere;
  }

  /* 话题记忆：会话行 */
  .branch {
    display: flex;
    flex-direction: column;
  }
  .topic {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 14px;
    flex-wrap: wrap; /* 回顾展开区占满整行 */
    transition: background var(--dur-fast) var(--ease-out);
  }
  .topic + .topic {
    border-top: 1px solid var(--line);
  }
  .topic:hover {
    background: var(--bg-soft);
  }
  .topic .name {
    font-size: 12.5px;
    color: var(--fg);
    font-weight: 550;
    overflow-wrap: anywhere;
  }
  .forkbtn {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: 10px;
    font-family: var(--font-mono);
    padding: 1px 8px;
    border-radius: 999px;
    cursor: pointer;
    transition: all var(--dur-fast) var(--ease-out);
  }
  .forkbtn:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .kbadge {
    flex: none;
    margin-left: 8px;
    font-size: 10px;
    font-weight: 500;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
    vertical-align: 1px;
  }
  .kbadge.live {
    color: #3fb950;
    background: rgb(63 185 80 / 12%);
  }
  .topic.cur {
    background: var(--bg-soft);
  }
  .summary {
    font-size: 11.5px;
    line-height: 1.55;
    color: var(--muted);
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-top: 2px;
  }
  .path {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    overflow-wrap: anywhere;
    margin-top: 2px;
  }
  .ops {
    display: flex;
    gap: 4px;
    flex: none;
  }
  .op {
    flex: none;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
  }
  .op:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .topic:hover .op {
    opacity: 1;
  }
  .review {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    margin-top: 8px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
    max-height: 320px;
    overflow-y: auto;
  }
  .rv {
    display: flex;
    gap: 10px;
    font-size: 12px;
    line-height: 1.5;
  }
  .rv.me .rtext {
    color: var(--muted);
  }
  .rrole {
    flex: none;
    width: 44px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    text-transform: uppercase;
    padding-top: 2px;
  }
  .rtext {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .del {
    flex: none;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: 11.5px;
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .topic:hover .del {
    opacity: 1;
  }
  .del:hover {
    color: #c0392b;
    background: color-mix(in srgb, #c0392b 8%, transparent);
  }
</style>
