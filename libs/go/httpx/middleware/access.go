// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"mindclade.internal/libs/go/requestmeta"
)

type AccessEvent struct {
	Context   context.Context
	Method    string
	Path      string
	Pattern   string
	Status    int
	Bytes     int64
	Duration  time.Duration
	UserAgent string
}

type AccessObserver interface{ ObserveAccess(AccessEvent) }
type AccessObserverFunc func(AccessEvent)

func (function AccessObserverFunc) ObserveAccess(event AccessEvent) { function(event) }

func Access(observer AccessObserver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			tracked, ok := writer.(*trackingWriter)
			if !ok {
				tracked = &trackingWriter{ResponseWriter: writer}
			}
			started := time.Now()
			next.ServeHTTP(tracked, request)
			if nilInterface(observer) {
				return
			}
			event := AccessEvent{
				Context: request.Context(), Method: request.Method,
				Path: request.URL.Path, Pattern: request.Pattern,
				Status: tracked.Status(), Bytes: tracked.Bytes(),
				Duration: time.Since(started), UserAgent: boundedUserAgent(request.UserAgent()),
			}
			func() { defer func() { _ = recover() }(); observer.ObserveAccess(event) }()
		})
	}
}

// SlogObserver emits one structured access record without query strings,
// headers, bodies, credentials, or raw principal claims.
type SlogObserver struct{ Logger *slog.Logger }

func (observer SlogObserver) ObserveAccess(event AccessEvent) {
	logger := observer.Logger
	if logger == nil {
		logger = slog.Default()
	}
	attributes := []any{
		"http.request.method", event.Method,
		"url.path", event.Path,
		"http.route", event.Pattern,
		"http.response.status_code", event.Status,
		"http.response.body.size", event.Bytes,
		"duration_ms", float64(event.Duration) / float64(time.Millisecond),
	}
	if event.UserAgent != "" {
		attributes = append(attributes, "user_agent.original", event.UserAgent)
	}
	if metadata, ok := requestmeta.FromContext(event.Context); ok {
		if !metadata.RequestID.IsZero() {
			attributes = append(attributes, "request_id", metadata.RequestID.String())
		}
		if !metadata.CorrelationID.IsZero() {
			attributes = append(attributes, "correlation_id", metadata.CorrelationID.String())
		}
	}
	level := slog.LevelInfo
	if event.Status >= 500 {
		level = slog.LevelError
	} else if event.Status >= 400 {
		level = slog.LevelWarn
	}
	logger.Log(event.Context, level, "http request", attributes...)
}

func boundedUserAgent(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "")
	if len(value) <= 256 {
		return value
	}
	end := 256
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}
