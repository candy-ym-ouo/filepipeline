package main

import (
	"context"
	"filepipeline/internal/config"
	"filepipeline/internal/migrate"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"filepipeline/internal/storage"
	pipelineworker "filepipeline/internal/worker"
	"log"
	"os"
	"os/signal"
	"syscall"
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
	validator := service.NewValidator(store)
	extractor := service.NewExtractor(store)
	scanner := service.NewScanner(cfg.ScanURL, cfg.ScanTimeout, store)
	pipeline := service.NewPipeline(validator, extractor, scanner)
	retry := service.NewRetryPolicy()
	processor := pipelineworker.NewProcessor(repo, pipeline, retry, cfg.CallbackWait, logger)
	worker := pipelineworker.New(repo, processor, pipelineworker.Config{
		WorkerCount: cfg.WorkerCount, PollInterval: cfg.PollInterval,
		BatchSize: cfg.BatchSize, Lease: cfg.Lease,
	}, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Printf("[worker] started workers=%d db=%s scanner=%s", cfg.WorkerCount, cfg.DBPath, cfg.ScanURL)
	if err := worker.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Fatalf("worker stopped: %v", err)
	}
	logger.Printf("[worker] stopped")
}
