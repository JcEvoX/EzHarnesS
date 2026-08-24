/* 全局状态机：bootstrap + SSE 事件归约（时间线块 / fork / 通知 / 生命体征），Svelte 5 runes。 */

import {
  api,
  subscribe,
  type DecisionRecord,
  type HistoryMessage,
  type Settings,
  type SseEvent,
  type Status,
  type StatusPayload,
} from './api'

export interface ToolBlockData {
  id: string
  name: string
  args: string
  result: string
  err: string
  state: 'building' | 'running' | 'done' // building＝模型流式构造参数中
  decision?: string // 人机决策徽标（已批准/已拒绝…，刷新后由 decisions.jsonl 重建）
}

export interface ForkState {
  id: string
  task: string
  status: 'running' | 'done'
  text: string
  reasoning: string
  tools: ToolBlockData[]
  answer: string
  stopReason: string
  collapsed: boolean
}

export interface DecisionData {
  id: string
  dtype: 'approve' | 'ask' | 'plan'
  name: string
  args: string
  question: string
  plan: string
  forkId: string
  resolved: boolean
  resolution: string
}

export interface NoticeData {
  id: string
  kind: 'approve' | 'ask' | 'plan' | 'info'
  source: string // 'agent' 或 fork 标识
  title: string
  detail: string
  time: string
  status: 'pending' | 'done'
  resolution: string
  target: string // 时间线跳转锚点（decision-<id>）
}

export type Block = { uid: number } & (
  | { kind: 'user'; text: string }
  | { kind: 'assistant'; text: string; reasoning: string; streaming: boolean }
  | { kind: 'tool' } & ToolBlockData
  | { kind: 'fork'; forkId: string }
  | { kind: 'decision' } & DecisionData
  | { kind: 'note'; text: string }
  | { kind: 'status'; text: string; data: StatusPayload | null }
)

export interface TotalUsage {
  prompt: number
  completion: number
  cached: number
}

function nowHM(): string {
  return new Date().toTimeString().slice(0, 5)
}

/* 解析 <agent_status> 载荷（旧格式 JSON；新格式中文文本返回 null） */
function parseStatus(content: string): StatusPayload | null {
  const open = '<agent_status>'
  const close = '</agent_status>'
  const i = content.indexOf(open)
  if (i < 0) return null
  const j = content.indexOf(close, i)
  if (j < 0) return null
  try {
    return JSON.parse(content.slice(i + open.length, j).trim())
  } catch {
    return null
  }
}

/* 提取 <end_reason> 正文为一行 */
function endReasonText(content: string): string {
  const m = content.match(/<end_reason>([\s\S]*?)<\/end_reason>/)
  return (m?.[1] ?? '').trim().replace(/\s+/g, ' ')
}

/* 终止原因文案（completed 由调用方排除，不产生提示） */
function stopNote(reason: string): string {
  switch (reason) {
    case 'cancelled':
      return '用户手动停止本轮'
    case 'max_iterations':
      return '达到最大迭代次数上限'
    case 'error':
      return '执行出错中止'
    case 'aborted':
      return '被策略中止'
    default:
      return `本轮结束（${reason}）`
  }
}

function fmtDur(totalSecs: number): string {
  if (totalSecs < 60) return `${totalSecs} 秒`
  if (totalSecs < 3600) return `${Math.floor(totalSecs / 60)} 分 ${totalSecs % 60} 秒`
  return `${Math.floor(totalSecs / 3600)} 小时 ${Math.floor((totalSecs % 3600) / 60)} 分钟`
}

class AppStore {
  activeId = $state('')
  blocks = $state<Block[]>([])
  forks = $state<Record<string, ForkState>>({})
  notices = $state<NoticeData[]>([])
  lastTool = $state('')
  busy = $state(false)
  lastStatus = $state('')
  tick = $state(0)
  /* 模型调用进行中（model_start→model_end），思考指示用 */
  modelActive = $state(false)

  status = $state<Status | null>(null)
  /* 最新 agent_status 快照（status.snapshot 事件实时更新，右上角水位条数据源） */
  live = $state<StatusPayload | null>(null)
  settings = $state<Settings | null>(null)
  total = $state<TotalUsage>({ prompt: 0, completion: 0, cached: 0 })

  /* 懒加载：compact 链上是否还有旧会话可翻、是否正在加载 */
  hasPrev = $state(false)
  loadingPrev = $state(false)
  /* 最近一次下拉拉出的块 uid 集合（展开动画用，渲染层命中加 class） */
  batchIds = $state<Set<number>>(new Set())
  private prevCursor = '' // 已翻到的会话 ID（沿 prevSession 链继续上翻）

  private unsub: (() => void) | null = null
  private uidSeq = 0

  private nuid(): number {
    return ++this.uidSeq
  }

  /* ── 启动 ── */

  async bootstrap() {
    const b = await api.bootstrap()
    this.activeId = b.sessionId
    this.settings = b.settings
    this.status = b.status
    await this.loadHistory()
    this.unsub?.()
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
  }

  async refreshStatus() {
    try {
      this.status = await api.status()
    } catch {
      /* 静默 */
    }
  }

  private resubscribe() {
    this.unsub?.()
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
  }

  private async loadHistory() {
    this.blocks = []
    this.forks = {}
    this.notices = []
    this.busy = false
    this.lastStatus = ''
    try {
      const s = await api.getHistory(this.activeId)
      this.blocks = this.buildBlocks(s.messages, s.decisions)
      this.busy = s.busy
      this.prevCursor = this.activeId
      this.hasPrev = !!s.prevSession
    } catch {
      /* 网络异常时保底空时间线 */
    }
  }

  /* 下拉懒加载：沿 compact 链拉出上一会话，一次一个；底部带压缩归档标记 */
  async loadPrev() {
    if (!this.prevCursor || this.loadingPrev) return
    this.loadingPrev = true
    try {
      const res = await api.getPrev(this.prevCursor)
      if (!res) {
        this.hasPrev = false
      } else {
        const prevBlocks = this.buildBlocks(res.messages)
        const sep: Block = {
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 上下文已压缩归档：${res.title || ''}`,
        }
        this.batchIds = new Set(prevBlocks.map((b) => b.uid))
        this.blocks = [...prevBlocks, sep, ...this.blocks]
        this.prevCursor = res.prevSession || ''
        this.hasPrev = !!res.prevSession
      }
    } catch {
      this.hasPrev = false
    } finally {
      this.loadingPrev = false
    }
  }

  /* 历史重建：user/assistant/tool 消息序列，tool_calls 展开为工具块；决策记录映射为徽标 */
  private buildBlocks(messages: HistoryMessage[], decisions?: DecisionRecord[]): Block[] {
    const dmap = new Map((decisions || []).map((d) => [d.callId, d.resolution]))
    const out: Block[] = []
    for (const m of messages) {
      if (m.role === 'user') {
        const d = parseStatus(m.content) // 旧格式：JSON 载荷
        if (d) {
          // 状态记录仅异常时（推荐压缩/资源变更）入时间线，平时只在右上角
          if (d.suggestCompact || d.changes?.length) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: d })
          }
        } else if (m.content.includes('<agent_status>')) {
          // 新格式：中文语义化文本；同样仅异常行进时间线
          if (m.content.includes('建议压缩') || m.content.includes('资源变更')) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: null })
          }
        } else if (m.content.includes('<end_reason>')) {
          out.push({ kind: 'note', uid: this.nuid(), text: `⏹ ${endReasonText(m.content)}` })
        } else {
          out.push({ kind: 'user', uid: this.nuid(), text: m.content })
        }
      } else if (m.role === 'assistant') {
        if (m.content || m.reasoning) {
          out.push({
            kind: 'assistant',
            uid: this.nuid(),
            text: m.content,
            reasoning: m.reasoning || '',
            streaming: false,
          })
        }
        // 展开工具调用：名称与参数来自 tool_calls（Args 序列化后是嵌套对象，非字符串）
        for (const tc of m.tool_calls || []) {
          out.push({
            kind: 'tool',
            uid: this.nuid(),
            id: tc.ID || '',
            name: tc.Name || '',
            args: typeof tc.Args === 'string' ? tc.Args : JSON.stringify(tc.Args ?? ''),
            result: '',
            err: '',
            state: 'done',
            decision: dmap.get(tc.ID || ''),
          })
        }
      } else if (m.role === 'tool') {
        // 按调用 ID 回填结果到对应工具块
        const target = [...out].reverse().find((b) => b.kind === 'tool' && b.id === m.tool_call_id)
        if (target && target.kind === 'tool') {
          target.result = m.content || ''
          target.err = m.err || ''
        } else {
          out.push({
            kind: 'tool',
            uid: this.nuid(),
            id: m.tool_call_id || '',
            name: '',
            args: '',
            result: m.content || '',
            err: m.err || '',
            state: 'done',
          })
        }
      }
    }
    return out
  }

  /* ── 发送 / 取消 ── */

  /* 打断式发送：运行中再来指令 = 先终止当前轮（等引擎真正退出，含工具树杀），
     再执行新指令；等待超时则放弃并提示。 */
  async send(text: string) {
    if (!this.activeId || !text.trim()) return
    if (this.busy) {
      this.lastStatus = '正在终止当前轮…'
      await this.cancel()
      if (!(await this.waitIdle(8000))) {
        this.lastStatus = '当前轮未能及时终止，请稍后重试'
        return
      }
    }
    this.blocks.push({ kind: 'user', uid: this.nuid(), text })
    this.busy = true
    this.lastStatus = ''
    try {
      await api.send(this.activeId, text)
    } catch (e) {
      this.busy = false
      this.lastStatus = `发送失败：${(e as Error).message}`
    }
  }

  /* 轮询等待轮结束（turn_end 置 busy=false）；超时返回 false。 */
  private waitIdle(timeoutMs: number): Promise<boolean> {
    return new Promise((resolve) => {
      const t0 = Date.now()
      const timer = setInterval(() => {
        if (!this.busy) {
          clearInterval(timer)
          resolve(true)
        } else if (Date.now() - t0 > timeoutMs) {
          clearInterval(timer)
          resolve(false)
        }
      }, 100)
    })
  }

  async cancel() {
    if (!this.activeId) return
    await api.cancel(this.activeId).catch(() => {})
  }

  async summarize() {
    if (!this.activeId) return
    this.lastStatus = '摘要中…'
    try {
      const { text } = await api.summarize(this.activeId)
      this.blocks.push({ kind: 'assistant', uid: this.nuid(), text: `📝 ${text}`, reasoning: '', streaming: false })
    } catch (e) {
      this.lastStatus = `摘要失败：${(e as Error).message}`
    }
  }

  /* ── 设置 / 话题 ── */

  async saveSettings(s: Settings) {
    await api.saveSettings(s)
    this.settings = s
    this.lastStatus = '设置已保存（模型与提示即时生效）'
    await this.refreshStatus()
  }

  async resumeTopic(id: string) {
    try {
      const r = await api.resumeTopic(id)
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      this.blocks.push({ kind: 'note', uid: this.nuid(), text: '⟲ 已回到该话题继续' })
      await this.refreshStatus()
    } catch (e) {
      this.lastStatus = `回到话题失败：${(e as Error).message}`
    }
  }

  /* ── 决策回传（时间线卡 + 通知联动） ── */

  private resolveNotice(id: string, resolution: string) {
    const n = this.notices.find((x) => x.id === id)
    if (n && n.status === 'pending') {
      n.status = 'done'
      n.resolution = resolution
    }
    // 对应工具卡打决策徽标（与 decisions.jsonl 重建同源）
    const t = this.blocks.find((b) => b.kind === 'tool' && b.id === id)
    if (t && t.kind === 'tool') t.decision = resolution
  }

  /* 关闭通知：仅已处理/过期可关，pending 保留待处理 */
  dismissNotice(id: string) {
    const n = this.notices.find((x) => x.id === id)
    if (n && n.status === 'done') {
      this.notices = this.notices.filter((x) => x.id !== id)
    }
  }

  async decideApprove(block: DecisionData, approve: boolean, reason: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = approve ? '已批准' : reason ? `已拒绝：${reason}` : '已拒绝'
    this.resolveNotice(block.id, block.resolution)
    await api.decideApprove(this.activeId, block.id, approve, reason).catch(() => {})
  }

  async decideAnswer(block: DecisionData, input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = input || '(未回答)'
    this.resolveNotice(block.id, block.resolution)
    await api.decideAnswer(this.activeId, block.id, input).catch(() => {})
  }

  async decidePlan(block: DecisionData, kind: 'execute' | 'reject' | 'revise', input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution =
      kind === 'execute' ? '已执行' : kind === 'reject' ? '已否决' : `修改意见：${input}`
    this.resolveNotice(block.id, block.resolution)
    await api.decidePlan(this.activeId, block.id, kind, input).catch(() => {})
  }

  /* ── 事件归约 ── */

  apply(ev: SseEvent) {
    this.tick++
    switch (ev.type) {
      case 'loop_start': {
        // 回放重建：本轮 user 输入（实时路径 send 已本地 push，同文本去重）
        const text = typeof ev.data === 'string' ? ev.data : ''
        const last = this.blocks[this.blocks.length - 1]
        if (text && !(last && last.kind === 'user' && last.text === text)) {
          this.blocks.push({ kind: 'user', uid: this.nuid(), text })
        }
        break
      }
      case 'decision.resolved': {
        // 回放纠正：已决审批的决策卡与工具卡徽标（实时路径本地已处理，幂等）
        const d = ev.data || {}
        const b = this.blocks.find((x) => x.kind === 'decision' && x.id === d.id)
        if (b && b.kind === 'decision' && !b.resolved) {
          b.resolved = true
          b.resolution = d.resolution || ''
        }
        if (d.id) this.resolveNotice(d.id, d.resolution || '')
        break
      }
      case 'model_start': {
        this.modelActive = true
        break
      }
      case 'tool_chunk': {
        // 流式工具调用增量：按 index 分桶累积成 building 态工具块
        const d = ev.data || {}
        const key = `b-${ev.forkId || 'm'}-${d.index ?? 0}`
        if (ev.forkId) {
          const f = this.forks[ev.forkId]
          if (!f) break
          let t = f.tools.find((b) => b.id === key && b.state === 'building')
          if (!t) {
            t = { id: key, name: '', args: '', result: '', err: '', state: 'building' }
            f.tools.push(t)
          }
          if (d.nameDelta) t.name += d.nameDelta
          if (d.argsDelta) t.args += d.argsDelta
        } else {
          const t = this.blocks.find(
            (b) => b.kind === 'tool' && b.id === key && b.state === 'building',
          )
          if (t && t.kind === 'tool') {
            if (d.nameDelta) t.name += d.nameDelta
            if (d.argsDelta) t.args += d.argsDelta
          } else {
            this.blocks.push({
              kind: 'tool',
              uid: this.nuid(),
              id: key,
              name: d.nameDelta || '',
              args: d.argsDelta || '',
              result: '',
              err: '',
              state: 'building',
            })
          }
        }
        break
      }
      case 'model_chunk':
      case 'reasoning_chunk': {
        const delta: string = typeof ev.data === 'string' ? ev.data : ''
        if (!delta) break
        if (ev.forkId) {
          const f = this.forks[ev.forkId]
          if (f) {
            if (ev.type === 'model_chunk') f.text += delta
            else f.reasoning += delta
          }
        } else {
          this.appendMain(delta, ev.type === 'model_chunk')
        }
        break
      }
      case 'model_end': {
        this.modelActive = false
        const last = this.lastStreamingAssistant()
        if (last) {
          last.streaming = false
        } else if (!ev.forkId && (ev.data?.content || ev.data?.reasoning)) {
          // 回放重建：无流式块时按聚合帧补完整回复（实时路径 chunk 已建块）
          this.blocks.push({
            kind: 'assistant',
            uid: this.nuid(),
            text: ev.data.content || '',
            reasoning: ev.data.reasoning || '',
            streaming: false,
          })
        }
        const u = ev.data?.usage
        if (u && this.status) {
          this.status.contextTokens = u.PromptTokens || 0
        }
        break
      }
      case 'tool_start': {
        const d = ev.data || {}
        const args = typeof d.args === 'string' ? d.args : JSON.stringify(d.args ?? '')
        // 认领流式构造期（building）的同名块：换真实 callID、完整 args、转执行态
        if (ev.forkId) {
          const f = this.forks[ev.forkId]
          const t = f?.tools.find((b) => b.state === 'building' && b.name === d.name)
          if (f && t) {
            t.id = d.id || ''
            t.args = args
            t.state = 'running'
          } else if (f) {
            f.tools.push({
              id: d.id || '', name: d.name || '', args, result: '', err: '', state: 'running',
            })
          }
        } else {
          const t = this.blocks.find(
            (b) => b.kind === 'tool' && b.state === 'building' && b.name === d.name,
          )
          if (t && t.kind === 'tool') {
            t.id = d.id || ''
            t.args = args
            t.state = 'running'
          } else {
            this.blocks.push({
              kind: 'tool', uid: this.nuid(), id: d.id || '', name: d.name || '',
              args, result: '', err: '', state: 'running',
            })
          }
        }
        if (!ev.forkId) this.lastTool = d.name || ''
        break
      }
      case 'tool_end': {
        const d = ev.data || {}
        if (ev.forkId) {
          const t = this.forks[ev.forkId]?.tools.find((x) => x.id === d.callId)
          if (t) {
            t.result = d.content || ''
            t.err = d.err || ''
            t.state = 'done'
          }
        } else {
          const t = this.blocks.find((b) => b.kind === 'tool' && b.id === d.callId)
          if (t && t.kind === 'tool') {
            t.result = d.content || ''
            t.err = d.err || ''
            t.state = 'done'
          }
        }
        if (!ev.forkId) this.lastTool = ''
        break
      }
      case 'task.start': {
        const d = ev.data || {}
        const fid = d.id || ev.forkId || ''
        this.forks[fid] = {
          id: fid,
          task: d.task || '',
          status: 'running',
          text: '',
          reasoning: '',
          tools: [],
          answer: '',
          stopReason: '',
          collapsed: false,
        }
        this.blocks.push({ kind: 'fork', uid: this.nuid(), forkId: fid })
        break
      }
      case 'task.end': {
        const fid = ev.forkId || ev.data?.id || ''
        const f = this.forks[fid]
        if (f) {
          f.status = 'done'
          f.answer = ev.data?.answer || ''
          f.stopReason = ev.data?.stopReason || ''
          f.collapsed = true
        }
        break
      }
      case 'approve.request':
      case 'askuser.request':
      case 'taskplan.request': {
        const d = ev.data || {}
        const id = d.id || ''
        // 去重：SSE 断线重连会重放 pending 帧
        if (!id || this.blocks.some((b) => b.kind === 'decision' && b.id === id)) break
        let args = d.args
        if (typeof args !== 'string') args = JSON.stringify(args ?? {})
        // 工具卡补插：刷新/重放时本轮快照未含此调用（turn 未落盘），
        // 决策卡之前补一个执行中的工具卡
        if (!ev.forkId && !this.blocks.some((b) => b.kind === 'tool' && b.id === id)) {
          this.blocks.push({
            kind: 'tool',
            uid: this.nuid(),
            id,
            name: d.name || '',
            args,
            result: '',
            err: '',
            state: 'running',
          })
        }
        let question = ''
        let plan = ''
        try {
          const a = typeof args === 'string' && args ? JSON.parse(args) : {}
          question = a.question || ''
          plan = a.plan || ''
        } catch {
          /* 非法 JSON 忽略 */
        }
        const dtype: DecisionData['dtype'] =
          ev.type === 'approve.request' ? 'approve' : ev.type === 'askuser.request' ? 'ask' : 'plan'
        const card: Block = {
          kind: 'decision',
          uid: this.nuid(),
          id,
          dtype,
          name: d.name || '',
          args: typeof args === 'string' ? args : '',
          question,
          plan,
          forkId: ev.forkId || '',
          resolved: false,
          resolution: '',
        }
        // 决策卡紧跟对应工具卡成组展示；无对应工具卡（fork 内等）时兜底追加末尾
        const ti = this.blocks.findIndex((b) => b.kind === 'tool' && b.id === id)
        if (ti >= 0) this.blocks.splice(ti + 1, 0, card)
        else this.blocks.push(card)
        // 通知栏同步：fork 内请求带 fork 标识，跳转锚点指向时间线决策卡；最新在最前
        this.notices.unshift({
          id,
          kind: dtype,
          source: ev.forkId || 'agent',
          title: d.name || '',
          detail: question || plan || '',
          time: nowHM(),
          status: 'pending',
          resolution: '',
          target: `decision-${id}`,
        })
        break
      }
      case 'error': {
        const msg = typeof ev.data === 'string' ? ev.data : JSON.stringify(ev.data ?? '')
        this.blocks.push({ kind: 'assistant', uid: this.nuid(), text: `⚠️ ${msg}`, reasoning: '', streaming: false })
        break
      }
      case 'status.snapshot': {
        const d = ev.data ?? null
        // 右上角实时同步：最新快照 + 上下文水位
        this.live = d
        if (d && this.status) this.status.contextTokens = d.ctxTokens || 0
        // 时间线仅异常时插块（send 已先本地 push user 块，插到它之前）
        if (d && (d.suggestCompact || d.changes?.length)) {
          const block: Block = { kind: 'status', uid: this.nuid(), text: '', data: d }
          let idx = -1
          for (let k = this.blocks.length - 1; k >= 0; k--) {
            if (this.blocks[k].kind === 'user') {
              idx = k
              break
            }
          }
          if (idx >= 0) this.blocks.splice(idx, 0, block)
          else this.blocks.push(block)
        }
        break
      }
      case 'session.compact': {
        // 压缩分隔线 + activeId 更新（SSE 绑 Session 对象无需重订阅，
        // 但 GET /api/sessions/:id 校验活动 ID，必须本地换新）
        this.live = null // 旧会话水位快照作废，状态卡按刷新后的 status 渲染
        const d = ev.data || {}
        this.blocks.push({
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 上下文已压缩归档：${d.title || ''}${d.auto ? '（自动）' : ''}`,
        })
        if (d.newId && d.newId !== this.activeId) {
          this.activeId = d.newId
          this.hasPrev = !!d.prevPath // 新会话可继续向上翻旧会话
        }
        void this.refreshStatus()
        break
      }
      case 'turn_end': {
        this.busy = false
        this.modelActive = false
        this.lastTool = ''
        // 兜底收尾：取消路径引擎不发 model_end，流式块的打字光标须在此收掉；
        // 残留 building 工具块（模型输出了调用但引擎未执行）同样标记完成
        for (const b of this.blocks) {
          if (b.kind === 'tool' && b.state === 'building') b.state = 'done'
          if (b.kind === 'assistant' && b.streaming) b.streaming = false
        }
        for (const f of Object.values(this.forks)) {
          for (const t of f.tools) {
            if (t.state === 'building') t.state = 'done'
          }
        }
        // 轮已结束：残留 pending 决策的回传会被后端丢弃，标记过期
        for (const n of this.notices) {
          if (n.status === 'pending') {
            n.status = 'done'
            n.resolution = '已过期'
          }
        }
        const d = ev.data || {}
        const u = d.usage
        if (u) {
          this.total = {
            prompt: this.total.prompt + (u.PromptTokens || 0),
            completion: this.total.completion + (u.CompletionTokens || 0),
            cached: this.total.cached + (u.CachedTokens || 0),
          }
        }
        this.lastStatus =
          `${d.stopReason || 'end'} · ${d.iterations ?? 0} 迭代` +
          (u ? ` · 本轮 ${u.PromptTokens}→${u.CompletionTokens} tokens（缓存 ${u.CachedTokens}）` : '')
        // 非正常终止：时间线补一条结束原因（持久化正文已由后端写入历史）
        if (d.stopReason && d.stopReason !== 'completed') {
          const secs = d.elapsedMs ? Math.round(d.elapsedMs / 1000) : 0
          this.blocks.push({
            kind: 'note',
            uid: this.nuid(),
            text: `⏹ ${stopNote(d.stopReason)}（${d.iterations ?? 0} 轮${secs ? ` · ${fmtDur(secs)}` : ''}）`,
          })
        }
        void this.refreshStatus()
        break
      }
    }
  }

  private appendMain(delta: string, isContent: boolean) {
    let last = this.lastStreamingAssistant()
    if (!last) {
      this.blocks.push({ kind: 'assistant', uid: this.nuid(), text: '', reasoning: '', streaming: true })
      last = this.blocks[this.blocks.length - 1] as Extract<Block, { kind: 'assistant' }>
    }
    if (isContent) last.text += delta
    else last.reasoning += delta
  }

  private lastStreamingAssistant(): Extract<Block, { kind: 'assistant' }> | null {
    for (let i = this.blocks.length - 1; i >= 0; i--) {
      const b = this.blocks[i]
      if (b.kind === 'assistant') {
        return b.streaming ? b : null
      }
      if (b.kind === 'user') return null
    }
    return null
  }
}

export const store = new AppStore()
