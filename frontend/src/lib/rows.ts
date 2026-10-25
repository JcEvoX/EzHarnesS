/*
rows 是时间线的行组装（主时间线与分身抽屉共用）：user 块独立成行，
其余块并入其后的回复组；组内连续 tool 段 ≥ TOOL_GROUP_MIN 折成摘要。
组 key 用前置 user 块 uid——组内中段增删不换 key，流式与展开态不抖。
*/

import type { Block } from './store.svelte'

export const TOOL_GROUP_MIN = 2

export type ToolBlockB = Extract<Block, { kind: 'tool' }>
export type Seg = { type: 'one'; b: Block } | { type: 'tools'; blocks: ToolBlockB[] }
export type Row =
  | { kind: 'leader'; key: 'L'; segs: Seg[] }
  | { kind: 'user'; key: string; b: Extract<Block, { kind: 'user' }> }
  | { kind: 'group'; key: string; segs: Seg[] }

export function buildRows(blocks: Block[]): Row[] {
  const out: Row[] = []
  let segs: Seg[] = []
  let toolRun: ToolBlockB[] = []
  let curKey = 'L'
  const flushTools = () => {
    if (toolRun.length) {
      segs.push({ type: 'tools', blocks: toolRun })
      toolRun = []
    }
  }
  const flushGroup = () => {
    flushTools()
    if (!segs.length) return
    if (curKey === 'L') out.push({ kind: 'leader', key: 'L', segs })
    else out.push({ kind: 'group', key: 'g' + curKey, segs })
    segs = []
  }
  for (const b of blocks) {
    if (b.kind === 'user') {
      flushGroup()
      out.push({ kind: 'user', key: 'u' + b.uid, b })
      curKey = String(b.uid)
    } else if (b.kind === 'tool') {
      if (b.name === 'memory') {
        // 记忆变更卡常显：不折进工具组，避免 diff 被"N 个工具调用"盖住
        flushTools()
        segs.push({ type: 'one', b })
      } else {
        toolRun.push(b)
      }
    } else {
      flushTools()
      segs.push({ type: 'one', b })
    }
  }
  flushGroup()
  return out
}
