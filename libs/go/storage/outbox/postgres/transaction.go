// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"context"
	"database/sql"

	storageoutbox "mindclade.internal/libs/go/storage/outbox"
	"mindclade.internal/libs/go/storage/sql/transaction"
)

// AppendInTransaction atomically invokes mutate and appends envelope using the
// transaction carried in ctx. The canonical repository detects that transaction
// through storage/sql/transaction and never opens a competing transaction.
func AppendInTransaction(
	ctx context.Context,
	beginner transaction.Beginner,
	options transaction.Options,
	repository storageoutbox.Repository,
	envelope storageoutbox.Envelope,
	mutate func(context.Context, *sql.Tx) error,
) error {
	if repository == nil || mutate == nil {
		return storageoutbox.ErrInvalidRequest
	}
	return transaction.RunVoid(ctx, beginner, options, func(txCtx context.Context, tx *sql.Tx) error {
		if err := mutate(txCtx, tx); err != nil {
			return err
		}
		return repository.Append(txCtx, envelope)
	})
}
