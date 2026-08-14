// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"time"

	"mindclade.internal/libs/go/clock"
	"mindclade.internal/libs/go/faults"
)

const (
	defaultStartupTimeout        = 60 * time.Second
	defaultShutdownTimeout       = 30 * time.Second
	defaultComponentDrainTimeout = 10 * time.Second
	defaultComponentStopTimeout  = 10 * time.Second
	defaultProbeTimeout          = 2 * time.Second
)

type configuration struct {
	startupTimeout        time.Duration
	shutdownTimeout       time.Duration
	componentDrainTimeout time.Duration
	componentStopTimeout  time.Duration
	probeTimeout          time.Duration
	observer              Observer
	clock                 clock.Clock
}

func defaultConfiguration() configuration {
	return configuration{
		startupTimeout:        defaultStartupTimeout,
		shutdownTimeout:       defaultShutdownTimeout,
		componentDrainTimeout: defaultComponentDrainTimeout,
		componentStopTimeout:  defaultComponentStopTimeout,
		probeTimeout:          defaultProbeTimeout,
		clock:                 clock.RealClock{},
	}
}

// Option configures a Service.
type Option func(*configuration) error

// WithStartupTimeout sets the total startup budget. Zero disables the package
// timeout; the parent context may still impose a deadline.
func WithStartupTimeout(timeout time.Duration) Option {
	return durationOption("startup_timeout", timeout, func(config *configuration, value time.Duration) {
		config.startupTimeout = value
	})
}

// WithShutdownTimeout sets the total graceful shutdown budget. Zero disables
// the package timeout.
func WithShutdownTimeout(timeout time.Duration) Option {
	return durationOption("shutdown_timeout", timeout, func(config *configuration, value time.Duration) {
		config.shutdownTimeout = value
	})
}

// WithComponentDrainTimeout sets the maximum time allocated to each component
// Drain hook. Zero means each hook is bounded only by the total shutdown budget.
func WithComponentDrainTimeout(timeout time.Duration) Option {
	return durationOption("component_drain_timeout", timeout, func(config *configuration, value time.Duration) {
		config.componentDrainTimeout = value
	})
}

// WithComponentStopTimeout sets the maximum time allocated to each component
// Stop hook. Zero means each hook is bounded only by the total shutdown budget.
func WithComponentStopTimeout(timeout time.Duration) Option {
	return durationOption("component_stop_timeout", timeout, func(config *configuration, value time.Duration) {
		config.componentStopTimeout = value
	})
}

// WithProbeTimeout sets the maximum time allocated to each liveness or
// readiness probe. Zero disables the package timeout.
func WithProbeTimeout(timeout time.Duration) Option {
	return durationOption("probe_timeout", timeout, func(config *configuration, value time.Duration) {
		config.probeTimeout = value
	})
}

// WithObserver installs a lifecycle observer. A nil observer disables events.
// Use CombineObservers to fan events out to multiple adapters.
func WithObserver(observer Observer) Option {
	return func(config *configuration) error {
		config.observer = observer
		return nil
	}
}

// WithClock installs the clock used for lifecycle timestamps and package-owned
// startup, shutdown, component-stop, and probe budgets. Production services
// normally use clock.RealClock; tests can use clock.FakeClock.
func WithClock(value clock.Clock) Option {
	return func(config *configuration) error {
		if nilInterface(value) {
			return structuredFault(
				nil,
				ErrNilClock,
				faults.CodeInvalidArgument,
				"service clock must not be nil",
				"nil_clock",
				operationNew,
				nil,
			)
		}
		config.clock = value
		return nil
	}
}

func durationOption(
	name string,
	value time.Duration,
	set func(*configuration, time.Duration),
) Option {
	return func(config *configuration) error {
		if value < 0 {
			return invalidDurationError(name, value)
		}
		set(config, value)
		return nil
	}
}
