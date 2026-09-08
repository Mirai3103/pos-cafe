package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
)

// Beginner is the subset of *sql.DB needed to start a transaction. Slices depend
// on this rather than on *sql.DB so they stay mockable, in the same spirit as the
// consumer-defined store interfaces.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// WithTx runs fn inside a single database transaction with the default isolation
// level, committing when fn returns nil and rolling back otherwise.
//
// Use it whenever one slice must perform several writes that have to succeed or
// fail together (placing an order: insert the order, insert its lines, decrement
// stock). For a single statement, call the sqlc query directly — wrapping one
// write in a transaction buys nothing.
//
//	err := database.WithTx(ctx, db, func(q *sqlc.Queries) error {
//	    order, err := q.CreateOrder(ctx, orderParams)
//	    if err != nil {
//	        return err
//	    }
//	    return q.DecrementStock(ctx, order.ProductID)
//	})
//
// The *sqlc.Queries handed to fn is bound to the transaction, so every query it
// runs is part of it. Never capture it beyond fn — it is invalid once WithTx
// returns.
func WithTx(ctx context.Context, db Beginner, fn func(*sqlc.Queries) error) error {
	return WithTxOptions(ctx, db, nil, fn)
}

// WithTxOptions is WithTx with an explicit isolation level or read-only flag,
// e.g. &sql.TxOptions{Isolation: sql.LevelSerializable} for logic that must not
// observe a phantom read.
func WithTxOptions(ctx context.Context, db Beginner, opts *sql.TxOptions, fn func(*sqlc.Queries) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// A panic inside fn must not leave the transaction (and its locks) dangling,
	// so unwind it here and let the panic continue to the caller.
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(sqlc.New(tx)); err != nil {
		// fn's error is the one that explains the failure, so it stays the wrapped
		// error. ErrTxDone is ignored: it just means the driver already rolled the
		// transaction back, typically on a cancelled context.
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
