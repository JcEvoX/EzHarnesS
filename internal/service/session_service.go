/*
SessionService：启动装配数据（bootstrap）、状态快照、历史、按需摘要。
*/
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"
	"github.com/xuanlv2002/ezloop/ext/hook/summary"
	"github.com/xuanlv2002/ezloop/types"

	"ezharness/internal/domain"
	"ezharness/internal/hooks"
)

/* ErrEmptySession 表示会话无消息可摘要。 */
var ErrEmptySession = errors.New("empty session")

/* SessionService 会话查询与摘要用例。 */
type SessionService struct {
	Hub *domain.Hub
}

/* BootstrapData 是前端启动所需的全量数据。 */
type BootstrapData struct {
	SessionID    string       `json:"sessionId"`
	Settings     SettingsView `json:"settings"`
	Status       Status       `json:"status"`
	MemoryExists bool         `json:"memoryExists"`
}

/* Bootstrap 汇总启动数据。 */
func (s *SessionService) Bootstrap() BootstrapData {
	sess := s.Hub.Active
	st := s.Hub.SettingsSnapshot()
	p := st.CompactPercent
	w := st.WorkDir
	return BootstrapData{
		SessionID:    sess.ID,
		Settings:     SettingsView{SystemExtra: st.SystemExtra, CompactPercent: &p, WorkDir: &w},
		Status:       s.Snapshot(),
		MemoryExists: memoryExists(s.Hub.Fsys),
	}
}

/* HistoryData 是历史响应。 */
type HistoryData struct {
	ID          string                 `json:"id"`
	Busy        bool                   `json:"busy"`
	Messages    []types.Message        `json:"messages"`
	PrevSession string                 `json:"prevSession,omitempty"` // compact 链上一会话（懒加载用）
	PrevTitle   string                 `json:"prevTitle,omitempty"`   // 上一话题标题（压缩标记用）
	Decisions   []hooks.DecisionRecord `json:"decisions,omitempty"`   // 人机决策记录（工具卡徽标用）
	Forks       []hooks.ForkSummary    `json:"forks,omitempty"`       // fork 分身摘要（入口卡重建，详情懒加载）
}

/* Status 是右栏状态卡数据（命中率与用量为本会话口径，切会话/重启清零）。 */
type Status struct {
	Model            string   `json:"model"`
	SessionID        string   `json:"sessionId"`
	SessionMsgs      int      `json:"sessionMsgs"`
	Busy             bool     `json:"busy"`
	ContextTokens    int      `json:"contextTokens"`
	ContextWindow    int      `json:"contextWindow"` // 主模型窗口（水位条分母）
	CacheHitRate     float64  `json:"cacheHitRate"`
	PromptTokens     int      `json:"promptTokens"`     // 本会话累计输入
	CompletionTokens int      `json:"completionTokens"` // 本会话累计输出
	Turns            int      `json:"turns"`
	Tools            []string `json:"tools"`
	McpServers       []string `json:"mcpServers"`
	Skills           []string `json:"skills"`
	TopicsCount      int      `json:"topicsCount"`
}

/* mainModelName 返回主模型名（空槽显示空）。 */
func mainModelName(h *domain.Hub) string {
	if m := h.ModelsSnapshot().ActiveMain(); m != nil {
		return m.Name
	}
	return ""
}

/* Snapshot 汇总活动会话状态。 */
func (s *SessionService) Snapshot() Status {
	sess := s.Hub.Active
	w := sess.Wired()
	var tools []string
	if w != nil {
		tools = w.ToolNames
	}
	if tools == nil {
		tools = []string{}
	}
	mcp := McpNames(s.Hub.Fsys)
	if mcp == nil {
		mcp = []string{}
	}
	u := sess.Sess.Usage()
	hit := 0.0
	if u.PromptTokens > 0 {
		hit = float64(u.CachedTokens) / float64(u.PromptTokens)
	}
	ctxTokens, ctxWindow := sess.Sess.CtxInfo()
	skills := []string{}
	if entries, err := skill.LoadDir(context.Background(), s.Hub.Fsys, hooks.SkillsDir); err == nil {
		for _, e := range entries {
			skills = append(skills, e.Name)
		}
	}
	return Status{
		Model:            mainModelName(s.Hub),
		SessionID:        sess.ID,
		SessionMsgs:      len(sess.History()),
		Busy:             sess.Busy(),
		ContextTokens:    ctxTokens,
		ContextWindow:    ctxWindow,
		CacheHitRate:     hit,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		Turns:            s.Hub.Stats.Turns(),
		Tools:            tools,
		McpServers:       mcp,
		Skills:           skills,
		TopicsCount:      len(s.Hub.Topics.Load()),
	}
}

/* History 返回活动会话历史。 */
func (s *SessionService) History() HistoryData {
	sess := s.Hub.Active
	h := HistoryData{ID: sess.ID, Busy: sess.Busy(), Messages: sess.History(), PrevSession: sess.Sess.PrevID()}
	if h.PrevSession != "" {
		for _, e := range s.Hub.Topics.Load() {
			if e.ID == h.PrevSession {
				h.PrevTitle = e.Title
				break
			}
		}
	}
	h.Decisions = hooks.LoadDecisions(context.Background(), s.Hub.Active.Fsys, h.ID)
	h.Forks = hooks.ListForks(context.Background(), s.Hub.Active.Fsys, h.ID)
	return h
}

/* ForkData 是 fork 分身详情响应（抽屉懒加载）。 */
type ForkData struct {
	ID        string                 `json:"id"`
	Messages  []types.Message        `json:"messages"`
	Decisions []hooks.DecisionRecord `json:"decisions,omitempty"` // 主库决策记录（前端按 callId 匹配）
}

/* Fork 返回 fork 分身增量消息（存档均为已结束分身；运行中靠实时事件）。 */
func (s *SessionService) Fork(ctx context.Context, id, fid string) (*ForkData, error) {
	snap, err := hooks.LoadFork(ctx, s.Hub.Fsys, id, fid)
	if err != nil {
		return nil, err
	}
	return &ForkData{
		ID:        snap.ID,
		Messages:  snap.Messages,
		Decisions: hooks.LoadDecisions(ctx, s.Hub.Fsys, id),
	}, nil
}

/* PrevData 是懒加载上一会话响应。 */
type PrevData struct {
	ID          string              `json:"id"`
	Title       string              `json:"title,omitempty"`
	Summary     string              `json:"summary,omitempty"`
	Messages    []types.Message     `json:"messages"`
	Forks       []hooks.ForkSummary `json:"forks,omitempty"`      // 旧库的分身摘要（入口卡重建）
	PrevSession string              `json:"prevSession,omitempty"` // 再上一级 ID（非空可继续上翻）
}

/*
Prev 沿 compact 链取 id 的上一会话内容（向上滚动懒加载）。id 允许
链上任一会话（读归档只读安全）；无上一级返回 ok=false。
*/
func (s *SessionService) Prev(ctx context.Context, id string) (*PrevData, bool, error) {
	cur, err := hooks.LoadSnap(ctx, s.Hub.Fsys, id)
	if err != nil {
		return nil, false, err
	}
	if cur.PrevSession == "" {
		return nil, false, nil
	}
	prev, err := hooks.LoadSnap(ctx, s.Hub.Fsys, cur.PrevSession)
	if err != nil {
		return nil, false, err
	}
	title, summary := "", prev.CompactSummary
	for _, e := range s.Hub.Topics.Load() {
		if e.ID == cur.PrevSession {
			title, summary = e.Title, e.Summary
			break
		}
	}
	return &PrevData{ID: prev.ID, Title: title, Summary: summary,
		Messages: prev.Messages, Forks: hooks.ListForks(ctx, s.Hub.Fsys, prev.ID),
		PrevSession: prev.PrevSession}, true, nil
}

/* Summarize 生成当前会话摘要（模型调用）。 */
func (s *SessionService) Summarize(ctx context.Context) (string, error) {
	if main := s.Hub.ModelsSnapshot().ActiveMain(); main == nil || main.APIKey == "" {
		return "", domain.ErrNoAPIKey
	}
	sess := s.Hub.Active
	hist := sess.History()
	if len(hist) == 0 {
		return "", ErrEmptySession
	}
	w := sess.Wired()
	if w == nil {
		return "", ErrEmptySession
	}
	return summary.Summarize(ctx, w.Provider, hist, "")
}

func memoryExists(fsys interface {
	Read(ctx context.Context, path string) ([]byte, error)
}) bool {
	data, err := fsys.Read(context.Background(), hooks.MemoryFile)
	return err == nil && len(strings.TrimSpace(string(data))) > 0
}
