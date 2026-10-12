/*
reschange 是 remind hook 的变更段：轮首对比 session 资源基线
（skills/mcps），有变化才产出条目（由 remind.OnStart 汇总为
<resource_change> 消息与 resource.change 事件，并附变更后完整清单）。

基线（ResSnapshot）持久化随 session：sessionstore 落盘、restoreFrom
还原，重启不重复报。首轮只建基线不 diff（迁移/新建会话不算变更）。
技能启停也算变更：基线记录启用/停用两份名单，启停切换渲染为
"启用技能/停用技能"（与新增/移除一体）；旧格式基线（V<2 无停用名单）
视为未建，静默重建，不把存量停用误报成变更。
*/
package hooks

import (
	"context"
	"slices"
	"sort"

	"github.com/xuanlv2002/ezloop/ext/hook/skill"

	"ezharness/core/internal/builtinskill"
)

/* StatusMcp 是状态基线与 available 清单用的 MCP 服务摘要。 */
type StatusMcp struct {
	Name string
	Desc string
}

/*
buildChanges 对比资源基线产出变更条目（中文完整短语，模型与前端
变更卡同源）与变更后完整清单（名+描述，技能只列启用的），并推进基线。
prev 为 nil（首轮或旧格式）只建基线。
*/
func (h *Remind) buildChanges(ctx context.Context) (items []string, skills, mcps []StatusMcp) {
	var off []string
	if h.disabled != nil {
		off = h.disabled()
	}
	isOff := func(path string) bool { return slices.Contains(off, SkillDirOf(path)) }

	// 全量收集（内建优先，与 <skills> 段同一归置），启停只标注不过滤
	type skillState struct {
		Name string
		Desc string
		Off  bool
	}
	var all []skillState
	for _, s := range builtinskill.Skills() {
		all = append(all, skillState{Name: s.Name, Desc: s.Description, Off: isOff(s.Path)})
	}
	if loaded, err := skill.LoadDir(ctx, h.fsys, SkillsDir); err == nil {
		for _, s := range loaded {
			if slices.ContainsFunc(all, func(x skillState) bool { return x.Name == s.Name }) {
				continue
			}
			all = append(all, skillState{Name: s.Name, Desc: s.Description, Off: isOff(s.Path)})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	if h.mcpList != nil {
		mcps = h.mcpList()
		sort.Slice(mcps, func(i, j int) bool { return mcps[i].Name < mcps[j].Name })
	}

	prev := h.store.ResSnap()
	curModel := ""
	if h.model != nil {
		curModel = h.model()
	}
	if prev != nil && prev.V == resSnapV {
		oldOn := prev.Skills
		oldOff := prev.SkillsOff
		known := append(slices.Clone(oldOn), oldOff...)
		for _, s := range all {
			switch {
			case !slices.Contains(known, s.Name):
				items = append(items, "新增技能 "+s.Name)
			case s.Off && slices.Contains(oldOn, s.Name):
				items = append(items, "停用技能 "+s.Name)
			case !s.Off && slices.Contains(oldOff, s.Name):
				items = append(items, "启用技能 "+s.Name)
			}
		}
		for _, n := range known {
			if !slices.ContainsFunc(all, func(x skillState) bool { return x.Name == n }) {
				items = append(items, "移除技能 "+n)
			}
		}
		items = append(items, diffNames(prev.Mcps, names(mcps), "mcp")...)
		// 主模型切换与 skill/mcp 同通道：轮首 diff 出变更条目（落盘 + 时间线卡 + 模型可见）
		if prev.Model != "" && curModel != "" && curModel != prev.Model {
			items = append(items, "切换模型到 "+curModel)
		}
	}

	// 推进基线（启用/停用分列）；available 清单与返回值只含启用的
	var onNames, offNames []string
	for _, s := range all {
		if s.Off {
			offNames = append(offNames, s.Name)
		} else {
			onNames = append(onNames, s.Name)
			skills = append(skills, StatusMcp{Name: s.Name, Desc: s.Desc})
		}
	}
	h.store.SetResSnap(&ResSnapshot{V: resSnapV, Skills: onNames, SkillsOff: offNames, Mcps: names(mcps), Model: curModel})
	return items, skills, mcps
}

/* names 取摘要清单的名字序列（基线 diff 用）。 */
func names(list []StatusMcp) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

/* diffNames 对比新旧名单产出变更记录（中文完整短语，模型可读）。 */
func diffNames(oldS, newS []string, kind string) []string {
	label := map[string]string{"skill": "技能", "mcp": "MCP 服务"}[kind]
	var out []string
	for _, n := range newS {
		if !slices.Contains(oldS, n) {
			out = append(out, "新增"+label+" "+n)
		}
	}
	for _, n := range oldS {
		if !slices.Contains(newS, n) {
			out = append(out, "移除"+label+" "+n)
		}
	}
	return out
}
