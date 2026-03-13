package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YingmoY/Apparition/internal/core"
)

const wpsLoginSessionTimeout = 2 * time.Minute

type wpsRuntimeSession struct {
	UserID    int64
	Auth      *core.WPSAuthService
	QRURL     string
	Status    string
	Message   string
	CreatedAt time.Time
	ExpireAt  time.Time
	mu        sync.RWMutex
}

func (a *App) handleWPSLoginSessionRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/wps/sessions" {
		a.handleCreateWPSLoginSession(w, r)
		return
	}

	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/qr") {
		a.handleWPSQRImage(w, r)
		return
	}

	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status") {
		a.handleWPSLoginSessionStatus(w, r)
		return
	}

	writeJSON(w, http.StatusNotFound, "not found", nil)
}

func (a *App) handleCreateWPSLoginSession(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	a.cleanupExpiredWPSRuns()

	authService, err := core.NewWPSAuthService()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "初始化 WPS 登录失败", nil)
		return
	}

	qrURL, err := authService.Start()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "获取二维码失败", nil)
		return
	}

	now := time.Now().UTC()
	sessionID, err := generateRandomID(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建会话失败", nil)
		return
	}

	run := &wpsRuntimeSession{
		UserID:    user.ID,
		Auth:      authService,
		QRURL:     qrURL,
		Status:    "pending",
		Message:   "waiting for scan",
		CreatedAt: now,
		ExpireAt:  now.Add(wpsLoginSessionTimeout),
	}

	a.wpsMu.Lock()
	a.wpsRuns[sessionID] = run
	a.wpsMu.Unlock()

	_, err = a.db.Exec(`
		INSERT INTO wps_login_sessions (id, user_id, channel_id, qr_url, qr_generated_at, status, ssid, expire_at, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
	`, sessionID, user.ID, "runtime", qrURL, now, run.Status, run.ExpireAt, now, now)
	if err != nil {
		a.wpsMu.Lock()
		delete(a.wpsRuns, sessionID)
		a.wpsMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, "保存登录会话失败", nil)
		return
	}

	go a.waitForWPSScan(sessionID, run)

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"session_id": sessionID,
		"status":     "pending",
		"expires_at": run.ExpireAt,
	})
}

func (a *App) handleWPSQRImage(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	sessionID, ok := parseWPSPathParam(r.URL.Path, "qr")
	if !ok {
		writeJSON(w, http.StatusBadRequest, "invalid path", nil)
		return
	}

	run, ok := a.getWPSRun(sessionID)
	if !ok || run.UserID != user.ID {
		writeJSON(w, http.StatusNotFound, "会话不存在", nil)
		return
	}

	ts := r.URL.Query().Get("ts")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-QR-Proxy-Ts", strconv.FormatInt(time.Now().UnixMilli(), 10))
	if ts != "" {
		w.Header().Set("X-QR-Client-Ts", ts)
	}

	resp, err := http.Get(run.QRURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "二维码代理失败", nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, "二维码源站异常", nil)
		return
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "image/png"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

func (a *App) handleWPSLoginSessionStatus(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	sessionID, ok := parseWPSPathParam(r.URL.Path, "status")
	if !ok {
		writeJSON(w, http.StatusBadRequest, "invalid path", nil)
		return
	}

	run, ok := a.getWPSRun(sessionID)
	if ok && run.UserID == user.ID {
		run.mu.RLock()
		status := run.Status
		message := run.Message
		expiresAt := run.ExpireAt
		run.mu.RUnlock()
		writeJSON(w, http.StatusOK, "ok", map[string]any{
			"session_id": sessionID,
			"status":     status,
			"message":    message,
			"expires_at": expiresAt,
		})
		return
	}

	var (
		dbUserID int64
		status   string
		updated  time.Time
	)
	err = a.db.QueryRow(`
		SELECT user_id, status, updated_at
		FROM wps_login_sessions
		WHERE id = ?
		LIMIT 1
	`, sessionID).Scan(&dbUserID, &status, &updated)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, "会话不存在", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "读取会话状态失败", nil)
		return
	}
	if dbUserID != user.ID {
		writeJSON(w, http.StatusNotFound, "会话不存在", nil)
		return
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"session_id": sessionID,
		"status":     status,
		"updated_at": updated,
	})
}

func (a *App) waitForWPSScan(sessionID string, run *wpsRuntimeSession) {
	ssid, err := run.Auth.WaitForScan(wpsLoginSessionTimeout)
	if err != nil {
		a.updateWPSRunStatus(sessionID, run, "failed", err.Error())
		return
	}

	cookies, err := run.Auth.Login(ssid)
	if err != nil {
		a.updateWPSRunStatus(sessionID, run, "failed", err.Error())
		return
	}

	cookieJSON, err := json.Marshal(map[string]any{"cookies": cookies})
	if err != nil {
		a.updateWPSRunStatus(sessionID, run, "failed", "cookie serialization failed")
		return
	}

	now := time.Now().UTC()
	_, err = a.db.Exec(`
		INSERT INTO user_cookies (user_id, cookie_json, source, valid, updated_at)
		VALUES (?, ?, 'wps_qr', 1, ?)
		ON CONFLICT(user_id)
		DO UPDATE SET cookie_json = excluded.cookie_json, source = excluded.source, valid = excluded.valid, updated_at = excluded.updated_at
	`, run.UserID, string(cookieJSON), now)
	if err != nil {
		a.updateWPSRunStatus(sessionID, run, "failed", "save cookies failed")
		return
	}

	a.updateWPSRunStatus(sessionID, run, "confirmed", "login success")
}

func (a *App) updateWPSRunStatus(sessionID string, run *wpsRuntimeSession, status, message string) {
	run.mu.Lock()
	run.Status = status
	run.Message = trimTo(message, 256)
	run.mu.Unlock()

	_, _ = a.db.Exec(`
		UPDATE wps_login_sessions
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, time.Now().UTC(), sessionID)
}

func (a *App) getWPSRun(sessionID string) (*wpsRuntimeSession, bool) {
	a.wpsMu.RLock()
	run, ok := a.wpsRuns[sessionID]
	a.wpsMu.RUnlock()
	return run, ok
}

func (a *App) cleanupExpiredWPSRuns() {
	now := time.Now().UTC()
	a.wpsMu.Lock()
	for id, run := range a.wpsRuns {
		run.mu.RLock()
		expired := now.After(run.ExpireAt)
		run.mu.RUnlock()
		if expired {
			delete(a.wpsRuns, id)
		}
	}
	a.wpsMu.Unlock()
}

func parseWPSPathParam(path, suffix string) (string, bool) {
	prefix := "/api/v1/wps/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, prefix)
	trimmed = strings.TrimSuffix(trimmed, "/"+suffix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func generateRandomID(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("invalid random id length")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
