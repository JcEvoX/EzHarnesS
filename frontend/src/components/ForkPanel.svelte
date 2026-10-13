<script lang="ts">
  /*
  分身抽屉：task 分身的独立聊天框。主时间线只留入口卡（ForkCard），
  点击打开本抽屉查看完整执行记录（任务输入→思考→工具→决策→交付）。
  分身的审批/询问/OAuth 决策卡路由到此（通知栏点击跳转定位），非模态
  半开——主对话仍可见。多分身时顶部 tab 切换。
  行模型/吸底/活动指示与 Timeline 同构（lib/rows 共用组装）。
  */
  import { store } from '../lib/store.svelte'
  import { buildRows, TOOL_GROUP_MIN } from '../lib/rows'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ToolGroup from './ToolGroup.svelte'
  import DecisionCard from './DecisionCard.svelte'
  import EzAvatar from './EzAvatar.svelte'

  let body: HTMLDivElement | undefined = $state()
  let stick = $state(true) // 贴底才跟随流式（上翻看历史时不拽回）
  let lastFid = ''
  let openGroups = $state<Set<number>>(new Set())

  const fork = $derived(store.forks[store.activeForkId])
  const rows = $derived(fork ? buildRows(fork.blocks) : [])

  function toggleGroup(key: number) {
    const s = new Set(openGroups)
    if (s.has(key)) s.delete(key)
    else s.add(key)
    openGroups = s
  }

  /* ── 吸底（对齐 Timeline）：程序滚动不算用户滚动（selfScroll），
  滞回 <30px 恢复跟随 / >120px 脱离，杜绝边界抖动 ── */
  let selfScroll = false

  function follow() {
    if (!body) return
    selfScroll = true
    body.scrollTop = body.scrollHeight
  }

  function onScroll() {
    if (!body) return
    if (selfScroll) {
      selfScroll = false
      return
    }
    const dist = body.scrollHeight - body.scrollTop - body.clientHeight
    if (dist < 30) stick = true
    else if (dist > 120) stick = false
  }

  /* 切换分身滚动归零；通知跳转定位决策卡；运行中且贴底才跟随流式 */
  $effect(() => {
    if (!fork || !body) return
    if (fork.id !== lastFid) {
      lastFid = fork.id
      stick = true
      body.scrollTop = 0
    }
    void store.tick
    if (store.jumpDecision) {
      const el = body.querySelector(`#decision-${store.jumpDecision}`)
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        stick = false // 用户在读这张卡，别再拽底
        store.jumpDecision = ''
        return
      }
    }
    if (fork.status === 'running' && stick) follow()
  })

  /* 活动指示（对齐 Timeline 的 modelPhase，按分身自己的块推导）：
  尾部 building 工具=输出中；流式块无正文/user=思考中；正文已流出不显
  （内容本身是进度），执行中工具由工具卡自带 spinner */
  const phase = $derived.by(() => {
    if (!fork || fork.status !== 'running') return ''
    for (let i = fork.blocks.length - 1; i >= 0; i--) {
      const b = fork.blocks[i]
      if (b.kind === 'assistant') return b.streaming && b.text ? '' : 'think'
      if (b.kind === 'user') return 'think'
      if (b.kind === 'tool' && b.state === 'building') return 'gen'
    }
    return 'think'
  })
</script>

{#if fork}
  <aside class="drawer">
    <header class="head">
      <div class="htop">
        {#if fork.status === 'running'}
          <span class="breathe"></span>
        {:else}
          <span class="dot"></span>
        {/if}
        <span class="fid">{fork.id}</span>
        <span class="state" class:running={fork.status === 'running'}>
          {fork.status === 'running' ? '运行中' : fork.stopReason || 'done'}
        </span>
        <button class="close" onclick={() => store.closeFork()} title="关闭">✕</button>
      </div>
      <p class="task" title={fork.task}>{fork.task || '子任务'}</p>
    </header>

    <div class="body" bind:this={body} onscroll={onScroll}>
      {#if fork.blocks.length === 0}
        <div class="placeholder">
          {#if fork.status === 'done'}
            {fork.answer ? '执行记录加载中…' : '（无执行记录）'}
          {:else}
            <span class="caret"></span>
          {/if}
        </div>
      {/if}
      {#each rows as row (row.key)}
        {#if row.kind === 'user'}
          <MessageItem text={row.b.text} images={row.b.images} role="user" />
        {:else}
          <div class="grp" class:leader={row.kind === 'leader'}>
            {#if row.kind === 'group'}
              {@const live = rows[rows.length - 1] === row}
              <span class="tag">
                <EzAvatar size={36} kind="task" run={live && fork.status === 'running'} animate={live}>ez</EzAvatar>
              </span>
            {/if}
            <div class="gbody">
              {#each row.segs as seg}
                {#if seg.type === 'tools'}
                  {@const key = seg.blocks[0].uid}
                  {#if seg.blocks.length >= TOOL_GROUP_MIN}
                    <ToolGroup blocks={seg.blocks} open={openGroups.has(key)} onToggle={() => toggleGroup(key)} />
                  {:else}
                    {#each seg.blocks as b (b.uid)}
                      <ToolBlock data={b} />
                    {/each}
                  {/if}
                {:else if seg.b.kind === 'assistant'}
                  <MessageItem text={seg.b.text} reasoning={seg.b.reasoning} streaming={seg.b.streaming} role="assistant" bare />
                {:else if seg.b.kind === 'decision'}
                  <div id={`decision-${seg.b.id}`}>
                    <DecisionCard data={seg.b} />
                  </div>
                {:else if seg.b.kind === 'note'}
                  <div class="note">
                    <span class="line"></span>
                    {seg.b.text}
                    <span class="line"></span>
                  </div>
                {/if}
              {/each}
            </div>
          </div>
        {/if}
      {/each}
      {#if phase}
        <div class="thinking">
          <span class="tdot"></span>{phase === 'gen' ? '模型输出中…' : '模型思考中…'}
        </div>
      {/if}
      {#if fork.status === 'done' && fork.answer}
        <div class="answer">
          <div class="label">最终交付</div>
          <div class="text">{fork.answer}</div>
        </div>
      {/if}
    </div>
  </aside>
{/if}

<style>
  .drawer {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(520px, 48vw);
    /* 高于 brand-foot（fixed z50）：打开期间盖住右下角脚标，关闭恢复 */
    z-index: 55;
    display: flex;
    flex-direction: column;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border-left: 1px solid var(--line);
    box-shadow: -12px 0 32px rgb(0 0 0 / 10%);
    animation: slide-in var(--dur, 0.22s) var(--ease-out, ease-out) both;
  }
  @keyframes slide-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
  /* 头部：两行（上行标识/状态，下行任务名），无分割线靠留白分隔 */
  .head {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 13px 16px 11px;
  }
  .htop {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .breathe {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: breathe 2.4s ease-in-out infinite;
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.35;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  .fid {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 700;
    color: var(--accent);
    flex: none;
  }
  .task {
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    margin-left: auto;
    flex: none;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .state.running {
    color: var(--accent);
  }
  .close {
    flex: none;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
    cursor: pointer;
    padding: 2px 6px;
    border-radius: 6px;
  }
  .close:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden; /* 长路径/长参数只换行不横滚 */
    scrollbar-gutter: stable; /* 预留滚动条槽：内容加载不因滚动条出现而晃动 */
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    --av-size: 36px; /* 头像列宽：MessageItem 用户行占位与本组件头像共用 */
  }
  /* 子项不压缩：flex 列容器溢出时默认 shrink 会把消息块挤扁（伪乱序） */
  .body > :global(*) {
    flex: none;
  }
  .placeholder {
    color: var(--faint);
    font-size: 12.5px;
    text-align: center;
    padding: 24px 0;
  }
  .caret {
    display: inline-block;
    width: 7px;
    height: 15px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  @keyframes caret {
    50% {
      opacity: 0;
    }
  }
  /* 回复组（与 Timeline 同构，头像 36px 适配抽屉窄列） */
  .grp {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .grp .tag {
    flex: none;
    display: grid;
    place-items: center;
  }
  .grp .tag:has(> :global(img)) {
    border-color: transparent;
    background: transparent;
  }
  .gbody {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding-top: 2px;
    --proc-indent: 0px; /* 组内过程卡对齐正文（Timeline 同规则） */
  }
  /* 引导组：首 user 之前的记录，无头像平铺 */
  .grp.leader .gbody {
    gap: 10px;
  }
  .note {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 11.5px;
    color: var(--faint);
    font-family: var(--font-mono);
    padding: 4px 0;
  }
  .note .line {
    flex: 1;
    height: 1px;
    background: var(--line);
  }
  /* 活动指示（与 Timeline 的 thinking 同款） */
  .thinking {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--faint);
    padding: 2px 0;
  }
  .tdot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--line-strong);
    animation: tdot-breath 1.4s ease-in-out infinite;
  }
  @keyframes tdot-breath {
    0%,
    100% {
      opacity: 0.25;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .answer {
    border: 1px solid var(--accent-soft, var(--line-strong));
    border-radius: 12px;
    padding: 10px 14px;
    background: var(--bg-soft);
  }
  .answer .label {
    font-size: 10px;
    letter-spacing: 0.08em;
    color: var(--accent);
    margin-bottom: 4px;
  }
  .answer .text {
    font-size: 13px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.65;
  }
</style>
