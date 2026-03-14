package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type notifyChannel struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
}

func (a *App) handleGetNotifyChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	channels, err := a.getNotifyChannels(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知渠道失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, "ok", channels)
}

func (a *App) handlePutNotifyEmail(w http.ResponseWriter, r *http.Request) {
	a.handlePutNotifyChannel(w, r, "email")
}

func (a *App) handlePutNotifyGotify(w http.ResponseWriter, r *http.Request) {
	a.handlePutNotifyChannel(w, r, "gotify")
}

func (a *App) handlePutNotifyBark(w http.ResponseWriter, r *http.Request) {
	a.handlePutNotifyChannel(w, r, "bark")
}

func (a *App) handlePutNotifyChannel(w http.ResponseWriter, r *http.Request, channelType string) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	var payload map[string]any
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	enabled := parseEnabled(payload)

	if err := a.validateNotifyConfig(channelType, payload); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	configJSON, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存通知配置失败", nil)
		return
	}

	now := time.Now().UTC()
	_, err = a.db.Exec(`
		INSERT INTO notification_channels (user_id, channel_type, enabled, config_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, channel_type)
		DO UPDATE SET enabled = excluded.enabled, config_json = excluded.config_json, updated_at = excluded.updated_at
	`, user.ID, channelType, boolToInt(enabled), string(configJSON), now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存通知配置失败", nil)
		return
	}
	a.writeAuditLog(&user.ID, "user", "update_notify_channel", "notification_channels", channelType, "用户更新了通知渠道配置", map[string]any{"channel": channelType, "enabled": enabled})

	writeJSON(w, http.StatusOK, "ok", map[string]any{"channel": channelType})
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
		Channel string `json:"channel"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	channel := strings.ToLower(strings.TrimSpace(payload.Channel))
	if channel != "email" && channel != "gotify" && channel != "bark" {
		writeJSON(w, http.StatusBadRequest, "channel 必须是 email/gotify/bark", nil)
		return
	}
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "Apparition 测试通知"
	}
	body := strings.TrimSpace(payload.Body)
	if body == "" {
		body = "这是一条测试通知。"
	}

	channelConfig, enabled, err := a.loadNotifyChannelConfig(user.ID, channel)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知配置失败", nil)
		return
	}
	if !enabled {
		writeJSON(w, http.StatusBadRequest, "该通知渠道未启用", nil)
		return
	}

	err = a.sendTestNotification(r.Context(), user.Email, channel, channelConfig, title, body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "发送测试通知失败", map[string]any{"error": err.Error()})
		return
	}
	a.writeAuditLog(&user.ID, "user", "test_notify_channel", "notification_channels", channel, "用户发送了测试通知", nil)

	writeJSON(w, http.StatusOK, "ok", map[string]any{"channel": channel})
}

func (a *App) getNotifyChannels(userID int64) (map[string]notifyChannel, error) {
	result := map[string]notifyChannel{
		"email":  {Enabled: false, Config: map[string]any{}},
		"gotify": {Enabled: false, Config: map[string]any{}},
		"bark":   {Enabled: false, Config: map[string]any{}},
	}

	rows, err := a.db.Query(`
		SELECT channel_type, enabled, config_json
		FROM notification_channels
		WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			channelType string
			enabledInt  int
			configRaw   string
		)
		if err := rows.Scan(&channelType, &enabledInt, &configRaw); err != nil {
			return nil, err
		}
		config := map[string]any{}
		if configRaw != "" {
			_ = json.Unmarshal([]byte(configRaw), &config)
		}
		result[channelType] = notifyChannel{Enabled: enabledInt == 1, Config: config}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (a *App) loadNotifyChannelConfig(userID int64, channelType string) (map[string]any, bool, error) {
	var (
		enabledInt int
		configRaw  string
	)
	err := a.db.QueryRow(`
		SELECT enabled, config_json
		FROM notification_channels
		WHERE user_id = ? AND channel_type = ?
		LIMIT 1
	`, userID, channelType).Scan(&enabledInt, &configRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}
	config := map[string]any{}
	if configRaw != "" {
		if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
			return nil, false, err
		}
	}
	return config, enabledInt == 1, nil
}

func (a *App) validateNotifyConfig(channelType string, payload map[string]any) error {
	switch channelType {
	case "email":
		toEmail, _ := payload["to_email"].(string)
		if strings.TrimSpace(toEmail) == "" {
			return fmt.Errorf("to_email 不能为空")
		}
		if err := validateEmail(strings.TrimSpace(toEmail)); err != nil {
			return err
		}
	case "gotify":
		serverURL, _ := payload["server_url"].(string)
		appToken, _ := payload["app_token"].(string)
		if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(appToken) == "" {
			return fmt.Errorf("gotify 配置不完整")
		}
	case "bark":
		serverURL, _ := payload["server_url"].(string)
		deviceKey, _ := payload["device_key"].(string)
		if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(deviceKey) == "" {
			return fmt.Errorf("bark 配置不完整")
		}
	default:
		return fmt.Errorf("不支持的通知渠道")
	}
	return nil
}

func parseEnabled(payload map[string]any) bool {
	value, exists := payload["enabled"]
	if !exists {
		return true
	}
	if b, ok := value.(bool); ok {
		return b
	}
	if f, ok := value.(float64); ok {
		return f != 0
	}
	return true
}

func (a *App) sendTestNotification(ctx context.Context, userEmail, channelType string, cfg map[string]any, title, body string) error {
	switch channelType {
	case "email":
		toEmail, _ := cfg["to_email"].(string)
		if strings.TrimSpace(toEmail) == "" {
			toEmail = userEmail
		}
		message := buildMIMEMessage(a.cfg.SMTP.FromName, a.cfg.SMTP.FromEmail, strings.TrimSpace(toEmail), title, body, "<p>"+body+"</p>")
		if !a.cfg.SMTP.Enabled {
			return fmt.Errorf("smtp 未启用")
		}
		return sendSMTPMail(a.cfg.SMTP, a.cfg.SMTP.FromEmail, strings.TrimSpace(toEmail), message)
	case "gotify":
		serverURL, _ := cfg["server_url"].(string)
		appToken, _ := cfg["app_token"].(string)
		priority := 5
		if v, ok := cfg["priority"].(float64); ok {
			priority = int(v)
		}
		return sendGotify(ctx, strings.TrimSpace(serverURL), strings.TrimSpace(appToken), title, body, priority)
	case "bark":
		serverURL, _ := cfg["server_url"].(string)
		deviceKey, _ := cfg["device_key"].(string)
		group, _ := cfg["group"].(string)
		sound, _ := cfg["sound"].(string)
		icon, _ := cfg["icon"].(string)
		return sendBark(ctx, strings.TrimSpace(serverURL), strings.TrimSpace(deviceKey), title, body, group, sound, icon)
	default:
		return fmt.Errorf("unsupported channel")
	}
}

func sendGotify(ctx context.Context, serverURL, appToken, title, message string, priority int) error {
	payload := map[string]any{
		"title":    title,
		"message":  message,
		"priority": priority,
	}
	body, _ := json.Marshal(payload)
	urlValue := strings.TrimRight(serverURL, "/") + "/message"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlValue, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", appToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gotify response %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func sendBark(ctx context.Context, serverURL, deviceKey, title, body, group, sound, icon string) error {
	base := strings.TrimRight(serverURL, "/") + "/" + url.PathEscape(deviceKey) + "/" + url.PathEscape(title) + "/" + url.PathEscape(body)
	query := url.Values{}
	if group != "" {
		query.Set("group", group)
	}
	if sound != "" {
		query.Set("sound", sound)
	}
	if icon != "" {
		query.Set("icon", icon)
	}
	if encoded := query.Encode(); encoded != "" {
		base = base + "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("bark response %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
