// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package watch

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8swatch "k8s.io/apimachinery/pkg/watch"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/kubernetes"
)

// Reworded off the leading proper noun rather than capitalised: Go error strings are read
// with their package qualifier (`watch.ErrClosed`), so "Kubernetes" was already implied.
var ErrClosed = errors.New("watch closed")

type Options struct {
	// IncludeBookmarks delivers BOOKMARK events to the handler. By default they
	// are consumed internally because most object reconcilers do not need them.
	IncludeBookmarks bool

	// AllowCleanClosure treats a result-channel closure as successful. The
	// default is false because production watches normally need relisting and
	// re-establishment when the server closes the stream.
	AllowCleanClosure bool
}

type Handler func(context.Context, k8swatch.Event) error

// Consume synchronously consumes watcher until context cancellation, a watch
// error event, handler failure, or stream closure. Consume always calls Stop.
func Consume(ctx context.Context, watcher k8swatch.Interface, options Options, handler Handler) error {
	const operation = "kubernetes.watch.Consume"
	if ctx == nil || nilInterface(watcher) || handler == nil {
		return invalid(operation)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return kubernetes.Qualify(ctx, ctx.Err(), operation, nil)
		case event, open := <-watcher.ResultChan():
			if !open {
				if options.AllowCleanClosure {
					return nil
				}
				return faults.Wrap(
					ErrClosed,
					faults.CodeUnavailable,
					"Kubernetes watch closed",
					faults.WithReason("kubernetes_watch_closed"),
					faults.WithOperation(operation),
					faults.WithContextMetadata(ctx),
					faults.WithRetryPolicy(faults.ImmediateRetry(0)),
				)
			}
			if event.Type == k8swatch.Error {
				if event.Object == nil {
					return faults.New(
						faults.CodeDataLoss,
						"Kubernetes watch returned an invalid error event",
						faults.WithReason("invalid_watch_error_event"),
						faults.WithOperation(operation),
						faults.WithContextMetadata(ctx),
						faults.WithRetryPolicy(faults.ImmediateRetry(0)),
					)
				}
				return kubernetes.Qualify(ctx, apierrors.FromObject(event.Object), operation, nil)
			}
			if event.Type == k8swatch.Bookmark && !options.IncludeBookmarks {
				continue
			}
			if err := invokeHandler(ctx, handler, event, operation); err != nil {
				return err
			}
		}
	}
}

func invokeHandler(ctx context.Context, handler Handler, event k8swatch.Event, operation string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = faults.New(
				faults.CodeInternal,
				"Kubernetes watch handler panicked",
				faults.WithCause(fmt.Errorf("watch handler panic: %v", recovered)),
				faults.WithReason("watch_handler_panicked"),
				faults.WithOperation(operation),
				faults.WithContextMetadata(ctx),
				faults.WithRetryPolicy(faults.NoRetry()),
			)
		}
	}()
	return handler(ctx, event)
}

type Predicate func(k8swatch.Event) (bool, error)

// Until consumes events until predicate returns true. The matching event is
// returned. A nil predicate is rejected.
func Until(ctx context.Context, watcher k8swatch.Interface, options Options, predicate Predicate) (matched k8swatch.Event, err error) {
	if predicate == nil {
		return k8swatch.Event{}, invalid("kubernetes.watch.Until")
	}
	err = Consume(ctx, watcher, options, func(_ context.Context, event k8swatch.Event) error {
		ok, predicateErr := predicate(event)
		if predicateErr != nil {
			return predicateErr
		}
		if ok {
			matched = event
			return errMatched
		}
		return nil
	})
	if errors.Is(err, errMatched) {
		return matched, nil
	}
	return k8swatch.Event{}, err
}

var errMatched = errors.New("watch predicate matched")

func invalid(operation string) error {
	return faults.New(faults.CodeInvalidArgument, "invalid Kubernetes watch request", faults.WithReason("invalid_watch_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
