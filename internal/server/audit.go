package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func (a *App) writeAuditLog(userID *int64, actorType, action, targetType, targetID, summary string, detail any) {
	detailJSON := "{}"
	if detail != nil {
		if data, err := json.Marshal(detail); err == nil {
			detailJSON = string(data)
		}
	}
	userValue := any(nil)
	if userID != nil {
		userValue = *userID
	}
	_, _ = a.db.Exec(`
		INSERT INTO audit_logs (user_id, actor_type, action, target_type, target_id, summary, detail_json, request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
	`, userValue, actorType, action, targetType, targetID, trimTo(summary, 256), detailJSON, time.Now().UTC())
}

func (a *App) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
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
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM audit_logs WHERE user_id = ? OR user_id IS NULL`, user.ID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取日志统计失败", nil)
		return
	}
	rows, err := a.db.Query(`
		SELECT id, actor_type, action, target_type, target_id, summary, detail_json, created_at
		FROM audit_logs
		WHERE user_id = ? OR user_id IS NULL
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, user.ID, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取日志失败", nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var actorType, action, targetType, targetID, summary, detailJSON string
		var createdAt time.Time
		if err := rows.Scan(&id, &actorType, &action, &targetType, &targetID, &summary, &detailJSON, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取日志失败", nil)
			return
		}
		items = append(items, map[string]any{
			"id":          id,
			"actor_type":  actorType,
			"action":      action,
			"target_type": targetType,
			"target_id":   targetID,
			"summary":     summary,
			"detail_json": detailJSON,
			"created_at":  createdAt,
		})
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (a *App) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if _, err := a.requireAdmin(r); err != nil {
		writeJSON(w, http.StatusForbidden, "无管理员权限", nil)
		return
	}
	page, pageSize, offset := parsePagination(r)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM audit_logs`).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取日志统计失败", nil)
		return
	}
	rows, err := a.db.Query(`
		SELECT id, user_id, actor_type, action, target_type, target_id, summary, detail_json, created_at
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取日志失败", nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var userID sql.NullInt64
		var actorType, action, targetType, targetID, summary, detailJSON string
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &actorType, &action, &targetType, &targetID, &summary, &detailJSON, &createdAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取日志失败", nil)
			return
		}
		items = append(items, map[string]any{
			"id":          id,
			"user_id":     nullableInt64(userID),
			"actor_type":  actorType,
			"action":      action,
			"target_type": targetType,
			"target_id":   targetID,
			"summary":     summary,
			"detail_json": detailJSON,
			"created_at":  createdAt,
		})
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}
