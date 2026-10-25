<script lang="ts">
  import type { NoticeData } from '../lib/store.svelte'

  /*
  通知栏：agent 的人机交互请求（审批/询问/规划）在此集中呈现。
  可内联完成，也可「查看」跳转到时间线对应卡片（如 fork 内的审批块）。
  数据由 store.notices 驱动（SSE 决策请求帧 + 全局轮询）。
  */
  let {
    notices = [],
    onResolve,
    onJump,
    onDismiss,
    standalone = false,
  }: {
    notices?: NoticeData[]
    onResolve?: (id: string, action: string, input?: string) => void
    onJump?: (n: NoticeData) => void
    onDismiss?: (id: string) => void
    /* standalone：独立通知窗里用——常驻展开、不写 localStorage（同源会污染
       主窗的折叠记忆），也不显示收起按钮 */
    standalone?: boolean
  } = $props()

  const kindMeta: Record<NoticeData['kind'], { label: string; icon: string }> = {
    approve: { label: '审批', icon: '<path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3z"/><path d="M9 12l2 2 4-4"/>' },
    ask: { label: '询问', icon: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 1 1 3.4 2.3c-.6.3-.9.8-.9 1.4v.3"/><path d="M12 17h.01"/>' },
    info: { label: '通知', icon: '<circle cx="12" cy="12" r="9"/><path d="M12 8h.01M12 11v5"/>' },
    mcp_auth: { label: '认证', icon: '<circle cx="8" cy="15" r="4"/><path d="M11 12l7-7M16 5l2 2M14 7l2 2"/>' },
    memory: { label: '记忆', icon: '<path d="M12 3a4 4 0 0 0-4 4c0 1.1.45 2.1 1.17 2.83L12 12l2.83-2.17A4 4 0 0 0 12 3z"/><path d="M5 8h14l1 12H4L5 8z"/>' },
  }

  /* 记忆待确认：点击行展开详情全文（确认前查看完整内容） */
  let memExpanded = $state<Set<string>>(new Set())
  function toggleMemoryDetail(id: string) {
    const s = new Set(memExpanded)
    if (s.has(id)) s.delete(id)
    else s.add(id)
    memExpanded = s
  }
  /* detail = 摘要首行 + 正文：行内只显示摘要，展开才给正文（不重复堆叠） */
  const memSummary = (n: NoticeData) => (n.detail || '').split('\n')[0]
  const memBody = (n: NoticeData) => (n.detail || '').split('\n').slice(1).join('\n').trim()

  let answers = $state<Record<string, string>>({})

  /* 收起为小方块：有待处理通知时右上角红点提示，localStorage 记忆收起态 */
  const collapsedKey = 'ezh.noticePanel.collapsed'
  let collapsed = $state(standalone
    ? false
    : (() => {
        try {
          return localStorage.getItem(collapsedKey) !== '0' // 无记录默认折叠
        } catch {
          return true
        }
      })())
  function fold(v: boolean) {
    collapsed = v
    try {
      localStorage.setItem(collapsedKey, v ? '1' : '0')
    } catch {
      /* 存储不可用时仅本次生效 */
    }
  }
  const pendingCount = $derived(notices.filter((n) => n.status === 'pending').length)
  /* 完成通知（info）：非待处理，但也要挂上小点（收起态的铃铛） */
  const infoCount = $derived(notices.filter((n) => n.kind === 'info').length)
</script>

{#if collapsed}
  <button
    class="mini"
    onclick={() => fold(false)}
    title={`通知 · 点击展开${pendingCount > 0 ? `（${pendingCount} 条待处理）` : ''}`}
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </svg>
    {#if pendingCount + infoCount > 0}
      <i class="dot"></i>
    {/if}
  </button>
{:else}
  <div class="panel">
    <div class="head">
      <h2>通知</h2>
      {#if pendingCount > 0}
        <span class="badge">{pendingCount} 待处理</span>
      {/if}
      {#if !standalone}
        <button class="fold" onclick={() => fold(true)} title="收起">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
            <path d="M5 12h14" />
          </svg>
        </button>
      {/if}
    </div>

  {#if notices.length === 0}
    <p class="empty">暂无通知——审批、提问与执行完成会出现在这里</p>
  {:else}
    <div class="list">
      {#each notices as n (`${n.rootId}:${n.id}`)}
        <div class="notice" class:pending={n.status === 'pending'} class:done={n.status === 'done'}>
          <div
            class="row"
            role={n.rootId ? 'button' : undefined}
            tabindex="0"
            onclick={() => {
              if (n.kind === 'memory') toggleMemoryDetail(n.id)
              else n.rootId && onJump?.(n)
            }}
          >
            <span class="kind-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                {@html kindMeta[n.kind].icon}
              </svg>
            </span>
            <div class="info">
              <span class="title">
                <span class="t">{n.title}</span>
                {#if n.source !== 'agent'}
                  <i class="src">{n.source}</i>
                {/if}
              </span>
              {#if n.detail}
                <span class="detail">{n.kind === 'memory' ? memSummary(n) : n.detail}</span>
              {/if}
            </div>
            <span class="time">{n.time}</span>
            {#if n.kind === 'memory'}
              <span class="exp" class:on={memExpanded.has(n.id)} title={memExpanded.has(n.id) ? '收起详情' : '展开详情'}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M6 9l6 6 6-6" />
                </svg>
              </span>
            {/if}
            {#if n.status === 'done'}
              <button class="close" onclick={() => onDismiss?.(n.id)} title="关闭通知">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            {/if}
          </div>

          {#if n.kind === 'memory' && memExpanded.has(n.id) && memBody(n)}
            <pre class="memfull">{memBody(n)}</pre>
          {/if}

          {#if n.status === 'pending'}
            <div class="actions">
              {#if n.kind === 'memory'}
                <button class="btn ok" onclick={() => onResolve?.(n.id, 'accept')}>确认</button>
                <button class="btn no" onclick={() => onResolve?.(n.id, 'cancel')}>取消</button>
              {:else if n.kind === 'mcp_auth'}
                <button class="btn ok" onclick={() => onResolve?.(n.id, 'authorize')}>去授权</button>
                <button class="btn no" onclick={() => onResolve?.(n.id, 'cancel')}>取消</button>
              {:else if n.kind === 'approve'}
                <button class="btn ok" onclick={() => onResolve?.(n.id, 'approve')}>批准</button>
                <button class="btn no" onclick={() => onResolve?.(n.id, 'reject')}>拒绝</button>
              {:else if n.kind === 'ask'}
                <input
                  type="text"
                  placeholder="输入回答…"
                  bind:value={answers[n.id]}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' && answers[n.id]?.trim()) onResolve?.(n.id, 'answer', answers[n.id].trim())
                  }}
                />
                <button class="btn ok" onclick={() => answers[n.id]?.trim() && onResolve?.(n.id, 'answer', answers[n.id].trim())}>回答</button>
              {/if}
              {#if n.target}
                <button class="jump" onclick={() => onJump?.(n)} title="跳转到对应位置">查看 →</button>
              {/if}
            </div>
          {:else}
            <div class="resolved">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M5 13l4 4L19 7" />
              </svg>
              {n.resolution || '已处理'}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
  </div>
{/if}

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    width: 100%;
    max-height: 100%;
    padding: 12px 14px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 1px 3px rgb(0 0 0 / 4%);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  h2 {
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--muted);
  }
  .badge {
    font-size: var(--font-size-small);
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
  }
  /* 收起态小方块：铃铛图标，待处理通知时右上角红点；靠右贴边（.side 列的末端对齐） */
  .mini {
    position: relative;
    flex: none;
    align-self: flex-end;
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    color: var(--muted);
    cursor: pointer;
    padding: 0;
  }
  .mini:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .mini svg {
    width: 16px;
    height: 16px;
  }
  .dot {
    position: absolute;
    top: -3px;
    right: -3px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #f85149;
    border: 2px solid var(--bg);
  }
  .fold {
    flex: none;
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .panel:hover .fold {
    opacity: 1;
  }
  .fold:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .fold svg {
    width: 11px;
    height: 11px;
  }
  .empty {
    font-size: var(--font-size-small);
    color: var(--faint);
    padding: 4px 0 6px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 45vh;
    overflow-y: auto;
    overflow-x: hidden;
    min-height: 0;
    /* 两侧留 6px：pending 态负边距（-6px）恰好贴到 padding box 边缘，
       既保留左侧通栏高亮又不产生横向溢出 */
    padding: 0 6px;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
  }
  .notice {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 8px 6px;
    border-radius: 8px;
  }
  .notice + .notice {
    border-top: 1px solid var(--line);
    border-radius: 0;
  }
  .notice.pending {
    background: color-mix(in srgb, var(--accent) 5%, var(--bg));
    box-shadow: inset 3px 0 0 var(--accent);
    margin: 0 -6px;
    padding: 8px 8px 8px 11px;
    border-radius: 0 8px 8px 0;
  }
  .notice.pending + .notice {
    border-top: none;
    padding-top: 8px;
  }
  .notice.done {
    opacity: 0.6;
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  .row[role='button'] {
    cursor: pointer;
  }
  .kind-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--muted);
  }
  .notice.pending .kind-icon {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .kind-icon svg {
    width: 12px;
    height: 12px;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  /* 行数恒定防换行抖动：标题单行截断、详情固定两行高度（max-height
  替代 -webkit-line-clamp——后者在文本更新时换行计算会出错，hover
  重绘才恢复）。 */
  .title {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    overflow: hidden;
    font-size: var(--font-size-ui);
    font-weight: 500;
    color: var(--fg);
  }
  .title .t {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* 会话名标签不收缩（截断让位给工具名 .t），保持完整可读 */
  .src {
    flex: none;
    font-style: normal;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0 4px;
    white-space: nowrap;
  }
  .detail {
    min-width: 0;
    font-size: var(--font-size-small);
    line-height: 1.4;
    max-height: 2.8em;
    overflow: hidden;
    color: var(--faint);
  }
  .time {
    flex: none;
    min-width: 34px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .close {
    flex: none;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    margin: -2px -2px 0 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--faint);
    opacity: 0;
    cursor: pointer;
  }
  .close svg {
    width: 9px;
    height: 9px;
  }
  .notice:hover .close {
    opacity: 1;
  }
  .close:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* 记忆待确认：行尾展开箭头（收起/展开详情） */
  .exp {
    flex: none;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    margin-top: 1px;
    color: var(--faint);
    transition:
      transform var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .exp.on {
    transform: rotate(180deg);
    color: var(--accent);
  }
  .exp svg {
    width: 11px;
    height: 11px;
  }
  /* 记忆待确认展开正文：安静的一块，不要花哨描边（行内摘要已给上下文） */
  .memfull {
    margin: 0;
    padding: 9px 11px;
    max-height: 220px;
    overflow: auto;
    font-family: inherit;
    font-size: var(--font-size-ui);
    line-height: 1.65;
    color: var(--muted);
    background: var(--bg-soft);
    border-radius: 8px;
    white-space: pre-wrap;
    word-break: break-word;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
  }
  .actions input {
    flex: 1;
    min-width: 80px;
    border: 1px solid var(--line);
    border-radius: 7px;
    padding: 4px 8px;
    font-size: var(--font-size-small);
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .actions input:focus {
    border-color: var(--line-strong);
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    border-radius: 7px;
    padding: 4px 12px;
    font-size: var(--font-size-small);
    font-weight: 500;
  }
  .btn.ok {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn:hover {
    opacity: 0.85;
  }
  .jump {
    margin-left: auto;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: var(--font-size-small);
    padding: 4px 2px;
  }
  .resolved {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .resolved svg {
    width: 11px;
    height: 11px;
    color: var(--accent);
  }
</style>
