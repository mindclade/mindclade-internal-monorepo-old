// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package admissionmetrics instruments the control-plane admission engine with
// bounded Prometheus metrics and owns its separate scrape listener.
package admissionmetrics

import (
	"context"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.mindclade.dev/control/admission"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
	"go.mindclade.dev/libs/go/resourceversion"
)

const (
	operationAdmit   = "admit"
	operationCommit  = "commit"
	operationRelease = "release"

	resultAllow       = "allow"
	resultDeny        = "deny"
	resultExhausted   = "exhausted"
	resultConflict    = "conflict"
	resultNotFound    = "not_found"
	resultUnavailable = "unavailable"
	resultInternal    = "internal"
	resultDeadline    = "deadline"
	resultInvalid     = "invalid"
)

var (
	operations = [...]string{operationAdmit, operationCommit, operationRelease}
	results    = [...]string{
		resultAllow,
		resultDeny,
		resultExhausted,
		resultConflict,
		resultNotFound,
		resultUnavailable,
		resultInternal,
		resultDeadline,
		resultInvalid,
	}
)

// Engine is the narrow admission surface instrumented by Runtime. It mirrors
// the domain service rather than exposing a transport or repository concern.
type Engine interface {
	Admit(context.Context, admission.AdmitRequest) (admission.Decision, error)
	Commit(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string, admission.Quota) (admission.Decision, error)
	Release(context.Context, identifiers.ID, resourceversion.Version, identifiers.Digest, string) (admission.Decision, error)
}

type collectors struct {
	decisions *prometheus.CounterVec
	duration  *prometheus.HistogramVec
}

func newCollectors() (*prometheus.Registry, collectors, error) {
	registry := prometheus.NewRegistry()
	values := collectors{
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mindclade",
			Subsystem: "control_admission",
			Name:      "decisions_total",
			Help:      "Admission engine decisions partitioned by bounded operation and result taxonomies.",
		}, []string{"operation", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mindclade",
			Subsystem: "control_admission",
			Name:      "decision_duration_seconds",
			Help:      "Elapsed admission engine decision time in seconds partitioned by bounded operation taxonomy.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
	}
	if err := registry.Register(values.decisions); err != nil {
		return nil, collectors{}, err
	}
	if err := registry.Register(values.duration); err != nil {
		return nil, collectors{}, err
	}

	// Materialize the complete bounded series inventory. This makes an absent
	// result distinguishable from an absent or misconfigured scrape target.
	for _, operation := range operations {
		values.duration.WithLabelValues(operation)
		for _, result := range results {
			values.decisions.WithLabelValues(operation, result)
		}
	}
	return registry, values, nil
}

// Admit records one admission decision without placing request or tenant data
// in the metric identity. A panic is counted as internal and still propagates
// to the transport's canonical recovery boundary.
func (runtime *Runtime) Admit(ctx context.Context, request admission.AdmitRequest) (decision admission.Decision, err error) {
	started := time.Now()
	result := resultInternal
	defer func() { runtime.observe(operationAdmit, result, started) }()
	decision, err = runtime.delegate.Admit(ctx, request)
	result = classifyResult(err)
	return decision, err
}

// Commit records one reservation-commit decision.
func (runtime *Runtime) Commit(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string, actual admission.Quota) (decision admission.Decision, err error) {
	started := time.Now()
	result := resultInternal
	defer func() { runtime.observe(operationCommit, result, started) }()
	decision, err = runtime.delegate.Commit(ctx, id, expected, digest, subject, actual)
	result = classifyResult(err)
	return decision, err
}

// Release records one reservation-release decision.
func (runtime *Runtime) Release(ctx context.Context, id identifiers.ID, expected resourceversion.Version, digest identifiers.Digest, subject string) (decision admission.Decision, err error) {
	started := time.Now()
	result := resultInternal
	defer func() { runtime.observe(operationRelease, result, started) }()
	decision, err = runtime.delegate.Release(ctx, id, expected, digest, subject)
	result = classifyResult(err)
	return decision, err
}

func (runtime *Runtime) observe(operation, result string, started time.Time) {
	runtime.metrics.duration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
	runtime.metrics.decisions.WithLabelValues(operation, result).Inc()
}

func classifyResult(err error) string {
	if err == nil {
		return resultAllow
	}
	switch faults.CodeOf(err) {
	case faults.CodePermissionDenied, faults.CodeUnauthenticated:
		return resultDeny
	case faults.CodeResourceExhausted:
		return resultExhausted
	case faults.CodeAlreadyExists, faults.CodeFailedPrecondition, faults.CodeConflict, faults.CodeAborted:
		return resultConflict
	case faults.CodeNotFound:
		return resultNotFound
	case faults.CodeUnavailable:
		return resultUnavailable
	case faults.CodeCanceled, faults.CodeDeadlineExceeded:
		return resultDeadline
	case faults.CodeInvalidArgument, faults.CodeOutOfRange:
		return resultInvalid
	case faults.CodeUnknown, faults.CodeNotImplemented, faults.CodeInternal, faults.CodeDataLoss:
		return resultInternal
	default:
		// Normalize any future fault code to a bounded safe category until this
		// taxonomy is deliberately revised with its alert consumers.
		return resultInternal
	}
}

func nilEngine(engine Engine) bool {
	if engine == nil {
		return true
	}
	value := reflect.ValueOf(engine)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ Engine = (*Runtime)(nil)
