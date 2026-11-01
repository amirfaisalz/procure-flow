package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

// Handler processes one event. Returning an error triggers bounded retries and
// then dead-lettering, so handlers should be idempotent (delivery is
// at-least-once).
type Handler func(ctx context.Context, ev event.Event) error

// ConsumerConfig describes one durable queue and its retry policy.
type ConsumerConfig struct {
	Queue          string
	RoutingKeys    []string
	Prefetch       int
	MaxAttempts    int
	RetryDelay     time.Duration
	DialTimeout    time.Duration
	ReconnectDelay time.Duration
}

// DefaultConsumerConfig returns conservative consumer defaults.
func DefaultConsumerConfig(queue string, routingKeys ...string) ConsumerConfig {
	return ConsumerConfig{
		Queue:          queue,
		RoutingKeys:    routingKeys,
		Prefetch:       10,
		MaxAttempts:    3,
		RetryDelay:     500 * time.Millisecond,
		DialTimeout:    10 * time.Second,
		ReconnectDelay: 5 * time.Second,
	}
}

// Consumer subscribes a queue to the event exchange and processes deliveries.
// A failed handler is retried in-process and then dead-lettered to
// <queue>.dlq through <queue>.dlx.
type Consumer struct {
	url      string
	exchange string
	cfg      ConsumerConfig
	handler  Handler
	logger   *slog.Logger
}

// NewConsumer creates a consumer. Zero config fields fall back to defaults.
func NewConsumer(url, exchange string, cfg ConsumerConfig, handler Handler, logger *slog.Logger) *Consumer {
	defaults := DefaultConsumerConfig(cfg.Queue)
	if cfg.Queue == "" {
		cfg.Queue = defaults.Queue
	}
	if cfg.Prefetch <= 0 {
		cfg.Prefetch = defaults.Prefetch
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaults.MaxAttempts
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = defaults.RetryDelay
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaults.DialTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaults.ReconnectDelay
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{url: url, exchange: exchange, cfg: cfg, handler: handler, logger: logger}
}

// Run consumes until ctx is cancelled, reconnecting with a fixed delay after a
// lost connection.
func (c *Consumer) Run(ctx context.Context) {
	for {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		c.logger.ErrorContext(ctx, "consumer disconnected; reconnecting",
			slog.String("queue", c.cfg.Queue),
			slog.Duration("retry_in", c.cfg.ReconnectDelay),
			slog.String("error", err.Error()),
		)
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.cfg.ReconnectDelay):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Dial:      amqp.DefaultDial(c.cfg.DialTimeout),
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = channel.Close() }()

	if err := channel.ExchangeDeclare(c.exchange, exchangeType, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}

	deadLetterExchange := c.cfg.Queue + ".dlx"
	deadLetterQueue := c.cfg.Queue + ".dlq"
	if err := channel.ExchangeDeclare(deadLetterExchange, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter exchange: %w", err)
	}
	if _, err := channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := channel.QueueBind(deadLetterQueue, "", deadLetterExchange, false, nil); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}

	queue, err := channel.QueueDeclare(c.cfg.Queue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": deadLetterExchange,
	})
	if err != nil {
		return fmt.Errorf("declare queue %q: %w", c.cfg.Queue, err)
	}
	for _, key := range c.cfg.RoutingKeys {
		if err := channel.QueueBind(queue.Name, key, c.exchange, false, nil); err != nil {
			return fmt.Errorf("bind queue %q to %q: %w", queue.Name, key, err)
		}
	}
	if err := channel.Qos(c.cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("set prefetch: %w", err)
	}

	deliveries, err := channel.Consume(queue.Name, c.cfg.Queue+"-consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume queue %q: %w", queue.Name, err)
	}

	c.logger.Info("consumer started",
		slog.String("queue", queue.Name),
		slog.String("exchange", c.exchange),
		slog.Any("routing_keys", c.cfg.RoutingKeys),
	)

	for {
		select {
		case <-ctx.Done():
			_ = channel.Cancel(c.cfg.Queue+"-consumer", false)
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			c.handle(ctx, delivery)
		}
	}
}

// handle decodes one delivery, runs the handler with retries, and settles it.
func (c *Consumer) handle(ctx context.Context, delivery amqp.Delivery) {
	var ev event.Event
	if err := json.Unmarshal(delivery.Body, &ev); err != nil {
		c.logger.ErrorContext(ctx, "discarding an undecodable event",
			slog.String("queue", c.cfg.Queue),
			slog.String("error", err.Error()),
		)
		_ = delivery.Nack(false, false)
		return
	}
	if err := ev.Validate(); err != nil {
		c.logger.ErrorContext(ctx, "discarding an invalid event",
			slog.String("queue", c.cfg.Queue),
			slog.String("error", err.Error()),
		)
		_ = delivery.Nack(false, false)
		return
	}

	if err := c.deliver(ctx, ev); err != nil {
		if ctx.Err() != nil {
			// Shutting down: leave the delivery unsettled so the broker
			// requeues it for another replica instead of dead-lettering it.
			c.logger.WarnContext(ctx, "shutdown interrupted event handling; leaving the delivery unacknowledged",
				slog.String("queue", c.cfg.Queue),
				slog.String("event_id", ev.ID),
			)
			return
		}
		c.logger.ErrorContext(ctx, "event handler failed; dead-lettering",
			slog.String("queue", c.cfg.Queue),
			slog.String("event_id", ev.ID),
			slog.String("event_type", ev.Type),
			slog.String("error", err.Error()),
		)
		_ = delivery.Nack(false, false)
		return
	}

	if err := delivery.Ack(false); err != nil {
		c.logger.ErrorContext(ctx, "failed to acknowledge an event",
			slog.String("queue", c.cfg.Queue),
			slog.String("event_id", ev.ID),
			slog.String("error", err.Error()),
		)
	}
}

// deliver retries the handler with exponential backoff.
func (c *Consumer) deliver(ctx context.Context, ev event.Event) error {
	var err error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if err = c.handler(ctx, ev); err == nil {
			return nil
		}
		if attempt == c.cfg.MaxAttempts {
			break
		}
		delay := c.cfg.RetryDelay << (attempt - 1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}
