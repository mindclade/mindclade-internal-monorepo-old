// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package httpx

import (
	"crypto/rand"
	"encoding/base64"
	"sync/atomic"

	"go.mindclade.dev/libs/go/observability"
)

// sessionDecryptFailures counts cookies that could not be opened.
//
// Worth a dedicated counter rather than a log line, because its SHAPE carries
// the diagnosis:
//
//	non-zero right after a key rotation  the key overlap is too short, and the
//	                                     next rotation will log everyone out
//	non-zero at any other time           a bug, or someone probing with forged
//	                                     cookies
//
// Expiry is the ordinary case and is indistinguishable from the above at this
// layer, so alert on the RATE against a baseline rather than on any failure.
var sessionDecryptFailures atomic.Int64

// SessionDecryptFailures returns the running count, for export as a metric.
func SessionDecryptFailures() int64 { return sessionDecryptFailures.Load() }

// SessionDecryptFailureMetric is the collector form of the counter above.
//
// A monotonic counter rather than a gauge, so the alert can be written on
// rate() as the comment on sessionDecryptFailures describes. The absolute value
// carries no meaning on its own: every session that expires normally lands here
// too, and it never resets except on restart.
func SessionDecryptFailureMetric() observability.Measurement {
	return observability.Measurement{
		Name:        SessionDecryptFailureMetricName,
		Kind:        observability.MetricCounter,
		Value:       float64(sessionDecryptFailures.Load()),
		Description: "Session cookies that could not be opened.",
	}
}

// SessionDecryptFailureMetricName is the estate-vocabulary name for the counter.
const SessionDecryptFailureMetricName = "studio.session.decrypt.failures"

// newSessionID returns an opaque identifier for log correlation.
//
// It carries no authority: it is sealed inside the cookie alongside the
// subject, and nothing authorizes on it. Its only job is joining log lines
// across one session without putting the subject in every one.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
