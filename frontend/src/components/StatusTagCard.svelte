<script lang="ts">
  import type { StatusPayload } from '../lib/api'

  let { data, raw }: { data: StatusPayload | null; raw: string } = $props()

  const hm = $derived(data ? data.now.slice(11) : '')
  const pct = $derived(
    data && data.ctxWindow > 0 ? Math.min(100, (data.ctxTokens / data.ctxWindow) * 100) : 0,
  )
  const fmt = (n: number) => (n >= 10000 ? Math.round(n / 1000) + 'k' : String(n))

  /* 新格式（中文语义化文本）解析为结构化片段：时间 / 水位+警示 / 变更条目，
  与旧 JSON 载荷渲染对齐——历史重建不再全文铺开 */
  const parsed = $derived.by(() => {
    const text = raw.replace(/^<agent_status>|<\/agent_status>$/g, '').trim()
    let time = ''
    let level = ''
    let warn = false
    const changes: string[] = []
    for (const line of text.split('\n')) {
      if (line.startsWith('当前时间：')) {
        time = line.slice('当前时间：'.length).trim().slice(11) // 取 hh:mm
      }
      if (line.includes('建议') && line.includes('整理')) {
        warn = true
        const m = line.match(/上下文水位：(\d+)\s*\/\s*(\d+)/)
        if (m) level = `${fmt(Number(m[1]))}/${fmt(Number(m[2]))}`
      }
      if (line.startsWith('本轮资源变更：')) {
        for (const c of line.slice('本轮资源变更：'.length).split('；')) {
          if (c.trim()) changes.push(c.trim())
        }
      }
    }
    return { time, level, warn, changes }
  })
</script>

<!-- 仅异常时渲染（推荐压缩/资源变更），一条细警示行 -->
<div class="alert">
  {#if data}
    <span class="t">{hm}</span>
    {#if data.suggestCompact}
      <span class="mono">{fmt(data.ctxTokens)}/{fmt(data.ctxWindow)}</span>
      <span class="warn">推荐压缩</span>
    {/if}
    {#each data.changes || [] as c (c)}
      <span class={c.startsWith('新增') ? 'add' : 'del'}>{c}</span>
    {/each}
  {:else}
    <span class="t">{parsed.time}</span>
    {#if parsed.warn}
      <span class="mono">{parsed.level}</span>
      <span class="warn">推荐压缩</span>
    {/if}
    {#each parsed.changes as c (c)}
      <span class={c.startsWith('新增') || c.startsWith('用户') ? 'add' : 'del'}>{c}</span>
    {/each}
  {/if}
</div>

<style>
  .alert {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 3px 12px;
    font-size: 11.5px;
    color: var(--muted);
    border-left: 2px solid #d29922;
  }
  .t {
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .warn {
    color: #d29922;
    font-weight: 600;
  }
  .add {
    color: #3fb950;
    font-family: var(--font-mono);
  }
  .del {
    color: #f85149;
    font-family: var(--font-mono);
  }
</style>
