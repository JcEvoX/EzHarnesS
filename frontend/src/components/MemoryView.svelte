<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type HistoryMessage, type MemoryConfig, type SessionNode } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /*
  记忆 = 长期记忆/能力记忆两个文件夹（GET /api/memory/config）+ 会话树
  （GET /api/memory/tree）：话题记忆展示完整会话树——每条分支（线）从根
  到当前叶，compact 旧世代标"已归档"，fork 分叉可见；操作：回顾（只读
  展开）、切到分支（跳对话页）、手动归档、删整条线。
  */
  let { onNavigate }: { onNavigate?: (v: string) => void } = $props()

  let cfg = $state<MemoryConfig | null>(null)
  let tree = $state<SessionNode[] | null>(null)
  let message = $state('')
  let openTopic = $state('') // 展开回顾的 session id
  let topicMsgs = $state<HistoryMessage[]>([])
  let confirmId = $state('') // 待确认删线的节点

  async function loadTree() {
    try {
      const t = await api.getMemoryTree()
      tree = t
      // 默认全展开（树规模桌面尺度；有孩子才需要进集合）
      expanded = new Set(t.filter((n) => t.some((k) => k.targetId === n.id)).map((n) => n.id))
    } catch {
      message = '会话树加载失败（后端不可达）'
    }
  }

  onMount(async () => {
    try {
      cfg = await api.getMemoryConfig()
    } catch {
      message = '记忆数据加载失败（后端不可达）'
    }
    await loadTree()
  })

  /* 回顾：展开只读全文（再点收起）；任意 session 节点都可回顾 */
  async function reviewTopic(n: SessionNode) {
    if (openTopic === n.id) {
      openTopic = ''
      return
    }
    try {
      const d = await api.getTopic(n.id)
      topicMsgs = d.messages || []
      openTopic = n.id
    } catch (e) {
      message = `回顾失败：${(e as Error).message}`
    }
  }

  /* 切到分支：该节点所属线的当前叶恢复为活动会话并跳对话页 */
  async function switchLine(n: SessionNode) {
    if (!n.lineRoot) return
    try {
      await store.resumeTopic(n.lineRoot)
      onNavigate?.('chat')
    } catch (e) {
      message = `切换分支失败：${(e as Error).message}`
    }
  }

  /* 手动归档开关（活动/运行中的当前叶后端拒绝） */
  async function archiveNode(n: SessionNode) {
    try {
      await api.archiveSession(n.id, !n.archived)
      await loadTree()
    } catch (e) {
      message = `归档失败：${(e as Error).message}`
    }
  }

  /* 删整条线（二次确认；线身份 = 节点的 lineRoot） */
  async function removeLine(n: SessionNode) {
    if (!n.lineRoot) return
    if (confirmId !== n.id) {
      confirmId = n.id
      setTimeout(() => {
        if (confirmId === n.id) confirmId = ''
      }, 3000)
      return
    }
    confirmId = ''
    try {
      await api.deleteTopic(n.lineRoot)
      if (n.isActiveLine && n.isLeaf) await store.newBranch()
      await loadTree()
      await store.refreshBranches()
    } catch (e) {
      message = `删除失败：${(e as Error).message}`
    }
  }

  function fmtSize(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1024 / 1024).toFixed(1)} MB`
  }

  /* 会话树：按 targetId 组树（fork 子孙也是子节点），根按时间倒序、
     子节点按时间正序（世代从上往下长）；展开集控制折叠 */
  type Row = { n: SessionNode; depth: number; kids: number; open: boolean }
  let expanded = $state<Set<string>>(new Set())

  function toggle(id: string) {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    expanded = next
  }

  const treeRows = $derived.by(() => {
    if (!tree) return [] as Row[]
    const byId = new Set(tree.map((n) => n.id))
    const kids = new Map<string, SessionNode[]>()
    const roots: SessionNode[] = []
    for (const n of tree) {
      if (n.targetId && byId.has(n.targetId)) {
        const arr = kids.get(n.targetId) || []
        arr.push(n)
        kids.set(n.targetId, arr)
      } else {
        roots.push(n)
      }
    }
    roots.sort((a, b) => b.createdAt - a.createdAt)
    const out: Row[] = []
    const walk = (n: SessionNode, depth: number) => {
      const ch = (kids.get(n.id) || []).sort((a, b) => a.createdAt - b.createdAt)
      const open = expanded.has(n.id)
      out.push({ n, depth, kids: ch.length, open })
      if (open) for (const c of ch) walk(c, depth + 1)
    }
    for (const r of roots) walk(r, 0)
    return out
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

  <!-- ── 话题记忆：完整会话树 ── -->
  <section>
    <header>
      <div>
        <h2>话题记忆</h2>
        <p class="hint">完整会话树：每条分支从根到当前叶，压缩旧世代标"已归档"，⑂ 为分叉。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.topics.dir : '—'}</p>
    <div class="treelist">
      {#if treeRows.length > 0}
        {#each treeRows as row, i (`${row.n.id}-${i}`)}
          {@const n = row.n}
          <div class="branch">
            <div class="topic" class:cur={n.isActiveLine && n.isLeaf} style="padding-left:{14 + row.depth * 20}px">
              {#if row.kids > 0}
                <button class="tw" onclick={() => toggle(n.id)} title={row.open ? '收起子节点' : '展开子节点'}>
                  <span class="farrow" class:open={row.open}>{row.open ? '▾' : '▸'}</span>
                </button>
              {:else}
                <span class="tw dot">·</span>
              {/if}
              <div class="info">
                <span class="name">
                  {n.title || '未命名会话'}
                  {#if n.archived}<span class="kbadge arc">已归档</span>{/if}
                  {#if n.seedKind === 'fork'}<span class="kbadge" title={n.forkedFrom?.title ? `分叉自：${n.forkedFrom.title}` : '分叉产生的分支'}>⑂</span>{/if}
                  {#if n.isActiveLine && n.isLeaf}<span class="kbadge live">进行中</span>{/if}
                  {#if n.msgs === 0}<span class="kbadge mut">空</span>{/if}
                </span>
                <span class="desc">{fmtDate(n.createdAt)} · {n.msgs} 条消息</span>
              </div>
              <div class="ops">
                <button class="op" onclick={() => reviewTopic(n)}>
                  {openTopic === n.id ? '收起' : '回顾'}
                </button>
                {#if n.isLeaf && n.lineRoot && !(n.isActiveLine)}
                  <button class="op" onclick={() => switchLine(n)} title="切到该分支的当前叶继续">切到分支</button>
                {/if}
                <button class="op" onclick={() => archiveNode(n)} title={n.archived ? '取消手动归档' : '手动归档（不再作为恢复候选）'}>
                  {n.archived ? '取消归档' : '归档'}
                </button>
                {#if n.isLeaf && n.lineRoot}
                  <button class="del" class:confirm={confirmId === n.id} onclick={() => removeLine(n)}
                    title={confirmId === n.id ? '再点一次确认删除整条线' : '删除整条线（全部世代）'}>
                    {confirmId === n.id ? '确认?' : '删除'}
                  </button>
                {/if}
              </div>
            </div>
            {#if openTopic === n.id}
              <div class="review" style="margin-left:{14 + row.depth * 20}px">
                {#each topicMsgs as m, j (j)}
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
      {:else if tree}
        <div class="empty">暂无会话——在对话页开聊后会沉淀到这里。</div>
      {:else}
        <div class="empty">会话树加载中…</div>
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

  /* 话题记忆：会话树 */
  .treelist {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .branch {
    display: flex;
    flex-direction: column;
    border-left: 1px solid var(--line);
    margin-left: 14px;
  }
  .branch:first-child {
    border-left: none;
    margin-left: 0;
  }
  .topic {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-top: 9px;
    padding-bottom: 9px;
    padding-right: 14px;
    flex-wrap: wrap; /* 回顾展开区占满整行 */
    transition: background var(--dur-fast) var(--ease-out);
  }
  /* 展开钮 / 叶点 */
  .tw {
    flex: none;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: 11px;
    cursor: pointer;
    border-radius: 4px;
    padding: 0;
  }
  .tw:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .tw.dot {
    cursor: default;
  }
  .farrow {
    display: inline-block;
    transition: transform var(--dur-fast) var(--ease-out);
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
  .kbadge.arc {
    color: var(--muted);
    background: var(--bg-soft);
    border: 1px solid var(--line);
  }
  .kbadge.mut {
    color: var(--faint);
    background: transparent;
    border: 1px dashed var(--line);
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
