package postgres

import (
	"context"
	"database/sql"
)

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

// UnitOfWork runs a function inside a single database transaction. Repositories
// resolve the transaction from the context, so use cases can compose multiple
// writes atomically without leaking SQL into the application layer.
type UnitOfWork struct {
	db *sql.DB
}

// NewUnitOfWork creates a transaction manager for db.
func NewUnitOfWork(db *sql.DB) *UnitOfWork {
	return &UnitOfWork{db: db}
}

// WithinTx executes fn inside a transaction. Nested calls join the existing
// transaction.
func (u *UnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx)
	}

	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// querierFrom returns the transaction carried by ctx, or the pool.
func querierFrom(ctx context.Context, db *sql.DB) querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return db
}
