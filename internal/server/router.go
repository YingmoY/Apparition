package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TS      int64  `json:"ts"`
	Data    any    `json:"data,omitempty"`
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (a *App) router() http.Handler {
	mux := http.NewServeMux()

	// system
	mux.HandleFunc("/healthz", a.handleHealthz)

	// auth
	mux.HandleFunc("/api/v1/auth/email/send", a.handleSendRegisterCode)
	mux.HandleFunc("/api/v1/auth/register", a.handleRegister)
	mux.HandleFunc("/api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", a.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", a.handleMe)
	mux.HandleFunc("/api/v1/auth/change-password", a.handleChangePassword)

	// cookies
	mux.HandleFunc("/api/v1/cookies", a.handleCookies)

	// clockin
	mux.HandleFunc("/api/v1/clockin/profile", a.handleClockinProfile)
	mux.HandleFunc("/api/v1/clockin/schedule", a.handleClockinSchedule)
	mux.HandleFunc("/api/v1/clockin/run", a.handleClockinManualRun)
	mux.HandleFunc("/api/v1/clockin/runs", a.handleClockinRuns)

	// wps
	mux.HandleFunc("/api/v1/wps/sessions", a.handleWPSSessionRoutes)
	mux.HandleFunc("/api/v1/wps/sessions/", a.handleWPSSessionRoutes)

	// audit
	mux.HandleFunc("/api/v1/audit/logs", a.handleAuditLogs)

	// notifications
	mux.HandleFunc("/api/v1/notify/channels", a.handleNotifyChannels)
	mux.HandleFunc("/api/v1/notify/test", a.handleNotifyTest)

	// admin
	mux.HandleFunc("/api/v1/admin/users", a.handleAdminUsers)
	mux.HandleFunc("/api/v1/admin/runs", a.handleAdminRuns)
	mux.HandleFunc("/api/v1/admin/logs", a.handleAdminLogs)

	// pages
	mux.HandleFunc("/", a.handlePageRoutes)

	return loggingMiddleware(mux)
}

func (a *App) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "ok", map[string]any{"status": "ok"})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start).Truncate(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(apiResponse{
		Code:    code,
		Message: message,
		TS:      time.Now().UnixMilli(),
		Data:    data,
	})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeJSON(w, http.StatusBadRequest, "empty request body", nil)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, "invalid json payload", nil)
		return false
	}
	return true
}

func extractClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if parts := strings.SplitN(xff, ",", 2); len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

func trimTo(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

type pagination struct {
	Page     int
	PageSize int
	Offset   int
}

func parsePagination(r *http.Request) pagination {
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	size := parsePositiveInt(r.URL.Query().Get("pageSize"), 20)
	if size > 100 {
		size = 100
	}
	return pagination{Page: page, PageSize: size, Offset: (page - 1) * size}
}

func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
