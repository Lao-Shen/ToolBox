package main

import (
	"encoding/base64"
	"net/url"
	"runtime"
	"testing"
)

func TestUpdateCheckURL(t *testing.T) {
	old := currentVersion
	currentVersion = "1.2.3"
	defer func() { currentVersion = old }()

	u, err := url.Parse(updateCheckURL())
	if err != nil {
		t.Fatalf("解析 URL 失败: %v", err)
	}
	q := u.Query()
	cases := map[string]string{
		"fullname": "toolbox",
		"platform": runtime.GOOS,
		"arch":     runtime.GOARCH,
		"version":  "1.2.3",
		"channel":  "stable",
	}
	for k, want := range cases {
		if got := q.Get(k); got != want {
			t.Errorf("%s=%q, want %q", k, got, want)
		}
	}
}

func TestSafeVersionDir(t *testing.T) {
	cases := map[string]string{
		"1.2.3":    "1.2.3",
		"":         "latest",
		"a/b\\c":   "a_b_c",
		" 1.0.0  ": "1.0.0",
	}
	for in, want := range cases {
		if got := safeVersionDir(in); got != want {
			t.Errorf("safeVersionDir(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestSafeUpdateName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ToolBox.exe", "ToolBox.exe"},
		{"../../evil.exe", "evil.exe"},
		{"", "update.exe"},
	}
	for _, c := range cases {
		if got := safeUpdateName(&updateArtifact{Filename: c.in}); got != c.want {
			t.Errorf("safeUpdateName(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestVerifyUpdateSignatureRejectsInvalid(t *testing.T) {
	if err := verifyUpdateSignature([]byte("digest"), "not-base64!!"); err == nil {
		t.Fatal("应拒绝非法 base64 签名")
	}
	bad := base64.StdEncoding.EncodeToString(make([]byte, 64))
	if err := verifyUpdateSignature([]byte("digest"), bad); err == nil {
		t.Fatal("应拒绝错误的 ed25519 签名")
	}
}
