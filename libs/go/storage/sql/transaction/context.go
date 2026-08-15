// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

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
