package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type cronScheduler struct {
	c  *cron.Cron
	mu sync.Mutex
}

const calibrationStartupDelay = 3 * time.Second

const schedulerQueueSize = 1024

type scheduledRun struct {
	userID      int64
	triggerType string
	scheduledAt time.Time
}

func newCronScheduler() *cronScheduler {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Printf("警告: 加载 Asia/Shanghai 时区失败: %v, 使用 UTC+8 固定偏移", err)
		loc = time.FixedZone("CST", 8*3600)
	}
	return &cronScheduler{
		c: cron.New(cron.WithLocation(loc), cron.WithSeconds()),
	}
}

func (s *cronScheduler) start() {
	s.c.Start()
}

func (s *cronScheduler) stop() {
	s.c.Stop()
}

func (a *App) initScheduler() {
	a.cron = newCronScheduler()
	a.startSchedulerRunner()
	a.loadAllCronJobs()
	a.cron.start()
	a.startSchedulerCalibrationLoop()
}

type cronJobEntry struct {
	userID   int64
	cronExpr string
}

func (a *App) loadAllCronJobs() {
	// First: query DB and collect all jobs (release DB conn quickly)
	rows, err := a.db.Query(`SELECT user_id, cron_expr FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`)
	if err != nil {
		log.Printf("加载定时任务失败: %v", err)
		return
	}
	var jobs []cronJobEntry
	for rows.Next() {
		var j cronJobEntry
		if err := rows.Scan(&j.userID, &j.cronExpr); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	// Second: under lock, rebuild cron entries
	a.cron.mu.Lock()
	defer a.cron.mu.Unlock()

	for _, entry := range a.cron.c.Entries() {
		a.cron.c.Remove(entry.ID)
	}

	count := 0
	for _, j := range jobs {
		uid := j.userID
		_, err := a.cron.c.AddFunc(j.cronExpr, func() {
			log.Printf("cron 触发打卡任务: user=%d", uid)
			_ = a.submitScheduledRun(uid, "scheduler", time.Now())
		})
		if err != nil {
			log.Printf("添加 cron 任务失败 user=%d expr=%s: %v", uid, j.cronExpr, err)
			continue
		}
		count++
	}
	log.Printf("已加载 %d 个定时任务", count)
}

func (a *App) reloadCron() {
	a.loadAllCronJobs()
}

func (a *App) startSchedulerRunner() {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()

	if a.schedStop != nil {
		return
	}

	a.schedStop = make(chan struct{})
	a.schedQueue = make(chan scheduledRun, schedulerQueueSize)
	a.schedWg.Add(1)
	go func(stop <-chan struct{}, queue <-chan scheduledRun) {
		defer a.schedWg.Done()
		a.runSchedulerRunner(stop, queue)
	}(a.schedStop, a.schedQueue)
}

func (a *App) stopSchedulerRunner() {
	a.schedMu.Lock()
	stop := a.schedStop
	a.schedStop = nil
	a.schedQueue = nil
	a.schedMu.Unlock()

	if stop == nil {
		return
	}
	close(stop)
	a.schedWg.Wait()
}

func (a *App) runSchedulerRunner(stop <-chan struct{}, queue <-chan scheduledRun) {
	for {
		select {
		case <-stop:
			return
		case task := <-queue:
			delay := time.Since(task.scheduledAt).Truncate(time.Second)
			if delay < 0 {
				delay = 0
			}
			log.Printf("调度执行器: 开始执行 user=%d trigger=%s delay=%s", task.userID, task.triggerType, delay)
			runID, status, message := a.executeClockinRun(task.userID, task.triggerType)
			log.Printf("调度执行器: 执行完成 user=%d run=%d status=%s message=%s", task.userID, runID, status, message)
		}
	}
}

func (a *App) submitScheduledRun(userID int64, triggerType string, scheduledAt time.Time) bool {
	a.schedMu.Lock()
	stop := a.schedStop
	queue := a.schedQueue
	a.schedMu.Unlock()

	if stop == nil || queue == nil {
		log.Printf("调度执行器: 未启动，跳过入队 user=%d trigger=%s", userID, triggerType)
		return false
	}

	task := scheduledRun{userID: userID, triggerType: triggerType, scheduledAt: scheduledAt}

	select {
	case <-stop:
		return false
	case queue <- task:
		return true
	default:
		log.Printf("调度执行器: 队列繁忙，改为后台等待入队 user=%d trigger=%s", userID, triggerType)
		go func() {
			select {
			case <-stop:
				return
			case queue <- task:
			}
		}()
		return true
	}
}

func (a *App) startSchedulerCalibrationLoop() {
	intervalMinutes := a.cfg.Server.SchedulerCalibrationMinute
	if intervalMinutes <= 0 {
		log.Printf("调度校准: 已禁用 (server.scheduler_calibration_minutes=%d)", intervalMinutes)
		return
	}

	a.calibMu.Lock()
	defer a.calibMu.Unlock()

	if a.calibStop != nil {
		return
	}

	a.calibStop = make(chan struct{})
	a.calibWg.Add(1)
	go func(stop <-chan struct{}, interval time.Duration) {
		defer a.calibWg.Done()
		a.runSchedulerCalibrationLoop(stop, interval)
	}(a.calibStop, time.Duration(intervalMinutes)*time.Minute)
}

func (a *App) stopSchedulerCalibrationLoop() {
	a.calibMu.Lock()
	stop := a.calibStop
	a.calibStop = nil
	a.calibMu.Unlock()

	if stop == nil {
		return
	}
	close(stop)
	a.calibWg.Wait()
}

func (a *App) runSchedulerCalibrationLoop(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run one pass shortly after startup so a just-missed slot is recovered quickly.
	startupTimer := time.NewTimer(calibrationStartupDelay)
	defer startupTimer.Stop()

	for {
		select {
		case <-stop:
			return
		case <-startupTimer.C:
			a.calibrateOnce(context.Background())
		case <-ticker.C:
			a.calibrateOnce(context.Background())
		}
	}
}

func (a *App) calibrateOnce(ctx context.Context) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}

	now := time.Now().In(loc)

	rows, err := a.db.QueryContext(ctx, `SELECT user_id, cron_expr FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`)
	if err != nil {
		log.Printf("调度校准: 查询任务失败: %v", err)
		return
	}

	var jobs []cronJobEntry
	for rows.Next() {
		var j cronJobEntry
		if err := rows.Scan(&j.userID, &j.cronExpr); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	if len(jobs) == 0 {
		return
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	for _, j := range jobs {
		sched, err := parser.Parse(j.cronExpr)
		if err != nil {
			log.Printf("调度校准: 解析 cron 失败 user=%d expr=%s: %v", j.userID, j.cronExpr, err)
			continue
		}

		missed := a.listMissedSchedules(j.userID, sched, now, loc)
		if len(missed) == 0 {
			continue
		}

		for _, scheduled := range missed {
			nextDue := sched.Next(scheduled)
			if a.hasRunForScheduleSlot(j.userID, scheduled, nextDue) {
				continue
			}
			log.Printf("调度校准: 入队补偿执行 user=%d scheduled=%s delay=%s", j.userID, scheduled.Format("15:04:05"), now.Sub(scheduled).Truncate(time.Second))
			_ = a.submitScheduledRun(j.userID, "scheduler_calibration", scheduled)
		}
	}
}

func (a *App) listMissedSchedules(userID int64, sched cron.Schedule, now time.Time, loc *time.Location) []time.Time {
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(-1 * time.Second)

	var lastRunRaw sql.NullString
	err := a.db.QueryRow(`SELECT MAX(started_at) FROM clockin_runs
		WHERE user_id = ?
		  AND trigger_type IN ('scheduler', 'scheduler_calibration', 'startup_recovery')`, userID).Scan(&lastRunRaw)
	if err != nil {
		log.Printf("调度校准: 查询最近执行记录失败 user=%d: %v", userID, err)
	} else if lastRunRaw.Valid && strings.TrimSpace(lastRunRaw.String) != "" {
		parsed, parseErr := parseSQLiteDateTime(strings.TrimSpace(lastRunRaw.String), loc)
		if parseErr != nil {
			log.Printf("调度校准: 解析最近执行时间失败 user=%d value=%q: %v", userID, lastRunRaw.String, parseErr)
		} else {
			anchor = parsed
		}
	}

	first := sched.Next(anchor)
	if first.IsZero() || first.After(now) {
		return nil
	}

	missed := make([]time.Time, 0, 8)
	for t := first; !t.After(now); t = sched.Next(t) {
		missed = append(missed, t)
	}
	return missed
}

func parseSQLiteDateTime(raw string, loc *time.Location) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.In(loc), nil
		}
	}

	if t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, loc); err == nil {
		return t.In(loc), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", raw, loc); err == nil {
		return t.In(loc), nil
	}

	return time.Time{}, fmt.Errorf("unsupported datetime format")
}

func (a *App) hasRunForScheduleSlot(userID int64, scheduled, nextDue time.Time) bool {
	windowStart := scheduled.Add(-90 * time.Second).UTC()
	windowEnd := nextDue.UTC()

	var count int
	err := a.db.QueryRow(`SELECT COUNT(1) FROM clockin_runs
		WHERE user_id = ?
		  AND started_at >= ?
		  AND started_at < ?
		  AND trigger_type IN ('scheduler', 'scheduler_calibration', 'startup_recovery')`,
		userID, windowStart, windowEnd).Scan(&count)
	if err != nil {
		log.Printf("调度校准: 查询执行记录失败 user=%d: %v", userID, err)
		return false
	}
	return count > 0
}

func (a *App) validateCronExpr(expr string) error {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}
	next := sched.Next(time.Now().In(loc))
	if next.IsZero() {
		return fmt.Errorf("无法计算下次执行时间")
	}
	return nil
}

func (a *App) countEnabledJobs() int {
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`).Scan(&count)
	return count
}

func (a *App) recoverMissedJobs() {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		log.Printf("警告: 加载 Asia/Shanghai 时区失败: %v, 使用 UTC+8 固定偏移", err)
		loc = time.FixedZone("CST", 8*3600)
	}

	now := time.Now().In(loc)
	todayStr := now.Format("20060102")
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// Step 1: Query all enabled cron jobs
	rows, err := a.db.Query(`SELECT user_id, cron_expr FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`)
	if err != nil {
		log.Printf("恢复遗漏任务: 查询定时任务失败: %v", err)
		return
	}
	var jobs []cronJobEntry
	for rows.Next() {
		var j cronJobEntry
		if err := rows.Scan(&j.userID, &j.cronExpr); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	if len(jobs) == 0 {
		log.Printf("恢复遗漏任务: 没有已启用的定时任务")
		return
	}

	// Step 2: Find users who already have a clockin record today
	existingRows, err := a.db.Query(`SELECT DISTINCT user_id FROM clockin_runs WHERE run_date = ?`, todayStr)
	if err != nil {
		log.Printf("恢复遗漏任务: 查询今日打卡记录失败: %v", err)
		return
	}
	existingUsers := make(map[int64]bool)
	for existingRows.Next() {
		var uid int64
		if err := existingRows.Scan(&uid); err != nil {
			continue
		}
		existingUsers[uid] = true
	}
	existingRows.Close()

	// Step 3: Parse each cron expression and check if today's scheduled time has passed
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	recoveredCount := 0
	for _, j := range jobs {
		if existingUsers[j.userID] {
			continue
		}

		sched, err := parser.Parse(j.cronExpr)
		if err != nil {
			log.Printf("恢复遗漏任务: 解析 cron 表达式失败 user=%d expr=%s: %v", j.userID, j.cronExpr, err)
			continue
		}

		// First scheduled time today: Next() returns the first time strictly after the argument
		nextTime := sched.Next(startOfToday.Add(-1 * time.Second))

		if nextTime.In(loc).Format("20060102") == todayStr && nextTime.Before(now) {
			log.Printf("恢复遗漏任务: 执行补签 user=%d scheduled=%s", j.userID, nextTime.In(loc).Format("15:04:05"))
			runID, status, message := a.executeClockinRun(j.userID, "startup_recovery")
			log.Printf("恢复遗漏任务: 完成 user=%d run=%d status=%s message=%s", j.userID, runID, status, message)
			recoveredCount++
		}
	}

	log.Printf("恢复遗漏任务: 共补签 %d 个任务", recoveredCount)
}
