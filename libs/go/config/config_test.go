// Copyright 2026 Mindclade. All rights reserved.
package config

import (
	"context"
	"errors"
	"testing"
)

func TestStrictMergeRedactionAndDigest(t *testing.T) {
	fields := []Field{{Key: "service.name", Required: true}, {Key: "database.dsn", Required: true, Secret: true}, {Key: "log.level", Default: String("info"), Reloadable: true}}
	loader, err := New(fields, MapSource{SourceName: "file", Values: map[string]string{"service.name": "scheduler", "database.dsn": "postgres://secret"}}, MapSource{SourceName: "environment", Values: map[string]string{"log.level": "debug"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MustGet("log.level") != "debug" || snapshot.Redacted()["database.dsn"] != "[REDACTED]" || snapshot.Digest().IsZero() {
		t.Fatalf("snapshot=%v", snapshot.Redacted())
	}
	origin, _ := snapshot.Origin("log.level")
	if origin.Source != "environment" {
		t.Fatalf("origin=%v", origin)
	}
}
func TestUnknownAndRequiredFailClosed(t *testing.T) {
	loader, err := New([]Field{{Key: "service.name", Required: true}}, MapSource{Values: map[string]string{"unknown": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(context.Background()); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("err=%v", err)
	}
}
func TestReloadRejectsNonReloadableChange(t *testing.T) {
	fields := []Field{{Key: "service.name", Required: true}, {Key: "log.level", Reloadable: true}}
	firstLoader, _ := New(fields, MapSource{Values: map[string]string{"service.name": "api", "log.level": "info"}})
	first, _ := firstLoader.Load(context.Background())
	atomic, err := NewAtomic(first)
	if err != nil {
		t.Fatal(err)
	}
	secondLoader, _ := New(fields, MapSource{Values: map[string]string{"service.name": "other", "log.level": "debug"}})
	second, _ := secondLoader.Load(context.Background())
	if err := atomic.Apply(second); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("err=%v", err)
	}
	thirdLoader, _ := New(fields, MapSource{Values: map[string]string{"service.name": "api", "log.level": "debug"}})
	third, _ := thirdLoader.Load(context.Background())
	if err := atomic.Apply(third); err != nil {
		t.Fatal(err)
	}
}
