/*
memory 工具：长期记忆的唯一读写通道。变更不直接写记忆——先落待确认区
（用户在通知栏逐条确认才应用，取消即丢弃；重启不丢），工具立即返回。
条目 CRUD（add/update/remove，target=user|soul）+ 项目记忆
（project-save/project-remove/project-read 按 id 读全文）。
*/
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/* Memory 返回记忆工具。 */
func Memory(fsys osfs.OS) types.Tool { return memoryTool{fsys} }

type memoryTool struct{ fsys osfs.OS }

func (memoryTool) Name() string { return "memory" }

func (memoryTool) Description() string {
	return "长期记忆的唯一读写通道（记忆文件不可用文件工具读写）。记忆是条目式数据库：" +
		"user=用户个人信息（偏好/背景/约定）、soul=你的灵魂（用户对你的期望：性格、表达方式、工作方式与协作原则；不是项目知识）。" +
		"action=add|update|remove 增删改条目（target=user|soul，id 见 system <memory> 段标注）；" +
		"action=project-save 保存项目记忆（name/description/content，id 缺省由 name 生成；" +
		"名称与描述默认随索引加载进上下文——写项目的基本描述，content 详情是关于项目的具体记忆（坑点/约束这类），按需读取）；" +
		"action=project-remove 删除项目；action=project-read 按 id 读项目记忆全文（读项目详情的唯一方式）。" +
		"提交的变更先存待确认区，用户在通知栏确认后才生效（取消即丢弃）——同一条信息不要重复提交，" +
		"生效与否以下一轮 resource_change 为准；project-read 不需确认直接返回"
}

func (memoryTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["add", "update", "remove", "project-save", "project-remove", "project-read"], "description": "操作"},
			"target": {"type": "string", "enum": ["user", "soul"], "description": "条目目标（add/update/remove 用）"},
			"id": {"type": "string", "description": "条目 id（update/remove）或项目 id（project-* 可选，缺省由 name 生成）"},
			"text": {"type": "string", "description": "条目内容（add/update）"},
			"name": {"type": "string", "description": "项目名（project-save）"},
			"description": {"type": "string", "description": "项目一句话描述（project-save）"},
			"content": {"type": "string", "description": "项目记忆全文 markdown（project-save）"}
		},
		"required": ["action"]
	}`)
}

func (t memoryTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a memoryArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	switch a.Action {
	case "add", "update", "remove":
		return t.submitItem(a)
	case "project-save":
		return t.submitProject(a)
	case "project-remove":
		return t.submit(hooks.PendingMemoryOp{
			Op:        hooks.PendingOpProjectRemove,
			ProjectID: strings.TrimSpace(a.ID),
			Source:    "ai",
		})
	case "project-read":
		return t.projectRead(ctx, a.ID)
	}
	return "", fmt.Errorf("未知 action %q", a.Action)
}

/* submit 校验并落待确认区，返回统一文案。 */
func (t memoryTool) submit(op hooks.PendingMemoryOp) (string, error) {
	op.Source = "ai"
	op.CreatedAt = time.Now()
	saved, err := hooks.AppendPendingOp(context.Background(), t.fsys, op)
	if err != nil {
		return "", err
	}
	return "记忆已存储，待用户确认后生效：" + saved.Describe(), nil
}

/* memoryArgs 是 memory 工具的参数集（action 决定哪些字段生效）。 */
type memoryArgs struct {
	Action      string `json:"action"`
	Target      string `json:"target"`
	ID          string `json:"id"`
	Text        string `json:"text"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

/* submitItem 条目类操作（add/update/remove）入待确认区。 */
func (t memoryTool) submitItem(a memoryArgs) (string, error) {
	switch a.Target {
	case "user", "soul":
	default:
		return "", errors.New("target 需为 user 或 soul")
	}
	id := strings.TrimSpace(a.ID)
	text := strings.TrimSpace(a.Text)
	op := hooks.PendingMemoryOp{Target: a.Target, ItemID: id, Text: text}
	switch a.Action {
	case "add":
		if text == "" {
			return "", errors.New("add 需要 text")
		}
		op.Op = hooks.PendingOpAdd
	case "update":
		if id == "" || text == "" {
			return "", errors.New("update 需要 id 与 text")
		}
		op.Op = hooks.PendingOpUpdate
	case "remove":
		if id == "" {
			return "", errors.New("remove 需要 id")
		}
		op.Op = hooks.PendingOpRemove
	}
	return t.submit(op)
}

/* submitProject 项目保存入待确认区。 */
func (t memoryTool) submitProject(a memoryArgs) (string, error) {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return "", errors.New("project-save 需要 name")
	}
	return t.submit(hooks.PendingMemoryOp{
		Op:          hooks.PendingOpProjectSave,
		ProjectID:   strings.TrimSpace(a.ID),
		Name:        name,
		Description: strings.TrimSpace(a.Description),
		Content:     a.Content,
	})
}

/* projectRead 按项目 id 读记忆全文（只读，不需确认）。 */
func (t memoryTool) projectRead(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("project-read 需要 id")
	}
	projects, err := hooks.LoadProjects(ctx, t.fsys)
	if err != nil {
		return "", err
	}
	if i := func() int {
		for i, p := range projects {
			if p.ID == id {
				return i
			}
		}
		return -1
	}(); i < 0 {
		return "", fmt.Errorf("无项目 %s（id 见 system <memory> 段项目索引）", id)
	}
	data, err := t.fsys.Read(ctx, hooks.ProjectContentPath(id))
	if err != nil {
		return "（项目 " + id + " 尚无记忆全文）", nil
	}
	return string(data), nil
}
