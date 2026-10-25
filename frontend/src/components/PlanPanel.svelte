<script lang="ts">
  import { store } from '../lib/store.svelte'

  const plan = $derived(store.plan)
  const running = $derived(plan?.progress.find((p) => p.status === 'running') ?? null)
  const done = $derived(plan?.progress.filter((p) => p.status === 'completed').length ?? 0)
  const total = $derived(plan?.progress.length ?? 0)
  const allDone = $derived(total > 0 && done === total)
  /* 完整方案可能几百字：默认折叠，只留一行摘要；点头部展开全文与步骤 */
  let open = $state(false)
  const peek = $derived((plan?.plan || '').split('\n').find((l) => l.trim())?.trim() || '')
</script>

{#if plan && (plan.plan || total)}
  <div class="plan" class:finished={allDone}>
    <button class="head" onclick={() => (open = !open)} title={open ? '收起计划' : '展开完整计划'}>
      <span class="label">任务计划</span>
      {#if running}
        <span class="now"><span class="spin"></span>正在 {running.title}</span>
      {:else if allDone}
        <span class="now done">✓ 全部完成</span>
      {:else}
        <span class="now idle">等待推进</span>
      {/if}
      <span class="count">{done}/{total}</span>
      <span class="arrow" class:open>{open ? '▾' : '▸'}</span>
    </button>
    {#if open}
      {#if plan.plan}<pre class="goal">{plan.plan}</pre>{/if}
      {#if total}
        <ol class="steps">
          {#each plan.progress as p, i (i)}
            <li class:running={p.status === 'running'} class:done={p.status === 'completed'}>
              <span class="ico">{p.status === 'completed' ? '✓' : p.status === 'running' ? '●' : '○'}</span>
              <span class="tt">{p.title}</span>
            </li>
          {/each}
        </ol>
      {/if}
    {:else if peek}
      <p class="peek">{peek}</p>
    {/if}
  </div>
{/if}

<style>
  .plan {
    margin: 0 4px 10px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
    overflow: hidden;
  }
  .plan.finished {
    opacity: 0.75;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: none;
    background: transparent;
    text-align: left;
    color: var(--fg);
  }
  .head:hover {
    background: color-mix(in srgb, var(--bg-invert) 4%, transparent);
  }
  .label {
    flex: none;
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    color: var(--faint);
  }
  .now {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    color: var(--fg);
  }
  .now.idle {
    color: var(--muted);
  }
  .now.done {
    color: #3fb950;
  }
  .spin {
    flex: none;
    width: 9px;
    height: 9px;
    border: 1.5px solid var(--line);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .count {
    flex: none;
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .arrow {
    flex: none;
    font-size: 10px;
    color: var(--faint);
  }
  .peek {
    margin: 0;
    padding: 0 10px 7px 10px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    color: var(--muted);
  }
  .goal {
    margin: 0;
    padding: 0 10px 8px;
    max-height: 200px;
    overflow-y: auto;
    font-size: 12px;
    line-height: 1.55;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 6px 10px 8px;
    border-top: 1px solid var(--line);
    max-height: 180px;
    overflow-y: auto;
  }
  .steps li {
    display: flex;
    align-items: flex-start;
    gap: 7px;
    padding: 2px 0;
    font-size: 12px;
    line-height: 1.45;
    color: var(--muted);
  }
  .steps li.running {
    color: var(--fg);
  }
  .steps li.done {
    color: var(--faint);
  }
  .steps li.done .tt {
    text-decoration: line-through;
    text-decoration-color: var(--line-strong);
  }
  .ico {
    flex: none;
    width: 12px;
    text-align: center;
    font-size: 11px;
  }
  li.running .ico {
    color: var(--accent);
  }
  li.done .ico {
    color: #3fb950;
  }
  .tt {
    min-width: 0;
    word-break: break-word;
  }
</style>
