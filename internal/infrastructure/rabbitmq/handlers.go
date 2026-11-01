package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/amirfaisalz/procure-flow/internal/domain/event"
)

// procurementSnapshot is the subset of the event payload the worker needs.
type procurementSnapshot struct {
	ID          string `json:"id"`
	Number      string `json:"number"`
	Title       string `json:"title"`
	Department  string `json:"department"`
	Requester   string `json:"requester"`
	RequesterID string `json:"requester_id"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
}

type notification struct {
	EventID     string    `json:"event_id"`
	EventType   string    `json:"event_type"`
	Recipient   string    `json:"recipient"`
	RecipientID string    `json:"recipient_id"`
	Subject     string    `json:"subject"`
	Message     string    `json:"message"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// NotificationHandler notifies the requester about a lifecycle change. When a
// webhook is configured it is POSTed as JSON; otherwise the notification is
// logged so the flow can be exercised locally. Webhook payloads carry the
// event_id so downstream systems can deduplicate at-least-once deliveries.
func NotificationHandler(logger *slog.Logger, webhookURL string, client *http.Client) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return func(ctx context.Context, ev event.Event) error {
		var snapshot procurementSnapshot
		if err := json.Unmarshal(ev.Payload, &snapshot); err != nil {
			return fmt.Errorf("notification: decode payload: %w", err)
		}

		message := notification{
			EventID:     ev.ID,
			EventType:   ev.Type,
			Recipient:   snapshot.Requester,
			RecipientID: snapshot.RequesterID,
			Subject:     fmt.Sprintf("Procurement %s: %s", snapshot.Number, snapshot.Status),
			Message:     describeChange(snapshot),
			OccurredAt:  ev.OccurredAt,
		}

		if webhookURL == "" {
			logger.InfoContext(ctx, "notification dispatched",
				slog.String("event_id", ev.ID),
				slog.String("recipient", snapshot.Requester),
				slog.String("message", message.Message),
			)
			return nil
		}

		if err := postJSON(ctx, client, webhookURL, message); err != nil {
			return fmt.Errorf("notification: %w", err)
		}
		logger.InfoContext(ctx, "notification delivered",
			slog.String("event_id", ev.ID),
			slog.String("recipient", snapshot.Requester),
		)
		return nil
	}
}

// ERPHandler forwards approved procurements to the ERP integration. Without a
// configured endpoint the sync is simulated with a log line.
func ERPHandler(logger *slog.Logger, webhookURL string, client *http.Client) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return func(ctx context.Context, ev event.Event) error {
		if webhookURL == "" {
			logger.InfoContext(ctx, "ERP sync simulated (no webhook configured)",
				slog.String("event_id", ev.ID),
				slog.String("aggregate_id", ev.AggregateID),
			)
			return nil
		}

		if err := postJSON(ctx, client, webhookURL, ev); err != nil {
			return fmt.Errorf("erp: %w", err)
		}
		logger.InfoContext(ctx, "ERP notified",
			slog.String("event_id", ev.ID),
			slog.String("aggregate_id", ev.AggregateID),
		)
		return nil
	}
}

func describeChange(snapshot procurementSnapshot) string {
	switch snapshot.Status {
	case "draft":
		return fmt.Sprintf("Procurement %s (%s) was created.", snapshot.Number, snapshot.Title)
	case "submitted":
		return fmt.Sprintf("Procurement %s (%s) was submitted for approval.", snapshot.Number, snapshot.Title)
	case "approved":
		return fmt.Sprintf("Procurement %s (%s) was approved.", snapshot.Number, snapshot.Title)
	case "rejected":
		return fmt.Sprintf("Procurement %s (%s) was rejected.", snapshot.Number, snapshot.Title)
	case "cancelled":
		return fmt.Sprintf("Procurement %s (%s) was cancelled.", snapshot.Number, snapshot.Title)
	default:
		return fmt.Sprintf("Procurement %s (%s) is now %s.", snapshot.Number, snapshot.Title, snapshot.Status)
	}
}

func postJSON(ctx context.Context, client *http.Client, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}
