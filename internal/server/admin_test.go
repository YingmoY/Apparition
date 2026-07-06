package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminDisableAllClockinJobs(t *testing.T) {
	db := newTestDB(t)
	app := &App{db: db}
	adminCookie := seedUserSession(t, db, 1, "admin@example.com", "admin")
	seedClockinJob(t, db, 10, 2, true, "0 30 8 * * *")
	seedClockinJob(t, db, 11, 3, true, "0 45 8 * * *")
	seedClockinJob(t, db, 12, 4, false, "0 0 9 * * *")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clockin-jobs/disable-all", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	app.handleAdminDisableAllClockinJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var enabledCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM clockin_jobs WHERE enabled = 1`).Scan(&enabledCount); err != nil {
		t.Fatalf("query enabled jobs failed: %v", err)
	}
	if enabledCount != 0 {
		t.Fatalf("expected all jobs disabled, got %d enabled", enabledCount)
	}

	var body apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data := body.Data.(map[string]any)
	if data["disabled_count"].(float64) != 2 {
		t.Fatalf("expected disabled_count 2, got %v", data["disabled_count"])
	}
}

func TestAdminToggleClockinJob(t *testing.T) {
	db := newTestDB(t)
	app := &App{db: db}
	adminCookie := seedUserSession(t, db, 1, "admin@example.com", "admin")
	seedClockinJob(t, db, 20, 2, true, "0 30 8 * * *")

	payload := bytes.NewBufferString(`{"enabled":false}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/clockin-jobs/20", payload)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()

	app.handleAdminClockinJob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM clockin_jobs WHERE id = 20`).Scan(&enabled); err != nil {
		t.Fatalf("query job failed: %v", err)
	}
	if enabled != 0 {
		t.Fatalf("expected job disabled, got enabled=%d", enabled)
	}
}

func seedUserSession(t *testing.T, db *sql.DB, userID int64, email, role string) *http.Cookie {
	t.Helper()
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, nickname, role, status, created_at, updated_at)
		VALUES (?, ?, 'hash', ?, ?, 'active', ?, ?)`, userID, email, email, role, now, now)
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}
	token, tokenHash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO sessions (user_id, token_hash, user_agent, client_ip, expire_at, created_at)
		VALUES (?, ?, 'test', '127.0.0.1', ?, ?)`, userID, tokenHash, now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: token}
}

func seedClockinJob(t *testing.T, db *sql.DB, jobID, userID int64, enabled bool, cronExpr string) {
	t.Helper()
	seedUserWithoutSession(t, db, userID)
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO clockin_jobs (id, user_id, enabled, cron_expr, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, jobID, userID, boolToInt(enabled), cronExpr, now, now)
	if err != nil {
		t.Fatalf("insert clockin job failed: %v", err)
	}
}

func seedUserWithoutSession(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	now := time.Now().UTC()
	email := formatUserID(userID) + "@example.com"
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, nickname, role, status, created_at, updated_at)
		VALUES (?, ?, 'hash', ?, 'user', 'active', ?, ?)`, userID, email, email, now, now)
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}
}
