<script lang="ts">
  import { marked } from 'marked'
  import DOMPurify from 'dompurify'
  import type { ImagePayload } from '../lib/api'

  let {
    text,
    images,
    reasoning = '',
    streaming = false,
    role,
    onFork,
  }: {
    text: string
    images?: ImagePayload[]
    reasoning?: string
    streaming?: boolean
    role: 'user' | 'assistant'
    onFork?: () => void
  } = $props()

  marked.setOptions({ breaks: true, gfm: true })

  /* 模型输出渲染 markdown（XSS 消毒）；mermaid 在流结束后由 effect 替换渲染 */
  const mdHtml = $derived(text ? DOMPurify.sanitize(marked.parse(text) as string) : '')

  let bodyEl: HTMLDivElement | undefined = $state()
  let mmdSeq = 0

  /* mermaid 代码块 → svg：动态加载（包体大，不占首屏）；语法错误保留源码块 */
  $effect(() => {
    if (streaming || !bodyEl || !mdHtml.includes('language-mermaid')) return
    void renderMermaids(bodyEl)
  })

  async function renderMermaids(root: HTMLElement) {
    const blocks = [...root.querySelectorAll('pre > code.language-mermaid')] as HTMLElement[]
    if (!blocks.length) return
    const mermaid = (await import('mermaid')).default
    mermaid.initialize({ startOnLoad: false, theme: 'neutral', securityLevel: 'strict' })
    for (const code of blocks) {
      const pre = code.parentElement
      if (!pre || (pre as HTMLElement).dataset.mmd === '1') continue
      ;(pre as HTMLElement).dataset.mmd = '1'
      try {
        const { svg } = await mermaid.render('mmd-' + ++mmdSeq, code.textContent ?? '')
        const box = document.createElement('div')
        box.className = 'mermaid-box'
        box.innerHTML = svg
        pre.replaceWith(box)
      } catch {
        delete (pre as HTMLElement).dataset.mmd
      }
    }
  }

  let copied = $state(false)

  async function copyText() {
    try {
      await navigator.clipboard.writeText(text)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      /* 剪贴板不可用（非安全上下文等）静默 */
    }
  }
</script>

{#if role === 'user'}
  <div class="user enter-rise">
    <span class="tag">你</span>
    <div class="ucontent">
      {#if images?.length}
        <div class="imgs">
          {#each images as img, i (i)}
            <img src={`data:${img.mimeType};base64,${img.data}`} alt="附件图片 {i + 1}" loading="lazy" />
          {/each}
        </div>
      {/if}
      {#if text}
        <div class="bubble">{text}</div>
      {/if}
    </div>
    {#if onFork}
      <button class="forkbtn" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
    {/if}
  </div>
{:else}
  <div class="assistant">
    <span class="tag">ez</span>
    <div class="body">
      {#if reasoning}
        <details class="reasoning">
          <summary>思考过程</summary>
          <div class="reasoning-text">{reasoning}</div>
        </details>
      {/if}
      {#if text}
        <div class="md" bind:this={bodyEl}>
          {@html mdHtml}
        </div>
        <div class="foot">
          {#if streaming}<span class="caret"></span>{/if}
          {#if onFork}
            <button class="forkbtn" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
          {/if}
          <button class="copy" onclick={copyText} title="复制原文">
            {copied ? '已复制 ✓' : '复制'}
          </button>
        </div>
      {:else if streaming}
        <div class="text"><span class="caret"></span></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .user,
  .assistant {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  /* 分叉按钮：与复制按钮同款弱化样式，hover 显形 */
  .forkbtn {
    flex: none;
    align-self: flex-end;
    font-size: 11px;
    color: var(--faint);
    padding: 2px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .user:hover .forkbtn,
  .assistant:hover .forkbtn,
  .forkbtn:focus-visible {
    opacity: 1;
  }
  /* hover 分叉按钮时高亮所属消息：user/assistant 按钮垂直相邻，
     不高亮难以分辨"分叉到哪条"（误点即多带一条回复） */
  .user:has(.forkbtn:hover) .bubble,
  .user:has(.forkbtn:focus-visible) .bubble {
    outline: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    outline-offset: 2px;
  }
  .assistant:has(.forkbtn:hover) .body,
  .assistant:has(.forkbtn:focus-visible) .body {
    outline: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    outline-offset: 4px;
    border-radius: 6px;
  }
  .forkbtn:hover {
    color: var(--accent);
    background: var(--bg-soft);
  }
  .tag {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    margin-top: 2px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
  }
  .user .tag {
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-color: var(--bg-invert);
  }
  /* 黑底气泡保留；全局 ::selection 是黑底，在黑气泡上选中态隐形——
     气泡内覆盖为白色半透明，复制范围清晰可见 */
  .bubble {
    background: var(--bg-invert);
    color: var(--fg-invert);
    padding: 10px 14px;
    border-radius: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    max-width: 86%;
  }
  .bubble::selection {
    background: rgb(255 255 255 / 32%);
    color: var(--fg-invert);
  }
  /* 多模态图片：缩略网格（点击原生放大交给浏览器，保持零依赖） */
  .ucontent {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    min-width: 0;
    max-width: 86%;
  }
  .imgs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .imgs img {
    max-width: 240px;
    max-height: 240px;
    border-radius: 10px;
    border: 1px solid var(--line);
    cursor: zoom-in;
  }
  .body {
    min-width: 0;
    flex: 1;
    padding-top: 3px;
  }
  .text {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .caret {
    display: inline-block;
    width: 8px;
    height: 17px;
    margin-left: 2px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  .reasoning {
    margin-bottom: 8px;
    border-left: 2px solid var(--line);
    padding-left: 12px;
    color: var(--muted);
    font-size: 13px;
  }
  .reasoning summary {
    cursor: pointer;
    user-select: none;
    font-size: 12px;
    letter-spacing: 0.02em;
  }
  .reasoning-text {
    white-space: pre-wrap;
    margin-top: 6px;
    line-height: 1.6;
  }
  /* 复制按钮行：右下角，弱化存在感 */
  .foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 2px;
    height: 22px;
  }
  .copy {
    font-size: 11px;
    color: var(--faint);
    padding: 2px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .assistant:hover .copy,
  .copy:focus-visible {
    opacity: 1;
  }
  .copy:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  /* ── markdown 正文：scoped 样式不作用于 {@html} 注入节点，全部走 :global ── */
  .md {
    line-height: 1.7;
  }
  .md :global(p) {
    margin: 6px 0;
  }
  .md :global(p:first-child) {
    margin-top: 0;
  }
  .md :global(p:last-child) {
    margin-bottom: 0;
  }
  .md :global(h1),
  .md :global(h2),
  .md :global(h3),
  .md :global(h4) {
    margin: 14px 0 6px;
    font-weight: 650;
    line-height: 1.4;
  }
  .md :global(h1) {
    font-size: 1.25em;
  }
  .md :global(h2) {
    font-size: 1.15em;
  }
  .md :global(h3),
  .md :global(h4) {
    font-size: 1.05em;
  }
  .md :global(ul),
  .md :global(ol) {
    margin: 6px 0;
    padding-left: 1.5em;
  }
  .md :global(li) {
    margin: 2px 0;
  }
  .md :global(code) {
    font-family: var(--font-mono);
    font-size: 0.88em;
    background: var(--bg-soft);
    border-radius: 5px;
    padding: 1px 5px;
  }
  .md :global(pre) {
    margin: 8px 0;
    padding: 10px 12px;
    background: var(--bg-soft);
    border-radius: 10px;
    overflow-x: auto;
  }
  .md :global(pre code) {
    background: none;
    border: none;
    padding: 0;
    font-size: 12.5px;
    line-height: 1.6;
  }
  .md :global(blockquote) {
    margin: 8px 0;
    padding: 2px 12px;
    border-left: 3px solid var(--line-strong);
    color: var(--muted);
  }
  /* 极简表格：无竖线无外框，表头仅一条深色底线，行间细线分隔 */
  .md :global(table) {
    margin: 10px 0;
    border-collapse: collapse;
    font-size: 0.94em;
  }
  .md :global(th),
  .md :global(td) {
    padding: 6px 18px 6px 0;
    text-align: left;
    vertical-align: top;
  }
  .md :global(th) {
    border-bottom: 2px solid var(--line-strong);
    font-weight: 650;
    background: none;
  }
  .md :global(td) {
    border-bottom: 1px solid var(--line);
  }
  .md :global(tr:last-child td) {
    border-bottom: none;
  }
  .md :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .md :global(a:hover) {
    text-decoration: underline;
  }
  .md :global(hr) {
    margin: 12px 0;
    border: none;
    border-top: 1px solid var(--line);
  }
  .md :global(.mermaid-box) {
    margin: 10px 0;
    padding: 12px;
    background: var(--bg-soft);
    border-radius: 10px;
    overflow-x: auto;
    text-align: center;
  }
</style>
