package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// --- Notification channel config types ---

// Email channel: user only configures recipient address; SMTP uses global config.
type emailNotifyConfig struct {
	RecipientEmail string `json:"recipient_email"`
}

type gotifyNotifyConfig struct {
	ServerURL string `json:"server_url"`
	AppToken  string `json:"app_token"`
}

type barkNotifyConfig struct {
	ServerURL string `json:"server_url"`
	DeviceKey string `json:"device_key"`
}

type userChannelDispatch struct {
	userID      int64
	channelType string
	cfgJSON     string
}

type broadcastNotifyResult struct {
	UsersWithChannels   int            `json:"users_with_channels"`
	AttemptCount        int            `json:"attempt_count"`
	SuccessCount        int            `json:"success_count"`
	FailCount           int            `json:"fail_count"`
	ChannelTypeAttempts map[string]int `json:"channel_type_attempts"`
	ChannelTypeSuccess  map[string]int `json:"channel_type_success"`
	ChannelTypeFail     map[string]int `json:"channel_type_fail"`
}

// --- Notification event types ---
const (
	notifyEventLogin          = "login"
	notifyEventClockinSuccess = "clockin_success"
	notifyEventClockinFailed  = "clockin_failed"
)

// --- DB table: notification_channels ---
// channel_type: "email", "gotify", "bark"
// config_json: JSON of the above config structs
// notify_events: comma-separated event types, e.g. "login,clockin_success,clockin_failed"

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

	rows, err := a.db.Query(`SELECT channel_type, enabled, config_json, notify_events FROM notification_channels WHERE user_id = ?`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询通知渠道失败", nil)
		return
	}
	defer rows.Close()

	channels := make(map[string]any)
	for rows.Next() {
		var chType string
		var enabled int
		var cfgJSON, notifyEvents string
		if err := rows.Scan(&chType, &enabled, &cfgJSON, &notifyEvents); err != nil {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal([]byte(cfgJSON), &cfg)
		channels[chType] = map[string]any{
			"enabled":       enabled == 1,
			"config":        cfg,
			"notify_events": notifyEvents,
		}
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
		ChannelType  string         `json:"channel_type"`
		Enabled      bool           `json:"enabled"`
		Config       map[string]any `json:"config"`
		NotifyEvents string         `json:"notify_events"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	chType := strings.TrimSpace(payload.ChannelType)
	if chType != "email" && chType != "gotify" && chType != "bark" {
		writeJSON(w, http.StatusBadRequest, "不支持的通知渠道类型", nil)
		return
	}

	// Validate notify_events
	events := normalizeNotifyEvents(payload.NotifyEvents)

	cfgJSON, _ := json.Marshal(payload.Config)

	now := time.Now().UTC()
	_, err = a.db.Exec(`INSERT INTO notification_channels (user_id, channel_type, enabled, config_json, notify_events, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, channel_type) DO UPDATE SET
		enabled=excluded.enabled, config_json=excluded.config_json, notify_events=excluded.notify_events, updated_at=excluded.updated_at`,
		user.ID, chType, boolToInt(payload.Enabled), string(cfgJSON), events, now, now)
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

// sendUserNotifications sends a notification of a specific event to all enabled channels that subscribe to that event.
func (a *App) sendUserNotifications(userID int64, eventType, title, body string) {
	rows, err := a.db.Query(`SELECT channel_type, config_json, notify_events FROM notification_channels WHERE user_id = ? AND enabled = 1`, userID)
	if err != nil {
		log.Printf("查询用户通知渠道失败 user=%d: %v", userID, err)
		return
	}

	// Collect all channels first, then close rows to release the DB connection
	// before doing slow network I/O (SMTP/HTTP). With MaxOpenConns(1), holding
	// rows open during dispatch would block all other DB operations.
	type channelInfo struct {
		chType       string
		cfgJSON      string
		notifyEvents string
	}
	var channels []channelInfo
	for rows.Next() {
		var ci channelInfo
		if err := rows.Scan(&ci.chType, &ci.cfgJSON, &ci.notifyEvents); err != nil {
			continue
		}
		channels = append(channels, ci)
	}
	rows.Close()

	for _, ci := range channels {
		if !eventSubscribed(ci.notifyEvents, eventType) {
			continue
		}
		if err := a.dispatchNotification(ci.chType, ci.cfgJSON, title, body); err != nil {
			log.Printf("发送通知失败 user=%d channel=%s event=%s: %v", userID, ci.chType, eventType, err)
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

func (a *App) collectAllUserEnabledChannels() ([]userChannelDispatch, int, error) {
	rows, err := a.db.Query(`SELECT nc.user_id, nc.channel_type, nc.config_json
		FROM notification_channels nc
		JOIN users u ON u.id = nc.user_id
		WHERE nc.enabled = 1 AND u.status = 'active'`)
	if err != nil {
		return nil, 0, err
	}

	channels := make([]userChannelDispatch, 0)
	users := make(map[int64]struct{})
	for rows.Next() {
		var ch userChannelDispatch
		if err := rows.Scan(&ch.userID, &ch.channelType, &ch.cfgJSON); err != nil {
			continue
		}
		users[ch.userID] = struct{}{}
		channels = append(channels, ch)
	}
	rows.Close()

	return channels, len(users), nil
}

func (a *App) broadcastNotificationToAllChannels(title, body string) (broadcastNotifyResult, error) {
	result := broadcastNotifyResult{
		ChannelTypeAttempts: make(map[string]int),
		ChannelTypeSuccess:  make(map[string]int),
		ChannelTypeFail:     make(map[string]int),
	}

	channels, usersWithChannels, err := a.collectAllUserEnabledChannels()
	if err != nil {
		return result, err
	}
	result.UsersWithChannels = usersWithChannels

	for _, ch := range channels {
		result.AttemptCount++
		result.ChannelTypeAttempts[ch.channelType]++

		if err := a.dispatchNotification(ch.channelType, ch.cfgJSON, title, body); err != nil {
			result.FailCount++
			result.ChannelTypeFail[ch.channelType]++
			log.Printf("全员通知发送失败 user=%d channel=%s: %v", ch.userID, ch.channelType, err)
			continue
		}

		result.SuccessCount++
		result.ChannelTypeSuccess[ch.channelType]++
	}

	return result, nil
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
	if !a.cfg.SMTP.Enabled {
		log.Printf("[DEV] 通知邮件 to=%s subject=%s", cfg.RecipientEmail, subject)
		return nil
	}
	to := cfg.RecipientEmail
	if to == "" {
		return fmt.Errorf("收件人邮箱未配置")
	}
	msg := buildMIMEMessage(a.cfg.SMTP.FromName, a.cfg.SMTP.FromEmail, to, subject, body,
		"<html><body><p>"+body+"</p></body></html>")
	return a.sendSMTPMail(to, msg)
}

// httpNotifyClient is a shared HTTP client with a timeout for notification calls.
// Using http.DefaultClient (no timeout) risks hanging indefinitely on unresponsive
// Gotify/Bark servers, which could delay cron job completion and block goroutines.
var httpNotifyClient = &http.Client{Timeout: 15 * time.Second}

func sendGotifyNotification(cfg gotifyNotifyConfig, title, body string) error {
	url := strings.TrimRight(cfg.ServerURL, "/") + "/message?token=" + cfg.AppToken
	payload, _ := json.Marshal(map[string]any{"title": title, "message": body, "priority": 5})
	resp, err := httpNotifyClient.Post(url, "application/json", strings.NewReader(string(payload)))
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
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	if serverURL == "" {
		serverURL = "https://api.day.app"
	}
	deviceKey := strings.TrimSpace(cfg.DeviceKey)
	if deviceKey == "" {
		return fmt.Errorf("Bark Device Key 未配置")
	}

	payload, err := json.Marshal(map[string]string{
		"device_key": deviceKey,
		"title":      title,
		"body":       body,
	})
	if err != nil {
		return fmt.Errorf("序列化 Bark 请求失败: %w", err)
	}

	resp, err := httpNotifyClient.Post(serverURL+"/push", "application/json; charset=utf-8", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Bark 返回状态码 %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var apiResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if len(respBody) > 0 && json.Unmarshal(respBody, &apiResp) == nil && apiResp.Code != 0 && apiResp.Code != 200 {
		if strings.TrimSpace(apiResp.Message) == "" {
			apiResp.Message = strings.TrimSpace(string(respBody))
		}
		return fmt.Errorf("Bark 返回错误 code=%d message=%s", apiResp.Code, apiResp.Message)
	}

	return nil
}

// --- Helper: auto-create default email notification channel for a new user ---

func (a *App) createDefaultEmailNotifyChannel(userID int64, email string) {
	cfg := emailNotifyConfig{RecipientEmail: email}
	cfgJSON, _ := json.Marshal(cfg)
	events := "login,clockin_success,clockin_failed"
	now := time.Now().UTC()
	_, err := a.db.Exec(`INSERT INTO notification_channels (user_id, channel_type, enabled, config_json, notify_events, created_at, updated_at)
		VALUES (?, 'email', 1, ?, ?, ?, ?)
		ON CONFLICT(user_id, channel_type) DO NOTHING`,
		userID, string(cfgJSON), events, now, now)
	if err != nil {
		log.Printf("创建默认邮件通知渠道失败 user=%d: %v", userID, err)
	}
}

// --- Helper: event subscription check ---

func normalizeNotifyEvents(raw string) string {
	valid := map[string]bool{
		notifyEventLogin:          true,
		notifyEventClockinSuccess: true,
		notifyEventClockinFailed:  true,
	}
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if valid[s] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "login,clockin_success,clockin_failed"
	}
	return strings.Join(out, ",")
}

func eventSubscribed(notifyEvents, eventType string) bool {
	if notifyEvents == "" {
		return true // backward compat: if empty, subscribe to all
	}
	for _, s := range strings.Split(notifyEvents, ",") {
		if strings.TrimSpace(s) == eventType {
			return true
		}
	}
	return false
}
