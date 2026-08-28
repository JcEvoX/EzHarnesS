/*
ChatService 编排对话用例：发送、取消、决策回传与事件流消费。
按分支路由（rootID=线根 ID，稳定不随 compact 换代）：发送/取消/决策
作用于所属分支；轮由 Send 启动的 goroutine 消费完毕——后台分支照常
跑完，事件进各自 turnFrames，切回即重放。
*/
package service

import (
	"context"
	"time"

	"github.com/xuanlv2002/ezloop/event"
	"github.com/xuanlv2002/ezloop/ext/hook/approve"
	"github.com/xuanlv2002/ezloop/ext/hook/askuser"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ChatService 对话用例。 */
type ChatService struct {
	Hub *domain.Hub
}

/* resolve 按线根 ID 定位分支（未知/为空回落活动分支，兼容过渡期）。 */
func (c *ChatService) resolve(rootID string) *domain.Session {
	if s := c.Hub.SessionOf(rootID); s != nil {
		return s
	}
	return c.Hub.Active
}

/* Send 启动一轮异步运行：事件流扇出 SSE，结束更新历史并发 turn_end。 */
func (c *ChatService) Send(rootID, text string) error {
	if main := c.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return domain.ErrNoAPIKey
	}
	s := c.resolve(rootID)
	h, cancel, err := s.StartRun(context.Background(), text)
	if err != nil {
		return err
	}
	c.ensureIndexed(s, text) // 首次发言落线索引（分支面板/重启恢复依据）

	go func() {
		started := time.Now()
		for ev := range h.Events() {
			if ev.Type == event.EventModelEnd {
				if r, ok := ev.Data.(*types.ModelResponse); ok {
					s.SetCtxTokens(r.Usage.PromptTokens)
				}
			}
			s.Publish(domain.MapEvent(ev))
		}
		state, waitErr := h.Wait()
		var usage *types.Usage
		stop, iters := "", 0
		if state != nil {
			usage = &state.Usage
			stop, iters = string(state.StopReason), state.Iteration
		}
		if stop == "" && waitErr != nil {
			stop = "error" // 与 TurnEnd 帧同口径
		}
		s.FinishRun(state, waitErr)
		cancel() // 释放 turnCtx（决策 select 的 Done 依赖）
		c.Hub.Stats.AddTurn(usage)
		c.Hub.RecordUsage(usage) // 主模型条目用量累计
		c.refreshLine(s)
		s.Publish(domain.TurnEnd(stop, iters, usage, waitErr, time.Since(started).Milliseconds()))
	}()
	return nil
}

/* Cancel 取消当前轮。 */
func (c *ChatService) Cancel(rootID string) { c.resolve(rootID).Cancel() }

/* DecideApprove 回传审批决策。 */
func (c *ChatService) DecideApprove(rootID, callID string, approve bool, reason string) {
	s := c.resolve(rootID)
	res := "已批准"
	if !approve {
		res = "已拒绝"
		if reason != "" {
			res = "已拒绝：" + reason
		}
	}
	c.recordDecision(s, "approve", callID, res)
	s.DecideApprove(approveDecision(callID, approve, reason))
}

/* DecideAnswer 回传提问回答。 */
func (c *ChatService) DecideAnswer(rootID, callID, input string) {
	s := c.resolve(rootID)
	c.recordDecision(s, "ask", callID, input)
	s.DecideAnswer(answerOf(callID, input))
}

/* recordDecision 持久化决策记录（轮末刷新后工具卡徽标用）并发
decision.resolved 帧（轮内刷新回放时纠正决策卡与徽标）。失败静默。 */
func (c *ChatService) recordDecision(s *domain.Session, kind, callID, resolution string) {
	if callID == "" {
		return
	}
	hooks.AppendDecision(context.Background(), s.Fsys, s.ID, hooks.DecisionRecord{
		CallID: callID, Kind: kind, Resolution: resolution, Ts: time.Now().UnixMilli(),
	})
	s.Publish(domain.Event{Type: "decision.resolved", Data: domain.Raw(
		map[string]string{"id": callID, "resolution": resolution})})
}

/*
ensureIndexed 首次发言时落线索引（NewBranch 延迟建索引，防空线粉尘）。
fork/迁移线创建时已索引，此处只兜 new 线。
*/
func (c *ChatService) ensureIndexed(s *domain.Session, firstText string) {
	if s.RootID == "" {
		return
	}
	if _, ok := c.Hub.Topics.Get(s.RootID); ok {
		return
	}
	now := time.Now().UnixMilli()
	title := hooks.FirstUserTitle([]types.Message{{Role: types.RoleUser, Content: firstText}})
	_ = c.Hub.Topics.Add(hooks.TopicEntry{
		ID: s.RootID, LeafID: s.ID, Title: title, Kind: "new",
		CreatedAt: now, UpdatedAt: now, Msgs: len(s.History()),
	})
}

/* refreshLine 轮末刷新线（叶子/活动时间/规模；fork 线出现自己的首条
新增 user 消息后标题切换）。 */
func (c *ChatService) refreshLine(s *domain.Session) {
	if s.RootID == "" {
		return
	}
	entry, ok := c.Hub.Topics.Get(s.RootID)
	if !ok {
		return
	}
	msgs := s.History()
	if entry.Kind == "fork" && entry.Origin != nil && len(msgs) > entry.Origin.Anchor {
		if t2 := hooks.FirstUserTitle(msgs[entry.Origin.Anchor:]); t2 != "未命名话题" {
			c.Hub.Topics.SetTitle(s.RootID, t2)
		}
	}
	_ = c.Hub.Topics.UpdateLeaf(s.RootID, s.ID, "", time.Now().UnixMilli(), len(msgs))
}

func approveDecision(callID string, ok bool, reason string) approve.Decision {
	return approve.Decision{CallID: callID, Approve: ok, Reason: reason}
}

func answerOf(callID, input string) askuser.Answer {
	return askuser.Answer{CallID: callID, Input: input}
}
