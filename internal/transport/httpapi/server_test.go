package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi"
)

func TestNewServerConfig(t *testing.T) {
	cfg := config.HTTP{
		Addr:              ":9090",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		MaxHeaderBytes:    2048,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httpapi.NewServer(cfg, http.NotFoundHandler(), logger)

	if srv.Addr != cfg.Addr {
		t.Errorf("Addr = %q, want %q", srv.Addr, cfg.Addr)
	}
	if srv.ReadHeaderTimeout != cfg.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, cfg.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != cfg.ReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", srv.ReadTimeout, cfg.ReadTimeout)
	}
	if srv.WriteTimeout != cfg.WriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", srv.WriteTimeout, cfg.WriteTimeout)
	}
	if srv.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, cfg.IdleTimeout)
	}
	if srv.MaxHeaderBytes != cfg.MaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, cfg.MaxHeaderBytes)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
	if srv.ErrorLog == nil {
		t.Error("ErrorLog is nil")
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- httpapi.Run(ctx, listener, srv, time.Second, logger)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after shutdown")
	}
}

func TestRunReturnsServeError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	_ = listener.Close()

	srv := &http.Server{Handler: http.NotFoundHandler()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := httpapi.Run(context.Background(), listener, srv, time.Second, logger); err == nil {
		t.Fatal("Run() error = nil, want a listener error")
	}
}
