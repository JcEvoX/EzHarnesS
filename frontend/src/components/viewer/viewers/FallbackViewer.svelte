<script lang="ts">
  /*
  Fallback 查看器：暂无专属查看器的资源类型（ppt/zip/二进制…）的占位
  提示——每种资源后续逐个补充专属 viewer（registry 注册 + ResourcePane
  挂载），此处只说明与路径。
  */

  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab } = $props()
</script>

<div class="fallback">
  <p class="ico">🗂</p>
  <p>{tab.name}</p>
  <p class="path">{tab.path}</p>
  <p class="hint">该类型暂无专属查看器——可经系统默认程序打开，或等待后续版本（每种资源将逐步获得专属查看器）</p>
</div>

<style>
  .fallback {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--muted);
    font-size: var(--font-size-section);
    padding: 0 24px;
    text-align: center;
  }
  .ico {
    font-size: 28px;
  }
  .path {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    word-break: break-all;
  }
  .hint {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
