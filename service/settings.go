package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"changeme/core"
)

// 应用设置业务

// Settings 应用设置项
type Settings struct {
	AutoStart  bool `json:"autoStart"`  // 开机自启（状态以注册表为准）
	HideToTray bool `json:"hideToTray"` // 关闭时隐藏到托盘
}

// SettingsService 设置服务：
// - autoStart 状态存于注册表（core.AutoStartEnabled 查询）
// - hideToTray 持久化到 %APPDATA%\ToolBox\settings.json，默认开启
type SettingsService struct {
	mu         sync.Mutex
	loaded     bool
	hideToTray bool
}

// GetSettings 读取设置
func (s *SettingsService) GetSettings() (Settings, error) {
	autoStart, err := core.AutoStartEnabled()
	if err != nil {
		return Settings{}, err
	}
	return Settings{AutoStart: autoStart, HideToTray: s.loadHideToTray()}, nil
}

// SetHideToTray 设置并持久化"关闭时隐藏到托盘"
func (s *SettingsService) SetHideToTray(enabled bool) error {
	s.mu.Lock()
	s.loaded = true
	s.hideToTray = enabled
	s.mu.Unlock()

	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]bool{"hideToTray": enabled})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// HideToTray 返回当前开关状态（供窗口关闭钩子使用）
func (s *SettingsService) HideToTray() bool {
	return s.loadHideToTray()
}

// loadHideToTray 懒加载配置文件，缺省 true（默认开启）
func (s *SettingsService) loadHideToTray() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.hideToTray
	}
	s.loaded = true
	s.hideToTray = true

	path, err := settingsFilePath()
	if err != nil {
		return s.hideToTray
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s.hideToTray
	}
	var cfg struct {
		HideToTray bool `json:"hideToTray"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return s.hideToTray
	}
	s.hideToTray = cfg.HideToTray
	return s.hideToTray
}

// settingsFilePath 配置文件路径：%APPDATA%\ToolBox\settings.json
func settingsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ToolBox", "settings.json"), nil
}
