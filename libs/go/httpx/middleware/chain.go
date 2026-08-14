// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import (
	"net/http"
	"reflect"
)

// Middleware decorates an HTTP handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in declaration order: the first item is outermost
// and observes the request first.
func Chain(handler http.Handler, values ...Middleware) http.Handler {
	if nilHTTPHandler(handler) {
		handler = http.NotFoundHandler()
	}
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] != nil {
			handler = values[index](handler)
		}
	}
	return handler
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
