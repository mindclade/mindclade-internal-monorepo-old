// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package transaction

import (
	"context"
	"database/sql"
)

type contextKey struct{}

func ContextWithTx(ctx context.Context, tx *sql.Tx) (context.Context, error) {
	if ctx == nil || tx == nil {
		return nil, invalid(nil, ErrInvalidRequest, "transaction.ContextWithTx")
	}
	if _, ok := FromContext(ctx); ok {
		return nil, invalid(ctx, ErrNested, "transaction.ContextWithTx")
	}
	return context.WithValue(ctx, contextKey{}, tx), nil
}
func FromContext(ctx context.Context) (*sql.Tx, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(contextKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}
