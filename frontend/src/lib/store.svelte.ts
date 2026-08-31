/* 全局状态机：bootstrap + SSE 事件归约（时间线块 / fork / 通知 / 生命体征），Svelte 5 runes。 */

import {
  api,
  subscribe,
  type BranchView,
  type DecisionRecord,
  type ForkSummary,
  type HistoryMessage,
  type ImagePayload,
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

/* ForkState 是分身聊天框的数据模型：blocks 与主时间线同构，
实时事件归约与存档回放共用 buildBlocks。owner=所属会话 ID（fork 存档
在所属库的 forks/ 下，compact 链上的旧库分身懒加载按 owner 取）。 */
export interface ForkState {
  id: string
  owner: string
  task: string
  status: 'running' | 'done'
  blocks: Block[]
  answer: string
  stopReason: string
  loaded: boolean
}

export interface DecisionData {
  id: string
  dtype: 'approve' | 'ask'
  name: string
  args: string
  question: string
  options: string[]
  forkId: string
  resolved: boolean
  resolution: string
}

export interface NoticeData {
  id: string
  kind: 'approve' | 'ask' | 'info'
  source: string // 'agent' 或 fork 标识
  forkId: string // 非空＝分身请求：跳转打开分身抽屉而非主时间线
  title: string
  detail: string
  time: string
  status: 'pending' | 'done'
  resolution: string
  target: string // 时间线跳转锚点（decision-<id>）
}

/* 消息锚点（分叉定位）：owner=消息所属 session ID（leaf 或上翻出的旧世代），
   msgIdx=该会话 messages 数组下标；分叉复制 [0, msgIdx]（含选中消息） */
export type Block = { uid: number } & (
  | { kind: 'user'; text: string; images?: ImagePayload[]; owner?: string; msgIdx?: number }
  | { kind: 'assistant'; text: string; reasoning: string; streaming: boolean; owner?: string; msgIdx?: number }
  | { kind: 'tool' } & ToolBlockData
  | { kind: 'fork'; forkId: string }
  | { kind: 'decision' } & DecisionData
  | { kind: 'note'; text: string }
  | { kind: 'status'; text: string; data: StatusPayload | null }
  | { kind: 'endtick'; icon: string; title: string }
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

/* 解析 <end_reason> 字段拼一行收尾文案——与 turn_end 实时收尾同款格式
   （历史回放与实时两条路径的 endtick 文案保持一致）；无字段的旧格式
   剔除系统提示语后原样压行 */
function endReasonText(content: string): string {
  const m = content.match(/<end_reason>([\s\S]*?)<\/end_reason>/)
  const body = m?.[1] ?? ''
  const get = (k: string) => body.match(new RegExp(`${k}：\\s*(.+)`))?.[1]?.trim() ?? ''
  const reason = get('结束原因')
  if (!reason) return body.replace(/（系统自动记录[^）]*）/g, '').trim().replace(/\s+/g, ' ')
  const iters = get('运行轮次')
  const dur = get('运行时长')
  const hm = get('结束时间').slice(11, 16) // YYYY-MM-DD HH:MM:SS → HH:MM
  const errd = get('错误详情')
  return `${reason}${errd ? `：${errd}` : ''} · ${iters || '?'} 轮${dur ? ` · ${dur}` : ''}${hm ? ` · ${hm}` : ''}`
}

/* 轮次收尾小图标（按结束原因语义选形，悬浮 title 显示详情） */
function endIcon(text: string): string {
  if (text.includes('正常') || text.includes('completed')) return '✓'
  if (text.includes('取消') || text.includes('手动停止') || text.includes('cancelled')) return '⏹'
  if (text.includes('迭代') || text.includes('max_iterations')) return '↻'
  if (text.includes('错误') || text.includes('中止') || text.includes('error')) return '⚠'
  return '·'
}

/* 提取 <context_trim> 摘要为一行（整理分割线文案） */
function trimText(content: string): string {
  const m = content.match(/摘要：\s*([\s\S]*?)<\/context_trim>/)
  const summary = (m?.[1] ?? '').trim().replace(/\s+/g, ' ')
  return `上下文已整理：此前的对话折叠为摘要。${summary}`
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
  /* activeId = 分支根 ID（稳定：compact 换代不变，SSE 订阅/路由键）；
     leafId = 当前叶 session ID（分身存档 owner、上翻游标起点） */
  activeId = $state('')
  leafId = $state('')
  branches = $state<BranchView[]>([])
  blocks = $state<Block[]>([])
  forks = $state<Record<string, ForkState>>({})
  notices = $state<NoticeData[]>([])
  lastTool = $state('')
  busy = $state(false)
  archivingRootId = $state('') // 归档进行中的分支（线根 ID，空=无）：锁该分支输入与按钮
  lastStatus = $state('')
  tick = $state(0)
  /* 分身抽屉：当前打开的分身与待定位的决策卡（通知跳转用） */
  activeForkId = $state('')
  jumpDecision = $state('')
  /* 魔法看板：开合/当前 tab/画板底图（编辑附件时为原 File）与待回流产物。
     boardSeq 在每次"从关到开"时递增（Panel 用 {#key} 重建画板=新画布）；
     pendingBoardFile 由 ChatView 消费进附件列表（tag 为编辑目标下标） */
  boardOpen = $state(false)
  boardTab = $state<'board' | 'browser' | 'terminal'>('board')
  boardSeq = $state(0)
  boardSource = $state<File | null>(null)
  pendingBoardFile = $state<{ file: File; tag: string; source: File | null } | null>(null)
  /* 看板浏览器:待打开的 URL(点消息链接 → 看板浏览器 tab 加载) */
  browserURL = $state('')
  private boardTag = ''
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
  /* term_* 工具的看板自动弹出:免审调用延迟 ~1s 打开(tool_start 先于
  approve.request 到达,1s 内无审批请求即视为免审直接执行);进入审批
  则等用户批准(decision.resolved=已批准)才打开——未批准时命令不会
  运行,提前弹板只是打扰。 */
  private termOpenTimers = new Map<string, ReturnType<typeof setTimeout>>()
  private termApprovals = new Map<string, string>()

  private nuid(): number {
    return ++this.uidSeq
  }

  /* ── 启动 ── */

  async bootstrap() {
    const b = await api.bootstrap()
    this.activeId = b.sessionId
    this.branches = b.branches ?? []
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

  /* 刷新分支列表（运行/等待指示：后台分支的事件不经当前 SSE，靠拉取） */
  async refreshBranches() {
    try {
      this.branches = await api.listTopics()
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
    this.activeForkId = ''
    this.busy = false
    this.lastStatus = ''
    // 分支切换：输出/工具指示与实时水位不跨分支（原分支的 turn_end
    // 已收不到——单 SSE 只订阅当前分支，残留标志会永远挂着）
    this.modelActive = false
    this.lastTool = ''
    this.live = null
    try {
      const s = await api.getHistory(this.activeId)
      this.leafId = s.id
      // 分身摘要重建（骨架，过程详情打开抽屉时懒加载）；存档挂在所属 session 目录下
      for (const f of s.forks ?? []) {
        this.forks[f.id] = {
          id: f.id,
          owner: s.id,
          task: f.task,
          status: 'done',
          blocks: [],
          answer: f.answer || '',
          stopReason: f.stopReason || '',
          loaded: false,
        }
      }
      this.blocks = this.buildBlocks(s.messages, s.decisions, s.forks ?? [], s.id)
      this.busy = s.busy
      this.prevCursor = s.id
      // fork 就是 fork：对话内容不标注来源（体内副本自包含）；
      // 来源信息只在记忆页的会话树上展示
      // 上翻余量由后端判定（fork 换源后算：源无上级则不可翻）
      this.hasPrev = !!s.canPrev
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
        // 旧库分身摘要建骨架（懒加载按所属库 ID 取详情）
        for (const fk of res.forks ?? []) {
          if (!this.forks[fk.id]) {
            this.forks[fk.id] = {
              id: fk.id,
              owner: res.id,
              task: fk.task,
              status: 'done',
              blocks: [],
              answer: fk.answer || '',
              stopReason: fk.stopReason || '',
              loaded: false,
            }
          }
        }
        const prevBlocks = this.buildBlocks(res.messages, undefined, res.forks ?? [], res.id)
        const sep: Block = {
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 上下文已压缩归档：${res.title || ''}`,
        }
        this.batchIds = new Set(prevBlocks.map((b) => b.uid))
        this.blocks = [...prevBlocks, sep, ...this.blocks]
        // 游标 = 已翻到的会话（下次取它的上一级）；可否继续由其上级是否存在决定
        this.prevCursor = res.id
        this.hasPrev = !!res.prevSession
      }
    } catch {
      this.hasPrev = false
    } finally {
      this.loadingPrev = false
    }
  }

  /* 历史重建：user/assistant/tool 消息序列，tool_calls 展开为工具块；决策记录映射为徽标。
     forks 摘要按 task 调用顺序插分身入口卡（forkID 升序与调用序一致）。
     owner=消息所属 session ID，与各消息下标配对供分叉定位。 */
  private buildBlocks(messages: HistoryMessage[], decisions?: DecisionRecord[], forks?: ForkSummary[], owner?: string): Block[] {
    const dmap = new Map((decisions || []).map((d) => [d.callId, d.resolution]))
    const forkQueue = [...(forks || [])]
    const out: Block[] = []
    for (let mi = 0; mi < messages.length; mi++) {
      const m = messages[mi]
      if (m.role === 'user') {
        const d = parseStatus(m.content) // 旧格式：JSON 载荷
        if (d) {
          // 状态记录仅异常时（推荐压缩/资源变更）入时间线，平时只在右上角
          if (d.suggestCompact || d.changes?.length) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: d })
          }
        } else if (m.content.includes('<agent_status>')) {
          // 新格式：中文语义化文本；同样仅异常行进时间线
          if (m.content.includes('建议整理') || m.content.includes('资源变更')) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: null })
          }
        } else if (m.content.includes('<end_reason>')) {
          const detail = endReasonText(m.content)
          out.push({ kind: 'endtick', uid: this.nuid(), icon: endIcon(detail), title: detail })
        } else if (m.content.includes('<context_trim')) {
          out.push({ kind: 'note', uid: this.nuid(), text: `✂️ ${trimText(m.content)}` })
        } else {
          out.push({ kind: 'user', uid: this.nuid(), text: m.content, images: m.images, owner, msgIdx: mi })
        }
      } else if (m.role === 'assistant') {
        if (m.content || m.reasoning) {
          out.push({
            kind: 'assistant',
            uid: this.nuid(),
            text: m.content,
            reasoning: m.reasoning || '',
            streaming: false,
            owner,
            msgIdx: mi,
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
          // task 调用紧随分身入口卡（真实启动的 fork 才有摘要：审批拒绝/失败的不插）
          if (tc.Name === 'task' && forkQueue.length > 0) {
            const f = forkQueue.shift()!
            out.push({ kind: 'fork', uid: this.nuid(), forkId: f.id })
          }
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

  /* ── 分身抽屉 ── */

  /* ensureFork 取分身状态，不存在则建骨架（SSE 重放/异常时序兜底）。 */
  private ensureFork(fid: string): ForkState {
    if (!this.forks[fid]) {
      this.forks[fid] = {
        id: fid,
        owner: this.leafId || this.activeId,
        task: '',
        status: 'running',
        blocks: [],
        answer: '',
        stopReason: '',
        loaded: false,
      }
    }
    return this.forks[fid]
  }

  /* openFork 打开/切换分身抽屉；decisionId 非空时打开后滚动定位到该决策卡，
     切换（无 decisionId）清掉旧跳转目标。 */
  openFork(fid: string, decisionId = '') {
    this.activeForkId = fid
    this.jumpDecision = decisionId
    void this.loadForkDetail(fid)
  }

  closeFork() {
    this.activeForkId = ''
  }

  /* ── 魔法看板 ── */

  /* openBoard 打开看板画板 tab；source 为编辑中的附件底图（tag 为其下标，
     回流时据此替换）。从关到开时递增 boardSeq（画板重建=新画布）。 */
  openBoard(source: File | null = null, tag = '') {
    if (!this.boardOpen) this.boardSeq++
    this.boardSource = source
    this.boardTag = tag
    this.boardTab = 'board'
    this.boardOpen = true
  }

  /* completeBoard 画板产物回流：交给 ChatView 消费（替换编辑目标或追加）。 */
  completeBoard(file: File) {
    this.pendingBoardFile = { file, tag: this.boardTag, source: this.boardSource }
    this.boardOpen = false
  }

  closeBoard() {
    this.boardOpen = false
  }

  toggleBoard() {
    if (this.boardOpen) this.closeBoard()
    else this.openBoard()
  }

  /* openInBrowser 在看板浏览器 tab 打开链接(共览场景入口)。 */
  openInBrowser(url: string) {
    this.browserURL = url
    this.boardOpen = true
    this.boardTab = 'browser'
  }

  /* openTermBoard 打开看板并切到终端 tab（AI term_* 实际执行时调用）。 */
  private openTermBoard() {
    this.boardOpen = true
    this.boardTab = 'terminal'
  }

  /* loadForkDetail 懒加载存档详情（已结束分身的执行记录重建）；运行中的
     走实时流。实时已累积过内容的分身不覆盖（避免 uid 全换导致折叠态重置）。 */
  private async loadForkDetail(fid: string) {
    const f = this.forks[fid]
    if (!f || f.loaded || f.status === 'running') return
    f.loaded = true
    try {
      const r = await api.getFork(f.owner || this.activeId, fid)
      if (f.blocks.length === 0) f.blocks = this.buildBlocks(r.messages, r.decisions)
    } catch {
      f.loaded = false // 失败可重试
    }
  }

  /* ── 发送 / 取消 ── */

  /* 打断式发送：运行中再来指令 = 先终止当前轮（等引擎真正退出，含工具树杀），
     再执行新指令；等待超时则放弃并提示。图片为可选多模态输入。 */
  async send(text: string, images?: ImagePayload[]) {
    if (!this.activeId || (!text.trim() && !images?.length)) return
    if (this.busy) {
      this.lastStatus = '正在终止当前轮…'
      await this.cancel()
      if (!(await this.waitIdle(8000))) {
        this.lastStatus = '当前轮未能及时终止，请稍后重试'
        return
      }
    }
    this.blocks.push({ kind: 'user', uid: this.nuid(), text, images })
    this.busy = true
    this.lastStatus = ''
    try {
      await api.send(this.activeId, text, images)
      void this.refreshBranches() // 首次发言落线索引 + 运行指示
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
      this.blocks.push({ kind: 'note', uid: this.nuid(), text: '⟲ 已切换到该分支' })
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `切换分支失败：${(e as Error).message}`
    }
  }

  /* ── 分支三操作 ── */

  /* 切换分支：状态/审批/水位随切换（SSE 重订阅时 ReplayFrames 重建
     时间线与未决审批；后台分支的轮不因切换取消） */
  async switchBranch(rootId: string) {
    if (rootId === this.activeId) return
    try {
      const r = await api.activateBranch(rootId)
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `切换分支失败：${(e as Error).message}`
    }
  }

  /* 开新线（New） */
  async newBranch() {
    try {
      const r = await api.newBranch()
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `新建分支失败：${(e as Error).message}`
    }
  }

  /* 归档换代（分支列表行操作）：指定分支总结归档开新篇，线不变叶子换代。
     期间可自由切换/新建分支对话——锁只作用于被归档分支自身 */
  async compactTopic(rootId?: string) {
    const id = rootId || this.activeId
    if (this.archivingRootId) return
    const active = id === this.activeId
    if (active && this.busy) {
      this.lastStatus = '会话运行中，稍后再归档'
      return
    }
    this.archivingRootId = id
    if (active) this.lastStatus = '正在归档话题…'
    try {
      await api.compactTopic(rootId)
      if (active) {
        await this.loadHistory()
        await this.refreshStatus()
      }
      await this.refreshBranches()
      if (active) this.lastStatus = ''
    } catch (e) {
      if (active) this.lastStatus = `归档失败：${(e as Error).message}`
    } finally {
      this.archivingRootId = ''
    }
  }

  /* 从任意消息分叉（Copy）：复制源会话 [0, msgIdx]（含选中消息）开新线 */
  async forkFrom(owner: string, msgIdx: number) {
    try {
      const r = await api.forkSession(owner, msgIdx + 1)
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `分叉失败：${(e as Error).message}`
    }
  }

  /* ── 决策回传（时间线卡 + 通知联动） ── */

  private resolveNotice(id: string, resolution: string) {
    const n = this.notices.find((x) => x.id === id)
    if (n && n.status === 'pending') {
      n.status = 'done'
      n.resolution = resolution
    }
    // 对应工具卡打决策徽标（与 decisions.jsonl 重建同源）；分身工具卡在分身块数组里
    for (const bs of [this.blocks, ...Object.values(this.forks).map((f) => f.blocks)]) {
      const t = bs.find((b) => b.kind === 'tool' && b.id === id)
      if (t && t.kind === 'tool') {
        t.decision = resolution
        break
      }
    }
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
    this.settleNotice(block.id)
    this.removeResolvedDecisions(block.id)
    await api.decideApprove(this.activeId, block.id, approve, reason).catch(() => {})
  }

  async decideAnswer(block: DecisionData, input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = input || '(未回答)'
    this.resolveNotice(block.id, block.resolution)
    this.settleNotice(block.id)
    this.removeResolvedDecisions(block.id)
    await api.decideAnswer(this.activeId, block.id, input).catch(() => {})
  }

  /* settleNotice 决策完成后立即移除通知条目（结果已在决策卡上可见，通知不留副本）。 */
  private settleNotice(id: string) {
    this.notices = this.notices.filter((x) => x.id !== id)
  }

  /* removeResolvedDecisions 已决决策卡整体移除：审批结果以工具卡徽标呈现，
     询问的回答即工具卡结果，时间线不留独立的决策卡副本。 */
  private removeResolvedDecisions(id: string) {
    const strip = (bs: Block[]) => {
      const i = bs.findIndex((b) => b.kind === 'decision' && b.id === id)
      if (i >= 0) bs.splice(i, 1)
    }
    strip(this.blocks)
    for (const f of Object.values(this.forks)) strip(f.blocks)
  }

  /* ── 事件归约 ── */

  apply(ev: SseEvent) {
    this.tick++
    switch (ev.type) {
      case 'loop_start': {
        // 回放重建：本轮 user 输入（实时路径 send 已本地 push，同文本去重）
        const text = typeof ev.data === 'string' ? ev.data : ''
        if (ev.forkId) {
          // 分身输入进分身聊天框（含任务包装前缀，即分身收到的原文）
          const f = this.ensureFork(ev.forkId)
          const last = f.blocks[f.blocks.length - 1]
          if (text && !(last && last.kind === 'user' && last.text === text)) {
            f.blocks.push({ kind: 'user', uid: this.nuid(), text })
          }
          break
        }
        const last = this.blocks[this.blocks.length - 1]
        if (text && !(last && last.kind === 'user' && last.text === text)) {
          this.blocks.push({ kind: 'user', uid: this.nuid(), text })
        }
        break
      }
      case 'decision.resolved': {
        // 回放纠正：已决决策卡直接移除（结果在工具卡上可见），通知与徽标同步
        const d = ev.data || {}
        if (d.id) {
          this.resolveNotice(d.id, d.resolution || '')
          this.removeResolvedDecisions(d.id)
        }
        // term_* 审批通过 → 现在才弹看板（拒绝则什么都不做）
        if (d.id && this.termApprovals.has(d.id)) {
          this.termApprovals.delete(d.id)
          if ((d.resolution || '').startsWith('已批准')) this.openTermBoard()
        }
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
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const t = bs.find((b) => b.kind === 'tool' && b.id === key && b.state === 'building')
        if (t && t.kind === 'tool') {
          if (d.nameDelta) t.name += d.nameDelta
          if (d.argsDelta) t.args += d.argsDelta
        } else {
          bs.push({
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
        break
      }
      case 'model_chunk':
      case 'reasoning_chunk': {
        const delta: string = typeof ev.data === 'string' ? ev.data : ''
        if (!delta) break
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        this.appendDelta(bs, delta, ev.type === 'model_chunk')
        break
      }
      case 'model_end': {
        this.modelActive = false
        // 关闭本次调用所属块流的流式态（fork 关自己的）：否则下一轮正文
        // 会追加进工具调用前的旧流式块（思考直调工具时正文顺序错乱）
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const last = this.lastStreaming(bs)
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
        if (!ev.forkId) {
          const u = ev.data?.usage
          if (u && this.status) {
            this.status.contextTokens = u.PromptTokens || 0
          }
        }
        break
      }
      case 'tool_start': {
        const d = ev.data || {}
        const args = typeof d.args === 'string' ? d.args : JSON.stringify(d.args ?? '')
        // 认领流式构造期（building）的同名块：换真实 callID、完整 args、转执行态
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        // 去重：决策路径已补插过同 id 工具卡时只补名参，不再push（防重复块乱序）
        const dup = bs.find((b) => b.kind === 'tool' && b.id === d.id && b.state !== 'building')
        if (dup && dup.kind === 'tool') {
          if (!dup.name) dup.name = d.name || ''
          if (!dup.args) dup.args = args
          break
        }
        const t = bs.find((b) => b.kind === 'tool' && b.state === 'building' && b.name === d.name)
        if (t && t.kind === 'tool') {
          t.id = d.id || ''
          t.args = args
          t.state = 'running'
        } else {
          bs.push({
            kind: 'tool', uid: this.nuid(), id: d.id || '', name: d.name || '',
            args, result: '', err: '', state: 'running',
          })
        }
        if (!ev.forkId) this.lastTool = d.name || ''
        // AI 用共享终端工具:延迟弹看板(见 termOpenTimers 注释——审批路径
        // 由 approve.request 取消计时,批准后才弹)
        if ((d.name || '').startsWith('term_') && d.id && !this.termApprovals.has(d.id)) {
          const id = d.id
          this.termOpenTimers.get(id) && clearTimeout(this.termOpenTimers.get(id))
          this.termOpenTimers.set(
            id,
            setTimeout(() => {
              this.termOpenTimers.delete(id)
              this.openTermBoard()
            }, 1000),
          )
        }
        break
      }
      case 'tool_end': {
        const d = ev.data || {}
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const t = bs.find((b) => b.kind === 'tool' && b.id === d.callId)
        if (t && t.kind === 'tool') {
          t.result = d.content || ''
          t.err = d.err || ''
          t.state = 'done'
        }
        if (!ev.forkId) this.lastTool = ''
        break
      }
      case 'task.start': {
        const d = ev.data || {}
        const fid = d.id || ev.forkId || ''
        const f = this.ensureFork(fid)
        f.task = d.task || ''
        f.status = 'running'
        // 入口卡紧跟对应的 task 工具卡（callId 精确匹配，回退最后一张 task 卡）
        let ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.id === d.callId)
        if (ti < 0) ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.name === 'task')
        const card: Block = { kind: 'fork', uid: this.nuid(), forkId: fid }
        if (ti >= 0) this.blocks.splice(ti + 1, 0, card)
        else this.blocks.push(card)
        break
      }
      case 'task.end': {
        const fid = ev.forkId || ev.data?.id || ''
        const f = this.forks[fid]
        if (f) {
          f.status = 'done'
          f.answer = ev.data?.answer || ''
          f.stopReason = ev.data?.stopReason || ''
        }
        break
      }
      case 'approve.request':
      case 'askuser.request': {
        const d = ev.data || {}
        const id = d.id || ''
        // term_* 进入审批：取消免审弹板计时，等批准后再弹
        if (ev.type === 'approve.request' && this.termOpenTimers.has(id)) {
          clearTimeout(this.termOpenTimers.get(id))
          this.termOpenTimers.delete(id)
          this.termApprovals.set(id, d.name || '')
        }
        // 分身请求路由进分身聊天框（不进主时间线）；bs=目标块数组
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        // 去重：SSE 断线重连会重放 pending 帧
        if (!id || bs.some((b) => b.kind === 'decision' && b.id === id)) break
        let args = d.args
        if (typeof args !== 'string') args = JSON.stringify(args ?? {})
        // 工具卡补插：刷新/重放时本轮快照未含此调用（turn 未落盘），
        // 决策卡之前补一个执行中的工具卡
        if (!bs.some((b) => b.kind === 'tool' && b.id === id)) {
          bs.push({
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
        let options: string[] = []
        try {
          const a = typeof args === 'string' && args ? JSON.parse(args) : {}
          question = a.question || ''
          if (Array.isArray(a.options)) options = a.options.filter((o: unknown) => typeof o === 'string')
        } catch {
          /* 非法 JSON 忽略 */
        }
        const dtype: DecisionData['dtype'] = ev.type === 'approve.request' ? 'approve' : 'ask'
        const card: Block = {
          kind: 'decision',
          uid: this.nuid(),
          id,
          dtype,
          name: d.name || '',
          args: typeof args === 'string' ? args : '',
          question,
          options,
          forkId: ev.forkId || '',
          resolved: false,
          resolution: '',
        }
        // 决策卡紧跟对应工具卡成组展示；无对应工具卡时兜底追加末尾
        const ti = bs.findIndex((b) => b.kind === 'tool' && b.id === id)
        if (ti >= 0) bs.splice(ti + 1, 0, card)
        else bs.push(card)
        // 通知栏同步：fork 内请求带 fork 标识，跳转打开分身抽屉定位决策卡；最新在最前
        this.notices.unshift({
          id,
          kind: dtype,
          source: ev.forkId || 'agent',
          forkId: ev.forkId || '',
          title: d.name || '',
          detail: question || '',
          time: nowHM(),
          status: 'pending',
          resolution: '',
          target: `decision-${id}`,
        })
        break
      }
      case 'error': {
        const msg = typeof ev.data === 'string' ? ev.data : JSON.stringify(ev.data ?? '')
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        bs.push({ kind: 'assistant', uid: this.nuid(), text: `⚠️ ${msg}`, reasoning: '', streaming: false })
        break
      }
      case 'status.snapshot': {
        // 分身状态快照不入主时间线、不碰主水位（分身上下文与主循环无关）
        if (ev.forkId) break
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
      case 'session.compacting': {
        // 水位自动压缩开始（无工具卡可见）：时间线提示压缩进行中
        const msg = typeof ev.data === 'string' ? ev.data : '上下文正在压缩归档…'
        this.blocks.push({ kind: 'note', uid: this.nuid(), text: `⇳ ${msg}` })
        break
      }
      case 'session.trimming': {
        // 水位自动整理开始：时间线提示整理进行中
        const msg = typeof ev.data === 'string' ? ev.data : '上下文正在整理…'
        this.blocks.push({ kind: 'note', uid: this.nuid(), text: `⇳ ${msg}` })
        break
      }
      case 'session.trim': {
        // 整理完成：上下文已就地折叠（marker 已入历史，重建时间线时渲染分割线）
        this.blocks.push({
          kind: 'note',
          uid: this.nuid(),
          text: `✂️ 上下文已整理：早期对话折叠为摘要（${ev.data?.folded ?? '?'} 条 → 保留最近 ${ev.data?.kept ?? '?'} 条）`,
        })
        void this.refreshStatus()
        break
      }
      case 'session.compact': {
        // 归档换代：根 ID 不变（SSE/路由稳定），只换叶与上翻游标；
        // 分支列表刷新（LeafID 更新，条目数不变）
        this.live = null // 旧会话水位快照作废，状态卡按刷新后的 status 渲染
        const d = ev.data || {}
        this.blocks.push({
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 话题已归档：${d.title || ''}`,
        })
        if (d.newId) {
          this.leafId = d.newId
          this.prevCursor = d.newId
          this.hasPrev = !!d.prevPath // 新叶可继续向上翻旧世代
        }
        void this.refreshStatus()
        void this.refreshBranches()
        break
      }
      case 'turn_end': {
        this.busy = false
        this.modelActive = false
        this.lastTool = ''
        // 兜底收尾：取消路径引擎不发 model_end，流式块的打字光标须在此收掉；
        // 残留 building 工具块（模型输出了调用但引擎未执行）同样标记完成
        for (const bs of [this.blocks, ...Object.values(this.forks).map((f) => f.blocks)]) {
          for (const b of bs) {
            if (b.kind === 'tool' && b.state === 'building') b.state = 'done'
            if (b.kind === 'assistant' && b.streaming) b.streaming = false
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
        void this.refreshBranches() // 运行指示熄灭（后台分支靠拉取）
        // 每轮收尾：小图标实时入时间线（悬浮显示详情；持久化正文由后端 endnote 写入历史）
        {
          const secs = d.elapsedMs ? Math.round(d.elapsedMs / 1000) : 0
          const reason = stopNote(d.stopReason || 'completed')
          // 错误详情跟在原因后（endtick 超宽截断、悬浮看全文）；取消路径
          // 的 err 是 context.Canceled，无信息量不拼
          const errTxt =
            d.err && (d.stopReason || 'error') === 'error'
              ? `：${String(d.err).replace(/\s+/g, ' ').slice(0, 300)}`
              : ''
          const title =
            `${reason}${errTxt} · ${d.iterations ?? 0} 轮${secs ? ` · ${fmtDur(secs)}` : ''}` +
            ` · ${new Date().toTimeString().slice(0, 5)}`
          this.blocks.push({
            kind: 'endtick',
            uid: this.nuid(),
            icon: endIcon(reason),
            title,
          })
        }
        void this.refreshStatus()
        break
      }
    }
  }

  /* appendDelta 流式文本追加到块数组（主时间线与分身聊天框共用）。
     正文与工具同响应乱序兜底：部分 provider 按"思考→工具调用→正文"
     的顺序发增量，正文若追进旧思考块会渲染在其后工具行的上方——
     候选块之下已有本轮工具/决策行时，正文另起新块插到末尾。 */
  private appendDelta(bs: Block[], delta: string, isContent: boolean) {
    let idx = -1
    for (let i = bs.length - 1; i >= 0; i--) {
      const b = bs[i]
      if (b.kind === 'assistant') {
        if (!b.streaming) break
        idx = i
        break
      }
      if (b.kind === 'user') break
    }
    let staleGap = false
    if (idx >= 0 && isContent) {
      for (let i = idx + 1; i < bs.length; i++) {
        const k = bs[i].kind
        if (k === 'tool' || k === 'decision') {
          staleGap = true
          break
        }
        if (k === 'assistant') break
      }
    }
    if (idx < 0 || staleGap) {
      bs.push({ kind: 'assistant', uid: this.nuid(), text: '', reasoning: '', streaming: true })
      idx = bs.length - 1
    }
    const b = bs[idx] as Extract<Block, { kind: 'assistant' }>
    if (isContent) b.text += delta
    else b.reasoning += delta
  }

  private lastStreamingAssistant(): Extract<Block, { kind: 'assistant' }> | null {
    return this.lastStreaming(this.blocks)
  }

  private lastStreaming(bs: Block[]): Extract<Block, { kind: 'assistant' }> | null {
    for (let i = bs.length - 1; i >= 0; i--) {
      const b = bs[i]
      if (b.kind === 'assistant') {
        return b.streaming ? b : null
      }
      if (b.kind === 'user') return null
    }
    return null
  }
}

export const store = new AppStore()
