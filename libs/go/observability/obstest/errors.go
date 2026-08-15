// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package obstest

import "sync"

// ErrorRecorder is a concurrency-safe observability.ErrorHandler.
type ErrorRecorder struct {
	mu     sync.Mutex
	errors []error
}

func (recorder *ErrorRecorder) Handle(err error) {
	if recorder == nil || err == nil {
		return
	}
	recorder.mu.Lock()
	recorder.errors = append(recorder.errors, err)
	recorder.mu.Unlock()
}

func (recorder *ErrorRecorder) Errors() []error {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	output := append([]error(nil), recorder.errors...)
	recorder.mu.Unlock()
	return output
}

func (recorder *ErrorRecorder) Reset() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.errors = nil
	recorder.mu.Unlock()
}
