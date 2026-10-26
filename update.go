package main

import (
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// currentVersion 当前应用版本号，构建时由
// -ldflags "-X main.currentVersion=x.y.z" 注入；未注入时为 dev
var currentVersion = "dev"

// updaterPublicKey 更新签名验签公钥（PKIX PEM），随客户端编译固定
//
//go:embed updater.pub
var updaterPublicKey []byte

// updateManifestURL 更新检查地址（本应用 fullname = toolbox，见 project.json）
//
//	platform/arch/version/channel 由 UpdateService 追加
const updateManifestURL = "https://www.earwind.top/api/v1/update/check?fullname=toolbox"

const (
	// UpdateProgressEvent 下载进度：app:update:progress
	UpdateProgressEvent = "app:update:progress"
	// UpdateReadyEvent 更新待用户确认：自动更新时已下载完成，手动检查时等待确认下载
	UpdateReadyEvent = "app:update:ready"
)

func init() {
	application.RegisterEvent[UpdateProgress](UpdateProgressEvent)
	application.RegisterEvent[UpdateInfo](UpdateReadyEvent)
}

// verifyUpdateSignature 用内置公钥校验更新包的 ed25519 签名
//
//	签名对象是构件的 sha256 摘要（与发布端 signDigestBase64 一致）。
func verifyUpdateSignature(digest []byte, sigB64 string) error {
	block, _ := pem.Decode(updaterPublicKey)
	if block == nil {
		return fmt.Errorf("更新公钥格式错误")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("解析更新公钥失败: %w", err)
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("更新公钥不是 ed25519")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("签名解码失败: %w", err)
	}
	if !ed25519.Verify(pub, digest, sig) {
		return fmt.Errorf("更新包签名校验失败")
	}
	return nil
}

// setupUpdate 挂上启动时的静默自动更新
//
//	仅检查 + 下载，下载完成后发事件让前端弹窗确认，不自动重启。
func setupUpdate(app *application.App, svc *UpdateService) {
	svc.app = app
	go func() {
		time.Sleep(4 * time.Second)
		if !loadSettings().AutoUpdate {
			return
		}
		// 开发版（未注入版本号）不自动检查，避免"永远有更新"
		if currentVersion == "" || currentVersion == "dev" {
			return
		}
		if err := svc.AutoUpdate(); err != nil {
			log.Printf("自动更新检查失败: %v", err)
		}
	}()
}
