package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// --- Notification channel config types ---

type emailNotifyConfig struct {
	SMTPHost  string `json:"smtp_host"`
	SMTPPort  int    `json:"smtp_port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	FromName  string `json:"from_name"`
	FromEmail string `json:"from_email"`
	TLSMode   string `json:"tls_mode"`
}

type gotifyNotifyConfig struct {
	ServerURL string `json:"server_url"`
	AppToken  string `json:"app_token"`
}

type barkNotifyConfig struct {
	ServerURL string `json:"server_url"`
	DeviceKey string `json:"device_key"`
}

// --- API handlers ---

func (a *App) handleNotifyChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getNotifyChannels(w, r)
	case http.MethodPut:
		a.putNotifyChannel(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getNotifyChannels(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	rows, err := a.db.Query(`SELECT channel_type, enabled, config_json FROM notification_channels WHERE user_id = ?`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询通知渠道失败", nil)
		return
	}
	defer rows.Close()

	channels := make(map[string]any)
	for rows.Next() {
		var chType string
		var enabled int
		var cfgJSON string
		if err := rows.Scan(&chType, &enabled, &cfgJSON); err != nil {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal([]byte(cfgJSON), &cfg)
		// Mask passwords
		if pw, ok := cfg["password"]; ok && pw != nil {
			if s, ok := pw.(string); ok && len(s) > 0 {
				cfg["password"] = "******"
			}
		}
		channels[chType] = map[string]any{"enabled": enabled == 1, "config": cfg}
	}
	writeJSON(w, http.StatusOK, "ok", channels)
}

func (a *App) putNotifyChannel(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	var payload struct {
		ChannelType string         `json:"channel_type"`
		Enabled     bool           `json:"enabled"`
		Config      map[string]any `json:"config"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	chType := strings.TrimSpace(payload.ChannelType)
	if chType != "email" && chType != "gotify" && chType != "bark" {
		writeJSON(w, http.StatusBadRequest, "不支持的通知渠道类型", nil)
		return
	}

	cfgJSON, _ := json.Marshal(payload.Config)

	now := time.Now().UTC()
	_, err = a.db.Exec(`INSERT INTO notification_channels (user_id, channel_type, enabled, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, channel_type) DO UPDATE SET
		enabled=excluded.enabled, config_json=excluded.config_json, updated_at=excluded.updated_at`,
		user.ID, chType, boolToInt(payload.Enabled), string(cfgJSON), now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存通知渠道失败", nil)
		return
	}

	a.writeAuditLog(&user.ID, "user", "update_notify_channel", "notification_channels", chType, "更新通知渠道: "+chType, nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

func (a *App) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	var payload struct {
		ChannelType string `json:"channel_type"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	err = a.sendNotification(user.ID, payload.ChannelType, "Apparition 通知测试", "这是一条测试通知。如果您收到了此消息，说明通知渠道配置正确。")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "发送测试通知失败: "+err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, "ok", nil)
}

// --- Notification dispatch ---

func (a *App) sendUserNotifications(userID int64, title, body string) {
	rows, err := a.db.Query(`SELECT channel_type, config_json FROM notification_channels WHERE user_id = ? AND enabled = 1`, userID)
	if err != nil {
		log.Printf("查询用户通知渠道失败 user=%d: %v", userID, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var chType, cfgJSON string
		if err := rows.Scan(&chType, &cfgJSON); err != nil {
			continue
		}
		if err := a.dispatchNotification(chType, cfgJSON, title, body); err != nil {
			log.Printf("发送通知失败 user=%d channel=%s: %v", userID, chType, err)
		}
	}
}

func (a *App) sendNotification(userID int64, channelType, title, body string) error {
	var cfgJSON string
	err := a.db.QueryRow(`SELECT config_json FROM notification_channels WHERE user_id = ? AND channel_type = ?`,
		userID, channelType).Scan(&cfgJSON)
	if err == sql.ErrNoRows {
		return fmt.Errorf("未配置该通知渠道")
	}
	if err != nil {
		return err
	}
	return a.dispatchNotification(channelType, cfgJSON, title, body)
}

func (a *App) dispatchNotification(channelType, cfgJSON, title, body string) error {
	switch channelType {
	case "email":
		var cfg emailNotifyConfig
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			return fmt.Errorf("解析邮件配置失败: %w", err)
		}
		return a.sendNotifyEmail(cfg, title, body)
	case "gotify":
		var cfg gotifyNotifyConfig
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			return fmt.Errorf("解析 Gotify 配置失败: %w", err)
		}
		return sendGotifyNotification(cfg, title, body)
	case "bark":
		var cfg barkNotifyConfig
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			return fmt.Errorf("解析 Bark 配置失败: %w", err)
		}
		return sendBarkNotification(cfg, title, body)
	default:
		return fmt.Errorf("不支持的渠道类型: %s", channelType)
	}
}

func (a *App) sendNotifyEmail(cfg emailNotifyConfig, subject, body string) error {
	msg := buildMIMEMessage(cfg.FromName, cfg.FromEmail, cfg.Username, subject, body,
		"<html><body><p>"+body+"</p></body></html>")

	smtpCfg := SMTPSection{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		Username: cfg.Username, Password: cfg.Password,
		FromEmail: cfg.FromEmail, TLSMode: cfg.TLSMode,
	}
	oldSMTP := a.cfg.SMTP
	a.cfg.SMTP = smtpCfg
	err := a.sendSMTPMail(cfg.Username, msg)
	a.cfg.SMTP = oldSMTP
	return err
}

func sendGotifyNotification(cfg gotifyNotifyConfig, title, body string) error {
	url := strings.TrimRight(cfg.ServerURL, "/") + "/message?token=" + cfg.AppToken
	payload, _ := json.Marshal(map[string]any{"title": title, "message": body, "priority": 5})
	resp, err := http.Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Gotify 返回状态码 %d", resp.StatusCode)
	}
	return nil
}

func sendBarkNotification(cfg barkNotifyConfig, title, body string) error {
	url := strings.TrimRight(cfg.ServerURL, "/") + "/" + cfg.DeviceKey + "/" + title + "/" + body
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Bark 返回状态码 %d", resp.StatusCode)
	}
	return nil
}
