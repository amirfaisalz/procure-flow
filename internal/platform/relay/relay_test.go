package relay

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

type fakeRepo struct {
	claimed     []event.Event
	claimErr    error
	published   []string
	failures    []failure
	publishAt   time.Time
	claimLimits []int
}

type failure struct {
	id    string
	cause string
	next  time.Time
}

func (f *fakeRepo) Enqueue(context.Context, event.Event) error { return nil }

func (f *fakeRepo) Claim(_ context.Context, limit int, _ time.Time, _ time.Duration) ([]event.Event, error) {
	f.claimLimits = append(f.claimLimits, limit)
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return f.claimed, nil
}

func (f *fakeRepo) MarkPublished(_ context.Context, id string, at time.Time) error {
	f.published = append(f.published, id)
	f.publishAt = at
	return nil
}

func (f *fakeRepo) MarkFailed(_ context.Context, id, cause string, next time.Time) error {
	f.failures = append(f.failures, failure{id: id, cause: cause, next: next})
	return nil
}

func (f *fakeRepo) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

type fakePublisher struct {
	err       error
	published []event.Event
}

func (f *fakePublisher) Publish(_ context.Context, ev event.Event) error {
	f.published = append(f.published, ev)
	return f.err
}

func testEvent(id string) event.Event {
	return event.Event{
		ID:            id,
		Type:          "procurement.created",
		AggregateType: "procurement",
		AggregateID:   "prc_1",
		OccurredAt:    time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
		Payload:       []byte(`{"id":"prc_1"}`),
	}
}

func TestRunOncePublishesClaimedEvents(t *testing.T) {
	repo := &fakeRepo{claimed: []event.Event{testEvent("e1"), testEvent("e2")}}
	publisher := &fakePublisher{}
	now := time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC)
	relay := New(repo, publisher, slog.New(slog.DiscardHandler), Config{BatchSize: 10})
	relay.now = func() time.Time { return now }

	delivered, err := relay.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if delivered != 2 {
		t.Errorf("delivered = %d, want 2", delivered)
	}
	if len(publisher.published) != 2 {
		t.Errorf("published %d events, want 2", len(publisher.published))
	}
	if len(repo.published) != 2 || repo.published[0] != "e1" || repo.published[1] != "e2" {
		t.Errorf("marked published = %v, want [e1 e2]", repo.published)
	}
	if !repo.publishAt.Equal(now) {
		t.Errorf("published_at = %v, want %v", repo.publishAt, now)
	}
	if len(repo.claimLimits) != 1 || repo.claimLimits[0] != 10 {
		t.Errorf("claim limits = %v, want [10]", repo.claimLimits)
	}
}

func TestRunOnceSchedulesBackoffOnFailure(t *testing.T) {
	repo := &fakeRepo{claimed: []event.Event{testEvent("e1")}}
	publisher := &fakePublisher{err: errors.New("broker unavailable")}
	now := time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC)
	relay := New(repo, publisher, slog.New(slog.DiscardHandler), Config{})

	relay.now = func() time.Time { return now }
	if _, err := relay.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if len(repo.published) != 0 {
		t.Error("failed event was marked published")
	}
	if len(repo.failures) != 1 {
		t.Fatalf("recorded %d failures, want 1", len(repo.failures))
	}
	if got := repo.failures[0].next; !got.Equal(now.Add(time.Second)) {
		t.Errorf("retry_at = %v, want now+1s", got)
	}
	if repo.failures[0].cause == "" {
		t.Error("failure cause is empty")
	}
}

func TestRunOnceSurfacesClaimErrors(t *testing.T) {
	repo := &fakeRepo{claimErr: errors.New("database down")}
	relay := New(repo, &fakePublisher{}, nil, Config{})

	if _, err := relay.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want the claim error")
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	relay := New(&fakeRepo{}, &fakePublisher{}, nil, Config{MaxBackoff: 5 * time.Minute})

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 5, want: 16 * time.Second},
		{attempt: 20, want: 5 * time.Minute},
	}
	for _, tt := range tests {
		if got := relay.backoff(tt.attempt); got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	relay := New(&fakeRepo{}, &fakePublisher{}, nil, Config{Interval: time.Millisecond})

	done := make(chan struct{})
	go func() {
		relay.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}
