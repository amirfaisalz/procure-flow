// Command worker consumes procurement events and performs asynchronous side
// effects: notifying requesters and syncing approved procurements with the ERP.
// The HTTP API never waits for these tasks, so a slow downstream system cannot
// delay the approval response.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/amirfaisalz/procure-flow/internal/config"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/rabbitmq"
	"github.com/amirfaisalz/procure-flow/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := logging.New(cfg.LogLevel, string(cfg.Env), os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	notificationKeys := []string{
		"procurement.created",
		"procurement.submitted",
		"procurement.approved",
		"procurement.rejected",
		"procurement.cancelled",
	}
	notificationConfig := rabbitmq.DefaultConsumerConfig("procure.notifications", notificationKeys...)
	integrationConfig := rabbitmq.DefaultConsumerConfig("procure.integrations", "procurement.approved")

	consumers := []*rabbitmq.Consumer{
		rabbitmq.NewConsumer(
			cfg.RabbitMQ.URL,
			cfg.RabbitMQ.Exchange,
			notificationConfig,
			rabbitmq.NotificationHandler(logger, cfg.Integrations.NotificationWebhookURL, nil),
			logger.With(slog.String("consumer", "notifications")),
		),
		rabbitmq.NewConsumer(
			cfg.RabbitMQ.URL,
			cfg.RabbitMQ.Exchange,
			integrationConfig,
			rabbitmq.ERPHandler(logger, cfg.Integrations.ERPWebhookURL, nil),
			logger.With(slog.String("consumer", "integrations")),
		),
	}

	logger.Info("worker starting",
		slog.String("exchange", cfg.RabbitMQ.Exchange),
		slog.Bool("notification_webhook", cfg.Integrations.NotificationWebhookURL != ""),
		slog.Bool("erp_webhook", cfg.Integrations.ERPWebhookURL != ""),
	)

	var wg sync.WaitGroup
	for _, consumer := range consumers {
		wg.Add(1)
		go func(consumer *rabbitmq.Consumer) {
			defer wg.Done()
			consumer.Run(ctx)
		}(consumer)
	}

	<-ctx.Done()
	logger.Info("worker shutting down")
	wg.Wait()
	logger.Info("worker stopped")
	return nil
}
