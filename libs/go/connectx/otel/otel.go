// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package otel

import (
	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"

	"go.mindclade.dev/libs/go/faults"
)

// NewInterceptor constructs the official OpenTelemetry Connect interceptor.
// Providers, propagators, filters, and attribute policy remain caller-owned.
func NewInterceptor(options ...otelconnect.Option) (connect.Interceptor, error) {
	interceptor, err := otelconnect.NewInterceptor(options...)
	if err != nil {
		return nil, faults.Wrap(
			err,
			faults.CodeInternal,
			"unable to initialize Connect telemetry",
			faults.WithReason("connect_telemetry_initialization_failed"),
			faults.WithOperation("connectx/otel.NewInterceptor"),
		)
	}
	return interceptor, nil
}
