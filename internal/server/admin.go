package server

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
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
			id                            int64
			email, nickname, role, status string
			lastLoginAt                   sql.NullTime
			createdAt                     time.Time
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
			id                                  int64
			email, triggerType, status, message string
			startedAt, finishedAt               time.Time
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

func (a *App) handleAdminBulkClockin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	if err := a.requireAdmin(r); err != nil {
		writeJSON(w, http.StatusForbidden, "无权限", nil)
		return
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	todayStr := time.Now().In(loc).Format("20060102")

	rows, err := a.db.Query(`
		SELECT u.id, u.email FROM users u
		WHERE u.status = 'active'
		AND u.id NOT IN (
			SELECT DISTINCT user_id FROM clockin_runs
			WHERE run_date = ? AND status = 'success'
		)
		ORDER BY u.id`, todayStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询用户失败", nil)
		return
	}

	type pendingUser struct {
		id    int64
		email string
	}
	var users []pendingUser
	for rows.Next() {
		var u pendingUser
		if err := rows.Scan(&u.id, &u.email); err != nil {
			continue
		}
		users = append(users, u)
	}
	rows.Close()

	type bulkResult struct {
		UserID  int64  `json:"user_id"`
		Email   string `json:"email"`
		RunID   int64  `json:"run_id"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	var results []bulkResult
	successCount, failCount := 0, 0

	for _, u := range users {
		runID, status, message := a.executeClockinRun(u.id, "admin_bulk")
		results = append(results, bulkResult{
			UserID: u.id, Email: u.email,
			RunID: runID, Status: status, Message: message,
		})
		if status == "success" {
			successCount++
		} else {
			failCount++
		}
		log.Printf("管理员批量打卡: user=%d email=%s status=%s", u.id, u.email, status)
	}

	a.writeAuditLog(nil, "admin", "bulk_clockin", "clockin_runs", todayStr,
		fmt.Sprintf("批量打卡: 总计%d人, 成功%d, 失败%d", len(users), successCount, failCount), nil)

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"date":          todayStr,
		"total":         len(users),
		"success_count": successCount,
		"fail_count":    failCount,
		"results":       results,
	})
}

func (a *App) handleAdminBroadcastNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	admin, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	if admin.Role != "admin" {
		writeJSON(w, http.StatusForbidden, "无权限", nil)
		return
	}

	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	title := strings.TrimSpace(payload.Title)
	body := strings.TrimSpace(payload.Body)
	if title == "" {
		writeJSON(w, http.StatusBadRequest, "标题不能为空", nil)
		return
	}
	if body == "" {
		writeJSON(w, http.StatusBadRequest, "内容不能为空", nil)
		return
	}

	result, err := a.broadcastNotificationToAllChannels(title, body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "发送全员通知失败: "+err.Error(), nil)
		return
	}

	a.writeAuditLog(&admin.ID, "admin", "broadcast_notify", "notification_channels", "all_users",
		fmt.Sprintf("全员通知: 标题=%s, 尝试=%d, 成功=%d, 失败=%d", trimTo(title, 80), result.AttemptCount, result.SuccessCount, result.FailCount),
		map[string]any{
			"title":                title,
			"users_with_channels":  result.UsersWithChannels,
			"attempt_count":        result.AttemptCount,
			"success_count":        result.SuccessCount,
			"fail_count":           result.FailCount,
			"channel_type_attempts": result.ChannelTypeAttempts,
			"channel_type_success": result.ChannelTypeSuccess,
			"channel_type_fail":    result.ChannelTypeFail,
		})

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"users_with_channels":  result.UsersWithChannels,
		"attempt_count":        result.AttemptCount,
		"success_count":        result.SuccessCount,
		"fail_count":           result.FailCount,
		"channel_type_attempts": result.ChannelTypeAttempts,
		"channel_type_success": result.ChannelTypeSuccess,
		"channel_type_fail":    result.ChannelTypeFail,
	})
}
