// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package identifiers

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestGeneratorUUIDv7IsMonotonicWithinMillisecond(t *testing.T) {
	t.Parallel()

	timestamp := time.UnixMilli(1_800_000_000_000)
	generator, err := NewGenerator(
		WithTimeSource(func() time.Time { return timestamp }),
		WithEntropySource(zeroReader{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := generator.UUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.UUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	if first.Compare(second) >= 0 {
		t.Fatalf("first=%s second=%s", first, second)
	}
	if first[15] != 0 || second[15] != 1 {
		t.Fatalf("monotonic field did not increment: %x %x", first[15], second[15])
	}
}

func TestGeneratorPreservesMonotonicityAcrossClockRegression(t *testing.T) {
	t.Parallel()

	generator, err := NewGenerator(WithEntropySource(zeroReader{}))
	if err != nil {
		t.Fatal(err)
	}
	later := time.UnixMilli(2_000)
	earlier := time.UnixMilli(1_000)

	first, err := generator.UUIDv7At(later)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.UUIDv7At(earlier)
	if err != nil {
		t.Fatal(err)
	}
	if first.Compare(second) >= 0 {
		t.Fatalf("first=%s second=%s", first, second)
	}
	secondTime, ok := second.Time()
	if !ok || secondTime.UnixMilli() != later.UnixMilli() {
		t.Fatalf("regressed timestamp = %s, %v", secondTime, ok)
	}
}

func TestGeneratorRejectsOutOfRangeTime(t *testing.T) {
	t.Parallel()

	generator, err := NewGenerator()
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.UUIDv7At(time.UnixMilli(-1))
	if !errors.Is(err, ErrTimeRange) {
		t.Fatalf("UUIDv7At() error = %v", err)
	}
}

func TestGeneratorReportsEntropyFailure(t *testing.T) {
	t.Parallel()

	generator, err := NewGenerator(WithEntropySource(errorReader{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.UUIDv4(); !errors.Is(err, ErrEntropy) {
		t.Fatalf("UUIDv4() error = %v", err)
	}
	if _, err := generator.UUIDv7At(time.UnixMilli(1)); !errors.Is(err, ErrEntropy) {
		t.Fatalf("UUIDv7At() error = %v", err)
	}
}

func TestGeneratorOptionsRejectNilSources(t *testing.T) {
	t.Parallel()

	if _, err := NewGenerator(WithTimeSource(nil)); err == nil {
		t.Fatal("WithTimeSource(nil) succeeded")
	}
	if _, err := NewGenerator(WithEntropySource(nil)); !errors.Is(err, ErrEntropy) {
		t.Fatalf("WithEntropySource(nil) error = %v", err)
	}
}

func TestGeneratorConcurrentUniqueness(t *testing.T) {
	generator, err := NewGenerator(
		WithTimeSource(func() time.Time { return time.UnixMilli(1_000) }),
		WithEntropySource(bytes.NewReader(make([]byte, 10))),
	)
	if err != nil {
		t.Fatal(err)
	}

	const count = 512
	results := make(chan UUID, count)
	errorsChannel := make(chan error, count)
	var wait sync.WaitGroup
	wait.Add(count)
	for index := 0; index < count; index++ {
		go func() {
			defer wait.Done()
			uuid, generateErr := generator.UUIDv7()
			if generateErr != nil {
				errorsChannel <- generateErr
				return
			}
			results <- uuid
		}()
	}
	wait.Wait()
	close(results)
	close(errorsChannel)

	for generateErr := range errorsChannel {
		t.Fatalf("UUIDv7() error = %v", generateErr)
	}
	seen := make(map[UUID]struct{}, count)
	for uuid := range results {
		if _, exists := seen[uuid]; exists {
			t.Fatalf("duplicate uuid %s", uuid)
		}
		seen[uuid] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("generated %d UUIDs, want %d", len(seen), count)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
