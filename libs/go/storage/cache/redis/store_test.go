// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package redis

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	redisapi "github.com/redis/go-redis/v9"

	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/storage/cache"
)

type noopScripter struct{}

func (noopScripter) Eval(context.Context, string, []string, ...interface{}) *redisapi.Cmd {
	return nil
}
func (noopScripter) EvalSha(context.Context, string, []string, ...interface{}) *redisapi.Cmd {
	return nil
}
func (noopScripter) EvalRO(context.Context, string, []string, ...interface{}) *redisapi.Cmd {
	return nil
}
func (noopScripter) EvalShaRO(context.Context, string, []string, ...interface{}) *redisapi.Cmd {
	return nil
}
func (noopScripter) ScriptExists(context.Context, ...string) *redisapi.BoolSliceCmd { return nil }
func (noopScripter) ScriptLoad(context.Context, string) *redisapi.StringCmd         { return nil }

type fakeScript struct {
	result    any
	err       error
	keys      []string
	arguments []any
}

func (script *fakeScript) Run(_ context.Context, _ redisapi.Scripter, keys []string, arguments ...any) (any, error) {
	script.keys = append([]string(nil), keys...)
	script.arguments = append([]any(nil), arguments...)
	return script.result, script.err
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(noopScripter{}, WithPrefix("test:"), WithMaximumEntryBytes(32))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestGet(t *testing.T) {
	store := newTestStore(t)
	runner := &fakeScript{result: []any{"ok", []byte("value"), int64(2), "1234"}}
	store.getScript = runner

	entry, err := store.Get(context.Background(), cache.MustParseKey("runs/one"))
	if err != nil {
		t.Fatal(err)
	}
	if string(entry.Value) != "value" || entry.Version != 2 || !entry.ExpiresAt.Equal(time.UnixMilli(1234)) {
		t.Fatalf("entry = %#v", entry)
	}
	if len(runner.keys) != 1 || runner.keys[0] != "test:runs/one" {
		t.Fatalf("keys = %#v", runner.keys)
	}
}

func TestGetStatuses(t *testing.T) {
	tests := []struct {
		name   string
		result any
		code   faults.Code
	}{
		{name: "miss", result: []any{"miss"}, code: faults.CodeNotFound},
		{name: "corrupt", result: []any{"corrupt"}, code: faults.CodeDataLoss},
		{name: "empty", result: []any{}, code: faults.CodeDataLoss},
		{name: "shape", result: []any{"ok", "value"}, code: faults.CodeDataLoss},
		{name: "version", result: []any{"ok", "value", "zero", "0"}, code: faults.CodeDataLoss},
		{name: "expiration", result: []any{"ok", "value", "1", "bad"}, code: faults.CodeDataLoss},
		{name: "unknown", result: []any{"other"}, code: faults.CodeDataLoss},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			store.getScript = &fakeScript{result: test.result}
			_, err := store.Get(context.Background(), cache.MustParseKey("key"))
			if !faults.IsCode(err, test.code) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSetUsesServerExpiration(t *testing.T) {
	store := newTestStore(t)
	runner := &fakeScript{result: []any{"ok", "3", "2000"}}
	store.setScript = runner

	input := []byte("value")
	entry, err := store.Set(context.Background(), cache.MustParseKey("key"), input, cache.SetOptions{TTL: 500 * time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != 3 || !entry.ExpiresAt.Equal(time.UnixMilli(2000)) {
		t.Fatalf("entry = %#v", entry)
	}
	if len(runner.arguments) != 4 || runner.arguments[3] != "1" {
		t.Fatalf("arguments = %#v", runner.arguments)
	}
	input[0] = 'X'
	if string(entry.Value) != "value" {
		t.Fatalf("returned value aliases input: %q", entry.Value)
	}
}

func TestSetStatuses(t *testing.T) {
	tests := []struct {
		name   string
		result any
		code   faults.Code
	}{
		{name: "exists", result: []any{"exists", "1"}, code: faults.CodeAlreadyExists},
		{name: "mismatch", result: []any{"mismatch", "1"}, code: faults.CodeConflict},
		{name: "corrupt", result: []any{"corrupt"}, code: faults.CodeDataLoss},
		{name: "shape", result: []any{"ok", "1"}, code: faults.CodeDataLoss},
		{name: "version", result: []any{"ok", "0", "0"}, code: faults.CodeDataLoss},
		{name: "expiration", result: []any{"ok", "1", "bad"}, code: faults.CodeDataLoss},
		{name: "unknown", result: []any{"other"}, code: faults.CodeDataLoss},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			store.setScript = &fakeScript{result: test.result}
			_, err := store.Set(context.Background(), cache.MustParseKey("key"), []byte("value"), cache.SetOptions{})
			if !faults.IsCode(err, test.code) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSetRejectsOversizedEntry(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Set(context.Background(), cache.MustParseKey("key"), make([]byte, 33), cache.SetOptions{})
	if !faults.IsCode(err, faults.CodeResourceExhausted) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeleteStatuses(t *testing.T) {
	tests := []struct {
		name   string
		result any
		code   faults.Code
	}{
		{name: "ok", result: []any{"ok"}, code: ""},
		{name: "miss", result: []any{"miss"}, code: faults.CodeNotFound},
		{name: "mismatch", result: []any{"mismatch", "1"}, code: faults.CodeConflict},
		{name: "corrupt", result: []any{"corrupt"}, code: faults.CodeDataLoss},
		{name: "unknown", result: []any{"other"}, code: faults.CodeDataLoss},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			store.deleteScript = &fakeScript{result: test.result}
			err := store.Delete(context.Background(), cache.MustParseKey("key"), cache.DeleteOptions{})
			if test.code == "" && err != nil {
				t.Fatal(err)
			}
			if test.code != "" && !faults.IsCode(err, test.code) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestProviderErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code faults.Code
	}{
		{name: "canceled", err: context.Canceled, code: faults.CodeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, code: faults.CodeDeadlineExceeded},
		{name: "miss", err: redisapi.Nil, code: faults.CodeNotFound},
		{name: "timeout", err: timeoutError{}, code: faults.CodeDeadlineExceeded},
		{name: "unavailable", err: errors.New("connection reset"), code: faults.CodeUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			store.getScript = &fakeScript{err: test.err}
			_, err := store.Get(context.Background(), cache.MustParseKey("key"))
			if !faults.IsCode(err, test.code) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestArrayResult(t *testing.T) {
	t.Parallel()
	values, err := arrayResult([]any{"ok", []byte("value"), int64(2), nil})
	if err != nil || len(values) != 4 || values[1] != "value" || values[2] != "2" || values[3] != "" {
		t.Fatalf("arrayResult = %#v, %v", values, err)
	}
	if _, err := arrayResult("not-array"); err == nil {
		t.Fatal("non-array result accepted")
	}
	if _, err := arrayResult([]any{true}); err == nil {
		t.Fatal("unsupported value accepted")
	}
}

func TestParseExpiration(t *testing.T) {
	t.Parallel()
	if value, err := parseExpiration("0"); err != nil || !value.IsZero() {
		t.Fatalf("zero expiration = %v, %v", value, err)
	}
	expected := time.UnixMilli(1234)
	if value, err := parseExpiration("1234"); err != nil || !value.Equal(expected) {
		t.Fatalf("expiration = %v, %v", value, err)
	}
	for _, value := range []string{"-1", "bad"} {
		if _, err := parseExpiration(value); err == nil {
			t.Fatalf("parseExpiration(%q) returned nil", value)
		}
	}
}
