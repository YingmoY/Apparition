package server

import (
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultAdminUsername    = "admin"
	defaultAdminPassword   = "admin"
	defaultServerListenHost = "0.0.0.0"
	defaultServerListenPort = 5680
)

type ServerConfig struct {
	Server   ServerSection   `json:"server"`
	Admin    AdminSection    `json:"admin"`
	Security SecuritySection `json:"security"`
	SMTP     SMTPSection     `json:"smtp"`
}

type ServerSection struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	ReadTimeoutSec  int    `json:"read_timeout_sec"`
	WriteTimeoutSec int    `json:"write_timeout_sec"`
	IdleTimeoutSec  int    `json:"idle_timeout_sec"`
}

type AdminSection struct {
	Username           string `json:"username"`
	PasswordHash       string `json:"password_hash"`
	MustChangePassword bool   `json:"must_change_password"`
}

type SecuritySection struct {
	SessionTTLHours       int `json:"session_ttl_hours"`
	RememberMeTTLDays     int `json:"remember_me_ttl_days"`
	LoginRateLimitPerMin  int `json:"login_rate_limit_per_minute"`
	EmailSendLimitPerHour int `json:"email_send_limit_per_hour"`
}

type SMTPSection struct {
	Enabled   bool   `json:"enabled"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	FromName  string `json:"from_name"`
	FromEmail string `json:"from_email"`
	TLSMode   string `json:"tls_mode"`
}

func defaultServerConfig() ServerConfig {
	hash, _ := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
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
			PasswordHash:       string(hash),
			MustChangePassword: true,
		},
		Security: SecuritySection{
			SessionTTLHours:       24,
			RememberMeTTLDays:     7,
			LoginRateLimitPerMin:  10,
			EmailSendLimitPerHour: 6,
		},
		SMTP: SMTPSection{
			Enabled: false,
			Port:    465,
			TLSMode: "ssl",
		},
	}
}

func ensureConfigFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}

	cfg := defaultServerConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("序列化默认配置失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return false, fmt.Errorf("写入配置文件失败: %w", err)
	}
	return true, nil
}

func loadServerConfig(path string) (ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ServerConfig{}, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ServerConfig{}, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = defaultServerListenHost
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = defaultServerListenPort
	}
	return cfg, nil
}

func saveServerConfig(path string, cfg ServerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
