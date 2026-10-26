package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// appSettings 更新模块设置（%APPDATA%/ToolBox/update.json）
//
//	与 toolBox 的 settings.json（hideToTray 等）分开存放，互不覆盖。
type appSettings struct {
	AutoUpdate bool `json:"auto_update"` // 启动时自动检查更新
}

// settingsPath 设置文件路径
func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ToolBox", "update.json")
}

// loadSettings 读取设置；文件不存在或字段缺失时用默认值（自动更新默认开启）
func loadSettings() appSettings {
	s := appSettings{AutoUpdate: true}
	b, err := os.ReadFile(settingsPath())
	if err != nil {
		return s
	}
	var raw struct {
		AutoUpdate *bool `json:"auto_update"`
	}
	if json.Unmarshal(b, &raw) == nil && raw.AutoUpdate != nil {
		s.AutoUpdate = *raw.AutoUpdate
	}
	return s
}

// saveSettings 写入设置
func saveSettings(s appSettings) error {
	p := settingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
