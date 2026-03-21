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

	"max_bot/internal/reference"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		_, _ = fmt.Fprintln(os.Stderr, "config error: DATABASE_URL is required")
		os.Exit(1)
	}

	store, err := reference.NewPostgresStore(dsn)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "postgres error: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	addr := getenv("REFERENCE_API_ADDR", ":8090")
	readTimeout := getenvDuration("REFERENCE_API_READ_TIMEOUT", 5*time.Second)
	writeTimeout := getenvDuration("REFERENCE_API_WRITE_TIMEOUT", 5*time.Second)
	shutdownTimeout := getenvDuration("REFERENCE_API_SHUTDOWN_TIMEOUT", 10*time.Second)

	server := &http.Server{
		Addr:         addr,
		Handler:      reference.NewHandler(store, logger),
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

	logger.Info("reference api started", "addr", addr)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("reference api failed", "error", err.Error())
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
