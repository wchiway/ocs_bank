package main

import (
	"context"
	"errors"
	"flag"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/httpapi"
	"github.com/wchiway/ocs_bank/internal/provider"
	"github.com/wchiway/ocs_bank/internal/question"
	"github.com/wchiway/ocs_bank/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "config.json", "configuration file")
	flag.Parse()
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := store.Open(initCtx, c.Database, time.Duration(c.CacheTTLHours)*time.Hour)
	initCancel()
	if err != nil {
		return err
	}
	defer db.Close()
	ai, err := provider.New(c)
	if err != nil {
		return err
	}
	defer ai.Close()
	service, err := question.NewService(ctx, db, ai, question.ServiceOptions{Namespace: ai.Namespace(), Timeout: c.Timeout(), MaxConcurrent: c.MaxConcurrent, MaxPending: c.MaxPending, Logger: logger})
	if err != nil {
		return err
	}
	defer service.Close()
	h := httpapi.New(service, httpapi.Options{Token: c.AccessToken, Timeout: c.Timeout(), RequestsPerMinute: c.RequestsPerMinute, Logger: logger})
	srv := &http.Server{Addr: c.Listen, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: c.Timeout() + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 128 << 10}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	logger.Info("listening", "address", c.Listen)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		_ = srv.Close()
		return err
	}
	return nil
}
