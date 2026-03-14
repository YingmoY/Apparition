package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName        = "apparition_session"
	emailPurposeRegister     = "register"
	emailCodeTTLMinutes      = 10
	emailResendCooldownSec   = 60
	defaultSessionTTLHours   = 24
	defaultRememberMeTTLDays = 7
)

type authUser struct {
	ID                 int64
	Email              string
	Nickname           string
	Role               string
	Status             string
	MustChangePassword bool
}

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
	canSend, waitSeconds, err := a.canSendVerificationCode(email, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "检查验证码频率失败", nil)
		return
	}
	if !canSend {
		writeJSON(w, http.StatusTooManyRequests, "发送过于频繁", map[string]any{"retry_after_seconds": waitSeconds})
		return
	}

	code, err := generateNumericCode(6)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "生成验证码失败", nil)
		return
	}

	codeHash := hashVerificationCode(code, email, emailPurposeRegister)
	expireAt := now.Add(emailCodeTTLMinutes * time.Minute)
	clientIP := extractClientIP(r)

	_, err = a.db.Exec(`
		INSERT INTO email_verifications (email, purpose, code_hash, expire_at, consumed_at, request_ip, created_at)
		VALUES (?, ?, ?, ?, NULL, ?, ?)
	`, email, emailPurposeRegister, codeHash, expireAt, clientIP, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存验证码失败", nil)
		return
	}

	if err := a.sendRegisterCodeEmail(email, code, now, clientIP); err != nil {
		writeJSON(w, http.StatusInternalServerError, "发送验证码失败", nil)
		return
	}
	a.writeAuditLog(nil, "system", "send_register_code", "email_verifications", email, "发送注册验证码", map[string]any{"email": email})

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"expire_at": expireAt,
	})
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
	if len(strings.TrimSpace(payload.EmailCode)) != 6 {
		writeJSON(w, http.StatusBadRequest, "验证码格式错误", nil)
		return
	}

	now := time.Now().UTC()
	verificationID, ok, err := a.consumeVerificationCode(email, strings.TrimSpace(payload.EmailCode), now)
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

	result, err := a.db.Exec(`
		INSERT INTO users (email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (?, ?, ?, 'user', 0, 'active', ?, ?)
	`, email, string(passwordHash), nickname, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeJSON(w, http.StatusConflict, "邮箱已注册", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "创建用户失败", nil)
		return
	}

	_ = verificationID
	userID, _ := result.LastInsertId()
	a.writeAuditLog(&userID, "user", "register", "users", fmt.Sprintf("%d", userID), "用户完成注册", map[string]any{"email": email})
	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"id":       userID,
		"email":    email,
		"nickname": nickname,
	})
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
	_, err = a.db.Exec(`
		INSERT INTO sessions (user_id, token_hash, user_agent, client_ip, expire_at, revoked_at, created_at)
		VALUES (?, ?, ?, ?, ?, NULL, ?)
	`, user.ID, tokenHash, trimTo(payloadOrHeader(r.UserAgent(), "unknown"), 512), extractClientIP(r), expireAt, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建会话失败", nil)
		return
	}

	_, _ = a.db.Exec(`UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?`, now, now, user.ID)
	a.writeAuditLog(&user.ID, "user", "login", "sessions", tokenHash, "用户登录成功", map[string]any{"ip": extractClientIP(r)})
	a.emitAuthSecurityNotification(user.ID, user.Email, extractClientIP(r))

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		Expires:  expireAt,
	})

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"id":                   user.ID,
		"email":                user.Email,
		"nickname":             user.Nickname,
		"role":                 user.Role,
		"must_change_password": user.MustChangePassword,
		"expire_at":            expireAt,
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
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
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
		"id":                   user.ID,
		"email":                user.Email,
		"nickname":             user.Nickname,
		"role":                 user.Role,
		"must_change_password": user.MustChangePassword,
	})
}

func (a *App) canSendVerificationCode(email string, now time.Time) (bool, int64, error) {
	var createdAt sql.NullTime
	err := a.db.QueryRow(`
		SELECT created_at
		FROM email_verifications
		WHERE email = ? AND purpose = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, email, emailPurposeRegister).Scan(&createdAt)
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

func (a *App) consumeVerificationCode(email, code string, now time.Time) (int64, bool, error) {
	rows, err := a.db.Query(`
		SELECT id, code_hash, expire_at
		FROM email_verifications
		WHERE email = ? AND purpose = ? AND consumed_at IS NULL
		ORDER BY created_at DESC
		LIMIT 5
	`, email, emailPurposeRegister)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()

	incomingHash := hashVerificationCode(code, email, emailPurposeRegister)

	var selectedID int64
	for rows.Next() {
		var (
			id       int64
			codeHash string
			expireAt time.Time
		)
		if err := rows.Scan(&id, &codeHash, &expireAt); err != nil {
			return 0, false, err
		}
		if now.After(expireAt) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(codeHash), []byte(incomingHash)) == 1 {
			selectedID = id
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if selectedID == 0 {
		return 0, false, nil
	}

	result, err := a.db.Exec(`
		UPDATE email_verifications
		SET consumed_at = ?
		WHERE id = ? AND consumed_at IS NULL
	`, now, selectedID)
	if err != nil {
		return 0, false, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return 0, false, nil
	}

	return selectedID, true, nil
}

func (a *App) loadUserByEmail(email string) (authUser, string, error) {
	var (
		user         authUser
		mustChangePW int
		passwordHash string
	)

	err := a.db.QueryRow(`
		SELECT id, email, nickname, role, status, must_change_password, password_hash
		FROM users
		WHERE email = ?
		LIMIT 1
	`, email).Scan(&user.ID, &user.Email, &user.Nickname, &user.Role, &user.Status, &mustChangePW, &passwordHash)
	if err != nil {
		return authUser{}, "", err
	}
	user.MustChangePassword = mustChangePW == 1
	return user, passwordHash, nil
}

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
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expire_at > ?
		LIMIT 1
	`, tokenHash, time.Now().UTC()).Scan(&sessionID, &user.ID, &user.Email, &user.Nickname, &user.Role, &user.Status, &mustChangePW)
	if err != nil {
		return authUser{}, 0, err
	}
	user.MustChangePassword = mustChangePW == 1
	if user.Status != "active" {
		return authUser{}, 0, fmt.Errorf("user status invalid")
	}
	return user, sessionID, nil
}

func (a *App) sessionTTL(rememberMe bool) time.Duration {
	if rememberMe {
		ttlDays := a.cfg.Security.RememberMeTTLDays
		if ttlDays <= 0 {
			ttlDays = defaultRememberMeTTLDays
		}
		return time.Duration(ttlDays) * 24 * time.Hour
	}

	ttlHours := a.cfg.Security.SessionTTLHours
	if ttlHours <= 0 {
		ttlHours = defaultSessionTTLHours
	}
	return time.Duration(ttlHours) * time.Hour
}

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
	if length <= 0 {
		return "", fmt.Errorf("invalid code length")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = byte('0' + (buf[i] % 10))
	}
	return string(buf), nil
}

func hashVerificationCode(code, email, purpose string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{code, email, purpose}, "|")))
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

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "invalid request body", nil)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, "invalid json payload", nil)
		return false
	}
	return true
}

func extractClientIP(r *http.Request) string {
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	xri := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if xri != "" {
		return xri
	}
	if hostPort := strings.TrimSpace(r.RemoteAddr); hostPort != "" {
		return hostPort
	}
	return "unknown"
}

func trimTo(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func payloadOrHeader(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
