// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package retry

import (
	"reflect"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

// Classifier decides whether an error without explicit faults retry intent may
// be retried.
type Classifier interface {
	Retryable(error) bool
}

// ClassifierFunc adapts a function to Classifier.
type ClassifierFunc func(error) bool

func (function ClassifierFunc) Retryable(err error) bool {
	return function != nil && function(err)
}

// ExplicitFaultClassifier retries only errors carrying an explicit retryable
// faults.RetryPolicy.
func ExplicitFaultClassifier(err error) bool { return faults.IsRetryable(err) }

// CodesClassifier returns a classifier matching the supplied fault codes.
// Explicit faults.RetryKindNever still wins in Executor.Do.
func CodesClassifier(codes ...faults.Code) Classifier {
	allowed := make(map[faults.Code]struct{}, len(codes))
	for _, code := range codes {
		if code.Valid() {
			allowed[code] = struct{}{}
		}
	}
	return ClassifierFunc(func(err error) bool {
		_, ok := allowed[faults.CodeOf(err)]
		return ok
	})
}

type configuration struct {
	clock      clock.Clock
	observer   Observer
	classifier Classifier
	random     RandomSource
}

func defaultConfiguration() configuration {
	return configuration{
		clock:      clock.RealClock{},
		classifier: ClassifierFunc(ExplicitFaultClassifier),
		random:     defaultRandomSource(),
	}
}

// Option configures an Executor.
type Option func(*configuration) error

// WithClock injects a concurrency-safe clock.
func WithClock(value clock.Clock) Option {
	return func(configuration *configuration) error {
		if nilInterface(value) {
			return invalidArgument(ErrNilClock, "retry clock must not be nil", "nil_clock", operationNewExecutor, nil)
		}
		configuration.clock = value
		return nil
	}
}

// WithObserver installs a best-effort lifecycle observer.
func WithObserver(observer Observer) Option {
	return func(configuration *configuration) error {
		if nilObserver(observer) {
			configuration.observer = nil
		} else {
			configuration.observer = observer
		}
		return nil
	}
}

// WithClassifier permits selected errors without explicit faults retry intent.
// Explicit RetryKindNever remains authoritative.
func WithClassifier(classifier Classifier) Option {
	return func(configuration *configuration) error {
		if nilClassifier(classifier) {
			return invalidArgument(ErrNilClassifier, "retry classifier must not be nil", "nil_classifier", operationNewExecutor, nil)
		}
		configuration.classifier = classifier
		return nil
	}
}

// WithRandomSource injects deterministic jitter samples.
func WithRandomSource(source RandomSource) Option {
	return func(configuration *configuration) error {
		if nilRandomSource(source) {
			return invalidArgument(ErrNilRandomSource, "retry random source must not be nil", "nil_random_source", operationNewExecutor, nil)
		}
		configuration.random = source
		return nil
	}
}

func nilObserver(observer Observer) bool       { return nilInterface(observer) }
func nilClassifier(classifier Classifier) bool { return nilInterface(classifier) }

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
