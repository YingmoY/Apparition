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
