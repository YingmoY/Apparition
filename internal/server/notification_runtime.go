package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) createNotificationEvent(userID int64, runID *int64, eventType, title, body, level string) int64 {
	runValue := any(nil)
	if runID != nil {
		runValue = *runID
	}
	result, err := a.db.Exec(`
		INSERT INTO notification_events (user_id, run_id, event_type, title, body, level, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, runValue, eventType, trimTo(title, 200), trimTo(body, 1000), level, time.Now().UTC())
	if err != nil {
		log.Printf("创建通知事件失败: %v", err)
		return 0
	}
	id, _ := result.LastInsertId()
	return id
}

func (a *App) dispatchNotificationEvent(userID int64, eventID int64, title, body string) {
	channels, err := a.getNotifyChannels(userID)
	if err != nil {
		log.Printf("读取通知渠道失败: %v", err)
		return
	}
	for channelType, channel := range channels {
		if !channel.Enabled {
			continue
		}
		config, enabled, err := a.loadNotifyChannelConfig(userID, channelType)
		if err != nil || !enabled {
			continue
		}
		go a.deliverNotification(context.Background(), userID, eventID, channelType, config, title, body)
	}
}

func (a *App) deliverNotification(ctx context.Context, userID, eventID int64, channelType string, cfg map[string]any, title, body string) {
	createdAt := time.Now().UTC()
	result, err := a.db.Exec(`
		INSERT INTO notification_deliveries (event_id, channel_type, status, response_code, response_body, retry_count, sent_at, created_at)
		VALUES (?, ?, 'pending', NULL, '', 0, NULL, ?)
	`, eventID, channelType, createdAt)
	if err != nil {
		log.Printf("创建通知投递记录失败: %v", err)
		return
	}
	deliveryID, _ := result.LastInsertId()

	var sendErr error
	switch channelType {
	case "email":
		toEmail, _ := cfg["to_email"].(string)
		if strings.TrimSpace(toEmail) == "" {
			toEmail = a.lookupUserEmail(userID)
		}
		html := "<p>" + body + "</p>"
		message := buildMIMEMessage(a.cfg.SMTP.FromName, a.cfg.SMTP.FromEmail, strings.TrimSpace(toEmail), title, body, html)
		if !a.cfg.SMTP.Enabled {
			sendErr = fmt.Errorf("smtp 未启用")
		} else {
			sendErr = sendSMTPMail(a.cfg.SMTP, a.cfg.SMTP.FromEmail, strings.TrimSpace(toEmail), message)
		}
	case "gotify":
		serverURL, _ := cfg["server_url"].(string)
		appToken, _ := cfg["app_token"].(string)
		priority := 5
		if v, ok := cfg["priority"].(float64); ok {
			priority = int(v)
		}
		sendErr = sendGotify(ctx, strings.TrimSpace(serverURL), strings.TrimSpace(appToken), title, body, priority)
	case "bark":
		serverURL, _ := cfg["server_url"].(string)
		deviceKey, _ := cfg["device_key"].(string)
		group, _ := cfg["group"].(string)
		sound, _ := cfg["sound"].(string)
		icon, _ := cfg["icon"].(string)
		sendErr = sendBark(ctx, strings.TrimSpace(serverURL), strings.TrimSpace(deviceKey), title, body, group, sound, icon)
	}

	status := "sent"
	responseBody := "ok"
	if sendErr != nil {
		status = "failed"
		responseBody = trimTo(sendErr.Error(), 500)
	}
	_, _ = a.db.Exec(`
		UPDATE notification_deliveries
		SET status = ?, response_body = ?, sent_at = ?
		WHERE id = ?
	`, status, responseBody, time.Now().UTC(), deliveryID)
}

func (a *App) handleNotificationEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	page, pageSize, offset := parsePagination(r)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM notification_events WHERE user_id = ?`, user.ID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知事件统计失败", nil)
		return
	}
	rows, err := a.db.Query(`
		SELECT id, run_id, event_type, title, body, level, created_at
		FROM notification_events
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, user.ID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知事件失败", nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var runID sql.NullInt64
		var eventType, title, bodyText, level string
		var createdAt time.Time
		if err := rows.Scan(&id, &runID, &eventType, &title, &bodyText, &level, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取通知事件失败", nil)
			return
		}
		items = append(items, map[string]any{"id": id, "run_id": nullableInt64(runID), "event_type": eventType, "title": title, "body": bodyText, "level": level, "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (a *App) handleNotificationDeliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	page, pageSize, offset := parsePagination(r)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM notification_deliveries d JOIN notification_events e ON e.id = d.event_id WHERE e.user_id = ?`, user.ID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知投递统计失败", nil)
		return
	}
	rows, err := a.db.Query(`
		SELECT d.id, d.event_id, d.channel_type, d.status, d.response_body, d.retry_count, d.sent_at, d.created_at
		FROM notification_deliveries d
		JOIN notification_events e ON e.id = d.event_id
		WHERE e.user_id = ?
		ORDER BY d.created_at DESC
		LIMIT ? OFFSET ?
	`, user.ID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取通知投递失败", nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, eventID int64
		var channelType, status, responseBody string
		var retryCount int
		var sentAt sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &eventID, &channelType, &status, &responseBody, &retryCount, &sentAt, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取通知投递失败", nil)
			return
		}
		items = append(items, map[string]any{"id": id, "event_id": eventID, "channel_type": channelType, "status": status, "response_body": responseBody, "retry_count": retryCount, "sent_at": nullableTime(sentAt), "created_at": createdAt})
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (a *App) lookupUserEmail(userID int64) string {
	var email string
	if err := a.db.QueryRow(`SELECT email FROM users WHERE id = ? LIMIT 1`, userID).Scan(&email); err != nil {
		return ""
	}
	return email
}

func (a *App) emitClockinNotification(userID int64, runID int64, status, message string) {
	eventType := "clockin_success"
	title := "Apparition 打卡成功"
	level := "info"
	if status == "success" && !a.cfg.NotifierDefaults.OnClockInSuccess {
		return
	}
	if status != "success" {
		if !a.cfg.NotifierDefaults.OnClockInFailed {
			return
		}
		eventType = "clockin_failed"
		title = "Apparition 打卡失败"
		level = "error"
	}
	body := message
	eventID := a.createNotificationEvent(userID, &runID, eventType, title, body, level)
	if eventID > 0 {
		a.dispatchNotificationEvent(userID, eventID, title, body)
	}
}

func (a *App) emitAuthSecurityNotification(userID int64, email, clientIP string) {
	if !a.cfg.NotifierDefaults.OnAuthSecurity {
		return
	}
	body := fmt.Sprintf("账号 %s 在 %s 从 %s 登录。", email, time.Now().Format("2006-01-02 15:04:05"), clientIP)
	eventID := a.createNotificationEvent(userID, nil, "auth_security", "Apparition 登录安全提醒", body, "info")
	if eventID > 0 {
		a.dispatchNotificationEvent(userID, eventID, "Apparition 登录安全提醒", body)
	}
}
