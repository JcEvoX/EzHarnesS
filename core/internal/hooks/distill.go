/*
distill 是归档的沉淀步：归档换代前，把会话中值得跨会话长期保留的信息
融合进记忆。一次模型调用：输入 = 当前全部记忆（user/soul 条目 + 项目
索引 + 各项目全文）+ 会话，输出 = 修改方案 JSON（条目级 add/update/
remove、项目 save/remove），代码解析后直接应用。

失败静默降级（归档仍继续）：沉淀是增值动作，不该挡住用户显式触发的
归档；调用侧拿不到 LoopState，不落 trace span。
*/
package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/ext/fs"
	"github.com/xuanlv2002/ezloop/provider"
	"github.com/xuanlv2002/ezloop/types"
)

/* errDistillNoJSON：模型输出里找不到 JSON 主体（调用方静默降级）。 */
var errDistillNoJSON = errors.New("distill: no json object in model output")

const distillPrompt = "你在维护一个 agent 的长期记忆。下面是刚结束的一段会话、当前工作目录与全部记忆现状。\n" +
	"读记忆现状 → 融合会话中值得跨会话长期保留的信息 → 只输出一个修改方案 JSON（不要输出任何其他文字）。\n" +
	"记忆结构与规则：\n" +
	"- user 条目：用户个人信息——偏好与习惯、职业背景、工作环境、固定的表达与流程约定（总量 ≤30 条，宁精勿杂）\n" +
	"- soul 条目：agent 的灵魂——用户希望你具备的性格、表达方式、工作方式与协作原则，以及与你这位用户磨合出的有效做法（总量 ≤30 条）；项目相关的硬性约束归项目记忆，不要写进 soul\n" +
	"- 项目：id 必须用输入里已有的 id；新项目 id 用工作目录名（小写英文数字连字符）。content 为项目记忆全文（背景/约定/关键路径/交付物位置，≤40 行 markdown，首行 '# <项目名>'）\n" +
	"- update/remove 只能用已有条目的 id；同一事实新表述覆盖旧的（update），过时信息删除（remove）\n" +
	"- 不要提取：一次性任务过程、临时性决定、可从代码或文档中恢复的技术细节\n" +
	"- 会话与项目无关时 projects 给空数组；无变更的部分给空数组\n" +
	"输出 JSON 格式（严格遵守）：\n" +
	`{"user":{"add":[{"text":"…"}],"update":[{"id":"u1","text":"…"}],"remove":["u2"]},` +
	`"soul":{"add":[],"update":[],"remove":[]},` +
	`"projects":{"save":[{"id":"ezharness","name":"ezharness","description":"一句话描述","content":"…全文…"}],"remove":["oldproj"]}}` +
	"\n\n【工作目录】%s\n\n【记忆现状】"

/*
distillChanges 是模型输出的修改方案（条目级操作，直接应用）。
*/
type distillText struct {
	Text string `json:"text"`
}

type distillIDText struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type distillItemChanges struct {
	Add    []distillText `json:"add"`
	Update []distillIDText `json:"update"`
	Remove []string      `json:"remove"`
}

type distillChanges struct {
	User     distillItemChanges `json:"user"`
	Soul     distillItemChanges `json:"soul"`
	Projects struct {
		Save   []ProjectEntryContent `json:"save"`
		Remove []string              `json:"remove"`
	} `json:"projects"`
}

/* ProjectEntryContent 是项目保存操作的载荷（索引条目 + 记忆全文）。 */
type ProjectEntryContent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

/*
distillToMemory 执行沉淀：组装记忆现状 → 一次模型调用 → 解析修改方案
JSON → 逐条落待确认区（用户在通知栏确认后生效）。view 是模型视图
（与归档摘要同源，含 marker 链）。
*/
func distillToMemory(ctx context.Context, p provider.ModelProvider, fsys fs.FileSystem, view []types.Message, workDir string) error {
	ctx, cancel := context.WithTimeout(ctx, modelSummaryTimeout)
	defer cancel()

	type targetItems struct {
		path string
	}
	targets := map[string]targetItems{
		"user": {path: UserJson},
		"soul": {path: SoulJson},
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(distillPrompt, workDir) + "\n")
	for _, name := range []string{"user", "soul"} {
		items, err := LoadItems(ctx, fsys, targets[name].path)
		if err != nil {
			return err
		}
		b.WriteString("\n## " + name + " 条目\n")
		if len(items) == 0 {
			b.WriteString("（尚无条目）\n")
		}
		for _, it := range items {
			b.WriteString("- " + it.ID + " | " + strings.ReplaceAll(it.Text, "\n", " ") + "\n")
		}
	}
	projects, err := LoadProjects(ctx, fsys)
	if err != nil {
		return err
	}
	b.WriteString("\n## 项目索引\n")
	if len(projects) == 0 {
		b.WriteString("（尚无项目）\n")
	}
	for _, pr := range projects {
		b.WriteString("- " + pr.ID + " | " + pr.Name + " | " + pr.Description + "\n")
		data, rerr := fsys.Read(ctx, ProjectContentPath(pr.ID))
		if rerr == nil && len(data) > 0 {
			b.WriteString("\n### 项目 " + pr.ID + " 记忆全文\n" + string(data) + "\n")
		}
	}

	resp, err := p.Invoke(ctx, &types.ModelRequest{Messages: []types.Message{
		{Role: types.RoleUser, Content: b.String() + "\n\n【刚结束的会话】\n" + flatMsgs(view)},
	}})
	if err != nil {
		return err
	}
	changes, err := parseDistillJSON(resp.Content)
	if err != nil {
		return err
	}

	/* 不直接应用：逐条落待确认区（用户在通知栏确认后生效）。add 的
	   条目 id 在确认应用时分配，这里不预占。 */
	appendOp := func(op PendingMemoryOp) error {
		op.Source = "distill"
		_, err := AppendPendingOp(ctx, fsys, op)
		return err
	}
	for _, name := range []string{"user", "soul"} {
		ch := changes.User
		if name == "soul" {
			ch = changes.Soul
		}
		for _, ad := range ch.Add {
			if strings.TrimSpace(ad.Text) == "" {
				continue
			}
			if err := appendOp(PendingMemoryOp{Op: PendingOpAdd, Target: name, Text: ad.Text}); err != nil {
				return err
			}
		}
		for _, up := range ch.Update {
			if strings.TrimSpace(up.ID) == "" || strings.TrimSpace(up.Text) == "" {
				continue
			}
			if err := appendOp(PendingMemoryOp{Op: PendingOpUpdate, Target: name, ItemID: up.ID, Text: up.Text}); err != nil {
				return err
			}
		}
		for _, id := range ch.Remove {
			if id == "" {
				continue
			}
			if err := appendOp(PendingMemoryOp{Op: PendingOpRemove, Target: name, ItemID: id}); err != nil {
				return err
			}
		}
	}
	for _, sv := range changes.Projects.Save {
		if strings.TrimSpace(sv.Name) == "" {
			continue
		}
		if err := appendOp(PendingMemoryOp{
			Op: PendingOpProjectSave, ProjectID: sv.ID, Name: sv.Name,
			Description: sv.Description, Content: sv.Content,
		}); err != nil {
			return err
		}
	}
	for _, id := range changes.Projects.Remove {
		if id == "" {
			continue
		}
		if err := appendOp(PendingMemoryOp{Op: PendingOpProjectRemove, ProjectID: id}); err != nil {
			return err
		}
	}
	return nil
}

/*
parseDistillJSON 从模型输出解析修改方案：剥 markdown 围栏与前后杂文
（首个 { 到末个 }）后 Unmarshal；空输出报错（调用方静默降级）。
*/
func parseDistillJSON(text string) (*distillChanges, error) {
	i := strings.Index(text, "{")
	j := strings.LastIndex(text, "}")
	if i < 0 || j <= i {
		return nil, errDistillNoJSON
	}
	var ch distillChanges
	if err := json.Unmarshal([]byte(text[i:j+1]), &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

/* flatMsgs 把消息压平为 "role: content" 文本（与 summarizeMsgs 同拼装）。 */
func flatMsgs(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(string(m.Role) + ": " + m.Content + "\n")
	}
	return b.String()
}
