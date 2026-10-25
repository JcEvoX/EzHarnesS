/*
Stats 是跨会话累计的生命体征（data/stats.json）：首启时间（已服务
天数基准）、累计 usage、轮数。缓存命中率 = cached/prompt。
*/
package domain

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
)

const statsFile = "stats.json"

/* Stats 累计生命体征。 */
type Stats struct {
	mu        sync.Mutex
	fsys      osfs.OS
	firstSeen time.Time
	total     types.Usage
	turns     int
	days      map[string]*DayStats
}

/* ModelDayUsage 单模型单日用量。 */
type ModelDayUsage struct {
	In    int `json:"in"`
	Out   int `json:"out"`
	Cache int `json:"cache"`
	Turns int `json:"turns"`
}

/* DayStats 单日聚合（In=输入,Cache=其中缓存命中,Out=输出;Models 按模型拆分）。 */
type DayStats struct {
	Turns  int                       `json:"turns"`
	In     int                       `json:"in"`
	Out    int                       `json:"out"`
	Cache  int                       `json:"cache"`
	Models map[string]*ModelDayUsage `json:"models,omitempty"`
}

/* StatsView 是 stats 的下发视图。 */
type StatsView struct {
	FirstSeen int64 `json:"firstSeen"`
	Turns     int   `json:"turns"`
}

type statsFileLayout struct {
	FirstSeen int64                `json:"firstSeen"` // UnixMilli
	Turns     int                  `json:"turns"`
	Total     types.Usage          `json:"total"`
	Days      map[string]*DayStats `json:"days,omitempty"`
}

/* NewStats 读 stats.json；不存在则以当前时间为首启写盘。 */
func NewStats(fsys osfs.OS) *Stats {
	s := &Stats{fsys: fsys, firstSeen: time.Now(), days: map[string]*DayStats{}}
	if data, err := fsys.Read(context.Background(), statsFile); err == nil {
		var f statsFileLayout
		if json.Unmarshal(data, &f) == nil && f.FirstSeen > 0 {
			s.firstSeen = time.UnixMilli(f.FirstSeen)
			s.turns = f.Turns
			s.total = f.Total
			if f.Days != nil {
				s.days = f.Days
			}
			return s
		}
	}
	s.saveLocked()
	return s
}

/* AddTurn 累加一轮用量并落盘（model 为本轮主模型名,按天按模型分桶）。 */
func (s *Stats) AddTurn(u *types.Usage, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := s.dayLocked()
	if u != nil {
		s.total.Add(*u)
		day.In += u.PromptTokens
		day.Cache += u.CachedTokens
		day.Out += u.CompletionTokens
		if model != "" {
			m := day.Models[model]
			if m == nil {
				m = &ModelDayUsage{}
				day.Models[model] = m
			}
			m.In += u.PromptTokens
			m.Cache += u.CachedTokens
			m.Out += u.CompletionTokens
			m.Turns++
		}
	}
	day.Turns++
	s.turns++
	s.pruneLocked()
	s.saveLocked()
}

func (s *Stats) dayLocked() *DayStats {
	key := time.Now().Format("2006-01-02")
	d := s.days[key]
	if d == nil {
		d = &DayStats{Models: map[string]*ModelDayUsage{}}
		s.days[key] = d
	}
	return d
}

/* pruneLocked 只保留最近 120 天（按天分桶只喂趋势图,更早的留在累计总量里）。 */
func (s *Stats) pruneLocked() {
	if len(s.days) <= 120 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -120).Format("2006-01-02")
	for k := range s.days {
		if k < cutoff {
			delete(s.days, k)
		}
	}
}

/* DayView 日期 + 当日聚合（Daily 的下发条目）。 */
type DayView struct {
	Date string `json:"date"`
	DayStats
}

/* Daily 返回按日升序的用量序列（模型面板趋势图数据源）。 */
func (s *Stats) Daily() []DayView {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.days))
	for k := range s.days {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]DayView, 0, len(keys))
	for _, k := range keys {
		out = append(out, DayView{Date: k, DayStats: *s.days[k]})
	}
	return out
}

/* Total 返回累计用量副本。 */
func (s *Stats) Total() types.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

/* Turns 返回累计轮数。 */
func (s *Stats) Turns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns
}

/* DaysServed 返回已服务天数（首启起算，当天为 0）。 */
func (s *Stats) DaysServed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(time.Since(s.firstSeen).Hours() / 24)
}

/* CacheHitRate 返回缓存命中率（cached/prompt，prompt 为 0 时 0）。 */
func (s *Stats) CacheHitRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total.PromptTokens == 0 {
		return 0
	}
	return float64(s.total.CachedTokens) / float64(s.total.PromptTokens)
}

func (s *Stats) saveLocked() {
	data, err := json.MarshalIndent(statsFileLayout{
		FirstSeen: s.firstSeen.UnixMilli(),
		Turns:     s.turns,
		Total:     s.total,
		Days:      s.days,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = s.fsys.Write(context.Background(), statsFile, data)
}
