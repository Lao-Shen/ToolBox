package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UpdateInfo 检查更新结果（供前端弹窗展示）
type UpdateInfo struct {
	Available   bool   `json:"available"`             // 是否有新版本
	Ready       bool   `json:"ready"`                 // 是否已下载完成、等待重启
	Version     string `json:"version"`               // 版本号
	Notes       string `json:"notes"`                 // 更新说明
	Size        int64  `json:"size"`                  // 安装包大小
	PublishedAt string `json:"publishedAt,omitempty"` // 发布时间
}

// UpdateState 当前更新状态（设置页恢复进度条用）
type UpdateState struct {
	Status  string `json:"status"` // idle | downloading | ready | error
	Percent int    `json:"percent"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Size    int64  `json:"size"`
	Error   string `json:"error,omitempty"`
}

// UpdateProgress 下载进度事件
type UpdateProgress struct {
	Status  string `json:"status"`
	Percent int    `json:"percent"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// updateManifest 服务端更新清单
type updateManifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	Version       string           `json:"version"`
	Channel       string           `json:"channel,omitempty"`
	Name          string           `json:"name,omitempty"`
	Notes         string           `json:"notes,omitempty"`
	PublishedAt   string           `json:"publishedAt,omitempty"`
	Artifacts     []updateArtifact `json:"artifacts"`
}

// updateArtifact 单个平台/架构的构件
type updateArtifact struct {
	URL           string `json:"url"`
	Filename      string `json:"filename,omitempty"`
	Filetype      string `json:"filetype,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Platform      string `json:"platform,omitempty"`
	Arch          string `json:"arch,omitempty"`
	DigestAlgo    string `json:"digestAlgo,omitempty"`
	Digest        string `json:"digest,omitempty"`
	SignatureAlgo string `json:"signatureAlgo,omitempty"`
	Signature     string `json:"signature,omitempty"`
}

// UpdateService 应用设置与更新（绑定给前端）
type UpdateService struct {
	app *application.App

	mu           sync.Mutex
	state        UpdateState
	manifest     *updateManifest
	artifactPath string
}

func NewUpdateService() *UpdateService {
	return &UpdateService{state: UpdateState{Status: "idle"}}
}

// CurrentVersion 当前应用版本号（构建时注入，未注入为 dev）
func (s *UpdateService) CurrentVersion() string {
	return currentVersion
}

// AutoUpdateEnabled 是否启用"启动时自动检查更新"
func (s *UpdateService) AutoUpdateEnabled() bool {
	return loadSettings().AutoUpdate
}

// SetAutoUpdateEnabled 设置自动更新开关（写入本地 update.json）
func (s *UpdateService) SetAutoUpdateEnabled(enabled bool) error {
	st := loadSettings()
	st.AutoUpdate = enabled
	return saveSettings(st)
}

// CheckUpdate 检查是否有新版本，不下载
//
//	已经有下载完成待重启的包时直接返回 Ready，避免重复检查/下载。
func (s *UpdateService) CheckUpdate() (*UpdateInfo, error) {
	if currentVersion == "" || currentVersion == "dev" {
		return nil, fmt.Errorf("开发版不支持在线更新，请使用正式构建")
	}

	s.mu.Lock()
	if s.state.Status == "downloading" {
		s.mu.Unlock()
		return nil, fmt.Errorf("更新包正在下载，请稍候")
	}
	if s.state.Status == "ready" {
		st := s.state
		s.mu.Unlock()
		return &UpdateInfo{
			Available: true,
			Ready:     true,
			Version:   st.Version,
			Notes:     st.Notes,
			Size:      st.Size,
		}, nil
	}
	s.mu.Unlock()

	m, err := s.fetchManifest()
	if err != nil {
		return nil, err
	}
	if m == nil {
		s.mu.Lock()
		s.manifest = nil
		s.state = UpdateState{Status: "idle"}
		s.mu.Unlock()
		s.emitProgress("idle", 0, "", "")
		return &UpdateInfo{Available: false}, nil
	}

	a := m.Artifacts[0]
	s.mu.Lock()
	s.manifest = m
	s.state = UpdateState{
		Status:  "idle",
		Version: m.Version,
		Notes:   m.Notes,
		Size:    a.Size,
	}
	s.mu.Unlock()
	s.emitProgress("idle", 0, m.Version, "")

	return &UpdateInfo{
		Available:   true,
		Version:     m.Version,
		Notes:       m.Notes,
		Size:        a.Size,
		PublishedAt: m.PublishedAt,
	}, nil
}

// StartDownload 用户确认后开始下载并安装（下载完成后自动替换重启）
func (s *UpdateService) StartDownload() error {
	return s.startDownload(false)
}

// AutoUpdate 启动时静默检查并下载；下载完成后发事件让前端弹窗确认重启
func (s *UpdateService) AutoUpdate() error {
	info, err := s.CheckUpdate()
	if err != nil {
		return err
	}
	if info == nil || !info.Available || info.Ready {
		return nil
	}
	return s.startDownload(true)
}

// GetUpdateState 返回当前更新状态（打开设置页时恢复进度条）
func (s *UpdateService) GetUpdateState() *UpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	if st.Status == "" {
		st.Status = "idle"
	}
	return &st
}

// ApplyUpdate 用已下载的包替换当前程序并重启
func (s *UpdateService) ApplyUpdate() error {
	s.mu.Lock()
	if s.state.Status != "ready" || s.artifactPath == "" {
		s.mu.Unlock()
		return fmt.Errorf("没有已下载的更新包")
	}
	source := s.artifactPath
	s.mu.Unlock()

	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前平台暂不支持自动替换，请手动下载更新")
	}
	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前程序路径失败: %w", err)
	}
	if err := applyWindowsUpdate(target, source); err != nil {
		return err
	}

	if s.app != nil {
		s.app.Quit()
	} else if app := application.Get(); app != nil {
		app.Quit()
	}
	return nil
}

// startDownload 异步下载；auto=true 表示自动更新（下载完只发 ready 事件）
func (s *UpdateService) startDownload(auto bool) error {
	s.mu.Lock()
	if s.state.Status == "downloading" {
		s.mu.Unlock()
		return nil
	}
	if s.state.Status == "ready" {
		s.mu.Unlock()
		if auto {
			return nil
		}
		return s.ApplyUpdate()
	}
	if s.manifest == nil || len(s.manifest.Artifacts) == 0 {
		s.mu.Unlock()
		return fmt.Errorf("请先检查更新")
	}
	m := *s.manifest
	m.Artifacts = append([]updateArtifact(nil), s.manifest.Artifacts...)
	s.mu.Unlock()

	go s.downloadAndPrepare(&m, auto)
	return nil
}

// downloadAndPrepare 下载、校验、落盘；手动模式下载完直接重启
func (s *UpdateService) downloadAndPrepare(m *updateManifest, auto bool) {
	a := m.Artifacts[0]
	s.setState("downloading", 0, "")
	if err := s.downloadAndVerify(m, &a); err != nil {
		log.Printf("下载更新失败: %v", err)
		s.setState("error", 0, err.Error())
		return
	}

	s.mu.Lock()
	s.state.Status = "ready"
	s.state.Percent = 100
	s.state.Error = ""
	s.mu.Unlock()
	s.emitProgress("ready", 100, m.Version, "")

	if auto {
		s.emitReady(m, &a)
		return
	}

	// 手动流程：下载完成后重启客户端
	time.Sleep(700 * time.Millisecond)
	if err := s.ApplyUpdate(); err != nil {
		log.Printf("应用更新失败: %v", err)
		s.setState("error", 100, err.Error())
	}
}

// downloadAndVerify 下载到本地并校验大小 / sha256 / ed25519 签名
func (s *UpdateService) downloadAndVerify(m *updateManifest, a *updateArtifact) error {
	if a.URL == "" {
		return fmt.Errorf("更新地址为空")
	}
	if runtime.GOOS == "windows" {
		name := a.Filename
		if name == "" {
			name = a.URL
		}
		if !strings.HasSuffix(strings.ToLower(name), ".exe") {
			return fmt.Errorf("更新包不是可执行文件，无法自动替换: %s", name)
		}
	}

	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "ToolBox", "updates", safeVersionDir(m.Version))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建更新目录失败: %w", err)
	}
	tmp := filepath.Join(dir, safeUpdateName(a))

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(a.URL)
	if err != nil {
		return fmt.Errorf("下载更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新失败: HTTP %d", resp.StatusCode)
	}

	total := a.Size
	if total <= 0 {
		total = resp.ContentLength
	}

	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建更新文件失败: %w", err)
	}
	hasher := sha256.New()
	pw := &progressWriter{
		w: io.MultiWriter(out, hasher),
		cb: func(n int64) {
			pct := 0
			if total > 0 {
				pct = int(n * 100 / total)
				if pct > 99 {
					pct = 99
				}
			}
			s.emitProgress("downloading", pct, m.Version, "")
		},
	}
	written, copyErr := io.Copy(pw, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("下载更新失败: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入更新文件失败: %w", closeErr)
	}
	if total > 0 && written != total {
		os.Remove(tmp)
		return fmt.Errorf("更新包大小不一致（%d/%d）", written, total)
	}

	digest := hasher.Sum(nil)
	if a.Digest != "" {
		algo := strings.ToLower(strings.TrimSpace(a.DigestAlgo))
		if algo != "" && algo != "sha256" {
			os.Remove(tmp)
			return fmt.Errorf("不支持的摘要算法: %s", a.DigestAlgo)
		}
		want, err := base64.StdEncoding.DecodeString(a.Digest)
		if err != nil || !bytes.Equal(digest, want) {
			os.Remove(tmp)
			return fmt.Errorf("更新包摘要校验失败")
		}
	}
	if a.Signature != "" {
		algo := strings.ToLower(strings.TrimSpace(a.SignatureAlgo))
		if algo != "" && algo != "ed25519" {
			os.Remove(tmp)
			return fmt.Errorf("不支持的签名算法: %s", a.SignatureAlgo)
		}
		if err := verifyUpdateSignature(digest, a.Signature); err != nil {
			os.Remove(tmp)
			return err
		}
	} else {
		log.Printf("更新包 %s 未签名，仅校验 sha256", m.Version)
	}

	s.mu.Lock()
	s.artifactPath = tmp
	s.mu.Unlock()
	return nil
}

// fetchManifest 请求更新检查接口；无更新返回 (nil, nil)
func (s *UpdateService) fetchManifest() (*updateManifest, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", updateCheckURL(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接更新服务器失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("检查更新失败: HTTP %d", resp.StatusCode)
	}

	var m updateManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析更新信息失败: %w", err)
	}
	if m.Version == "" || len(m.Artifacts) == 0 {
		return nil, nil
	}
	return &m, nil
}

// updateCheckURL 拼上当前平台/架构/版本/渠道
func updateCheckURL() string {
	u, err := url.Parse(updateManifestURL)
	if err != nil {
		return updateManifestURL
	}
	q := u.Query()
	q.Set("platform", runtime.GOOS)
	q.Set("arch", runtime.GOARCH)
	q.Set("version", currentVersion)
	q.Set("channel", "stable")
	u.RawQuery = q.Encode()
	return u.String()
}

// setState 更新状态并推送进度事件
func (s *UpdateService) setState(status string, percent int, errMsg string) {
	s.mu.Lock()
	s.state.Status = status
	s.state.Percent = percent
	s.state.Error = errMsg
	version := s.state.Version
	s.mu.Unlock()
	s.emitProgress(status, percent, version, errMsg)
}

func (s *UpdateService) emitProgress(status string, percent int, version, errMsg string) {
	app := s.app
	if app == nil {
		app = application.Get()
	}
	if app == nil {
		return
	}
	app.Event.Emit(UpdateProgressEvent, UpdateProgress{
		Status:  status,
		Percent: percent,
		Version: version,
		Error:   errMsg,
	})
}

func (s *UpdateService) emitReady(m *updateManifest, a *updateArtifact) {
	app := s.app
	if app == nil {
		app = application.Get()
	}
	if app == nil {
		return
	}
	app.Event.Emit(UpdateReadyEvent, UpdateInfo{
		Available:   true,
		Ready:       true,
		Version:     m.Version,
		Notes:       m.Notes,
		Size:        a.Size,
		PublishedAt: m.PublishedAt,
	})
}

// applyWindowsUpdate 写一个批处理：等当前进程退出 → 覆盖 exe → 启动新 exe
func applyWindowsUpdate(target, source string) error {
	pid := os.Getpid()
	targetDir := filepath.Dir(target)
	sourceDir := filepath.Dir(source)
	script := filepath.Join(os.TempDir(), fmt.Sprintf("toolbox-update-%d.bat", time.Now().UnixNano()))
	content := fmt.Sprintf(`@echo off
setlocal
:wait
tasklist /FI "PID eq %d" 2>nul | find "%d" >nul
if not errorlevel 1 (
  ping -n 2 127.0.0.1 >nul
  goto wait
)
copy /Y "%s" "%s" >nul
if errorlevel 1 (
  start "" /D "%s" "%s"
) else (
  start "" /D "%s" "%s"
)
del "%%~f0"
`, pid, pid, source, target, sourceDir, source, targetDir, target)
	content = strings.ReplaceAll(content, "\n", "\r\n")
	if err := os.WriteFile(script, []byte(content), 0o644); err != nil {
		return fmt.Errorf("创建更新脚本失败: %w", err)
	}

	cmd := exec.Command("cmd", "/C", script)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}
	return nil
}

// progressWriter 统计已写入字节并节流回调
type progressWriter struct {
	w       io.Writer
	written int64
	last    time.Time
	cb      func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.written += int64(n)
	if p.cb != nil && (p.last.IsZero() || time.Since(p.last) >= 80*time.Millisecond) {
		p.last = time.Now()
		p.cb(p.written)
	}
	return n, err
}

func safeUpdateName(a *updateArtifact) string {
	name := filepath.Base(strings.TrimSpace(a.Filename))
	if name == "" || name == "." || name == ".." {
		name = "update.exe"
	}
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

func safeVersionDir(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.ReplaceAll(v, "\\", "_")
	if v == "" {
		v = "latest"
	}
	return v
}
