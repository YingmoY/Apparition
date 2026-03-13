package server

import (
	"database/sql"
	"net/http"
	"time"
)

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
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
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取用户统计失败", nil)
		return
	}

	rows, err := a.db.Query(`
		SELECT id, email, nickname, role, status, must_change_password, last_login_at, created_at, updated_at
		FROM users
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取用户列表失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id           int64
			email        string
			nickname     string
			role         string
			status       string
			mustChangePW int
			lastLoginAt  sql.NullTime
			createdAt    time.Time
			updatedAt    time.Time
		)
		if err := rows.Scan(&id, &email, &nickname, &role, &status, &mustChangePW, &lastLoginAt, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取用户列表失败", nil)
			return
		}
		items = append(items, map[string]any{
			"id":                   id,
			"email":                email,
			"nickname":             nickname,
			"role":                 role,
			"status":               status,
			"must_change_password": mustChangePW == 1,
			"last_login_at":        nullableTime(lastLoginAt),
			"created_at":           createdAt,
			"updated_at":           updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (a *App) handleAdminRuns(w http.ResponseWriter, r *http.Request) {
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
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM clockin_runs`).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取运行统计失败", nil)
		return
	}

	rows, err := a.db.Query(`
		SELECT r.id, r.user_id, u.email, r.job_id, r.trigger_type, r.status, r.message, r.started_at, r.finished_at, r.run_date
		FROM clockin_runs r
		JOIN users u ON u.id = r.user_id
		ORDER BY r.started_at DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取运行记录失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id          int64
			userID      int64
			email       string
			jobID       sql.NullInt64
			triggerType string
			status      string
			message     string
			startedAt   time.Time
			finishedAt  time.Time
			runDate     string
		)
		if err := rows.Scan(&id, &userID, &email, &jobID, &triggerType, &status, &message, &startedAt, &finishedAt, &runDate); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取运行记录失败", nil)
			return
		}
		items = append(items, map[string]any{
			"id":           id,
			"user_id":      userID,
			"email":        email,
			"job_id":       nullableInt64(jobID),
			"trigger_type": triggerType,
			"status":       status,
			"message":      message,
			"started_at":   startedAt,
			"finished_at":  finishedAt,
			"run_date":     runDate,
		})
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (a *App) requireAdmin(r *http.Request) (authUser, error) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		return authUser{}, err
	}
	if user.Role != "admin" {
		return authUser{}, sql.ErrNoRows
	}
	return user, nil
}

func nullableInt64(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
