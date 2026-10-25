/*
plan 工具：维护当前会话的任务计划与进度。计划落 sessions/<rootID>/plan.json，
每轮随 agent_status 注入模型；更新时同步推 plan.update 事件，前端在输入框
上方实时展示「正在做什么 + 完整计划」。
*/
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/hooks"
	"ezharness/core/internal/osfs"
)

/*
Plan 返回计划工具。rootID 决定计划归属的分支；onUpdate 在计划变化后回调
（nil = 已清除），由装配层转成 SSE 事件。
*/
func Plan(fsys osfs.OS, rootID string, onUpdate func(*hooks.SessionPlan)) types.Tool {
	return types.NewTool("plan",
		"维护当前会话的任务计划与进度（输入框上方实时展示，并随每轮 agent_status 注入上下文）。"+
			"复杂任务（≥3 个实质步骤）开始时用 action=new 提交方案，默认先请用户审批，批准后才保存；被拒绝按反馈修改后重新 new。"+
			"plan 字段规范（纯文本多行，30–4000 字）：\n"+
			"目标：要达成什么（1 行）\n"+
			"方案：采用的做法、选择理由与关键取舍（1–3 行，不写工具名流水账）\n"+
			"步骤：\n1. …\n2. …\n"+
			"风险：不确定点或回滚方式（可选，1 行）\n"+
			"要求：具体可执行，不复述用户原话、不写寒暄；步骤与 progress 一一对应。"+
			"progress 字段：2–12 步，title ≤60 字、动词开头、不带序号、不重复；status=pending|running|completed，最多一个 running。"+
			"action=update：完成或切换一步就提交，progress 必须传完整列表（整表替换），plan 可缺省（保留原值）。"+
			"action=clear：计划被放弃或不再适用时清除；任务完成保留全部 completed 即可，不必 clear。"+
			"一两步的简单任务不要用",
		func(ctx context.Context, in *planArgs) (string, error) {
			switch in.Action {
			case "new":
				plan := strings.TrimSpace(in.Plan)
				if err := validatePlanText(plan); err != nil {
					return "", err
				}
				progress, err := normalizeProgress(in.Progress)
				if err != nil {
					return "", err
				}
				p := &hooks.SessionPlan{Plan: plan, Progress: progress}
				if err := hooks.SavePlan(ctx, fsys, rootID, p); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(p)
				}
				return planSummary(p), nil
			case "update":
				hasPlan := strings.TrimSpace(in.Plan) != ""
				hasProgress := in.Progress != nil
				if !hasPlan && !hasProgress {
					return "", errors.New("update 需要 plan 或 progress")
				}
				current, err := hooks.LoadPlan(ctx, fsys, rootID)
				if err != nil {
					return "", err
				}
				if current == nil {
					current = &hooks.SessionPlan{}
				}
				if hasPlan {
					plan := strings.TrimSpace(in.Plan)
					if err := validatePlanText(plan); err != nil {
						return "", err
					}
					current.Plan = plan
				}
				if hasProgress {
					progress, err := normalizeProgress(in.Progress)
					if err != nil {
						return "", err
					}
					current.Progress = progress
				}
				if err := hooks.SavePlan(ctx, fsys, rootID, current); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(current)
				}
				return planSummary(current), nil
			case "clear":
				if err := hooks.SavePlan(ctx, fsys, rootID, &hooks.SessionPlan{}); err != nil {
					return "", err
				}
				if onUpdate != nil {
					onUpdate(nil)
				}
				return "计划已清除", nil
			}
			return "", fmt.Errorf("未知 action %q(new/update/clear)", in.Action)
		})
}

type planProgressArgs struct {
	Title  string `json:"title" desc:"步骤描述"`
	Status string `json:"status" enum:"pending|running|completed" desc:"pending=待执行;running=执行中;completed=已完成"`
}

type planArgs struct {
	Action   string             `json:"action" enum:"new|update|clear" desc:"new=提交新方案（需审批）;update=更新进度;clear=清除计划"`
	Plan     string             `json:"plan,omitempty" desc:"完整规划方案（new 必填；按 目标/方案/步骤 分行，30–4000 字；update 可选，缺省保留原值）"`
	Progress []planProgressArgs `json:"progress,omitempty" desc:"进度步骤清单（2–12 步；new 必填，update 整表替换；最多一个 running）"`
}

/* 计划规范（超出范围直接报错回给模型自纠）。 */
const (
	planMinRunes = 30
	planMaxRunes = 4000
	planMinSteps = 2
	planMaxSteps = 12
	stepMaxRunes = 60
)

/* validatePlanText 校验方案正文：长度与分行结构（目标/方案/步骤）。 */
func validatePlanText(plan string) error {
	n := len([]rune(plan))
	if n < planMinRunes {
		return fmt.Errorf("plan 太短（%d 字）：需按 目标/方案/步骤 写具体做法，至少 %d 字", n, planMinRunes)
	}
	if n > planMaxRunes {
		return fmt.Errorf("plan 太长（%d 字）：上限 %d 字，保留可执行的关键信息", n, planMaxRunes)
	}
	lines := 0
	for line := range strings.SplitSeq(plan, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines < 3 {
		return errors.New("plan 需分行写清 目标、方案、步骤（至少 3 个非空行）")
	}
	return nil
}

/*
normalizeProgress 校验并规范化步骤：2–12 步、标题单行且不重复、
status 只认三态、最多一个 running。
*/
func normalizeProgress(in []planProgressArgs) ([]hooks.PlanProgress, error) {
	if len(in) < planMinSteps || len(in) > planMaxSteps {
		return nil, fmt.Errorf("progress 需 %d–%d 步（当前 %d 步）", planMinSteps, planMaxSteps, len(in))
	}
	out := make([]hooks.PlanProgress, 0, len(in))
	seen := make(map[string]bool, len(in))
	running := 0
	for i, p := range in {
		title := strings.Join(strings.Fields(p.Title), " ")
		if title == "" {
			return nil, fmt.Errorf("progress[%d].title 不能为空", i)
		}
		if n := len([]rune(title)); n > stepMaxRunes {
			return nil, fmt.Errorf("progress[%d].title 太长（%d 字，上限 %d）", i, n, stepMaxRunes)
		}
		if seen[title] {
			return nil, fmt.Errorf("progress[%d].title 与前面步骤重复：%s", i, title)
		}
		seen[title] = true
		status := strings.TrimSpace(p.Status)
		switch status {
		case hooks.PlanPending, hooks.PlanRunning, hooks.PlanCompleted:
		default:
			return nil, fmt.Errorf("progress[%d].status 需为 pending/running/completed", i)
		}
		if status == hooks.PlanRunning {
			running++
		}
		out = append(out, hooks.PlanProgress{Title: title, Status: status})
	}
	if running > 1 {
		return nil, fmt.Errorf("最多一个步骤为 running（当前 %d 个）", running)
	}
	return out, nil
}

/* planSummary 返回一行工具结果（卡片正文由 plan.update 事件驱动，不重复整份 JSON）。 */
func planSummary(p *hooks.SessionPlan) string {
	done, running := 0, ""
	for _, step := range p.Progress {
		switch step.Status {
		case hooks.PlanCompleted:
			done++
		case hooks.PlanRunning:
			if running == "" {
				running = step.Title
			}
		}
	}
	s := fmt.Sprintf("计划已保存：%d/%d 步完成", done, len(p.Progress))
	if running != "" {
		s += "，正在：" + running
	}
	return s
}
