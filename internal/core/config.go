package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultConfigPath = "config.json"

var campaignIDPattern = regexp.MustCompile(`/([A-Za-z0-9]+)(?:#|$)`)

func LoadConfig(path string) (Config, error) {
	configPath := filepath.Clean(path)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if strings.TrimSpace(config.TargetURL) == "" {
		return Config{}, fmt.Errorf("配置项 target_url 不能为空")
	}
	if strings.TrimSpace(config.InputName) == "" {
		return Config{}, fmt.Errorf("配置项 input_name 不能为空")
	}

	return config, nil
}

func LoadCookieData(path string) (CookieData, error) {
	cookiePath := filepath.Clean(path)
	content, err := os.ReadFile(cookiePath)
	if err != nil {
		return CookieData{}, fmt.Errorf("读取 Cookie 文件失败: %w", err)
	}

	var cookieData CookieData
	if err := json.Unmarshal(content, &cookieData); err != nil {
		return CookieData{}, fmt.Errorf("解析 Cookie 文件失败: %w", err)
	}

	return cookieData, nil
}

func ResolveCookiePath(configPath, configuredPath, override string) string {
	if strings.TrimSpace(override) != "" {
		return filepath.Clean(override)
	}

	resolved := strings.TrimSpace(configuredPath)
	if resolved == "" {
		resolved = "cookie.json"
	}

	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved)
	}

	return filepath.Join(filepath.Dir(filepath.Clean(configPath)), resolved)
}

func ExtractCampaignID(targetURL string) (string, error) {
	matches := campaignIDPattern.FindStringSubmatch(targetURL)
	if len(matches) < 2 {
		return "", fmt.Errorf("无法从目标链接提取 campaign ID")
	}

	return matches[1], nil
}
