package server

import (
	"database/sql"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func ensureDefaultAdmin(db *sql.DB, cfg ServerConfig) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO users (email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (?, ?, ?, 'admin', ?, 'active', ?, ?)`,
		cfg.Admin.Username, cfg.Admin.PasswordHash, cfg.Admin.Username,
		boolToInt(cfg.Admin.MustChangePassword), now, now)
	return err
}

func verifyAdminPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *App) requireAdmin(r *http.Request) error {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		return err
	}
	if user.Role != "admin" {
		return http.ErrAbortHandler
	}
	return nil
}

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
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
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&total)

	rows, err := a.db.Query(`SELECT id, email, nickname, role, status, last_login_at, created_at
		FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`, p.PageSize, p.Offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询用户失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id                                   int64
			email, nickname, role, status         string
			lastLoginAt                          sql.NullTime
			createdAt                            time.Time
		)
		if err := rows.Scan(&id, &email, &nickname, &role, &status, &lastLoginAt, &createdAt); err != nil {
			continue
		}
		item := map[string]any{
			"id": id, "email": email, "nickname": nickname, "role": role,
			"status": status, "created_at": createdAt.Format(time.RFC3339),
		}
		if lastLoginAt.Valid {
			item["last_login_at"] = lastLoginAt.Time.Format(time.RFC3339)
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total, "page": p.Page, "page_size": p.PageSize,
	})
}

func (a *App) handleAdminRuns(w http.ResponseWriter, r *http.Request) {
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
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM clockin_runs`).Scan(&total)

	rows, err := a.db.Query(`SELECT r.id, u.email, r.trigger_type, r.status, r.message, r.started_at, r.finished_at
		FROM clockin_runs r JOIN users u ON u.id = r.user_id
		ORDER BY r.started_at DESC LIMIT ? OFFSET ?`, p.PageSize, p.Offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询执行记录失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id                                      int64
			email, triggerType, status, message      string
			startedAt, finishedAt                    time.Time
		)
		if err := rows.Scan(&id, &email, &triggerType, &status, &message, &startedAt, &finishedAt); err != nil {
			continue
		}
		items = append(items, map[string]any{
			"id": id, "email": email, "trigger_type": triggerType, "status": status, "message": message,
			"started_at": startedAt.Format(time.RFC3339), "finished_at": finishedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total, "page": p.Page, "page_size": p.PageSize,
	})
}
