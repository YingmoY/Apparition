package server

import (
	"encoding/json"
	"net/http"
	"time"
)

type apiResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	TS        int64  `json:"ts"`
	Data      any    `json:"data,omitempty"`
}

type bootstrapStatus struct {
	IsFirstRun              bool `json:"is_first_run"`
	AdminMustChangePassword bool `json:"admin_must_change_password"`
	DBReady                 bool `json:"db_ready"`
	SMTPEnabled             bool `json:"smtp_enabled"`
}

func (a *App) router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.HandleFunc("/api/v1/system/bootstrap/status", a.handleBootstrapStatus)
	mux.HandleFunc("/api/v1/auth/email/send", a.handleSendRegisterCode)
	mux.HandleFunc("/api/v1/auth/register", a.handleRegister)
	mux.HandleFunc("/api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", a.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", a.handleMe)
	mux.HandleFunc("/api/v1/auth/change-password", a.handleChangePassword)
	mux.HandleFunc("/api/v1/cookies", a.handleCookies)
	mux.HandleFunc("/api/v1/audit/logs", a.handleAuditLogs)
	mux.HandleFunc("/api/v1/clockin/profile", a.handleClockinProfile)
	mux.HandleFunc("/api/v1/clockin/jobs", a.handleClockinJobs)
	mux.HandleFunc("/api/v1/clockin/jobs/", a.handleClockinJobActions)
	mux.HandleFunc("/api/v1/clockin/runs", a.handleClockinRuns)
	mux.HandleFunc("/api/v1/notify/channels", a.handleGetNotifyChannels)
	mux.HandleFunc("/api/v1/notify/events", a.handleNotificationEvents)
	mux.HandleFunc("/api/v1/notify/deliveries", a.handleNotificationDeliveries)
	mux.HandleFunc("/api/v1/notify/channels/email", a.handlePutNotifyEmail)
	mux.HandleFunc("/api/v1/notify/channels/gotify", a.handlePutNotifyGotify)
	mux.HandleFunc("/api/v1/notify/channels/bark", a.handlePutNotifyBark)
	mux.HandleFunc("/api/v1/notify/test", a.handleNotifyTest)
	mux.HandleFunc("/api/v1/admin/auth/login", a.handleAdminLogin)
	mux.HandleFunc("/api/v1/admin/users", a.handleAdminUsers)
	mux.HandleFunc("/api/v1/admin/runs", a.handleAdminRuns)
	mux.HandleFunc("/api/v1/admin/logs", a.handleAdminLogs)
	mux.HandleFunc("/api/v1/wps/sessions", a.handleWPSLoginSessionRoutes)
	mux.HandleFunc("/api/v1/wps/sessions/", a.handleWPSLoginSessionRoutes)
	mux.HandleFunc("/", a.handlePageRoutes)

	return loggingMiddleware(mux)
}

func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "ok", map[string]any{"status": "ok"})
}

func (a *App) handleBootstrapStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "ok", bootstrapStatus{
		IsFirstRun:              a.state.FirstRun,
		AdminMustChangePassword: a.state.AdminMustChangePassword,
		DBReady:                 a.state.DBReady,
		SMTPEnabled:             a.cfg.SMTP.Enabled,
	})
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	user, hash, err := a.loadUserByEmail(payload.Username)
	if err != nil || user.Role != "admin" || !verifyAdminPassword(payload.Password, hash) {
		writeJSON(w, http.StatusUnauthorized, "用户名或密码错误", nil)
		return
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"must_change_password": user.MustChangePassword,
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(apiResponse{
		Code:      statusCode,
		Message:   message,
		RequestID: "",
		TS:        time.Now().UnixMilli(),
		Data:      data,
	})
}
