package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRenderLoginPage(t *testing.T) {
	app := &App{}
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()

	app.handlePageRoutes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRootRedirectToLoginWithoutSession(t *testing.T) {
	app := &App{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	app.handlePageRoutes(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected location /login, got %s", loc)
	}
}
