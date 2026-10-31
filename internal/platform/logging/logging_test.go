package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
)

func TestNewProductionUsesJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(slog.LevelInfo, "production", &buf)

	logger.Info("hello", "key", "value")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v (output=%q)", err, buf.String())
	}
	if record["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", record["msg"])
	}
	if record["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", record["level"])
	}
	if record["key"] != "value" {
		t.Errorf("key = %v, want value", record["key"])
	}
	if _, ok := record["source"]; ok {
		t.Error("production logs should not include source by default")
	}
}

func TestNewDevelopmentUsesText(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(slog.LevelInfo, "development", &buf)

	logger.Info("hello", "key", "value")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") || !strings.Contains(out, "key=value") {
		t.Errorf("output = %q, want text key=value format", out)
	}
	if !strings.Contains(out, "source=") {
		t.Errorf("output = %q, want source in development", out)
	}
}

func TestNewFiltersBelowLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(slog.LevelWarn, "production", &buf)

	logger.Info("hidden")
	if buf.Len() != 0 {
		t.Fatalf("Info was logged at WARN level: %q", buf.String())
	}

	logger.Warn("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Errorf("output = %q, want shown", buf.String())
	}
}

func TestContextLogger(t *testing.T) {
	if got := logging.From(context.Background()); got != slog.Default() {
		t.Error("From(background) should return slog.Default()")
	}

	custom := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	ctx := logging.WithLogger(context.Background(), custom)
	if got := logging.From(ctx); got != custom {
		t.Error("From(ctx) did not return the injected logger")
	}
}
