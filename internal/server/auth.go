package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName      = "apparition_session"
	emailCodeTTLMinutes    = 10
	emailResendCooldownSec = 60
)

type authUser struct {
	ID                 int64
	Email              string
	Nickname           string
	Role               string
	Status             string
	MustChangePassword bool
}

// --- Handlers ---

func (a *App) handleSendRegisterCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	var payload struct {
		Email string `json:"email"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	email := strings.TrimSpace(strings.ToLower(payload.Email))
	if err := validateEmail(email); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	now := time.Now().UTC()
	canSend, wait, err := a.canSendVerificationCode(email, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "检查验证码频率失败", nil)
		return
	}
	if !canSend {
		writeJSON(w, http.StatusTooManyRequests, "发送过于频繁", map[string]any{"retry_after_seconds": wait})
		return
	}

	code, err := generateNumericCode(6)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "生成验证码失败", nil)
		return
	}

	codeHash := hashVerificationCode(code, email)
	expireAt := now.Add(emailCodeTTLMinutes * time.Minute)
	clientIP := a.extractClientIP(r)

	_, err = a.db.Exec(`INSERT INTO email_verifications (email, code_hash, expire_at, request_ip, created_at)
		VALUES (?, ?, ?, ?, ?)`, email, codeHash, expireAt, clientIP, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存验证码失败", nil)
		return
	}

	if err := a.sendRegisterCodeEmail(email, code, now, clientIP); err != nil {
		writeJSON(w, http.StatusInternalServerError, "发送验证码失败", nil)
		return
	}
	a.writeAuditLog(nil, "system", "send_register_code", "email", email, "发送注册验证码", nil)
	writeJSON(w, http.StatusOK, "ok", map[string]any{"expire_at": expireAt})
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	var payload struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		EmailCode string `json:"emailCode"`
		Nickname  string `json:"nickname"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	email := strings.TrimSpace(strings.ToLower(payload.Email))
	if err := validateEmail(email); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := validatePassword(payload.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	code := strings.TrimSpace(payload.EmailCode)
	if len(code) != 6 {
		writeJSON(w, http.StatusBadRequest, "验证码格式错误", nil)
		return
	}

	now := time.Now().UTC()
	ok, err := a.consumeVerificationCode(email, code, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "校验验证码失败", nil)
		return
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, "验证码错误或已过期", nil)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "密码处理失败", nil)
		return
	}
	nickname := strings.TrimSpace(payload.Nickname)
	if nickname == "" {
		nickname = strings.Split(email, "@")[0]
	}

	result, err := a.db.Exec(`INSERT INTO users (email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (?, ?, ?, 'user', 0, 'active', ?, ?)`, email, string(passwordHash), nickname, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, "邮箱已注册", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "创建用户失败", nil)
		return
	}

	userID, _ := result.LastInsertId()
	a.writeAuditLog(&userID, "user", "register", "users", fmt.Sprintf("%d", userID), "用户完成注册", map[string]any{"email": email})

	// Auto-create default email notification channel
	a.createDefaultEmailNotifyChannel(userID, email)

	writeJSON(w, http.StatusOK, "ok", map[string]any{"id": userID, "email": email, "nickname": nickname})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	var payload struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	email := strings.TrimSpace(strings.ToLower(payload.Email))
	user, passwordHash, err := a.loadUserByEmail(email)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusUnauthorized, "邮箱或密码错误", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "登录失败", nil)
		return
	}
	if user.Status != "active" {
		writeJSON(w, http.StatusForbidden, "用户状态不可用", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(payload.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, "邮箱或密码错误", nil)
		return
	}

	ttl := a.sessionTTL(payload.RememberMe)
	token, tokenHash, err := generateSessionToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建会话失败", nil)
		return
	}
	now := time.Now().UTC()
	expireAt := now.Add(ttl)
	_, err = a.db.Exec(`INSERT INTO sessions (user_id, token_hash, user_agent, client_ip, expire_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, user.ID, tokenHash, trimTo(r.UserAgent(), 512), a.extractClientIP(r), expireAt, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建会话失败", nil)
		return
	}
	_, _ = a.db.Exec(`UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?`, now, now, user.ID)
	a.writeAuditLog(&user.ID, "user", "login", "sessions", tokenHash, "用户登录成功", map[string]any{"ip": a.extractClientIP(r)})

	// Send login notification (async)
	go a.sendUserNotifications(user.ID, notifyEventLogin, "新设备登录通知",
		fmt.Sprintf("您的账号 %s 于 %s 从 IP %s 登录。如非本人操作，请及时修改密码。",
			user.Email, now.Format("2006-01-02 15:04:05"), a.extractClientIP(r)))

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expireAt,
	})
	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"id": user.ID, "email": user.Email, "nickname": user.Nickname,
		"role": user.Role, "must_change_password": user.MustChangePassword, "expire_at": expireAt,
	})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		user, _, userErr := a.currentUserFromRequest(r)
		tokenHash := hashSessionToken(cookie.Value)
		_, _ = a.db.Exec(`UPDATE sessions SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, time.Now().UTC(), tokenHash)
		if userErr == nil {
			a.writeAuditLog(&user.ID, "user", "logout", "sessions", tokenHash, "用户退出登录", nil)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0),
	})
	writeJSON(w, http.StatusOK, "ok", nil)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"id": user.ID, "email": user.Email, "nickname": user.Nickname,
		"role": user.Role, "must_change_password": user.MustChangePassword,
	})
}

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

	var currentHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, user.ID).Scan(&currentHash); err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询用户失败", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(payload.CurrentPassword)) != nil {
		writeJSON(w, http.StatusBadRequest, "当前密码错误", nil)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(payload.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "密码处理失败", nil)
		return
	}
	now := time.Now().UTC()
	_, err = a.db.Exec(`UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?`, string(newHash), now, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "更新密码失败", nil)
		return
	}

	// If admin, sync password hash back to config file
	if user.Role == "admin" {
		a.cfg.Admin.PasswordHash = string(newHash)
		a.cfg.Admin.MustChangePassword = false
		_ = saveServerConfig(a.paths.ConfigPath, a.cfg)
	}

	a.writeAuditLog(&user.ID, "user", "change_password", "users", fmt.Sprintf("%d", user.ID), "修改密码", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

// --- Session helpers ---

func (a *App) currentUserFromRequest(r *http.Request) (authUser, int64, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return authUser{}, 0, fmt.Errorf("missing session")
	}
	return a.loadUserBySessionToken(cookie.Value)
}

func (a *App) loadUserBySessionToken(token string) (authUser, int64, error) {
	tokenHash := hashSessionToken(token)
	var (
		user         authUser
		sessionID    int64
		mustChangePW int
	)
	err := a.db.QueryRow(`
		SELECT s.id, u.id, u.email, u.nickname, u.role, u.status, u.must_change_password
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expire_at > ?
		LIMIT 1`, tokenHash, time.Now().UTC()).Scan(
		&sessionID, &user.ID, &user.Email, &user.Nickname, &user.Role, &user.Status, &mustChangePW)
	if err != nil {
		return authUser{}, 0, err
	}
	user.MustChangePassword = mustChangePW == 1
	if user.Status != "active" {
		return authUser{}, 0, fmt.Errorf("user inactive")
	}
	return user, sessionID, nil
}

func (a *App) loadUserByEmail(email string) (authUser, string, error) {
	var (
		user         authUser
		mustChangePW int
		passwordHash string
	)
	err := a.db.QueryRow(`SELECT id, email, nickname, role, status, must_change_password, password_hash
		FROM users WHERE email = ? LIMIT 1`, email).Scan(
		&user.ID, &user.Email, &user.Nickname, &user.Role, &user.Status, &mustChangePW, &passwordHash)
	if err != nil {
		return authUser{}, "", err
	}
	user.MustChangePassword = mustChangePW == 1
	return user, passwordHash, nil
}

func (a *App) sessionTTL(rememberMe bool) time.Duration {
	if rememberMe {
		days := a.cfg.Security.RememberMeTTLDays
		if days <= 0 {
			days = 7
		}
		return time.Duration(days) * 24 * time.Hour
	}
	hours := a.cfg.Security.SessionTTLHours
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

// --- Verification code helpers ---

func (a *App) canSendVerificationCode(email string, now time.Time) (bool, int64, error) {
	var createdAt sql.NullTime
	err := a.db.QueryRow(`SELECT created_at FROM email_verifications
		WHERE email = ? ORDER BY created_at DESC LIMIT 1`, email).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return true, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !createdAt.Valid {
		return true, 0, nil
	}
	nextAllowed := createdAt.Time.Add(emailResendCooldownSec * time.Second)
	if now.Before(nextAllowed) {
		return false, int64(nextAllowed.Sub(now).Seconds()) + 1, nil
	}
	return true, 0, nil
}

func (a *App) consumeVerificationCode(email, code string, now time.Time) (bool, error) {
	rows, err := a.db.Query(`SELECT id, code_hash, expire_at FROM email_verifications
		WHERE email = ? AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 5`, email)
	if err != nil {
		return false, err
	}

	incoming := hashVerificationCode(code, email)
	var selectedID int64
	for rows.Next() {
		var (
			id       int64
			codeHash string
			expireAt time.Time
		)
		if err := rows.Scan(&id, &codeHash, &expireAt); err != nil {
			rows.Close()
			return false, err
		}
		if now.After(expireAt) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(codeHash), []byte(incoming)) == 1 {
			selectedID = id
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	// Close rows BEFORE executing the UPDATE to release the DB connection.
	// With MaxOpenConns(1), the Exec below would deadlock if rows still held the conn.
	rows.Close()

	if selectedID == 0 {
		return false, nil
	}

	result, err := a.db.Exec(`UPDATE email_verifications SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, now, selectedID)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

// --- Crypto helpers ---

func validateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("邮箱不能为空")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("邮箱格式错误")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("密码长度不能小于 8")
	}
	return nil
}

func generateNumericCode(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = byte('0' + (buf[i] % 10))
	}
	return string(buf), nil
}

func hashVerificationCode(code, email string) string {
	sum := sha256.Sum256([]byte(code + "|" + email))
	return hex.EncodeToString(sum[:])
}

func generateSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashSessionToken(token), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
