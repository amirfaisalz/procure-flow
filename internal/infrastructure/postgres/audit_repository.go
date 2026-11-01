package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
)

const auditColumns = `id, entity_type, entity_id, action, actor_id, actor_name,
	actor_role, from_status, to_status, metadata, occurred_at`

// AuditRepository is the PostgreSQL adapter for the append-only audit port.
type AuditRepository struct {
	db *sql.DB
}

// NewAuditRepository creates an audit repository backed by db.
func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

// Append inserts an immutable audit event.
func (r *AuditRepository) Append(ctx context.Context, event audit.Event) error {
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("postgres: encode audit metadata: %w", err)
	}

	_, err = querierFrom(ctx, r.db).ExecContext(ctx, `
INSERT INTO audit_events (
	entity_type, entity_id, action, actor_id, actor_name, actor_role,
	from_status, to_status, metadata, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		event.EntityType, event.EntityID, event.Action, event.ActorID, event.ActorName, event.ActorRole,
		event.FromStatus, event.ToStatus, encoded, event.OccurredAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: append audit event: %w", err)
	}
	return nil
}

// List returns a page of events for one entity, newest first.
func (r *AuditRepository) List(ctx context.Context, filter audit.ListFilter) (audit.ListResult, error) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = audit.DefaultPageSize
	}
	if pageSize > audit.MaxPageSize {
		pageSize = audit.MaxPageSize
	}

	q := querierFrom(ctx, r.db)

	var total int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events WHERE entity_type = $1 AND entity_id = $2`,
		filter.EntityType, filter.EntityID,
	).Scan(&total); err != nil {
		return audit.ListResult{}, fmt.Errorf("postgres: count audit events: %w", err)
	}

	rows, err := q.QueryContext(ctx,
		`SELECT `+auditColumns+` FROM audit_events
		 WHERE entity_type = $1 AND entity_id = $2
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $3 OFFSET $4`,
		filter.EntityType, filter.EntityID, pageSize, pageStart(page, pageSize, total),
	)
	if err != nil {
		return audit.ListResult{}, fmt.Errorf("postgres: list audit events: %w", err)
	}
	defer rows.Close()

	events := make([]audit.Event, 0, pageSize)
	for rows.Next() {
		var (
			event    audit.Event
			metadata []byte
		)
		if err := rows.Scan(
			&event.ID, &event.EntityType, &event.EntityID, &event.Action, &event.ActorID, &event.ActorName,
			&event.ActorRole, &event.FromStatus, &event.ToStatus, &metadata, &event.OccurredAt,
		); err != nil {
			return audit.ListResult{}, fmt.Errorf("postgres: scan audit event: %w", err)
		}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
				return audit.ListResult{}, fmt.Errorf("postgres: decode audit metadata: %w", err)
			}
		}
		event.OccurredAt = event.OccurredAt.UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return audit.ListResult{}, fmt.Errorf("postgres: iterate audit events: %w", err)
	}

	return audit.ListResult{
		Events:   events,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
