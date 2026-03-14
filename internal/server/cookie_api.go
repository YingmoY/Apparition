package server

import (
	"database/sql"
	"net/http"
)

func (a *App) handleCookies(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.getCookieStatus(w, user.ID)
	case http.MethodDelete:
		a.deleteCookies(w, user.ID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getCookieStatus(w http.ResponseWriter, userID int64) {
	var cookieJSON string
	var valid int
	var updatedAt sql.NullTime
	err := a.db.QueryRow(`SELECT cookie_json, valid, updated_at FROM user_cookies WHERE user_id = ? LIMIT 1`, userID).Scan(&cookieJSON, &valid, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusOK, "ok", map[string]any{"exists": false, "valid": false, "cookie_count": 0, "updated_at": nil})
			return
		}
		writeJSON(w, http.StatusInternalServerError, "读取 Cookie 状态失败", nil)
		return
	}
	_ = cookieJSON
	cookieData, err := a.loadCookieDataFromDB(userID)
	if err != nil {
		writeJSON(w, http.StatusOK, "ok", map[string]any{"exists": true, "valid": valid == 1, "cookie_count": 0, "updated_at": nullableTime(updatedAt)})
		return
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"exists": true, "valid": valid == 1, "cookie_count": len(cookieData.Cookies), "updated_at": nullableTime(updatedAt)})
}

func (a *App) deleteCookies(w http.ResponseWriter, userID int64) {
	_, err := a.db.Exec(`DELETE FROM user_cookies WHERE user_id = ?`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "删除 Cookie 失败", nil)
		return
	}
	id := userID
	a.writeAuditLog(&id, "user", "delete_cookies", "user_cookies", "self", "用户删除了自己的 Cookie", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}
