package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCookiePathUsesConfigDirectory(t *testing.T) {
	configPath := filepath.Join("configs", "app.json")
	resolved := ResolveCookiePath(configPath, "cookie.json", "")
	want := filepath.Join("configs", "cookie.json")
	if resolved != want {
		t.Fatalf("cookie 路径解析错误: want %s, got %s", want, resolved)
	}
}

func TestResolveCookiePathUsesOverride(t *testing.T) {
	resolved := ResolveCookiePath(filepath.Join("configs", "app.json"), "cookie.json", filepath.Join("runtime", "custom.json"))
	want := filepath.Join("runtime", "custom.json")
	if resolved != want {
		t.Fatalf("override 路径未生效: want %s, got %s", want, resolved)
	}
}

func TestExtractCampaignID(t *testing.T) {
	campaignID, err := ExtractCampaignID("https://f.kdocs.cn/g/MmHrjlBS#routePrompt")
	if err != nil {
		t.Fatalf("提取 campaign ID 失败: %v", err)
	}
	if campaignID != "MmHrjlBS" {
		t.Fatalf("campaign ID 错误: got %s", campaignID)
	}
}

func TestNewServiceLoadsConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	configJSON := `{"cookie_file_path":"cookie.json","target_url":"https://f.kdocs.cn/g/MmHrjlBS#routePrompt","input_name":"测试用户"}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	service, err := NewService(configPath, "")
	if err != nil {
		t.Fatalf("加载服务失败: %v", err)
	}
	if service.Config.TargetURL == "" {
		t.Fatal("TargetURL 不应为空")
	}
	if service.CookiePath != filepath.Join(dir, "cookie.json") {
		t.Fatalf("CookiePath 错误: got %s", service.CookiePath)
	}
}
