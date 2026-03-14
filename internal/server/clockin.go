package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/YingmoY/Apparition/internal/core"
)

// --- Cookie handlers ---

func (a *App) handleCookies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getCookieStatus(w, r)
	case http.MethodDelete:
		a.deleteCookies(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getCookieStatus(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	var cookieJSON string
	var valid int
	err = a.db.QueryRow(`SELECT cookie_json, valid FROM user_cookies WHERE user_id = ?`, user.ID).Scan(&cookieJSON, &valid)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, "ok", map[string]any{"exists": false, "valid": false, "cookie_count": 0})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询 Cookie 失败", nil)
		return
	}
	var cd core.CookieData
	_ = json.Unmarshal([]byte(cookieJSON), &cd)
	writeJSON(w, http.StatusOK, "ok", map[string]any{"exists": true, "valid": valid == 1, "cookie_count": len(cd.Cookies)})
}

func (a *App) deleteCookies(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	_, _ = a.db.Exec(`DELETE FROM user_cookies WHERE user_id = ?`, user.ID)
	a.writeAuditLog(&user.ID, "user", "delete_cookies", "user_cookies", formatUserID(user.ID), "删除 Cookie", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

// --- Profile handlers ---

func (a *App) handleClockinProfile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getClockinProfile(w, r)
	case http.MethodPut:
		a.putClockinProfile(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getClockinProfile(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	var (
		targetURL, inputName, address, ua, locale, acceptLang, verifyCookies, tz string
		lon, lat                                                                 float64
	)
	err = a.db.QueryRow(`SELECT target_url, input_name, longitude, latitude, formatted_address,
		user_agent, locale, accept_language, verify_cookies, timezone
		FROM clockin_profiles WHERE user_id = ?`, user.ID).Scan(
		&targetURL, &inputName, &lon, &lat, &address, &ua, &locale, &acceptLang, &verifyCookies, &tz)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, "ok", nil)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询配置失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"target_url": targetURL, "input_name": inputName,
		"longitude": lon, "latitude": lat, "formatted_address": address,
		"user_agent": ua, "locale": locale, "accept_language": acceptLang,
		"verify_cookies": verifyCookies, "timezone": tz,
	})
}

func (a *App) putClockinProfile(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	var payload struct {
		TargetURL        string  `json:"target_url"`
		InputName        string  `json:"input_name"`
		Longitude        float64 `json:"longitude"`
		Latitude         float64 `json:"latitude"`
		FormattedAddress string  `json:"formatted_address"`
		UserAgent        string  `json:"user_agent"`
		Locale           string  `json:"locale"`
		AcceptLanguage   string  `json:"accept_language"`
		VerifyCookies    string  `json:"verify_cookies"`
		Timezone         string  `json:"timezone"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.TargetURL) == "" || strings.TrimSpace(payload.InputName) == "" {
		writeJSON(w, http.StatusBadRequest, "target_url 和 input_name 不能为空", nil)
		return
	}

	now := time.Now().UTC()
	_, err = a.db.Exec(`INSERT INTO clockin_profiles (user_id, target_url, input_name, longitude, latitude, formatted_address,
		user_agent, locale, accept_language, verify_cookies, timezone, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		target_url=excluded.target_url, input_name=excluded.input_name,
		longitude=excluded.longitude, latitude=excluded.latitude,
		formatted_address=excluded.formatted_address, user_agent=excluded.user_agent,
		locale=excluded.locale, accept_language=excluded.accept_language,
		verify_cookies=excluded.verify_cookies, timezone=excluded.timezone,
		updated_at=excluded.updated_at`,
		user.ID, payload.TargetURL, payload.InputName, payload.Longitude, payload.Latitude,
		payload.FormattedAddress, payload.UserAgent, payload.Locale, payload.AcceptLanguage,
		payload.VerifyCookies, payload.Timezone, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存配置失败", nil)
		return
	}
	a.writeAuditLog(&user.ID, "user", "update_clockin_profile", "clockin_profiles", formatUserID(user.ID), "更新打卡配置", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

// --- Schedule (single job per user) ---

func (a *App) handleClockinSchedule(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getClockinSchedule(w, r)
	case http.MethodPut:
		a.putClockinSchedule(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getClockinSchedule(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	var (
		id        int64
		enabled   int
		cronExpr  string
		lastRunAt sql.NullTime
		createdAt time.Time
	)
	err = a.db.QueryRow(`SELECT id, enabled, cron_expr, last_run_at, created_at
		FROM clockin_jobs WHERE user_id = ?`, user.ID).Scan(&id, &enabled, &cronExpr, &lastRunAt, &createdAt)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, "ok", nil)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询定时任务失败", nil)
		return
	}
	result := map[string]any{
		"id": id, "enabled": enabled == 1, "cron_expr": cronExpr,
		"created_at": createdAt.Format(time.RFC3339),
	}
	if lastRunAt.Valid {
		result["last_run_at"] = lastRunAt.Time.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, "ok", result)
}

func (a *App) putClockinSchedule(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	var payload struct {
		Enabled  bool   `json:"enabled"`
		CronExpr string `json:"cron_expr"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	cronExpr := strings.TrimSpace(payload.CronExpr)
	if cronExpr != "" {
		if err := a.validateCronExpr(cronExpr); err != nil {
			writeJSON(w, http.StatusBadRequest, "cron 表达式无效: "+err.Error(), nil)
			return
		}
	}

	now := time.Now().UTC()
	_, err = a.db.Exec(`INSERT INTO clockin_jobs (user_id, enabled, cron_expr, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		enabled=excluded.enabled, cron_expr=excluded.cron_expr, updated_at=excluded.updated_at`,
		user.ID, boolToInt(payload.Enabled), cronExpr, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存定时任务失败", nil)
		return
	}

	a.reloadCron()

	a.writeAuditLog(&user.ID, "user", "update_clockin_schedule", "clockin_jobs", formatUserID(user.ID), "更新打卡定时任务", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

func (a *App) handleClockinManualRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	runID, status, message := a.executeClockinRun(user.ID, "manual")
	writeJSON(w, http.StatusOK, "ok", map[string]any{"run_id": runID, "status": status, "message": message})
}

// --- Run history ---

func (a *App) handleClockinRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	p := parsePagination(r)
	var total int
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM clockin_runs WHERE user_id = ?`, user.ID).Scan(&total)

	rows, err := a.db.Query(`SELECT id, trigger_type, status, message, started_at, finished_at, run_date
		FROM clockin_runs WHERE user_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`, user.ID, p.PageSize, p.Offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询执行记录失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id                                    int64
			triggerType, status, message, runDate string
			startedAt, finishedAt                 time.Time
		)
		if err := rows.Scan(&id, &triggerType, &status, &message, &startedAt, &finishedAt, &runDate); err != nil {
			continue
		}
		items = append(items, map[string]any{
			"id": id, "trigger_type": triggerType, "status": status, "message": message,
			"started_at": startedAt.Format(time.RFC3339), "finished_at": finishedAt.Format(time.RFC3339),
			"run_date": runDate,
		})
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total,
		"page": p.Page, "page_size": p.PageSize,
	})
}

// --- Core execution logic ---

func (a *App) executeClockinRun(userID int64, triggerType string) (int64, string, string) {
	startedAt := time.Now().UTC()
	runDate := startedAt.Format("20060102")

	cfg, err := a.loadCoreConfigFromProfile(userID)
	if err != nil {
		return a.insertClockinRun(userID, triggerType, "failed", "加载打卡配置失败: "+err.Error(), startedAt, runDate)
	}

	cookieData, err := a.loadCookieDataFromDB(userID)
	if err != nil {
		return a.insertClockinRun(userID, triggerType, "failed", "加载 Cookie 失败: "+err.Error(), startedAt, runDate)
	}

	client, err := core.NewClockInClient(cfg, cookieData)
	if err != nil {
		return a.insertClockinRun(userID, triggerType, "failed", "创建打卡客户端失败: "+err.Error(), startedAt, runDate)
	}

	result := client.Run()

	status := "failed"
	if result.Success {
		status = "success"
	}
	runID, finalStatus, finalMsg := a.insertClockinRun(userID, triggerType, status, result.Message, startedAt, runDate)

	_, _ = a.db.Exec(`UPDATE clockin_jobs SET last_run_at = ?, updated_at = ? WHERE user_id = ?`, time.Now().UTC(), time.Now().UTC(), userID)

	title := "打卡成功"
	if status == "failed" {
		title = "打卡失败"
	}
	go a.sendUserNotifications(userID, title, result.Message)

	return runID, finalStatus, finalMsg
}

func (a *App) insertClockinRun(userID int64, triggerType, status, message string, startedAt time.Time, runDate string) (int64, string, string) {
	finishedAt := time.Now().UTC()
	result, err := a.db.Exec(`INSERT INTO clockin_runs (user_id, job_id, trigger_type, status, message, started_at, finished_at, run_date)
		VALUES (?, NULL, ?, ?, ?, ?, ?, ?)`, userID, triggerType, status, message, startedAt, finishedAt, runDate)
	if err != nil {
		log.Printf("插入执行记录失败: %v", err)
		return 0, status, message
	}
	runID, _ := result.LastInsertId()
	return runID, status, message
}

func (a *App) loadCoreConfigFromProfile(userID int64) (core.Config, error) {
	var cfg core.Config
	err := a.db.QueryRow(`SELECT target_url, input_name, longitude, latitude, formatted_address,
		user_agent, locale, accept_language, verify_cookies
		FROM clockin_profiles WHERE user_id = ?`, userID).Scan(
		&cfg.TargetURL, &cfg.InputName, &cfg.Longitude, &cfg.Latitude, &cfg.FormattedAddress,
		&cfg.UserAgent, &cfg.Locale, &cfg.AcceptLanguage, &cfg.VerifyCookies)
	if err != nil {
		return core.Config{}, fmt.Errorf("未找到打卡配置: %w", err)
	}
	return cfg, nil
}

func (a *App) loadCookieDataFromDB(userID int64) (core.CookieData, error) {
	var cookieJSON string
	var valid int
	err := a.db.QueryRow(`SELECT cookie_json, valid FROM user_cookies WHERE user_id = ?`, userID).Scan(&cookieJSON, &valid)
	if err != nil {
		return core.CookieData{}, fmt.Errorf("未找到 Cookie: %w", err)
	}
	if valid != 1 {
		return core.CookieData{}, fmt.Errorf("Cookie 已失效")
	}
	var cd core.CookieData
	if err := json.Unmarshal([]byte(cookieJSON), &cd); err != nil {
		return core.CookieData{}, fmt.Errorf("解析 Cookie 失败: %w", err)
	}
	return cd, nil
}
