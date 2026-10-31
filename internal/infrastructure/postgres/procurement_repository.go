package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
)

const procurementColumns = `id, number, title, description, department, requester, requester_id,
	amount, currency, status, rejection_reason, created_at, updated_at, version`

const insertProcurementSQL = `
INSERT INTO procurements (` + procurementColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

const updateProcurementSQL = `
UPDATE procurements SET
	title            = $2,
	description      = $3,
	department       = $4,
	requester        = $5,
	requester_id     = $6,
	amount           = $7,
	currency         = $8,
	status           = $9,
	rejection_reason = $10,
	updated_at       = $11,
	version          = $12
WHERE id = $1 AND version = $13`

// ProcurementRepository is the PostgreSQL adapter for the procurement port.
type ProcurementRepository struct {
	db *sql.DB
}

// NewProcurementRepository creates a repository backed by db.
func NewProcurementRepository(db *sql.DB) *ProcurementRepository {
	return &ProcurementRepository{db: db}
}

// Ping reports whether the database is reachable.
func (r *ProcurementRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// Create inserts a new procurement. A duplicate id or number maps to
// domain.ErrConflict.
func (r *ProcurementRepository) Create(ctx context.Context, p procurement.Procurement) error {
	_, err := querierFrom(ctx, r.db).ExecContext(ctx, insertProcurementSQL,
		p.ID, p.Number, p.Title, p.Description, p.Department, p.Requester, p.RequesterID,
		p.Amount, p.Currency, string(p.Status), p.RejectionReason, p.CreatedAt.UTC(), p.UpdatedAt.UTC(), p.Version,
	)
	return mapWriteError(err)
}

// GetByID returns a procurement or domain.ErrNotFound.
func (r *ProcurementRepository) GetByID(ctx context.Context, id string) (procurement.Procurement, error) {
	row := querierFrom(ctx, r.db).QueryRowContext(ctx,
		`SELECT `+procurementColumns+` FROM procurements WHERE id = $1`, id)

	p, err := scanProcurement(row)
	if errors.Is(err, sql.ErrNoRows) {
		return procurement.Procurement{}, domain.ErrNotFound
	}
	if err != nil {
		return procurement.Procurement{}, fmt.Errorf("postgres: get procurement: %w", err)
	}
	return p, nil
}

// Update writes p only when the stored version equals expectedVersion.
func (r *ProcurementRepository) Update(ctx context.Context, p procurement.Procurement, expectedVersion int) error {
	q := querierFrom(ctx, r.db)

	result, err := q.ExecContext(ctx, updateProcurementSQL,
		p.ID, p.Title, p.Description, p.Department, p.Requester, p.RequesterID,
		p.Amount, p.Currency, string(p.Status), p.RejectionReason, p.UpdatedAt.UTC(), p.Version,
		expectedVersion,
	)
	if err != nil {
		return mapWriteError(err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("postgres: update procurement rows affected: %w", err)
	}
	if affected == 1 {
		return nil
	}

	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM procurements WHERE id = $1)`, p.ID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("postgres: check procurement existence: %w", err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	return domain.ErrConflict
}

// List returns a filtered, paginated page ordered newest first.
func (r *ProcurementRepository) List(ctx context.Context, filter procurement.ListFilter) (procurement.ListResult, error) {
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = procurement.DefaultPageSize
	}
	if pageSize > procurement.MaxPageSize {
		pageSize = procurement.MaxPageSize
	}

	where, args := buildListWhere(filter)

	countQuery := `SELECT COUNT(*) FROM procurements` + where
	var total int
	if err := querierFrom(ctx, r.db).QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return procurement.ListResult{}, fmt.Errorf("postgres: count procurements: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	listQuery := fmt.Sprintf(
		`SELECT %s FROM procurements%s ORDER BY created_at DESC, id ASC LIMIT $%d OFFSET $%d`,
		procurementColumns, where, len(args)-1, len(args),
	)

	rows, err := querierFrom(ctx, r.db).QueryContext(ctx, listQuery, args...)
	if err != nil {
		return procurement.ListResult{}, fmt.Errorf("postgres: list procurements: %w", err)
	}
	defer rows.Close()

	items := make([]procurement.Procurement, 0, pageSize)
	for rows.Next() {
		p, err := scanProcurement(rows)
		if err != nil {
			return procurement.ListResult{}, fmt.Errorf("postgres: scan procurement: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return procurement.ListResult{}, fmt.Errorf("postgres: iterate procurements: %w", err)
	}

	return procurement.ListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func buildListWhere(filter procurement.ListFilter) (string, []any) {
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)

	if filter.Status != "" {
		args = append(args, string(filter.Status))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	if query := strings.TrimSpace(filter.Query); query != "" {
		args = append(args, "%"+escapeLike(query)+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions, fmt.Sprintf(
			`(number ILIKE %s ESCAPE '\' OR title ILIKE %s ESCAPE '\' OR description ILIKE %s ESCAPE '\' OR department ILIKE %s ESCAPE '\' OR requester ILIKE %s ESCAPE '\')`,
			placeholder, placeholder, placeholder, placeholder, placeholder,
		))
	}

	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func scanProcurement(scanner interface{ Scan(dest ...any) error }) (procurement.Procurement, error) {
	var (
		p      procurement.Procurement
		status string
	)

	err := scanner.Scan(
		&p.ID, &p.Number, &p.Title, &p.Description, &p.Department, &p.Requester, &p.RequesterID,
		&p.Amount, &p.Currency, &status, &p.RejectionReason, &p.CreatedAt, &p.UpdatedAt, &p.Version,
	)
	if err != nil {
		return procurement.Procurement{}, err
	}

	p.Status = procurement.Status(status)
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: duplicate %s", domain.ErrConflict, pgErr.ConstraintName)
	}
	return err
}
