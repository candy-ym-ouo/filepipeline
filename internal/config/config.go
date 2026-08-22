package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr     string
	DBPath       string
	FileDir      string
	WorkerCount  int
	PollInterval time.Duration
	BatchSize    int
	MaxAttempts  int
	Lease        time.Duration
	ScanURL      string
	ScanTimeout  time.Duration
	CallbackWait time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr: ":8080", DBPath: "./data/pipeline.db", FileDir: "./data/files",
		WorkerCount: 4, PollInterval: 500 * time.Millisecond, BatchSize: 8,
		MaxAttempts: 3, Lease: 30 * time.Second,
		ScanURL: "http://127.0.0.1:9090", ScanTimeout: 5 * time.Second,
		CallbackWait: 30 * time.Second,
	}
	cfg.HTTPAddr = envString("PIPE_HTTP_ADDR", cfg.HTTPAddr)
	cfg.DBPath = envString("PIPE_DB_PATH", cfg.DBPath)
	cfg.FileDir = envString("PIPE_FILE_DIR", cfg.FileDir)
	cfg.ScanURL = envString("PIPE_SCAN_URL", cfg.ScanURL)
	var err error
	if cfg.WorkerCount, err = envInt("PIPE_WORKER_COUNT", cfg.WorkerCount); err != nil {
		return Config{}, err
	}
	if cfg.BatchSize, err = envInt("PIPE_BATCH_SIZE", cfg.BatchSize); err != nil {
		return Config{}, err
	}
	if cfg.MaxAttempts, err = envInt("PIPE_MAX_ATTEMPTS", cfg.MaxAttempts); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = envDuration("PIPE_POLL_INTERVAL", cfg.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.ScanTimeout, err = envDuration("PIPE_SCAN_TIMEOUT", cfg.ScanTimeout); err != nil {
		return Config{}, err
	}
	if cfg.CallbackWait, err = envDuration("PIPE_CALLBACK_WAIT", cfg.CallbackWait); err != nil {
		return Config{}, err
	}
	leaseSeconds, err := envInt("PIPE_LEASE_SECONDS", int(cfg.Lease/time.Second))
	if err != nil {
		return Config{}, err
	}
	cfg.Lease = time.Duration(leaseSeconds) * time.Second
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
func (c Config) Validate() error {
	if c.HTTPAddr == "" || c.DBPath == "" || c.FileDir == "" {
		return fmt.Errorf("HTTP address, database path and file directory are required")
	}
	if c.WorkerCount < 1 || c.WorkerCount > 128 {
		return fmt.Errorf("worker count must be between 1 and 128")
	}
	if c.BatchSize < 1 || c.BatchSize > 1000 {
		return fmt.Errorf("batch size must be between 1 and 1000")
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 20 {
		return fmt.Errorf("max attempts must be between 1 and 20")
	}
	if c.PollInterval <= 0 || c.Lease <= 0 || c.ScanTimeout <= 0 || c.CallbackWait <= 0 {
		return fmt.Errorf("duration settings must be positive")
	}
	return nil
}
func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}
