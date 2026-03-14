package server

import (
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/YingmoY/Apparition/internal/server/assets"
)

func (a *App) handlePageRoutes(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, "not found", nil)
		return
	}
	switch r.URL.Path {
	case "/":
		a.handleRootPage(w, r)
	case "/login":
		a.renderPage(w, "login.html")
	case "/register":
		a.renderPage(w, "register.html")
	case "/dashboard":
		a.handleDashboardPage(w, r)
	case "/admin":
		a.handleAdminPage(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *App) handleRootPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.currentUserFromRequest(r); err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (a *App) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.currentUserFromRequest(r); err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	a.renderPage(w, "dashboard.html")
}

func (a *App) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if user.Role != "admin" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	a.renderPage(w, "admin.html")
}

func (a *App) renderPage(w http.ResponseWriter, pageName string) {
	filePath := fmt.Sprintf("web/%s", path.Clean(pageName))
	content, err := assets.WebAssets.ReadFile(filePath)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}
