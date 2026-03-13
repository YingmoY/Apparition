package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/YingmoY/Apparition/internal/core"
)

type clockinProfilePayload struct {
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

type clockinJobPayload struct {
	Enabled       bool   `json:"enabled"`
	ScheduleType  string `json:"schedule_type"`
	ScheduleValue string `json:"schedule_value"`
	RetryPolicy   string `json:"retry_policy"`
}

func (a *App) handleClockinProfile(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.getClockinProfile(w, user.ID)
	case http.MethodPut:
		a.putClockinProfile(w, r, user.ID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) handleClockinJobs(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.getClockinJobs(w, user.ID)
	case http.MethodPost:
		a.createClockinJob(w, r, user.ID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) handleClockinJobActions(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.currentUserFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "未登录", nil)
		return
	}

	id, action, ok := parseClockinJobPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusBadRequest, "invalid path", nil)
		return
	}

	switch {
	case action == "run" && r.Method == http.MethodPost:
		a.runClockinJobManually(w, user.ID, id)
	case action == "" && r.Method == http.MethodPut:
		a.updateClockinJob(w, r, user.ID, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, "method not allowed", nil)
	}
}

func (a *App) getClockinProfile(w http.ResponseWriter, userID int64) {
	var p clockinProfilePayload
	err := a.db.QueryRow(`
		SELECT target_url, input_name, longitude, latitude, formatted_address, user_agent,
		       locale, accept_language, verify_cookies, timezone
		FROM clockin_profiles
		WHERE user_id = ?
		LIMIT 1
	`, userID).Scan(&p.TargetURL, &p.InputName, &p.Longitude, &p.Latitude, &p.FormattedAddress, &p.UserAgent, &p.Locale, &p.AcceptLanguage, &p.VerifyCookies, &p.Timezone)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusOK, "ok", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "读取打卡配置失败", nil)
		return
	}

	writeJSON(w, http.StatusOK, "ok", p)
}

func (a *App) putClockinProfile(w http.ResponseWriter, r *http.Request, userID int64) {
	var payload clockinProfilePayload
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.TargetURL) == "" || strings.TrimSpace(payload.InputName) == "" {
		writeJSON(w, http.StatusBadRequest, "target_url 和 input_name 不能为空", nil)
		return
	}
	if strings.TrimSpace(payload.FormattedAddress) == "" {
		writeJSON(w, http.StatusBadRequest, "formatted_address 不能为空", nil)
		return
	}
	if strings.TrimSpace(payload.UserAgent) == "" {
		payload.UserAgent = "Mozilla/5.0"
	}
	if strings.TrimSpace(payload.Locale) == "" {
		payload.Locale = "zh-CN"
	}
	if strings.TrimSpace(payload.AcceptLanguage) == "" {
		payload.AcceptLanguage = "zh-CN,zh;q=0.9"
	}
	if strings.TrimSpace(payload.VerifyCookies) == "" {
		payload.VerifyCookies = "enable"
	}
	if strings.TrimSpace(payload.Timezone) == "" {
		payload.Timezone = "Asia/Shanghai"
	}

	now := time.Now().UTC()
	_, err := a.db.Exec(`
		INSERT INTO clockin_profiles (
			user_id, target_url, input_name, longitude, latitude, formatted_address, user_agent,
			locale, accept_language, verify_cookies, timezone, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id)
		DO UPDATE SET
			target_url = excluded.target_url,
			input_name = excluded.input_name,
			longitude = excluded.longitude,
			latitude = excluded.latitude,
			formatted_address = excluded.formatted_address,
			user_agent = excluded.user_agent,
			locale = excluded.locale,
			accept_language = excluded.accept_language,
			verify_cookies = excluded.verify_cookies,
			timezone = excluded.timezone,
			updated_at = excluded.updated_at
	`, userID, payload.TargetURL, payload.InputName, payload.Longitude, payload.Latitude, payload.FormattedAddress, payload.UserAgent,
		payload.Locale, payload.AcceptLanguage, payload.VerifyCookies, payload.Timezone, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "保存打卡配置失败", nil)
		return
	}

	writeJSON(w, http.StatusOK, "ok", nil)
}

func (a *App) getClockinJobs(w http.ResponseWriter, userID int64) {
	rows, err := a.db.Query(`
		SELECT id, enabled, schedule_type, schedule_value, next_run_at, last_run_at, retry_policy, created_at, updated_at
		FROM clockin_jobs
		WHERE user_id = ?
		ORDER BY id DESC
	`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "读取任务失败", nil)
		return
	}
	defer rows.Close()

	jobs := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id            int64
			enabled       int
			scheduleType  string
			scheduleValue string
			nextRunAt     time.Time
			lastRunAt     sql.NullTime
			retryPolicy   string
			createdAt     time.Time
			updatedAt     time.Time
		)
		if err := rows.Scan(&id, &enabled, &scheduleType, &scheduleValue, &nextRunAt, &lastRunAt, &retryPolicy, &createdAt, &updatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, "读取任务失败", nil)
			return
		}
		jobs = append(jobs, map[string]any{
			"id":             id,
			"enabled":        enabled == 1,
			"schedule_type":  scheduleType,
			"schedule_value": scheduleValue,
			"next_run_at":    nextRunAt,
			"last_run_at":    nullableTime(lastRunAt),
			"retry_policy":   retryPolicy,
			"created_at":     createdAt,
			"updated_at":     updatedAt,
		})
	}

	writeJSON(w, http.StatusOK, "ok", jobs)
}

func (a *App) createClockinJob(w http.ResponseWriter, r *http.Request, userID int64) {
	var payload clockinJobPayload
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if err := validateJobPayload(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	nextRunAt, err := calcNextRunAt(payload.ScheduleType, payload.ScheduleValue, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	now := time.Now().UTC()
	result, err := a.db.Exec(`
		INSERT INTO clockin_jobs (user_id, enabled, schedule_type, schedule_value, next_run_at, last_run_at, retry_policy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?)
	`, userID, boolToInt(payload.Enabled), payload.ScheduleType, payload.ScheduleValue, nextRunAt.UTC(), payload.RetryPolicy, now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "创建任务失败", nil)
		return
	}

	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusOK, "ok", map[string]any{"id": id, "next_run_at": nextRunAt.UTC()})
}

func (a *App) updateClockinJob(w http.ResponseWriter, r *http.Request, userID, jobID int64) {
	var payload clockinJobPayload
	if !decodeJSONBody(w, r, &payload) {
		return
	}
	if err := validateJobPayload(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	nextRunAt, err := calcNextRunAt(payload.ScheduleType, payload.ScheduleValue, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	result, err := a.db.Exec(`
		UPDATE clockin_jobs
		SET enabled = ?, schedule_type = ?, schedule_value = ?, next_run_at = ?, retry_policy = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, boolToInt(payload.Enabled), payload.ScheduleType, payload.ScheduleValue, nextRunAt.UTC(), payload.RetryPolicy, time.Now().UTC(), jobID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "更新任务失败", nil)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, "任务不存在", nil)
		return
	}
	writeJSON(w, http.StatusOK, "ok", map[string]any{"id": jobID, "next_run_at": nextRunAt.UTC()})
}

func (a *App) runClockinJobManually(w http.ResponseWriter, userID, jobID int64) {
	var exists int
	err := a.db.QueryRow(`SELECT 1 FROM clockin_jobs WHERE id = ? AND user_id = ? LIMIT 1`, jobID, userID).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, "任务不存在", nil)
			return
		}
		writeJSON(w, http.StatusInternalServerError, "读取任务失败", nil)
		return
	}

	runID, status, message := a.executeClockinRun(userID, &jobID, "manual")
	writeJSON(w, http.StatusOK, "ok", map[string]any{"run_id": runID, "status": status, "message": message})
}

func (a *App) executeClockinRun(userID int64, jobID *int64, triggerType string) (int64, string, string) {
	started := time.Now().UTC()

	cfg, err := a.loadCoreConfigFromProfile(userID)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", fmt.Sprintf("加载打卡配置失败: %v", err), started)
	}
	cookieData, err := a.loadCookieDataFromDB(userID)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", fmt.Sprintf("加载 Cookie 失败: %v", err), started)
	}
	client, err := core.NewClockInClient(cfg, cookieData)
	if err != nil {
		return a.insertClockinRun(userID, jobID, triggerType, "failed", fmt.Sprintf("初始化打卡客户端失败: %v", err), started)
	}

	result := client.Run()
	status := "success"
	if !result.Success {
		status = "failed"
	}
	return a.insertClockinRun(userID, jobID, triggerType, status, result.Message, started)
}

func (a *App) insertClockinRun(userID int64, jobID *int64, triggerType, status, message string, started time.Time) (int64, string, string) {
	finished := time.Now().UTC()
	runDate := started.Format("20060102")
	jobValue := any(nil)
	if jobID != nil {
		jobValue = *jobID
	}
	result, err := a.db.Exec(`
		INSERT INTO clockin_runs (user_id, job_id, trigger_type, status, message, started_at, finished_at, run_date, error_code, extra_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, '{}')
	`, userID, jobValue, triggerType, status, trimTo(message, 512), started, finished, runDate)
	if err != nil {
		return 0, "failed", "写入执行记录失败"
	}
	runID, _ := result.LastInsertId()
	return runID, status, message
}

func (a *App) loadCoreConfigFromProfile(userID int64) (core.Config, error) {
	var payload clockinProfilePayload
	err := a.db.QueryRow(`
		SELECT target_url, input_name, longitude, latitude, formatted_address, user_agent,
		       locale, accept_language, verify_cookies, timezone
		FROM clockin_profiles
		WHERE user_id = ?
		LIMIT 1
	`, userID).Scan(&payload.TargetURL, &payload.InputName, &payload.Longitude, &payload.Latitude, &payload.FormattedAddress, &payload.UserAgent, &payload.Locale, &payload.AcceptLanguage, &payload.VerifyCookies, &payload.Timezone)
	if err != nil {
		return core.Config{}, err
	}

	return core.Config{
		TargetURL:        payload.TargetURL,
		InputName:        payload.InputName,
		Longitude:        payload.Longitude,
		Latitude:         payload.Latitude,
		FormattedAddress: payload.FormattedAddress,
		UserAgent:        payload.UserAgent,
		Locale:           payload.Locale,
		AcceptLanguage:   payload.AcceptLanguage,
		VerifyCookies:    payload.VerifyCookies,
	}, nil
}

func (a *App) loadCookieDataFromDB(userID int64) (core.CookieData, error) {
	var cookieJSON string
	err := a.db.QueryRow(`
		SELECT cookie_json FROM user_cookies WHERE user_id = ? AND valid = 1 LIMIT 1
	`, userID).Scan(&cookieJSON)
	if err != nil {
		return core.CookieData{}, err
	}

	var cookieData core.CookieData
	if err := json.Unmarshal([]byte(cookieJSON), &cookieData); err != nil {
		return core.CookieData{}, err
	}
	return cookieData, nil
}

func validateJobPayload(payload *clockinJobPayload) error {
	payload.ScheduleType = strings.TrimSpace(strings.ToLower(payload.ScheduleType))
	payload.ScheduleValue = strings.TrimSpace(payload.ScheduleValue)
	payload.RetryPolicy = strings.TrimSpace(payload.RetryPolicy)
	if payload.RetryPolicy == "" {
		payload.RetryPolicy = "immediate_once"
	}
	if payload.ScheduleType != "daily" && payload.ScheduleType != "fixed_interval" {
		return fmt.Errorf("schedule_type 必须是 daily 或 fixed_interval")
	}
	if payload.ScheduleValue == "" {
		return fmt.Errorf("schedule_value 不能为空")
	}
	if payload.ScheduleType == "daily" {
		parts := strings.Split(payload.ScheduleValue, ":")
		if len(parts) != 2 {
			return fmt.Errorf("daily 类型 schedule_value 需为 HH:MM")
		}
	}
	if payload.ScheduleType == "fixed_interval" {
		if _, err := strconv.Atoi(payload.ScheduleValue); err != nil {
			return fmt.Errorf("fixed_interval 类型 schedule_value 需为秒数")
		}
	}
	return nil
}

func calcNextRunAt(scheduleType, scheduleValue string, now time.Time) (time.Time, error) {
	switch scheduleType {
	case "daily":
		parts := strings.Split(scheduleValue, ":")
		if len(parts) != 2 {
			return time.Time{}, fmt.Errorf("invalid daily schedule")
		}
		hour, err := strconv.Atoi(parts[0])
		if err != nil {
			return time.Time{}, err
		}
		minute, err := strconv.Atoi(parts[1])
		if err != nil {
			return time.Time{}, err
		}
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		return next, nil
	case "fixed_interval":
		seconds, err := strconv.Atoi(scheduleValue)
		if err != nil || seconds <= 0 {
			return time.Time{}, fmt.Errorf("invalid interval seconds")
		}
		return now.Add(time.Duration(seconds) * time.Second), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported schedule type")
	}
}

func parseClockinJobPath(pathValue string) (int64, string, bool) {
	prefix := "/api/v1/clockin/jobs/"
	if !strings.HasPrefix(pathValue, prefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(pathValue, prefix)
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return 0, "", false
	}
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	return id, action, true
}

func nullableTime(v sql.NullTime) any {
	if !v.Valid {
		return nil
	}
	return v.Time
}
