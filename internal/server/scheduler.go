package server

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type cronScheduler struct {
	c  *cron.Cron
	mu sync.Mutex
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
	a.loadAllCronJobs()
	a.cron.start()
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
			// Run in goroutine so concurrent cron triggers don't block each other
			go func() {
				runID, status, message := a.executeClockinRun(uid, "scheduler")
				log.Printf("cron 任务完成: user=%d run=%d status=%s message=%s", uid, runID, status, message)
			}()
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
