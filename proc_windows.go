//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow 隐藏子进程的控制台窗口（Windows）
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
