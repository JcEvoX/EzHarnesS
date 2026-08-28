/*
TopicService：分支（线）用例——新建、分叉（copy）、切换、列表、删除、回顾。
数据模型见会话树设计：session=节点（sessions/<id>/session.json），分支=
叶子到根的 compress 链，侧栏显示分支不显示会话；fork 从任意消息位置
复制前缀（含选中消息）开新线；归档世代继续聊走 fork@tip，同 ID 续写废弃。
*/
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ErrTopicNotFound 话题/分支存档不存在。 */
var ErrTopicNotFound = errors.New("topic not found")

/* ErrBadAnchor fork 锚点越界（不在目标会话消息范围内）。 */
var ErrBadAnchor = errors.New("fork anchor out of range")

/* TopicService 分支用例。 */
type TopicService struct {
	Hub    *domain.Hub
	Agents *AgentService // 装配新分支的 agent（main 注入）
}

/* BranchView 是分支面板条目（索引 + 运行态合成）。 */
type BranchView struct {
	hooks.TopicEntry
	Running bool `json:"running"`           // 有轮运行中（后台分支也亮）
	Waiting bool `json:"waiting"`           // 有未决审批/提问
	Active  bool `json:"active"`            // 当前所处分支
}

/* buildBranchViews 由索引+注册表合成分支列表（含未索引的活动新分支）。 */
func buildBranchViews(h *domain.Hub) []BranchView {
	active := h.Active
	entries := h.Topics.Load()
	out := make([]BranchView, 0, len(entries)+1)
	found := false
	for _, e := range entries {
		v := BranchView{TopicEntry: e}
		if active != nil && e.ID == active.RootID {
			v.Active, found = true, true
			v.LeafID = active.ID // 内存态可能更新（compact 刚换代）
		}
		if s := h.SessionOf(e.ID); s != nil {
			v.Running = s.Busy()
			v.Waiting = s.PendingCount() > 0
		}
		out = append(out, v)
	}
	if active != nil && active.RootID != "" && !found {
		// 新建未发言的分支未入索引：合成置顶条目
		out = append(out, BranchView{TopicEntry: hooks.TopicEntry{
			ID: active.RootID, LeafID: active.ID, Title: "新对话", Kind: "new",
			CreatedAt: time.Now().UnixMilli(), Msgs: len(active.History()),
		}, Active: true})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].UpdatedAt, out[j].UpdatedAt
		if ti == 0 {
			ti = out[i].CreatedAt
		}
		if tj == 0 {
			tj = out[j].CreatedAt
		}
		return ti > tj
	})
	return out
}

/* List 返回分支列表（侧栏/记忆页数据源）。 */
func (t *TopicService) List() []BranchView { return buildBranchViews(t.Hub) }

/* TopicDetail 是话题完整存档（只读回顾）。 */
type TopicDetail struct {
	Entry    hooks.TopicEntry `json:"entry"`
	Messages []types.Message  `json:"messages"`
}

/*
Get 读取分支完整存档（读 LeafID 快照；id 也允许是任意 session ID，
供分叉源会话的只读回顾）。
*/
func (t *TopicService) Get(ctx context.Context, id string) (TopicDetail, error) {
	if entry, ok := t.Hub.Topics.Get(id); ok {
		leaf := entry.LeafID
		if leaf == "" {
			leaf = id
		}
		if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, leaf); err == nil {
			return TopicDetail{Entry: entry, Messages: snap.Messages}, nil
		}
	}
	if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil {
		return TopicDetail{
			Entry:    hooks.TopicEntry{ID: id, Title: hooks.FirstUserTitle(snap.Messages), Msgs: len(snap.Messages)},
			Messages: snap.Messages,
		}, nil
	}
	return TopicDetail{}, ErrTopicNotFound
}

/*
NewBranch 开新线：新 session（SeedKind=new，挂空根）注册并切为活动分支。
索引延迟到首次发言（Send 时落），避免空线粉尘。
*/
func (t *TopicService) NewBranch() *domain.Session {
	s := t.Hub.CreateBranch()
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return s
}

/*
Fork 从任意消息位置复制前缀开新分支（copy 语义，快照隔离）：
新 session 体内携带目标 [0, anchor] 的完整副本（含选中消息），
system/水位承源；分叉出处仅存展示元数据，删源不伤内容。
*/
func (t *TopicService) Fork(ctx context.Context, sourceID string, anchor int) (*domain.Session, error) {
	src, err := hooks.LoadSnap(ctx, t.Hub.Fsys, sourceID)
	if err != nil {
		return nil, ErrTopicNotFound
	}
	if anchor <= 0 || anchor > len(src.Messages) {
		return nil, ErrBadAnchor
	}
	title := hooks.FirstUserTitle(src.Messages)
	newID := hooks.NewSessionID()
	now := time.Now().UnixMilli()
	snap := &hooks.SessionSnap{
		ID:           newID,
		CreatedAt:    now,
		Messages:     append([]types.Message(nil), src.Messages[:anchor]...),
		SystemPrompt: src.SystemPrompt,
		SystemBase:   src.SystemBase,
		SummaryBlock: src.SummaryBlock,
		Model:        src.Model,
		TargetID:     sourceID,
		Anchor:       anchor,
		SeedKind:     "fork",
		LineRoot:     newID,
		ForkedFrom:   &hooks.ForkOrigin{SourceID: sourceID, Title: title, Anchor: anchor},
		CtxTokens:    src.CtxTokens,
		CtxWindow:    src.CtxWindow,
	}
	if err := hooks.SaveSnap(ctx, t.Hub.Fsys, snap); err != nil {
		return nil, err
	}
	_ = t.Hub.Topics.Add(hooks.TopicEntry{
		ID: newID, LeafID: newID, Title: title, Kind: "fork",
		CreatedAt: now, UpdatedAt: now, Msgs: len(snap.Messages), Origin: snap.ForkedFrom,
	})
	s := t.Hub.LoadBranch(newID, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return s, nil
}

/*
Switch 切换分支：注册表命中直接切（后台轮不取消——阶段一线间并发，
切回时 ReplayFrames 重建时间线与未决审批）；未加载则按索引 LeafID
恢复。无 Busy 拒绝。
*/
func (t *TopicService) Switch(ctx context.Context, rootID string) error {
	if s := t.Hub.SessionOf(rootID); s != nil {
		t.Hub.SetActive(s)
		return nil
	}
	entry, ok := t.Hub.Topics.Get(rootID)
	if !ok {
		return ErrTopicNotFound
	}
	leaf := entry.LeafID
	if leaf == "" {
		leaf = rootID
	}
	snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, leaf)
	if err != nil {
		return ErrTopicNotFound
	}
	s := t.Hub.LoadBranch(rootID, snap)
	t.Agents.Assemble(s, t.Hub.SettingsSnapshot())
	t.Hub.SetActive(s)
	return nil
}

/* Resume 兼容入口：切换到线（id=线根 ID）。 */
func (t *TopicService) Resume(ctx context.Context, id string) error { return t.Switch(ctx, id) }

/*
Delete 删除分支：清线上全部世代目录（LineRoot 归属）+ 索引条目 + 注册表。
运行中拒绝；删活动分支时先换到全新分支。
*/
func (t *TopicService) Delete(ctx context.Context, rootID string) error {
	if s := t.Hub.SessionOf(rootID); s != nil {
		if s.Busy() {
			return domain.ErrBusy
		}
	}
	active := t.Hub.Active
	if active != nil && active.RootID == rootID {
		t.NewBranch() // 活动分支被删：先切到全新空线
	}
	t.Hub.Unregister(rootID)
	ids, _ := hooks.ListMain(ctx, t.Hub.Fsys)
	for _, id := range ids {
		if id == rootID {
			continue
		}
		if snap, err := hooks.LoadSnap(ctx, t.Hub.Fsys, id); err == nil && snap.LineRoot == rootID {
			_ = os.RemoveAll(filepath.Join(hooks.SessionsDir, id))
		}
	}
	_ = os.RemoveAll(filepath.Join(hooks.SessionsDir, rootID))
	t.Hub.Topics.Remove(rootID)
	return nil
}
