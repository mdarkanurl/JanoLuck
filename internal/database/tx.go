package database

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNoTransactionSupport = errors.New("database: transactions are not supported")

type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (q *Queries) InTx(ctx context.Context, fn func(*Queries) error) error {
	beginner, ok := q.db.(TxBeginner)
	if !ok {
		return ErrNoTransactionSupport
	}

	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(q.WithTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
