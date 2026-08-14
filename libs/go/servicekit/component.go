// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package servicekit

import (
	"context"
	"runtime/debug"
	"strings"
	"unicode"
)

const maxNameLength = 128

// Hook is a lifecycle function. Hooks must honor context cancellation.
type Hook func(context.Context) error

// Probe checks one liveness or readiness condition. Probes must be safe for
// concurrent invocation and must honor context cancellation.
type Probe func(context.Context) error

// Component is one independently managed part of a Service.
//
// Start performs bounded initialization and is called in registration order.
// Run starts only after every component has started successfully. The first
// Run function to return initiates shutdown; a nil return is graceful and a
// non-nil return fails the service. Drain is called in reverse registration
// order before Run contexts are canceled, allowing listeners and claim loops to
// stop admitting new work. Stop is then called in reverse registration order.
// Components without Run are passive lifecycle participants.
type Component struct {
	Name      string
	Start     Hook
	Run       Hook
	Drain     Hook
	Stop      Hook
	Liveness  Probe
	Readiness Probe
}

func (component Component) validate() error {
	if err := validateName("component", component.Name, operationAdd); err != nil {
		return err
	}
	if component.Start == nil && component.Run == nil && component.Drain == nil && component.Stop == nil &&
		component.Liveness == nil && component.Readiness == nil {
		return nilComponentError(component.Name)
	}
	return nil
}

func validateName(kind, name, operation string) error {
	if name == "" || len(name) > maxNameLength || strings.TrimSpace(name) != name {
		return invalidNameError(kind, name, operation)
	}

	for index, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if index > 0 && (r == '-' || r == '_' || r == '.' || r == '/') {
			continue
		}
		return invalidNameError(kind, name, operation)
	}
	return nil
}

func invoke(ctx context.Context, function func(context.Context) error) (err error) {
	if function == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &PanicError{Value: recovered, stack: debug.Stack()}
		}
	}()
	return function(ctx)
}

// invokeBounded prevents the lifecycle coordinator from blocking forever when
// a hook ignores cancellation. A hook that violates the contract may continue
// in its goroutine after this function returns and can leak work.
func invokeBounded(ctx context.Context, function func(context.Context) error) error {
	if function == nil {
		return nil
	}

	result := make(chan error, 1)
	go func() {
		result <- invoke(ctx, function)
	}()

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
