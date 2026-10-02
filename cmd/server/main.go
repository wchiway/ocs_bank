package main

import (
	"context"
	"errors"
	"flag"
	"github.com/wchiway/ocs_bank/internal/config"
	"github.com/wchiway/ocs_bank/internal/httpapi"
	"github.com/wchiway/ocs_bank/internal/question"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type pendingService struct{}

func (pendingService) Query(context.Context, question.Question) (question.Result, error) {
	return question.Result{}, question.ErrUnavailable
}
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
	h := httpapi.New(pendingService{}, httpapi.Options{Token: c.AccessToken, Timeout: c.Timeout(), RequestsPerMinute: c.RequestsPerMinute, Logger: logger})
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
