package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestHandleAuditLogsOnlyReturnsCurrentUser(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := openDatabase(dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()
	if err := migrateDatabase(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	now := time.Now().UTC()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec failed: %v", err)
		}
	}

	mustExec(`INSERT INTO users (id, email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (1, 'u1@example.com', 'x', 'u1', 'user', 0, 'active', ?, ?)
	`, now, now)
	mustExec(`INSERT INTO users (id, email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (2, 'u2@example.com', 'x', 'u2', 'user', 0, 'active', ?, ?)
	`, now, now)

	token := "token-user-1"
	mustExec(`INSERT INTO sessions (user_id, token_hash, user_agent, client_ip, expire_at, revoked_at, created_at)
		VALUES (1, ?, 'ua', '127.0.0.1', ?, NULL, ?)
	`, hashSessionToken(token), now.Add(24*time.Hour), now)

	mustExec(`INSERT INTO audit_logs (user_id, actor_type, action, target_type, target_id, summary, detail_json, request_id, created_at)
		VALUES (1, 'user', 'mine', 'users', '1', 'mine', '{}', '', ?)
	`, now)
	mustExec(`INSERT INTO audit_logs (user_id, actor_type, action, target_type, target_id, summary, detail_json, request_id, created_at)
		VALUES (NULL, 'system', 'global', 'email_verifications', '-', 'global', '{}', '', ?)
	`, now)
	mustExec(`INSERT INTO audit_logs (user_id, actor_type, action, target_type, target_id, summary, detail_json, request_id, created_at)
		VALUES (2, 'user', 'other', 'users', '2', 'other', '{}', '', ?)
	`, now)

	app := &App{db: db}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs?page=1&pageSize=20", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	app.handleAuditLogs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(resp.Data.Items) != 1 {
		t.Fatalf("expected exactly 1 audit item for current user, got %d: %s", len(resp.Data.Items), rr.Body.String())
	}
	action, _ := resp.Data.Items[0]["action"].(string)
	if action != "mine" {
		t.Fatalf("expected action mine, got %q full=%s", action, fmt.Sprintf("%v", resp.Data.Items[0]))
	}
}
