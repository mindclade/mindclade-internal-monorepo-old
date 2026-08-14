// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package retry

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math"
	mathrand "math/rand"
	"reflect"
	"sync"
	"time"
)

// RandomSource supplies samples in [0, 1). Implementations need not be
// concurrency-safe; Executor serializes access.
type RandomSource interface {
	Float64() float64
}

// RandomSourceFunc adapts a function to RandomSource.
type RandomSourceFunc func() float64

func (function RandomSourceFunc) Float64() float64 {
	if function == nil {
		return 0.5
	}
	return function()
}

type lockedRandom struct {
	mu     sync.Mutex
	source RandomSource
}

func (random *lockedRandom) sample() float64 {
	random.mu.Lock()
	value := random.source.Float64()
	random.mu.Unlock()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0.5
	}
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return math.Nextafter(1, 0)
	}
	return value
}

func defaultRandomSource() RandomSource {
	var seedBytes [8]byte
	if _, err := cryptorand.Read(seedBytes[:]); err == nil {
		seed := int64(binary.LittleEndian.Uint64(seedBytes[:]))
		return mathrand.New(mathrand.NewSource(seed))
	}
	return mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
}

func applyJitter(delay time.Duration, fraction, sample float64) time.Duration {
	if delay <= 0 || fraction <= 0 {
		return delay
	}
	factor := 1 - fraction + (2 * fraction * sample)
	if factor <= 0 {
		return 0
	}
	value := float64(delay) * factor
	if value >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(value)
}

func nilRandomSource(source RandomSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
