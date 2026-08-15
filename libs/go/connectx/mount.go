// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package connectx

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"mindclade.internal/libs/go/faults"
)

type Mux interface{ Handle(string, http.Handler) }

// Mount registers a generated Connect handler while converting ServeMux-style
// pattern panics into a structured configuration failure.
func Mount(mux Mux, path string, handler http.Handler) (err error) {
	if nilInterface(mux) {
		return faults.Wrap(ErrNilMux, faults.CodeInvalidArgument, "Connect mux is required", faults.WithReason("nil_connect_mux"))
	}
	if nilInterface(handler) {
		return faults.Wrap(ErrNilHandler, faults.CodeInvalidArgument, "Connect handler is required", faults.WithReason("nil_connect_handler"))
	}
	if path != strings.TrimSpace(path) || !strings.HasPrefix(path, "/") {
		return faults.New(faults.CodeInvalidArgument, "Connect handler path must be absolute and canonical", faults.WithReason("invalid_connect_path"))
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = faults.Wrap(
				fmt.Errorf("connect mux registration panic: %v", recovered),
				faults.CodeFailedPrecondition,
				"Connect handler could not be mounted",
				faults.WithReason("connect_mount_failed"),
				faults.WithOperation("connectx.Mount"),
			)
		}
	}()
	mux.Handle(path, handler)
	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}
