package main

import (
	"context"
	"errors"
	apihttp "filepipeline/internal/api"
	"filepipeline/internal/config"
	"filepipeline/internal/migrate"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"filepipeline/internal/storage"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}
	db, err := repository.Open(cfg.DBPath)
	if err != nil {
		logger.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := migrate.New().Apply(context.Background(), db); err != nil {
		logger.Fatalf("apply migrations: %v", err)
	}
	store, err := storage.NewLocal(cfg.FileDir)
	if err != nil {
		logger.Fatalf("open file storage: %v", err)
	}
	repo := repository.New(db)
	retry := service.NewRetryPolicy()
	handlers := apihttp.NewHandlers(repo, store, cfg.MaxAttempts, retry, logger)
	server := &http.Server{
		Addr: cfg.HTTPAddr, Handler: apihttp.NewRouter(handlers, "web", logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("[api] listening=%s db=%s files=%s", cfg.HTTPAddr, cfg.DBPath, cfg.FileDir)
		errCh <- server.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		logger.Printf("[api] shutdown requested")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("http server: %v", err)
		}
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("[api] graceful shutdown error=%v", err)
	}
}
