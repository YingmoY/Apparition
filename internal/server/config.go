package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

func defaultServerConfig() (ServerConfig, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("生成默认管理员密码哈希失败: %w", err)
	}

	return ServerConfig{
		Server: ServerSection{
			Host:            defaultServerListenHost,
			Port:            defaultServerListenPort,
			ReadTimeoutSec:  15,
			WriteTimeoutSec: 30,
			IdleTimeoutSec:  60,
		},
		Admin: AdminSection{
			Username:           defaultAdminUsername,
			PasswordHash:       string(hashedPassword),
			MustChangePassword: true,
		},
		Security: SecuritySection{
			SessionTTLHours:       24,
			RememberMeTTLDays:     7,
			CSRFEnabled:           true,
			LoginRateLimitPerMin:  10,
			EmailSendLimitPerHour: 6,
		},
		SMTP: SMTPSection{
			Enabled:   false,
			Host:      "smtp.example.com",
			Port:      465,
			Username:  "",
			Password:  "",
			FromName:  "Apparition",
			FromEmail: "no-reply@example.com",
			TLSMode:   "ssl",
		},
		NotifierDefaults: NotifierDefaultsSection{
			OnClockInSuccess: true,
			OnClockInFailed:  true,
			OnAuthSecurity:   true,
		},
		GotifyGlobal: GotifyGlobalSection{
			Enabled:         false,
			ServerURL:       "",
			AppToken:        "",
			DefaultPriority: 5,
		},
		BarkGlobal: BarkGlobalSection{
			Enabled:      false,
			ServerURL:    "https://api.day.app",
			DeviceKey:    "",
			DefaultSound: "bell",
			DefaultIcon:  "",
		},
	}, nil
}

func ensureConfigFile(configPath string) (bool, error) {
	if _, err := os.Stat(configPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("检查配置文件失败: %w", err)
	}

	cfg, err := defaultServerConfig()
	if err != nil {
		return false, err
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("序列化默认配置失败: %w", err)
	}

	cleanPath := filepath.Clean(configPath)
	if err := os.WriteFile(cleanPath, content, 0600); err != nil {
		return false, fmt.Errorf("写入默认配置失败: %w", err)
	}

	return true, nil
}

func loadServerConfig(configPath string) (ServerConfig, error) {
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return ServerConfig{}, fmt.Errorf("读取配置失败: %w", err)
	}

	var cfg ServerConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return ServerConfig{}, fmt.Errorf("解析配置失败: %w", err)
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = defaultServerListenHost
	}
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = defaultServerListenPort
	}

	return cfg, nil
}

func saveServerConfig(configPath string, cfg ServerConfig) error {
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(filepath.Clean(configPath), content, 0600); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	return nil
}
