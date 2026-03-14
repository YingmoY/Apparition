package server

import (
	"fmt"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
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
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if err := validatePassword(payload.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	currentUser, currentHash, err := a.loadUserByEmail(user.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取用户失败", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(payload.CurrentPassword)) != nil {
		writeJSON(w, http.StatusUnauthorized, "当前密码错误", nil)
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(payload.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "密码处理失败", nil)
		return
	}
	_, err = a.db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?`, string(newHash), time.Now().UTC(), currentUser.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "更新密码失败", nil)
		return
	}
	if currentUser.Role == "admin" && currentUser.Email == a.cfg.Admin.Username {
		a.cfg.Admin.PasswordHash = string(newHash)
		a.cfg.Admin.MustChangePassword = false
		a.state.AdminMustChangePassword = false
		if err := saveServerConfig(a.paths.ConfigPath, a.cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, fmt.Sprintf("密码已更新，但写回配置失败: %v", err), nil)
			return
		}
	}
	id := currentUser.ID
	a.writeAuditLog(&id, "user", "change_password", "users", fmt.Sprintf("%d", currentUser.ID), "用户修改了密码", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}
