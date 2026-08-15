// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package obstest

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogRecord is an immutable captured slog record. Attributes are flattened
// with dot-separated group paths.
type LogRecord struct {
	Time       time.Time
	Level      slog.Level
	Message    string
	Attributes map[string]any
}

func (record LogRecord) Fields() map[string]any {
	output := make(map[string]any, len(record.Attributes))
	for key, value := range record.Attributes {
		output[key] = value
	}
	return output
}

type captureState struct {
	mu      sync.Mutex
	records []LogRecord
}

// CaptureHandler is a concurrency-safe in-memory slog handler.
type CaptureHandler struct {
	state  *captureState
	attrs  []slog.Attr
	groups []string
	level  slog.Level
}

func NewCaptureHandler(level slog.Level) *CaptureHandler {
	return &CaptureHandler{state: &captureState{}, level: level}
}

func (handler *CaptureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return handler != nil && level >= handler.level
}

func (handler *CaptureHandler) Handle(_ context.Context, record slog.Record) error {
	if handler == nil {
		return nil
	}
	attributes := make(map[string]any)
	for _, attribute := range handler.attrs {
		flattenAttr(attributes, handler.groups, attribute)
	}
	record.Attrs(func(attribute slog.Attr) bool {
		flattenAttr(attributes, handler.groups, attribute)
		return true
	})
	captured := LogRecord{Time: record.Time, Level: record.Level, Message: record.Message, Attributes: attributes}
	handler.state.mu.Lock()
	handler.state.records = append(handler.state.records, captured)
	handler.state.mu.Unlock()
	return nil
}

func (handler *CaptureHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	if handler == nil {
		return NewCaptureHandler(slog.LevelInfo).WithAttrs(attributes)
	}
	cloned := *handler
	cloned.attrs = append(append([]slog.Attr(nil), handler.attrs...), attributes...)
	return &cloned
}

func (handler *CaptureHandler) WithGroup(name string) slog.Handler {
	if handler == nil {
		return NewCaptureHandler(slog.LevelInfo).WithGroup(name)
	}
	cloned := *handler
	if normalized := strings.TrimSpace(name); normalized != "" {
		cloned.groups = append(append([]string(nil), handler.groups...), normalized)
	}
	return &cloned
}

func (handler *CaptureHandler) Records() []LogRecord {
	if handler == nil || handler.state == nil {
		return nil
	}
	handler.state.mu.Lock()
	output := make([]LogRecord, len(handler.state.records))
	for index, record := range handler.state.records {
		output[index] = record
		output[index].Attributes = record.Fields()
	}
	handler.state.mu.Unlock()
	return output
}

func (handler *CaptureHandler) Reset() {
	if handler == nil || handler.state == nil {
		return
	}
	handler.state.mu.Lock()
	handler.state.records = nil
	handler.state.mu.Unlock()
}

func flattenAttr(output map[string]any, groups []string, attribute slog.Attr) {
	if attribute.Equal(slog.Attr{}) {
		return
	}
	value := attribute.Value.Resolve()
	keyGroups := groups
	if attribute.Key != "" {
		keyGroups = append(append([]string(nil), groups...), attribute.Key)
	}
	if value.Kind() == slog.KindGroup {
		for _, nested := range value.Group() {
			flattenAttr(output, keyGroups, nested)
		}
		return
	}
	output[strings.Join(keyGroups, ".")] = value.Any()
}

// SortedKeys returns stable attribute keys for assertions.
func (record LogRecord) SortedKeys() []string {
	keys := make([]string, 0, len(record.Attributes))
	for key := range record.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
