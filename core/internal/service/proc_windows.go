//go:build windows

package service

import (
	"fmt"
	"os/exec"

	pty "github.com/aymanbagabas/go-pty"
)

// killTree 结束终端进程树：taskkill /T 连孙进程一起终结（否则子进程
// 仍持有 PTY 管道，读泵永远不返回）。
func killTree(cmd *pty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
}
