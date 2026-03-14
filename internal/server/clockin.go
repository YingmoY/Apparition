package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

// --- Job handlers ---

func (a *App) handleClockinJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getClockinJobs(w, r)
	case http.MethodPost:
		a.createClockinJob(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) handleClockinJobActions(w http.ResponseWriter, r *http.Request) {
	// /api/v1/clockin/jobs/{id} or /api/v1/clockin/jobs/{id}/run
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clockin/jobs/")
	parts := strings.SplitN(path, "/", 2)
	jobID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "无效的任务 ID", nil)
		return
	}

	if len(parts) == 2 && parts[1] == "run" && r.Method == http.MethodPost {
		a.runClockinJobManually(w, r, jobID)
		return
	}
	if r.Method == http.MethodPut {
		a.updateClockinJob(w, r, jobID)
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
}

func (a *App) getClockinJobs(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	rows, err := a.db.Query(`SELECT id, enabled, schedule_type, schedule_value, next_run_at, last_run_at, retry_policy, created_at
		FROM clockin_jobs WHERE user_id = ? ORDER BY created_at DESC`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "查询任务失败", nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id                         int64
			enabled                    int
			stype, svalue, retryPolicy string
			nextRunAt                  time.Time
			lastRunAt                  sql.NullTime
			createdAt                  time.Time
		)
		if err := rows.Scan(&id, &enabled, &stype, &svalue, &nextRunAt, &lastRunAt, &retryPolicy, &createdAt); err != nil {
			continue
		}
		item := map[string]any{
			"id": id, "enabled": enabled == 1, "schedule_type": stype, "schedule_value": svalue,
			"next_run_at": nextRunAt.Format(time.RFC3339), "retry_policy": retryPolicy,
			"created_at": createdAt.Format(time.RFC3339),
		}
		if lastRunAt.Valid {
			item["last_run_at"] = lastRunAt.Time.Format(time.RFC3339)
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, "ok", items)
}

func (a *App) createClockinJob(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	var payload struct {
		Enabled       bool   `json:"enabled"`
		ScheduleType  string `json:"schedule_type"`
		ScheduleValue string `json:"schedule_value"`
		RetryPolicy   string `json:"retry_policy"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if err := validateJobPayload(payload.ScheduleType, payload.ScheduleValue); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if payload.RetryPolicy == "" {
		payload.RetryPolicy = "immediate_once"
	}

	now := time.Now().UTC()
	nextRun, err := calcNextRunAt(payload.ScheduleType, payload.ScheduleValue, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "计算调度时间失败: "+err.Error(), nil)
		return
	}

	result, err := a.db.Exec(`INSERT INTO clockin_jobs (user_id, enabled, schedule_type, schedule_value, next_run_at, retry_policy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, boolToInt(payload.Enabled), payload.ScheduleType, payload.ScheduleValue, nextRun.UTC(), payload.RetryPolicy, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建任务失败", nil)
		return
	}
	jobID, _ := result.LastInsertId()
	a.writeAuditLog(&user.ID, "user", "create_clockin_job", "clockin_jobs", fmt.Sprintf("%d", jobID), "创建打卡任务", nil)
	writeJSON(w, http.StatusOK, "ok", map[string]any{"id": jobID, "next_run_at": nextRun.Format(time.RFC3339)})
}

func (a *App) updateClockinJob(w http.ResponseWriter, r *http.Request, jobID int64) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	// Verify ownership
	var ownerID int64
	if err := a.db.QueryRow(`SELECT user_id FROM clockin_jobs WHERE id = ?`, jobID).Scan(&ownerID); err != nil {
		writeJSON(w, http.StatusNotFound, "任务不存在", nil)
		return
	}
	if ownerID != user.ID {
		writeJSON(w, http.StatusForbidden, "无权限", nil)
		return
	}

	var payload struct {
		Enabled       *bool  `json:"enabled"`
		ScheduleType  string `json:"schedule_type"`
		ScheduleValue string `json:"schedule_value"`
	}
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	now := time.Now().UTC()
	if payload.ScheduleType != "" && payload.ScheduleValue != "" {
		if err := validateJobPayload(payload.ScheduleType, payload.ScheduleValue); err != nil {
			writeJSON(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		nextRun, err := calcNextRunAt(payload.ScheduleType, payload.ScheduleValue, now)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, "计算调度时间失败", nil)
			return
		}
		_, err = a.db.Exec(`UPDATE clockin_jobs SET schedule_type = ?, schedule_value = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
			payload.ScheduleType, payload.ScheduleValue, nextRun.UTC(), now, jobID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, "更新任务失败", nil)
			return
		}
	}
	if payload.Enabled != nil {
		_, _ = a.db.Exec(`UPDATE clockin_jobs SET enabled = ?, updated_at = ? WHERE id = ?`, boolToInt(*payload.Enabled), now, jobID)
	}

	a.writeAuditLog(&user.ID, "user", "update_clockin_job", "clockin_jobs", fmt.Sprintf("%d", jobID), "更新打卡任务", nil)
	writeJSON(w, http.StatusOK, "ok", nil)
}

func (a *App) runClockinJobManually(w http.ResponseWriter, r *http.Request, jobID int64) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}
	var ownerID int64
	if err := a.db.QueryRow(`SELECT user_id FROM clockin_jobs WHERE id = ?`, jobID).Scan(&ownerID); err != nil {
		writeJSON(w, http.StatusNotFound, "任务不存在", nil)
		return
	}
	if ownerID != user.ID {
		writeJSON(w, http.StatusForbidden, "无权限", nil)
		return
	}

	runID, status, message := a.executeClockinRun(user.ID, &jobID, "manual")
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

	rows, err := a.db.Query(`SELECT id, job_id, trigger_type, status, message, started_at, finished_at, run_date
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
			jobID                                 sql.NullInt64
			triggerType, status, message, runDate string
			startedAt, finishedAt                 time.Time
		)
		if err := rows.Scan(&id, &jobID, &triggerType, &status, &message, &startedAt, &finishedAt, &runDate); err != nil {
			continue
		}
		item := map[string]any{
			"id": id, "trigger_type": triggerType, "status": status, "message": message,
			"started_at": startedAt.Format(time.RFC3339), "finished_at": finishedAt.Format(time.RFC3339),
			"run_date": runDate,
		}
		if jobID.Valid {
			item["job_id"] = jobID.Int64
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, "ok", map[string]any{
		"items": items, "total": total,
		"page": p.Page, "page_size": p.PageSize,
	})
}

// --- Core execution logic ---

func (a *App) executeClockinRun(userID int64, jobID *int64, triggerType string) (int64, string, string) {
	startedAt := time.Now().UTC()
	runDate := startedAt.Format("20060102")

	cfg, err := a.loadCoreConfigFromProfile(userID)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", "加载打卡配置失败: "+err.Error(), startedAt, runDate)
	}

	cookieData, err := a.loadCookieDataFromDB(userID)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", "加载 Cookie 失败: "+err.Error(), startedAt, runDate)
	}

	client, err := core.NewClockInClient(cfg, cookieData)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", "创建打卡客户端失败: "+err.Error(), startedAt, runDate)
	}

	result := client.Run()

	status := "failed"
	if result.Success {
		status = "success"
	}
	return a.insertClockinRun(userID, jobID, triggerType, status, result.Message, startedAt, runDate)
}

func (a *App) insertClockinRun(userID int64, jobID *int64, triggerType, status, message string, startedAt time.Time, runDate string) (int64, string, string) {
	finishedAt := time.Now().UTC()
	var jid sql.NullInt64
	if jobID != nil {
		jid = sql.NullInt64{Int64: *jobID, Valid: true}
	}
	result, err := a.db.Exec(`INSERT INTO clockin_runs (user_id, job_id, trigger_type, status, message, started_at, finished_at, run_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, jid, triggerType, status, message, startedAt, finishedAt, runDate)
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

// --- Schedule helpers ---

func validateJobPayload(scheduleType, scheduleValue string) error {
	switch scheduleType {
	case "daily":
		parts := strings.Split(scheduleValue, ":")
		if len(parts) != 2 {
			return fmt.Errorf("daily 类型格式为 HH:MM")
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return fmt.Errorf("时间格式错误")
		}
	case "fixed_interval":
		sec, err := strconv.Atoi(scheduleValue)
		if err != nil || sec < 60 {
			return fmt.Errorf("fixed_interval 最小间隔 60 秒")
		}
	default:
		return fmt.Errorf("不支持的调度类型: %s", scheduleType)
	}
	return nil
}

func calcNextRunAt(scheduleType, scheduleValue string, now time.Time) (time.Time, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	switch scheduleType {
	case "daily":
		parts := strings.Split(scheduleValue, ":")
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), h, m, 0, 0, loc)
		if today.Before(now) {
			today = today.Add(24 * time.Hour)
		}
		return today.UTC(), nil
	case "fixed_interval":
		sec, _ := strconv.Atoi(scheduleValue)
		return now.Add(time.Duration(sec) * time.Second), nil
	default:
		return time.Time{}, fmt.Errorf("unknown schedule type: %s", scheduleType)
	}
}
