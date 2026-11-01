package config_test

import (
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/config"
)

func TestLoadRabbitMQDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(env(nil))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.RabbitMQ.URL != "" {
		t.Errorf("RabbitMQ.URL = %q, want empty", cfg.RabbitMQ.URL)
	}
	if cfg.RabbitMQ.Exchange != "procure.events" {
		t.Errorf("RabbitMQ.Exchange = %q, want procure.events", cfg.RabbitMQ.Exchange)
	}
}

func TestLoadRabbitMQ(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"RABBITMQ_URL":      "amqp://guest:guest@127.0.0.1:5672/",
		"RABBITMQ_EXCHANGE": "custom.events",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.RabbitMQ.URL == "" {
		t.Error("RabbitMQ.URL = empty, want the configured URL")
	}
	if cfg.RabbitMQ.Exchange != "custom.events" {
		t.Errorf("RabbitMQ.Exchange = %q, want custom.events", cfg.RabbitMQ.Exchange)
	}
}

func TestLoadRabbitMQInvalid(t *testing.T) {
	_, err := config.LoadFrom(env(map[string]string{"RABBITMQ_URL": "kafka://localhost:9092"}))
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "RABBITMQ_URL") {
		t.Errorf("error = %q, want it to mention RABBITMQ_URL", err)
	}
}

func TestLoadWorkerRequiresBroker(t *testing.T) {
	if _, err := config.LoadWorkerFrom(env(nil)); err == nil {
		t.Fatal("LoadWorkerFrom() error = nil, want error")
	} else if !strings.Contains(err.Error(), "RABBITMQ_URL") {
		t.Errorf("error = %q, want it to mention RABBITMQ_URL", err)
	}

	// The worker does not need a database or authentication, even in production.
	if _, err := config.LoadWorkerFrom(env(map[string]string{
		"APP_ENV":      "production",
		"RABBITMQ_URL": "amqp://guest:guest@127.0.0.1:5672/",
	})); err != nil {
		t.Fatalf("LoadWorkerFrom() error = %v, want nil", err)
	}
}

func TestLoadIntegrations(t *testing.T) {
	cfg, err := config.LoadFrom(env(map[string]string{
		"NOTIFICATION_WEBHOOK_URL": "https://hooks.example.com/notify",
		"ERP_WEBHOOK_URL":          "http://erp.internal/api/events",
	}))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Integrations.NotificationWebhookURL != "https://hooks.example.com/notify" {
		t.Errorf("NotificationWebhookURL = %q", cfg.Integrations.NotificationWebhookURL)
	}
	if cfg.Integrations.ERPWebhookURL != "http://erp.internal/api/events" {
		t.Errorf("ERPWebhookURL = %q", cfg.Integrations.ERPWebhookURL)
	}

	if _, err := config.LoadFrom(env(map[string]string{"ERP_WEBHOOK_URL": "ftp://erp"})); err == nil {
		t.Fatal("LoadFrom() error = nil, want error for a non-HTTP webhook")
	}
}
