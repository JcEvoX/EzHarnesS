/*
Package tools 提供 ezharness 自有的增量工具：save_app（快应用生成）与
sleep（阻塞等待）。read_file / write_file / edit_file / terminal 复用
ezloop 的 filetools hook（原生 shell 执行，Windows 为 cmd），在
agent_service 装配。
*/
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuanlv2002/ezloop/types"

	"ezharness/core/internal/osfs"
	"ezharness/core/internal/quickapp"
)

/* appsDir 是快应用目录（工作目录相对）。 */
const appsDir = "apps"

/* SaveApp 返回快应用工具集。 */
func SaveApp(fsys osfs.OS) []types.Tool {
	return []types.Tool{saveAppTool{fsys}}
}

/* Sleep 返回阻塞等待工具。 */
func Sleep() types.Tool { return sleepTool{} }

/*
sleepTool 阻塞等待：Windows 的 timeout 在工具进程里 stdin 非控制台会
立即报错退出（>nul 连报错都看不见），terminal/term_action 里根本睡不住
——定时等待只有这条路是真的。
*/
type sleepTool struct{}

const sleepMaxSeconds = 900

func (sleepTool) Name() string { return "sleep" }

func (sleepTool) Description() string {
	return "阻塞等待 seconds 秒后再返回（等后端就绪、轮询间隔、长任务出结果前的间隙）。" +
		"不要用 terminal 或 term_action send 跑 timeout/ping 之类命令去睡——" +
		"Windows 的 timeout 在工具进程里立即退出，睡不住还白跑一趟。上限 900 秒；用户停止会立即中断"
}

func (sleepTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"seconds": {"type": "integer", "description": "等待秒数（1-900）"}
		},
		"required": ["seconds"]
	}`)
}

func (sleepTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Seconds int `json:"seconds"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.Seconds <= 0 || a.Seconds > sleepMaxSeconds {
		return "", fmt.Errorf("seconds 需在 1-%d 之间", sleepMaxSeconds)
	}
	select {
	case <-time.After(time.Duration(a.Seconds) * time.Second):
		return fmt.Sprintf("已等待 %d 秒", a.Seconds), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type saveAppTool struct{ fsys osfs.OS }

func (saveAppTool) Name() string { return "save_app" }
func (saveAppTool) Description() string {
	return "把一个快应用保存到 apps/<名>/（app.quick 声明 + index.html 前端）。" +
		"需要后端进程时给 backend（在应用目录里执行的命令，如 python probe.py），" +
		"卡片头像给 icon（应用目录内的图片文件名，如 icon.svg，先用 write_file 写进同目录）；" +
		"后端脚本等其余文件同样用 write_file 补进同一目录"
}

func (t saveAppTool) ArgsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "应用名（英文短名，如 certprobe）"},
			"title": {"type": "string", "description": "卡片标题（可读名；缺省用 name）"},
			"html": {"type": "string", "description": "完整 html 文档内容（前端入口 index.html）"},
			"backend": {"type": "string", "description": "后端启动命令（可选；cwd 是应用目录，如 python probe.py）"},
			"icon": {"type": "string", "description": "卡片头像：应用目录内的图片文件名（可选，如 icon.svg/png，先用 write_file 写好）"}
		},
		"required": ["name", "html"]
	}`)
}

func (t saveAppTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Name    string `json:"name"`
		Title   string `json:"title"`
		HTML    string `json:"html"`
		Backend string `json:"backend"`
		Icon    string `json:"icon"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimSpace(a.Name), ".html")
	if !quickapp.ValidName(name) {
		return "", errors.New("invalid app name")
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = name
	}
	if icon := strings.TrimSpace(a.Icon); icon != "" {
		if _, err := quickapp.SafeEntry(icon); err != nil {
			return "", fmt.Errorf("icon %w", err)
		}
	}
	spec := quickapp.Spec{Title: title, Entry: quickapp.DefaultEntry, Backend: strings.TrimSpace(a.Backend), Icon: strings.TrimSpace(a.Icon)}
	meta, err := spec.Marshal()
	if err != nil {
		return "", err
	}
	dir := appsDir + "/" + name
	if err := t.fsys.Write(ctx, dir+"/"+quickapp.FileName, meta); err != nil {
		return "", err
	}
	if err := t.fsys.Write(ctx, dir+"/"+quickapp.DefaultEntry, []byte(a.HTML)); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.FromSlash(dir))
	if err != nil {
		abs = dir
	}
	abs = filepath.ToSlash(abs)
	backend := ""
	if spec.HasBackend() {
		backend = fmt.Sprintf("；已声明后端，用户点启动会在内置终端执行 `%s`，后端脚本等文件写进 %s/", spec.Backend, abs)
	}
	return fmt.Sprintf("快应用已保存：%s（前端 %s/%s，用户可一键启动）%s",
		name, abs, quickapp.DefaultEntry, backend), nil
}
