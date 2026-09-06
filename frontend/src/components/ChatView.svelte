<script lang="ts">
  import Timeline from './Timeline.svelte'
  import InputBar from './InputBar.svelte'
  import StatusCard from './StatusCard.svelte'
  import NoticePanel from './NoticePanel.svelte'
  import ForkPanel from './ForkPanel.svelte'
  import BranchPanel from './BranchPanel.svelte'
  import { store } from '../lib/store.svelte'

  /* 拖拽附件：整个对话页是热区（dragenter/leave 计数防子元素抖动）。
  图片附件随消息多模态直发（粘贴/画板同路），非图片暂不支持。 */
  let files = $state<File[]>([])
  let dragging = $state(false)
  let depth = 0

  function onDragEnter(e: DragEvent) {
    if (!e.dataTransfer?.types.includes('Files')) return
    e.preventDefault()
    depth++
    dragging = true
  }

  function onDragLeave() {
    if (--depth <= 0) {
      depth = 0
      dragging = false
    }
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault() // 允许 drop
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    depth = 0
    dragging = false
    for (const f of e.dataTransfer?.files ?? []) {
      files = [...files, f]
    }
  }

  function removeFile(i: number) {
    files = files.filter((_, idx) => idx !== i)
  }

  function addFiles(fs: File[]) {
    files = [...files, ...fs]
  }

  function clearFiles() {
    files = []
  }

  /* 画板产物回流：tag 为编辑目标的下标且原附件未变时替换，否则追加
  （编辑期间附件被删/换了页面则降级追加）。本页未挂载时产物积压在
  store，回对话页后首跑消费，跨页不丢。 */
  $effect(() => {
    const p = store.pendingBoardFile
    if (!p) return
    store.pendingBoardFile = null
    const i = /^\d+$/.test(p.tag) ? Number(p.tag) : -1
    if (i >= 0 && files[i] === p.source) {
      const next = [...files]
      next[i] = p.file
      files = next
    } else {
      files = [...files, p.file]
    }
  })

  /* 通知跳转主时间线锚点：jumpMain 置位后滚动到目标卡（跨分支切换后
  历史异步加载，tick 依赖让块到达后重试；找到即滚动并清空） */
  $effect(() => {
    if (!store.jumpMain) return
    store.tick
    const el = document.getElementById(store.jumpMain)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      store.jumpMain = ''
    }
  })
</script>

<div
  class="chat"
  class:dragging
  ondragenter={onDragEnter}
  ondragleave={onDragLeave}
  ondragover={onDragOver}
  ondrop={onDrop}
>
  <div class="main-col">
    <Timeline />
    <InputBar
      {files}
      onRemove={removeFile}
      onEditImage={(i) => store.openBoard(files[i] ?? null, String(i))}
      onAddFiles={addFiles}
      onClearFiles={clearFiles}
    />
  </div>
  <aside class="side-left">
    <BranchPanel />
  </aside>
  <aside class="side">
    <StatusCard />
    <!-- 全局通知栏（数据跨分支轮询）：保持侧栏一列布局 -->
    <NoticePanel
      notices={store.notices}
      onResolve={(id, action, input) => {
        const n = store.notices.find((x) => x.id === id)
        if (n) void store.resolveNoticeGlobal(n, action, input)
      }}
      onJump={(n) => void store.jumpToNotice(n)}
      onDismiss={(id) => store.dismissNotice(id)}
    />
    <div class="entries">
      <button class="entry" onclick={() => store.toggleTermDrawer()} title="共享终端（用户与 AI 共写）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M5 8l4 4-4 4" />
          <path d="M12 16.5h7" />
        </svg>
      </button>
    </div>
  </aside>
  <ForkPanel />
  {#if dragging}    <div class="dropzone">
      <div class="hint-box">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 16V5" />
          <path d="M7 10l5-5 5 5" />
          <path d="M4 19h16" />
        </svg>
        松开以添加附件
      </div>
    </div>
  {/if}
</div>

<style>
  .chat {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
  }
  /* 主列：聊天记录 + 输入框。限制最大宽度并在剩余空间居中，
  时间线滚动条因此贴在内容右缘，而不是被推到窗口最右侧；
  右侧悬浮列不占布局宽度，窄屏时允许少量重叠 */
  .main-col {
    flex: 1;
    min-width: 0;
    max-width: 880px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }
  /* 左侧悬浮列：分支面板浮在内容之上（与右侧状态/通知列对称）。
  容器点击穿透，卡片自身可交互 */
  .side-left {
    position: absolute;
    top: 16px;
    left: 16px;
    bottom: 30px;
    width: 240px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
    pointer-events: none;
  }
  .chat > .side-left > :global(*) {
    pointer-events: auto;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .side-left > :global(.panel) {
    min-height: 0;
    width: 100%;
  }
  /* 右侧悬浮列：状态卡 + 通知栏浮在内容之上。容器点击穿透，
  卡片自身可交互；bottom 留出右下角 brand-foot 的位置 */
  .side {
    position: absolute;
    top: 16px;
    right: 16px;
    bottom: 30px;
    width: 260px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 10px;
    pointer-events: none;
  }
  .chat > .side > :global(*) {
    pointer-events: auto;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .side > :global(.panel) {
    min-height: 0; /* 通知过多时收缩，列表内部滚动 */
  }
  /* 终端抽屉入口：右列通知下方，方形图标钮（与卡片同视觉语言） */
  .entries {
    align-self: flex-end;
    display: flex;
    gap: 8px;
  }
  .entry {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--muted);
    border-radius: 10px;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .entry:hover {
    background: var(--bg-soft);
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .entry svg {
    width: 16px;
    height: 16px;
  }
  .dropzone {
    position: absolute;
    inset: 10px;
    z-index: 10;
    display: grid;
    place-items: center;
    border: 2px dashed var(--accent);
    border-radius: 16px;
    background: color-mix(in srgb, var(--bg) 75%, transparent);
    backdrop-filter: blur(2px);
    pointer-events: none;
    animation: drop-in var(--dur-fast) var(--ease-out) both;
  }
  .hint-box {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 14px;
    font-weight: 550;
    color: var(--accent);
    background: var(--bg);
    border: 1px solid var(--accent-soft);
    border-radius: 12px;
    padding: 12px 22px;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .hint-box svg {
    width: 18px;
    height: 18px;
  }
  @keyframes drop-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
</style>
