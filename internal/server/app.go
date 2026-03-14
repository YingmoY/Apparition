package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type App struct {
	paths    RuntimePaths
	cfg      ServerConfig
	db       *sql.DB
	http     *http.Server
	closeLog func()
	cron     *cronScheduler
	wpsMu    sync.RWMutex
	wpsRuns  map[string]*wpsRuntimeSession
}

func NewApp() (*App, error) {
	paths, err := resolveRuntimePaths()
	if err != nil {
		return nil, err
	}
	if err := ensureRuntimeDirectories(paths); err != nil {
		return nil, err
	}
	closeLog, err := configureLogging(paths.LogPath)
	if err != nil {
		return nil, err
	}

	if _, err := ensureConfigFile(paths.ConfigPath); err != nil {
		closeLog()
		return nil, err
	}
	cfg, err := loadServerConfig(paths.ConfigPath)
	if err != nil {
		closeLog()
		return nil, err
	}

	db, err := openDatabase(paths.DBPath)
	if err != nil {
		closeLog()
		return nil, err
	}
	if err := migrateDatabase(db); err != nil {
		_ = db.Close()
		closeLog()
		return nil, err
	}
	if err := ensureDefaultAdmin(db, cfg); err != nil {
		_ = db.Close()
		closeLog()
		return nil, err
	}

	app := &App{
		paths:    paths,
		cfg:      cfg,
		db:       db,
		closeLog: closeLog,
		wpsRuns:  make(map[string]*wpsRuntimeSession),
	}

	app.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      app.router(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	a.initScheduler()
	log.Printf("调度器已启动 (robfig/cron), enabled_jobs=%d", a.countEnabledJobs())

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Apparition server 正在监听 %s", a.http.Addr)
		serverErr <- a.http.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		a.cron.stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.http.Shutdown(shutdownCtx)
		_ = a.db.Close()
		if a.closeLog != nil {
			a.closeLog()
		}
		return nil
	case err := <-serverErr:
		a.cron.stop()
		_ = a.db.Close()
		if a.closeLog != nil {
			a.closeLog()
		}
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
