// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

// Package admissionmetrics observes the well-formed control-plane admission
// HTTP boundary with bounded Prometheus metrics and owns its scrape listener.
package admissionmetrics

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.mindclade.dev/libs/go/faults"
)

// Operation is the fixed admission mutation taxonomy. It is deliberately not
// derived from a route, model, provider, tenant, or request value.
type Operation string

const (
	OperationAdmit   Operation = "admit"
	OperationCommit  Operation = "commit"
	OperationRelease Operation = "release"

	resultAllow       = "allow"
	resultDeny        = "deny"
	resultExhausted   = "exhausted"
	resultConflict    = "conflict"
	resultNotFound    = "not_found"
	resultUnavailable = "unavailable"
	resultInternal    = "internal"
	resultDeadline    = "deadline"
	resultCanceled    = "canceled"
	resultInvalid     = "invalid"
)

var (
	operations = [...]Operation{OperationAdmit, OperationCommit, OperationRelease}
	results    = [...]string{
		resultAllow,
		resultDeny,
		resultExhausted,
		resultConflict,
		resultNotFound,
		resultUnavailable,
		resultInternal,
		resultDeadline,
		resultCanceled,
		resultInvalid,
	}
)

func (operation Operation) valid() bool {
	switch operation {
	case OperationAdmit, OperationCommit, OperationRelease:
		return true
	default:
		return false
	}
}

// Recorder is the narrow contract used by admission handlers. Qualify marks a
// request only after authentication, authorization, and structural transport
// decoding have succeeded. Complete supplies the terminal semantic result;
// the boundary writer separately observes status and response-write failure.
type Recorder interface {
	Qualify(context.Context, Operation)
	Complete(context.Context, error)
}

// Observer owns request-entry timing and terminal HTTP observation around the
// complete guarded admission handler.
type Observer interface {
	Recorder
	Middleware(http.Handler) http.Handler
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
			Help:      "Well-formed admission HTTP decisions partitioned by bounded operation and result taxonomies.",
		}, []string{"operation", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mindclade",
			Subsystem: "control_admission",
			Name:      "decision_duration_seconds",
			Help:      "Elapsed well-formed admission HTTP decision time from guarded request entry through response completion.",
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
		values.duration.WithLabelValues(string(operation))
		for _, result := range results {
			values.decisions.WithLabelValues(string(operation), result)
		}
	}
	return registry, values, nil
}

type observationKey struct{}

type observation struct {
	mu        sync.Mutex
	started   time.Time
	operation Operation
	result    string
	status    int
	writeErr  error
	qualified bool
	completed bool
}

// Middleware starts the admission boundary clock before the guarded HTTP
// stack and observes the terminal response without inspecting its body. It
// records once, after the complete response path returns. Nested use reuses
// the existing state so an accidental double installation cannot duplicate
// counters or histogram observations.
func (runtime *Runtime) Middleware(next http.Handler) http.Handler {
	if nilHTTPHandler(next) {
		next = http.NotFoundHandler()
	}
	if runtime == nil {
		return next
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if stateFrom(request.Context()) != nil {
			next.ServeHTTP(writer, request)
			return
		}
		state := &observation{started: time.Now()}
		request = request.WithContext(context.WithValue(request.Context(), observationKey{}, state))
		observedWriter := newResponseObserver(writer, state)
		defer func() {
			recovered := recover()
			// Telemetry must never mask the original handler outcome or panic.
			func() {
				defer func() { _ = recover() }()
				runtime.finish(state, request.Context().Err(), recovered != nil)
			}()
			if recovered != nil {
				panic(recovered)
			}
		}()
		next.ServeHTTP(observedWriter, request)
	})
}

// Qualify admits one structurally well-formed request into the SLI. The first
// valid operation wins so downstream code cannot relabel an in-flight request.
func (runtime *Runtime) Qualify(ctx context.Context, operation Operation) {
	if runtime == nil || !operation.valid() {
		return
	}
	state := stateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.qualified {
		return
	}
	state.operation = operation
	state.qualified = true
}

// Complete stores the terminal semantic result. Later calls replace the
// result but never emit another sample, allowing a response serialization
// failure to supersede an earlier successful engine outcome.
func (runtime *Runtime) Complete(ctx context.Context, err error) {
	if runtime == nil {
		return
	}
	state := stateFrom(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.qualified {
		return
	}
	state.result = classifyTerminal(ctx, err)
	state.completed = true
}

func (runtime *Runtime) finish(state *observation, contextErr error, panicked bool) {
	if runtime == nil || state == nil {
		return
	}
	state.mu.Lock()
	operation := state.operation
	result := state.result
	status := state.status
	writeErr := state.writeErr
	qualified := state.qualified
	completed := state.completed
	started := state.started
	state.mu.Unlock()
	if !qualified || !operation.valid() {
		return
	}
	switch {
	case panicked:
		result = resultInternal
	case errors.Is(contextErr, context.Canceled):
		result = resultCanceled
	case errors.Is(contextErr, context.DeadlineExceeded):
		result = resultDeadline
	case writeErr != nil:
		switch classifyResult(writeErr) {
		case resultCanceled:
			result = resultCanceled
		case resultDeadline:
			result = resultDeadline
		default:
			result = resultUnavailable
		}
	default:
		result = reconcileTerminal(result, completed, classifyStatus(status))
	}
	runtime.metrics.decisions.WithLabelValues(string(operation), result).Inc()
	// Caller cancellation is observable operational evidence, but it is not a
	// service availability result and contributes no SLI latency sample.
	if result != resultCanceled {
		runtime.metrics.duration.WithLabelValues(string(operation)).Observe(time.Since(started).Seconds())
	}
}

// reconcileTerminal makes the external HTTP result authoritative while still
// retaining the more precise fault taxonomy supplied by the handler. A
// mismatch is an internal contract failure, except that a recovered HTTP panic
// or other terminal server status is allowed to supersede a provisional allow.
func reconcileTerminal(semantic string, completed bool, transport string) string {
	if !completed {
		if transport == resultAllow {
			return resultInternal
		}
		return transport
	}
	if semantic == transport {
		return semantic
	}
	if semantic == resultAllow && transport != resultAllow {
		return transport
	}
	return resultInternal
}

func classifyStatus(status int) string {
	if status == 0 {
		status = http.StatusOK
	}
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return resultAllow
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return resultInvalid
	case http.StatusUnauthorized, http.StatusForbidden:
		return resultDeny
	case http.StatusNotFound:
		return resultNotFound
	case http.StatusRequestTimeout:
		return resultCanceled
	case http.StatusConflict, http.StatusPreconditionFailed:
		return resultConflict
	case http.StatusTooManyRequests:
		return resultExhausted
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return resultUnavailable
	case http.StatusGatewayTimeout:
		return resultDeadline
	default:
		return resultInternal
	}
}

func classifyTerminal(ctx context.Context, err error) string {
	if ctx != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return resultCanceled
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return resultDeadline
		}
	}
	return classifyResult(err)
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
	case faults.CodeCanceled:
		return resultCanceled
	case faults.CodeDeadlineExceeded:
		return resultDeadline
	case faults.CodeInvalidArgument, faults.CodeOutOfRange:
		return resultInvalid
	case faults.CodeUnknown, faults.CodeNotImplemented, faults.CodeInternal, faults.CodeDataLoss:
		return resultInternal
	default:
		// Normalize future fault codes to a bounded safe category until this
		// taxonomy is deliberately revised with all alert consumers.
		return resultInternal
	}
}

func stateFrom(ctx context.Context) *observation {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(observationKey{}).(*observation)
	return state
}

// responseObserver tracks only bounded terminal metadata. It deliberately
// never buffers or inspects response bodies. newResponseObserver adds only the
// optional interfaces implemented by the underlying writer, so direct type
// assertions retain their exact meaning.
type responseObserver struct {
	http.ResponseWriter
	state *observation
}

const (
	responseCanFlush = 1 << iota
	responseCanHijack
	responseCanPush
	responseCanReadFrom
)

// newResponseObserver uses promoted capability delegates to preserve the
// exact optional-interface set of the underlying writer. Go interfaces are
// structural, so a single wrapper type with every method would incorrectly
// advertise unsupported streaming behavior.
func newResponseObserver(writer http.ResponseWriter, state *observation) http.ResponseWriter {
	core := &responseObserver{ResponseWriter: writer, state: state}
	_, canFlush := writer.(http.Flusher)
	hijacker, canHijack := writer.(http.Hijacker)
	pusher, canPush := writer.(http.Pusher)
	readerFrom, canReadFrom := writer.(io.ReaderFrom)
	capabilities := 0
	if canFlush {
		capabilities |= responseCanFlush
	}
	if canHijack {
		capabilities |= responseCanHijack
	}
	if canPush {
		capabilities |= responseCanPush
	}
	if canReadFrom {
		capabilities |= responseCanReadFrom
	}

	flush := flushCapability{observer: core, target: writer}
	hijack := hijackCapability{target: hijacker}
	push := pushCapability{target: pusher}
	readFrom := readFromCapability{observer: core, target: readerFrom}
	switch capabilities {
	case 0:
		return core
	case responseCanFlush:
		return struct {
			*responseObserver
			flushCapability
		}{core, flush}
	case responseCanHijack:
		return struct {
			*responseObserver
			hijackCapability
		}{core, hijack}
	case responseCanPush:
		return struct {
			*responseObserver
			pushCapability
		}{core, push}
	case responseCanReadFrom:
		return struct {
			*responseObserver
			readFromCapability
		}{core, readFrom}
	case responseCanFlush | responseCanHijack:
		return struct {
			*responseObserver
			flushCapability
			hijackCapability
		}{core, flush, hijack}
	case responseCanFlush | responseCanPush:
		return struct {
			*responseObserver
			flushCapability
			pushCapability
		}{core, flush, push}
	case responseCanFlush | responseCanReadFrom:
		return struct {
			*responseObserver
			flushCapability
			readFromCapability
		}{core, flush, readFrom}
	case responseCanHijack | responseCanPush:
		return struct {
			*responseObserver
			hijackCapability
			pushCapability
		}{core, hijack, push}
	case responseCanHijack | responseCanReadFrom:
		return struct {
			*responseObserver
			hijackCapability
			readFromCapability
		}{core, hijack, readFrom}
	case responseCanPush | responseCanReadFrom:
		return struct {
			*responseObserver
			pushCapability
			readFromCapability
		}{core, push, readFrom}
	case responseCanFlush | responseCanHijack | responseCanPush:
		return struct {
			*responseObserver
			flushCapability
			hijackCapability
			pushCapability
		}{core, flush, hijack, push}
	case responseCanFlush | responseCanHijack | responseCanReadFrom:
		return struct {
			*responseObserver
			flushCapability
			hijackCapability
			readFromCapability
		}{core, flush, hijack, readFrom}
	case responseCanFlush | responseCanPush | responseCanReadFrom:
		return struct {
			*responseObserver
			flushCapability
			pushCapability
			readFromCapability
		}{core, flush, push, readFrom}
	case responseCanHijack | responseCanPush | responseCanReadFrom:
		return struct {
			*responseObserver
			hijackCapability
			pushCapability
			readFromCapability
		}{core, hijack, push, readFrom}
	default:
		return struct {
			*responseObserver
			flushCapability
			hijackCapability
			pushCapability
			readFromCapability
		}{core, flush, hijack, push, readFrom}
	}
}

func (writer *responseObserver) Unwrap() http.ResponseWriter {
	if writer == nil {
		return nil
	}
	return writer.ResponseWriter
}

func (writer *responseObserver) WriteHeader(status int) {
	writer.markStatus(status)
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseObserver) Write(value []byte) (int, error) {
	writer.markStatus(http.StatusOK)
	written, err := writer.ResponseWriter.Write(value)
	if err != nil {
		writer.markWriteError(err)
	} else if written != len(value) {
		writer.markWriteError(io.ErrShortWrite)
	}
	return written, err
}

type flushCapability struct {
	observer *responseObserver
	target   http.ResponseWriter
}

func (capability flushCapability) Flush() {
	capability.observer.markStatus(http.StatusOK)
	if err := http.NewResponseController(capability.target).Flush(); err != nil {
		capability.observer.markWriteError(err)
	}
}

type hijackCapability struct{ target http.Hijacker }

func (capability hijackCapability) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return capability.target.Hijack()
}

type pushCapability struct{ target http.Pusher }

func (capability pushCapability) Push(target string, options *http.PushOptions) error {
	return capability.target.Push(target, options)
}

type readFromCapability struct {
	observer *responseObserver
	target   io.ReaderFrom
}

func (capability readFromCapability) ReadFrom(reader io.Reader) (int64, error) {
	capability.observer.markStatus(http.StatusOK)
	read, err := capability.target.ReadFrom(reader)
	if err != nil {
		capability.observer.markWriteError(err)
	}
	return read, err
}

func (writer *responseObserver) markStatus(status int) {
	if writer == nil || writer.state == nil {
		return
	}
	writer.state.mu.Lock()
	defer writer.state.mu.Unlock()
	if writer.state.status == 0 {
		writer.state.status = status
	}
}

func (writer *responseObserver) markWriteError(err error) {
	if writer == nil || writer.state == nil || err == nil {
		return
	}
	writer.state.mu.Lock()
	defer writer.state.mu.Unlock()
	if writer.state.writeErr == nil {
		writer.state.writeErr = err
	}
}

func nilHTTPHandler(handler http.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ Observer = (*Runtime)(nil)
