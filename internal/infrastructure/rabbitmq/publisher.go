// Package rabbitmq adapts AMQP 0-9-1 to the event publisher and consumer used
// by the API relay and the asynchronous worker.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

const (
	exchangeType = "topic"
	contentType  = "application/json"
)

// ErrPublisherClosed is returned after Close has been called.
var ErrPublisherClosed = errors.New("rabbitmq: publisher is closed")

// Publisher publishes event envelopes to a durable topic exchange. Publishes
// run in confirm mode, so a successful Publish means the broker took
// responsibility for the message. The connection is re-established lazily
// after a failure.
type Publisher struct {
	url      string
	exchange string

	dialTimeout    time.Duration
	confirmTimeout time.Duration

	mu       sync.Mutex
	conn     *amqp.Connection
	channel  *amqp.Channel
	confirms chan amqp.Confirmation
	closed   bool
}

// NewPublisher creates a lazy publisher. Call Connect eagerly to surface broker
// problems at startup.
func NewPublisher(url, exchange string) *Publisher {
	return &Publisher{
		url:            url,
		exchange:       exchange,
		dialTimeout:    5 * time.Second,
		confirmTimeout: 5 * time.Second,
	}
}

// Connect opens the connection and declares the exchange.
func (p *Publisher) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPublisherClosed
	}
	if p.channel != nil && p.conn != nil && !p.conn.IsClosed() {
		return nil
	}
	return p.connectLocked(ctx)
}

func (p *Publisher) connectLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Drop any half-open connection; its close error is not actionable here.
	_ = p.resetLocked()

	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Dial:      amqp.DefaultDial(p.dialTimeout),
		Heartbeat: 10 * time.Second,
		Locale:    "en_US",
	})
	if err != nil {
		return fmt.Errorf("rabbitmq: dial: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: open channel: %w", err)
	}

	if err := channel.ExchangeDeclare(p.exchange, exchangeType, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: declare exchange %q: %w", p.exchange, err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: enable publisher confirms: %w", err)
	}

	p.conn = conn
	p.channel = channel
	p.confirms = channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	return nil
}

// Publish sends one event and waits for the broker confirmation.
func (p *Publisher) Publish(ctx context.Context, ev event.Event) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("rabbitmq: encode event: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPublisherClosed
	}
	if p.channel == nil || p.conn == nil || p.conn.IsClosed() {
		if err := p.connectLocked(ctx); err != nil {
			return err
		}
	}

	message := amqp.Publishing{
		ContentType:  contentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    ev.ID,
		Type:         ev.Type,
		Timestamp:    ev.OccurredAt,
		Body:         body,
		Headers: amqp.Table{
			"aggregate_type": ev.AggregateType,
			"aggregate_id":   ev.AggregateID,
			"actor_id":       ev.ActorID,
		},
	}

	if err := p.channel.PublishWithContext(ctx, p.exchange, ev.Type, false, false, message); err != nil {
		_ = p.resetLocked()
		return fmt.Errorf("rabbitmq: publish %s: %w", ev.ID, err)
	}

	timer := time.NewTimer(p.confirmTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		// Discard the channel and its pending confirmation; otherwise the next
		// publish would read this confirmation as its own verdict.
		_ = p.resetLocked()
		return ctx.Err()
	case <-timer.C:
		_ = p.resetLocked()
		return fmt.Errorf("rabbitmq: publish %s: confirmation timed out", ev.ID)
	case confirmation, ok := <-p.confirms:
		if !ok {
			_ = p.resetLocked()
			return fmt.Errorf("rabbitmq: publish %s: confirmation channel closed", ev.ID)
		}
		if !confirmation.Ack {
			_ = p.resetLocked()
			return fmt.Errorf("rabbitmq: publish %s: broker nacked the message", ev.ID)
		}
		return nil
	}
}

// Healthy reports whether the publisher currently holds a usable connection.
func (p *Publisher) Healthy(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPublisherClosed
	}
	if p.conn == nil || p.channel == nil || p.conn.IsClosed() {
		return errors.New("rabbitmq: publisher is not connected")
	}
	return nil
}

// Close releases the connection.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.resetLocked()
}

func (p *Publisher) resetLocked() error {
	var err error
	if p.channel != nil {
		if closeErr := p.channel.Close(); closeErr != nil && !errors.Is(closeErr, amqp.ErrClosed) {
			err = closeErr
		}
	}
	if p.conn != nil {
		if closeErr := p.conn.Close(); closeErr != nil && !errors.Is(closeErr, amqp.ErrClosed) && err == nil {
			err = closeErr
		}
	}
	p.channel = nil
	p.conn = nil
	p.confirms = nil
	return err
}
