<script lang="ts">
  import { store } from '../lib/store.svelte'
</script>

<!-- 快应用首次打开授权：把「将获得什么能力」摆出来，授权一次长期有效。
     宠物窗也渲染本卡（.trustmask 参与其点击穿透判定）。 -->
{#if store.appTrust}
  {@const p = store.appTrust}
  <div class="trustmask">
    <div class="card" role="dialog" aria-modal="true" aria-label="快应用首次打开授权">
      <h3>首次打开快应用</h3>
      <p class="name">{p.title}</p>
      <p class="lead">打开后，这个应用可以：</p>
      <ul>
        <li><b>调用本机模型</b><span>摘要、翻译、识别等，消耗模型额度</span></li>
        <li><b>调用 MCP 工具</b><span>可能读写文件、执行命令、访问外部服务</span></li>
      </ul>
      <p class="hint">文件、设置、终端、浏览器等其他接口对它关闭。授权一次，之后启动不再询问。</p>
      <div class="ops">
        <button class="ghost" onclick={() => store.resolveAppTrust(false)}>取消</button>
        <button class="go" onclick={() => store.resolveAppTrust(true)}>信任并打开</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .trustmask {
    position: fixed;
    inset: 0;
    z-index: 4000;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 12px;
    background: color-mix(in srgb, #0f1218 42%, transparent);
    backdrop-filter: blur(3px);
  }
  .card {
    box-sizing: border-box;
    width: min(400px, 100%);
    max-height: 100%;
    overflow: auto;
    padding: 18px 20px;
    border-radius: 14px;
    background: var(--bg);
    color: var(--fg);
    border: 1px solid var(--line);
    box-shadow: 0 18px 60px rgba(0, 0, 0, 0.28);
    animation: trust-in var(--dur-fast) var(--ease-out);
  }
  @keyframes trust-in {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.98);
    }
  }
  h3 {
    margin: 0 0 4px;
    font-size: var(--font-size-content);
  }
  .name {
    margin: 0 0 12px;
    font-size: var(--font-size-body);
    font-weight: 600;
    word-break: break-word;
  }
  .lead {
    margin: 0 0 6px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  ul {
    margin: 0 0 10px;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  li {
    font-size: var(--font-size-ui);
    color: var(--fg);
  }
  li b {
    font-weight: 600;
  }
  li span {
    display: block;
    margin-top: 2px;
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .hint {
    margin: 0 0 14px;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--faint);
  }
  .ops {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  button {
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 7px 14px;
    border-radius: 8px;
    border: 1px solid transparent;
    cursor: pointer;
  }
  .ghost {
    background: transparent;
    color: var(--muted);
    border-color: var(--line);
  }
  .ghost:hover {
    background: var(--bg-soft);
  }
  .go {
    background: var(--accent);
    color: #fff;
  }
  .go:hover {
    filter: brightness(1.06);
  }
</style>
