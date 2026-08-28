<script lang="ts">
  import { onMount } from 'svelte'
  import pkg from '../package.json'
  import { store } from './lib/store.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import TitleBar from './components/TitleBar.svelte'
  import ChatView from './components/ChatView.svelte'
  import ModelsView from './components/ModelsView.svelte'
  import MemoryView from './components/MemoryView.svelte'
  import KnowledgeView from './components/KnowledgeView.svelte'
  import ToolsView from './components/ToolsView.svelte'
  import McpView from './components/McpView.svelte'
  import SecurityView from './components/SecurityView.svelte'
  import SettingsView from './components/SettingsView.svelte'

  let view = $state<'chat' | 'models' | 'memory' | 'knowledge' | 'tools' | 'mcp' | 'security' | 'settings'>('chat')
  let expanded = $state(false)

  onMount(() => {
    void store.bootstrap().catch((e) => {
      console.error('bootstrap 失败', e)
      store.lastStatus = `启动失败：${(e as Error).message}`
    })
    // 窗口重新聚焦时刷新（skill/MCP 可能在别的窗口或本机文件系统被改；
    // 后台分支的运行/审批状态不经当前 SSE，聚焦时拉取）
    const onFocus = () => {
      if (view === 'chat') {
        void store.refreshStatus()
        void store.refreshBranches()
      }
    }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  })

  // 进入对话页即刷新：MCP/记忆等页面改动开关或 skill 后，右上角状态卡实时反映
  $effect(() => {
    if (view === 'chat') void store.refreshStatus()
  })
</script>

<div class="shell">
  <TitleBar />
  <div class="app" class:expanded>
    <Sidebar view={view} expanded={expanded} onNavigate={(v) => (view = v)} onToggle={() => (expanded = !expanded)} />
    <main>
      {#if view === 'chat'}
        <ChatView />
      {:else if view === 'models'}
        <ModelsView />
      {:else if view === 'memory'}
        <MemoryView onNavigate={(v) => (view = v as typeof view)} />
      {:else if view === 'knowledge'}
        <KnowledgeView />
      {:else if view === 'tools'}
        <ToolsView />
      {:else if view === 'mcp'}
        <McpView />
      {:else if view === 'security'}
        <SecurityView />
      {:else}
        <SettingsView />
      {/if}
    </main>
  </div>
</div>

<footer class="brand-foot">
  <span>ezharness v{pkg.version}</span>
  <span>·</span>
  <a href="https://github.com/xuanlv2002/ezloop" target="_blank" rel="noreferrer">powered by ezloop</a>
</footer>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .app {
    display: grid;
    grid-template-columns: 64px 1fr;
    flex: 1;
    min-height: 0;
    transition: grid-template-columns var(--dur-in) var(--ease-out);
  }
  .app.expanded {
    grid-template-columns: 176px 1fr;
  }
  main {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .brand-foot {
    position: fixed;
    right: 12px;
    bottom: 8px;
    z-index: 50;
    display: flex;
    gap: 6px;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
    user-select: none;
  }
  .brand-foot a {
    color: var(--faint);
    text-decoration: none;
    border-bottom: 1px dotted transparent;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .brand-foot a:hover {
    color: var(--accent);
  }
</style>
