package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *App) writeAuditLog(userID *int64, actorType, action, targetType, targetID, summary string, detail map[string]any) {
	detailJSON := "{}"
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			detailJSON = string(b)
		}
	}
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	_, _ = a.db.Exec(`INSERT INTO audit_logs (user_id, actor_type, action, target_type, target_id, summary, detail_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uid, actorType, action, targetType, targetID, summary, detailJSON, time.Now().UTC())
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

	p := parsePagination(r)
	var total int
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM audit_logs WHERE user_id = ?`, user.ID).Scan(&total)

	rows, err := a.db.Query(`SELECT id, actor_type, action, target_type, target_id, summary, detail_json, created_at
		FROM audit_logs WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, user.ID, p.PageSize, p.Offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询审计日志失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id                                                       int64
			actorType, action, targetType, targetID, summary, detail string
			createdAt                                                time.Time
		)
		if err := rows.Scan(&id, &actorType, &action, &targetType, &targetID, &summary, &detail, &createdAt); err != nil {
			continue
		}
		items = append(items, map[string]any{
			"id": id, "actor_type": actorType, "action": action,
			"target_type": targetType, "target_id": targetID, "summary": summary,
			"detail_json": detail, "created_at": createdAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total,
		"page": p.Page, "page_size": p.PageSize,
	})
}

func (a *App) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if err := a.requireAdmin(r); err != nil {
		writeJSON(w, http.StatusForbidden, "无权限", nil)
		return
	}

	p := parsePagination(r)
	var total int
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM audit_logs`).Scan(&total)

	rows, err := a.db.Query(`SELECT id, user_id, actor_type, action, target_type, target_id, summary, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, p.PageSize, p.Offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询审计日志失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id     int64
			uid    sql.NullInt64
			atype  string
			action string
			ttype  string
			tid    string
			sum    string
			cat    time.Time
		)
		if err := rows.Scan(&id, &uid, &atype, &action, &ttype, &tid, &sum, &cat); err != nil {
			continue
		}
		item := map[string]any{
			"id": id, "actor_type": atype, "action": action,
			"target_type": ttype, "target_id": tid, "summary": sum,
			"created_at": cat.Format(time.RFC3339),
		}
		if uid.Valid {
			item["user_id"] = uid.Int64
		} else {
			item["user_id"] = nil
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total,
		"page": p.Page, "page_size": p.PageSize,
	})
}

// formatUserID returns a string form suitable for audit log target_id.
func formatUserID(id int64) string {
	return fmt.Sprintf("%d", id)
}
