// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"strings"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/requestmeta"
)

const MaximumProcedureLength = 512

// OperationForProcedure maps /package.Service/Method to the stable operation
// name rpc.package.Service.Method.
func OperationForProcedure(procedure string) (requestmeta.Operation, error) {
	trimmed := strings.TrimSpace(procedure)
	if trimmed == "" || len(trimmed) > MaximumProcedureLength || !strings.HasPrefix(trimmed, "/") {
		return requestmeta.Operation{}, faults.Wrap(
			ErrInvalidProcedure,
			faults.CodeInvalidArgument,
			"invalid RPC procedure",
			faults.WithReason("invalid_rpc_procedure"),
			faults.WithOperation("connectx.OperationForProcedure"),
		)
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return requestmeta.Operation{}, faults.Wrap(
			ErrInvalidProcedure,
			faults.CodeInvalidArgument,
			"invalid RPC procedure",
			faults.WithReason("invalid_rpc_procedure"),
			faults.WithOperation("connectx.OperationForProcedure"),
		)
	}
	operation, err := requestmeta.ParseOperation("rpc." + parts[0] + "." + parts[1])
	if err != nil {
		return requestmeta.Operation{}, faults.Wrap(
			err,
			faults.CodeInvalidArgument,
			"invalid RPC procedure",
			faults.WithReason("invalid_rpc_procedure"),
			faults.WithOperation("connectx.OperationForProcedure"),
		)
	}
	return operation, nil
}
