package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"max_bot/internal/config"
	"max_bot/internal/reference"
	"max_bot/internal/reporting"
)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "dotenv error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		_, _ = fmt.Fprintln(os.Stderr, "config error: DATABASE_URL is required")
		os.Exit(1)
	}

	reportStore, err := reporting.NewPostgresStore(dsn)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "reporting postgres error: %v\n", err)
		os.Exit(1)
	}
	defer reportStore.Close()

	referenceStore, err := reference.NewPostgresStore(dsn)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "reference postgres error: %v\n", err)
		os.Exit(1)
	}
	defer referenceStore.Close()

	addr := getenv("CORE_API_ADDR", ":8091")
	readTimeout := getenvDuration("CORE_API_READ_TIMEOUT", 5*time.Second)
	writeTimeout := getenvDuration("CORE_API_WRITE_TIMEOUT", 5*time.Second)
	shutdownTimeout := getenvDuration("CORE_API_SHUTDOWN_TIMEOUT", 10*time.Second)

	server := &http.Server{
		Addr:         addr,
		Handler:      reporting.NewHandler(reporting.NewService(reportStore), referenceStore, logger),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("core api started", "addr", addr)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("core api failed", "error", err.Error())
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	if d, err := time.ParseDuration(value); err == nil {
		return d
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}

	return fallback
}
