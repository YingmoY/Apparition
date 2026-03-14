package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/YingmoY/Apparition/internal/core"
)

type wpsRuntimeSession struct {
	ID        string
	UserID    int64
	Auth      *core.WPSAuthService
	QRURL     string
	Status    string // pending, scanned, confirmed, failed
	Message   string
	CreatedAt time.Time
	mu        sync.Mutex
}

// --- Route dispatcher ---

func (a *App) handleWPSSessionRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/wps/sessions")
	path = strings.TrimPrefix(path, "/")

	// POST /api/v1/wps/sessions -> create
	if path == "" {
		if r.Method == http.MethodPost {
			a.handleCreateWPSSession(w, r)
			return
		}
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	// GET /api/v1/wps/sessions/{id}/qr
	// GET /api/v1/wps/sessions/{id}/status
	parts := strings.SplitN(path, "/", 2)
	sessionID := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "qr":
			if r.Method == http.MethodGet {
				a.handleWPSQRImage(w, r, sessionID)
				return
			}
		case "status":
			if r.Method == http.MethodGet {
				a.handleWPSSessionStatus(w, r, sessionID)
				return
			}
		}
	}
	writeJSON(w, http.StatusNotFound, "not found", nil)
}

func (a *App) handleCreateWPSSession(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	authSvc, err := core.NewWPSAuthService()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "初始化 WPS 服务失败: "+err.Error(), nil)
		return
	}
	qrURL, err := authSvc.Start()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建 WPS 登录会话失败: "+err.Error(), nil)
		return
	}

	sessionID := generateRandomID(16)
	session := &wpsRuntimeSession{
		ID:        sessionID,
		UserID:    user.ID,
		Auth:      authSvc,
		QRURL:     qrURL,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	a.wpsMu.Lock()
	a.wpsRuns[sessionID] = session
	a.wpsMu.Unlock()

	// Clean up expired sessions
	go a.cleanupExpiredWPSSessions()

	// Background: wait for scan
	go a.waitForWPSScan(session)

	a.writeAuditLog(&user.ID, "user", "create_wps_session", "wps", sessionID, "创建 WPS 登录会话", nil)
	writeJSON(w, http.StatusOK, "ok", map[string]any{"session_id": sessionID, "qr_url": qrURL})
}

func (a *App) handleWPSQRImage(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, _, err := a.currentUserFromRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	session := a.getWPSSession(sessionID)
	if session == nil {
		writeJSON(w, http.StatusNotFound, "会话不存在", nil)
		return
	}

	resp, err := http.Get(session.QRURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "获取二维码失败", nil)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, resp.Body)
}

func (a *App) handleWPSSessionStatus(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, _, err := a.currentUserFromRequest(r); err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	session := a.getWPSSession(sessionID)
	if session == nil {
		writeJSON(w, http.StatusNotFound, "会话不存在或已过期", nil)
		return
	}
	session.mu.Lock()
	status := session.Status
	message := session.Message
	session.mu.Unlock()

	writeJSON(w, http.StatusOK, "ok", map[string]any{"status": status, "message": message})
}

// --- Background WPS scan waiter ---

func (a *App) waitForWPSScan(session *wpsRuntimeSession) {
	ssid, err := session.Auth.WaitForScan(2 * time.Minute)
	if err != nil {
		a.updateWPSSessionStatus(session, "failed", "扫码超时或失败: "+err.Error())
		return
	}
	a.updateWPSSessionStatus(session, "scanned", "扫码成功，正在登录...")

	cookies, err := session.Auth.Login(ssid)
	if err != nil {
		a.updateWPSSessionStatus(session, "failed", "登录失败: "+err.Error())
		return
	}

	cookieJSON, err := json.Marshal(core.CookieData{Cookies: cookies})
	if err != nil {
		a.updateWPSSessionStatus(session, "failed", "序列化 Cookie 失败")
		return
	}

	now := time.Now().UTC()
	_, err = a.db.Exec(`INSERT INTO user_cookies (user_id, cookie_json, source, valid, updated_at)
		VALUES (?, ?, 'wps_qr', 1, ?)
		ON CONFLICT(user_id) DO UPDATE SET cookie_json=excluded.cookie_json, source='wps_qr', valid=1, updated_at=excluded.updated_at`,
		session.UserID, string(cookieJSON), now)
	if err != nil {
		a.updateWPSSessionStatus(session, "failed", "保存 Cookie 失败: "+err.Error())
		return
	}

	a.updateWPSSessionStatus(session, "confirmed", fmt.Sprintf("登录成功，获取 %d 个 Cookie", len(cookies)))
	a.writeAuditLog(&session.UserID, "user", "wps_login_success", "user_cookies", formatUserID(session.UserID), "WPS 登录成功", nil)
}

func (a *App) updateWPSSessionStatus(session *wpsRuntimeSession, status, message string) {
	session.mu.Lock()
	session.Status = status
	session.Message = message
	session.mu.Unlock()
}

func (a *App) getWPSSession(id string) *wpsRuntimeSession {
	a.wpsMu.RLock()
	defer a.wpsMu.RUnlock()
	return a.wpsRuns[id]
}

func (a *App) cleanupExpiredWPSSessions() {
	a.wpsMu.Lock()
	defer a.wpsMu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for id, s := range a.wpsRuns {
		if s.CreatedAt.Before(cutoff) {
			delete(a.wpsRuns, id)
		}
	}
}

func generateRandomID(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		log.Printf("生成随机ID失败: %v", err)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
