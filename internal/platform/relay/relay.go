// Package relay drains the transactional outbox to the message broker. It
// leases due events, publishes them, and records the outcome so delivery is
// at-least-once and survives process crashes.
package relay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
	"github.com/amirfaisalz/procure-flow/internal/domain/outbox"
)

// Publisher delivers one event to the broker.
type Publisher interface {
	Publish(ctx context.Context, ev event.Event) error
}

// Config tunes the relay loop.
type Config struct {
	Interval      time.Duration
	BatchSize     int
	Lease         time.Duration
	PruneInterval time.Duration
	Retention     time.Duration
	MaxBackoff    time.Duration
}

// DefaultConfig returns production-sensible relay settings.
func DefaultConfig() Config {
	return Config{
		Interval:      time.Second,
		BatchSize:     50,
		Lease:         30 * time.Second,
		PruneInterval: time.Hour,
		Retention:     7 * 24 * time.Hour,
		MaxBackoff:    5 * time.Minute,
	}
}

// Relay polls the outbox and publishes due events.
type Relay struct {
	repo      outbox.Repository
	publisher Publisher
	logger    *slog.Logger
	cfg       Config
	now       func() time.Time
}

// New creates a relay. Zero config fields fall back to DefaultConfig.
func New(repo outbox.Repository, publisher Publisher, logger *slog.Logger, cfg Config) *Relay {
	defaults := DefaultConfig()
	if cfg.Interval <= 0 {
		cfg.Interval = defaults.Interval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.Lease <= 0 {
		cfg.Lease = defaults.Lease
	}
	if cfg.PruneInterval <= 0 {
		cfg.PruneInterval = defaults.PruneInterval
	}
	if cfg.Retention <= 0 {
		cfg.Retention = defaults.Retention
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaults.MaxBackoff
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{repo: repo, publisher: publisher, logger: logger, cfg: cfg, now: time.Now}
}

// Run polls until ctx is cancelled. Publish failures are retried with
// exponential backoff and never stop the loop.
func (r *Relay) Run(ctx context.Context) {
	poll := time.NewTicker(r.cfg.Interval)
	defer poll.Stop()
	prune := time.NewTicker(r.cfg.PruneInterval)
	defer prune.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			if _, err := r.RunOnce(ctx); err != nil {
				r.logger.ErrorContext(ctx, "outbox relay iteration failed", slog.String("error", err.Error()))
			}
		case <-prune.C:
			r.prune(ctx)
		}
	}
}

// RunOnce publishes one batch of due events and reports how many were delivered.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	now := r.now().UTC()
	events, err := r.repo.Claim(ctx, r.cfg.BatchSize, now, r.cfg.Lease)
	if err != nil {
		return 0, fmt.Errorf("relay: claim events: %w", err)
	}

	delivered := 0
	for _, ev := range events {
		if err := r.publisher.Publish(ctx, ev); err != nil {
			// Anchor the retry at the failure time: a slow publish (confirm
			// timeout) must not consume the backoff window.
			next := r.now().UTC().Add(r.backoff(ev.Attempts + 1))
			if markErr := r.repo.MarkFailed(ctx, ev.ID, err.Error(), next); markErr != nil {
				r.logger.ErrorContext(ctx, "outbox relay could not record a failure",
					slog.String("event_id", ev.ID),
					slog.String("error", markErr.Error()),
				)
			}
			r.logger.WarnContext(ctx, "outbox relay publish failed",
				slog.String("event_id", ev.ID),
				slog.String("event_type", ev.Type),
				slog.Int("attempt", ev.Attempts+1),
				slog.Time("retry_at", next),
				slog.String("error", err.Error()),
			)
			continue
		}

		if err := r.repo.MarkPublished(ctx, ev.ID, r.now().UTC()); err != nil {
			r.logger.ErrorContext(ctx, "outbox relay could not mark an event published",
				slog.String("event_id", ev.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		delivered++
	}
	return delivered, nil
}

func (r *Relay) prune(ctx context.Context) {
	removed, err := r.repo.Prune(ctx, r.now().UTC().Add(-r.cfg.Retention))
	if err != nil {
		r.logger.ErrorContext(ctx, "outbox prune failed", slog.String("error", err.Error()))
		return
	}
	if removed > 0 {
		r.logger.InfoContext(ctx, "pruned published outbox events", slog.Int64("removed", removed))
	}
}

// backoff returns 1s, 2s, 4s, ... capped at MaxBackoff.
func (r *Relay) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 20)
	if delay > r.cfg.MaxBackoff || delay <= 0 {
		delay = r.cfg.MaxBackoff
	}
	return delay
}
